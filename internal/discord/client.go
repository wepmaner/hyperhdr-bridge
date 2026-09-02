package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
)

// Status — состояние подключения к Discord (для трея и веб-панели).
type Status string

const (
	StatusDisconnected  Status = "disconnected"
	StatusConnecting    Status = "connecting"
	StatusHandshaked    Status = "handshaked"    // READY получен, но не аутентифицированы
	StatusAuthenticated Status = "authenticated" // события идут
)

// ErrNotAuthorized означает, что нужен интерактивный AUTHORIZE.
var ErrNotAuthorized = errors.New("discord: требуется авторизация пользователя")

// rpcTimeout щедрый: AUTHORIZE ждёт, пока пользователь нажмёт кнопку в Discord.
const rpcTimeout = 2 * time.Minute

// channelEvents — события, которые подписываются на конкретный channel_id.
var channelEvents = []string{EvSpeakingStart, EvSpeakingStop, EvVoiceStateUpdate}

// Client — соединение с локальным Discord по IPC.
//
// Жизненный цикл одной сессии:
//
//	dial -> HANDSHAKE -> READY -> AUTHENTICATE -> SUBSCRIBE(глобальные события)
//	-> начальный снимок -> цикл чтения (+ переподписка при смене канала)
//
// При обрыве Run переподключается с экспоненциальной задержкой.
type Client struct {
	cfg    config.Discord
	log    *slog.Logger
	events chan Event

	mu       sync.Mutex
	conn     net.Conn
	userID   string
	username string
	status   Status
	lastErr  string
	pending  map[string]chan Payload // nonce -> ответ
	ready    chan ReadyData          // сигнал READY текущей сессии
	chanSel  chan []byte             // очередь VOICE_CHANNEL_SELECT
	curChan  string                  // канал, на события которого мы подписаны
}

func NewClient(cfg config.Discord, log *slog.Logger) *Client {
	return &Client{
		cfg:     cfg,
		log:     log,
		events:  make(chan Event, 64),
		status:  StatusDisconnected,
		pending: make(map[string]chan Payload),
	}
}

// Events — поток событий для потребителя.
func (c *Client) Events() <-chan Event { return c.events }

// UserID — id текущего пользователя (заполняется после READY).
func (c *Client) UserID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.userID
}

// Info возвращает состояние соединения для UI: статус, имя пользователя, последнюю ошибку.
func (c *Client) Info() (Status, string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status, c.username, c.lastErr
}

func (c *Client) setStatus(s Status, err error) {
	c.mu.Lock()
	c.status = s
	if err != nil {
		c.lastErr = err.Error()
	} else if s == StatusAuthenticated {
		c.lastErr = ""
	}
	c.mu.Unlock()
}

// Run держит соединение живым до отмены контекста.
func (c *Client) Run(ctx context.Context) error {
	const maxBackoff = 30 * time.Second
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c.setStatus(StatusConnecting, nil)
		err := c.session(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c.setStatus(StatusDisconnected, err)
		if errors.Is(err, ErrNotAuthorized) {
			// Ошибка конфигурации: переподключение её не вылечит.
			c.log.Error("Discord: нужна настройка", "err", err)
			return err
		}
		if err != nil {
			c.log.Warn("сессия Discord прервана", "err", err, "retry_in", backoff)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
		}
	}
}

// session = одно подключение целиком.
func (c *Client) session(parent context.Context) error {
	conn, path, err := dialIPC(c.cfg.PipeRange)
	if err != nil {
		return err
	}
	c.log.Info("подключено к Discord IPC", "socket", path)

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	ready := make(chan ReadyData, 1)
	c.mu.Lock()
	c.conn = conn
	c.ready = ready
	c.chanSel = make(chan []byte, 8)
	c.curChan = ""
	c.mu.Unlock()
	readDone := make(chan struct{})
	defer func() {
		// Порядок важен: сначала рвём сокет и дожидаемся выхода readLoop,
		// только потом закрываем каналы ожидающих ответа — иначе readLoop
		// может отправить кадр в уже закрытый канал.
		_ = conn.Close()
		select {
		case <-readDone:
		case <-time.After(2 * time.Second):
			c.log.Warn("readLoop не завершился вовремя")
		}
		c.mu.Lock()
		c.conn, c.ready, c.chanSel = nil, nil, nil
		for nonce, ch := range c.pending { // разбудить всех, кто ждёт ответа
			delete(c.pending, nonce)
			close(ch)
		}
		c.mu.Unlock()
	}()

	// 1. HANDSHAKE — представляемся приложением.
	if err := writeFrame(conn, OpHandshake, map[string]any{
		"v":         1,
		"client_id": c.cfg.ClientID,
	}); err != nil {
		return fmt.Errorf("handshake: %w", err)
	}

	readErr := make(chan error, 1)
	go func() {
		defer close(readDone)
		readErr <- c.readLoop(conn)
	}()
	go c.channelWorker(ctx)

	// 2. Ждём READY — только после него можно слать команды.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-readErr:
		return err
	case <-time.After(20 * time.Second):
		return errors.New("Discord не прислал READY за 20 c")
	case rd := <-ready:
		c.setStatus(StatusHandshaked, nil)
		c.log.Info("Discord READY", "user", rd.User.Username, "id", rd.User.ID)
	}

	// 3. Аутентификация, подписки, начальный снимок.
	if err := c.setup(ctx); err != nil {
		return fmt.Errorf("настройка сессии: %w", err)
	}
	c.setStatus(StatusAuthenticated, nil)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-readErr:
		return err
	}
}

// setup: AUTHENTICATE -> глобальные подписки -> начальное состояние.
func (c *Client) setup(ctx context.Context) error {
	if err := c.authenticate(ctx); err != nil {
		return err
	}
	c.log.Info("аутентификация в Discord RPC успешна")

	// Глобальные подписки (без args) — приходят всегда, независимо от канала.
	for _, evt := range []string{
		EvVoiceSettingsUpdate,   // локальные мут/глушение
		EvVoiceChannelSelect,    // вход/выход/переход между каналами
		EvVoiceConnectionStatus, // качество связи
	} {
		if err := c.subscribe(ctx, evt, nil); err != nil {
			return fmt.Errorf("подписка %s: %w", evt, err)
		}
	}

	c.initialSnapshot(ctx)
	return nil
}

// authenticate пробует кэшированный токен; при отказе один раз перевыдаёт его.
func (c *Client) authenticate(ctx context.Context) error {
	token, err := c.ensureToken(ctx)
	if err != nil {
		return err
	}
	err = c.call(ctx, CmdAuthenticate, map[string]any{"access_token": token}, nil)
	if err == nil {
		return nil
	}
	c.log.Warn("токен отвергнут, запрашиваю авторизацию заново", "err", err)
	c.dropToken()
	token, err = c.ensureToken(ctx)
	if err != nil {
		return err
	}
	return c.call(ctx, CmdAuthenticate, map[string]any{"access_token": token}, nil)
}

// initialSnapshot дочитывает состояние, сложившееся до нашего запуска:
// без него мы ничего не знаем до первого события.
func (c *Client) initialSnapshot(ctx context.Context) {
	if vs, err := c.VoiceSettings(ctx); err != nil {
		c.log.Warn("GET_VOICE_SETTINGS", "err", err)
	} else if raw, err := json.Marshal(vs); err == nil {
		c.emit(Event{Name: EvVoiceSettingsUpdate, Data: raw})
	}

	ch, err := c.SelectedVoiceChannel(ctx)
	if err != nil {
		c.log.Warn("GET_SELECTED_VOICE_CHANNEL", "err", err)
		return
	}
	var sel VoiceChannelSelect
	if ch != nil {
		sel.ChannelID, sel.GuildID, sel.Name = ch.ID, ch.GuildID, ch.Name
	}
	if raw, err := json.Marshal(sel); err == nil {
		c.pushChannelSelect(raw)
	}
}

// readLoop разбирает кадры и раскладывает их по ответам и событиям.
func (c *Client) readLoop(conn net.Conn) error {
	for {
		op, body, err := readFrame(conn)
		if err != nil {
			return err
		}
		switch op {
		case OpPing:
			// Не ответить на PING = разрыв соединения.
			if err := writeFrame(conn, OpPong, json.RawMessage(body)); err != nil {
				return err
			}
			continue
		case OpClose:
			return fmt.Errorf("Discord закрыл соединение: %s", string(body))
		case OpFrame:
		default:
			continue
		}

		var p Payload
		if err := json.Unmarshal(body, &p); err != nil {
			c.log.Warn("нераспознанный кадр", "err", err)
			continue
		}
		c.log.Debug("кадр RPC", "cmd", p.Cmd, "evt", p.Evt, "nonce", p.Nonce)

		// Ответ на нашу команду?
		if p.Nonce != "" {
			c.mu.Lock()
			ch, ok := c.pending[p.Nonce]
			delete(c.pending, p.Nonce)
			c.mu.Unlock()
			if ok {
				ch <- p
				continue
			}
		}
		if p.Evt == "" {
			continue
		}

		switch p.Evt {
		case EvReady:
			var rd ReadyData
			if err := json.Unmarshal(p.Data, &rd); err != nil {
				return fmt.Errorf("разбор READY: %w", err)
			}
			c.mu.Lock()
			c.userID, c.username = rd.User.ID, rd.User.Username
			ready := c.ready
			c.mu.Unlock()
			if ready != nil {
				select {
				case ready <- rd:
				default:
				}
			}
		case EvVoiceChannelSelect:
			// Наверх отдаём не сразу: сначала переподписка и запрос имени канала.
			c.pushChannelSelect(p.Data)
			continue
		}

		c.emit(Event{Name: p.Evt, Data: p.Data})
	}
}

func (c *Client) emit(ev Event) {
	select {
	case c.events <- ev:
	default:
		c.log.Warn("очередь событий переполнена, кадр отброшен", "evt", ev.Name)
	}
}

func (c *Client) pushChannelSelect(raw []byte) {
	c.mu.Lock()
	q := c.chanSel
	c.mu.Unlock()
	if q == nil {
		return
	}
	select {
	case q <- raw:
	default:
		c.log.Warn("очередь смены канала переполнена")
	}
}

// channelWorker последовательно обрабатывает смену голосового канала:
// снимает старые подписки, ставит новые, дополняет событие именем канала.
func (c *Client) channelWorker(ctx context.Context) {
	c.mu.Lock()
	q := c.chanSel
	c.mu.Unlock()
	for {
		select {
		case <-ctx.Done():
			return
		case raw, ok := <-q:
			if !ok {
				return
			}
			var sel VoiceChannelSelect
			if err := json.Unmarshal(raw, &sel); err != nil {
				c.log.Warn("разбор VOICE_CHANNEL_SELECT", "err", err)
				continue
			}
			c.syncChannelSubs(ctx, sel.ChannelID)
			if sel.ChannelID != "" && sel.Name == "" {
				if ch, err := c.SelectedVoiceChannel(ctx); err == nil && ch != nil && ch.ID == sel.ChannelID {
					sel.Name, sel.GuildID = ch.Name, ch.GuildID
				}
			}
			if out, err := json.Marshal(sel); err == nil {
				c.emit(Event{Name: EvVoiceChannelSelect, Data: out})
			}
		}
	}
}

// syncChannelSubs переносит подписки со старого канала на новый.
func (c *Client) syncChannelSubs(ctx context.Context, channelID string) {
	c.mu.Lock()
	old := c.curChan
	if old == channelID {
		c.mu.Unlock()
		return
	}
	c.curChan = channelID
	c.mu.Unlock()

	if old != "" {
		for _, evt := range channelEvents {
			if err := c.unsubscribe(ctx, evt, map[string]any{"channel_id": old}); err != nil {
				c.log.Debug("отписка не удалась", "evt", evt, "channel", old, "err", err)
			}
		}
	}
	if channelID == "" {
		c.log.Info("вышли из голосового канала")
		return
	}
	for _, evt := range channelEvents {
		if err := c.subscribe(ctx, evt, map[string]any{"channel_id": channelID}); err != nil {
			c.log.Warn("подписка на события канала", "evt", evt, "channel", channelID, "err", err)
		}
	}
	c.log.Info("подписан на события канала", "channel", channelID)
}

// call отправляет команду и ждёт ответ с тем же nonce.
func (c *Client) call(ctx context.Context, cmd string, args any, out any) error {
	return c.callFrame(ctx, map[string]any{"cmd": cmd}, cmd, args, out)
}

// callFrame — общий путь для CMD и SUBSCRIBE/UNSUBSCRIBE (у последних есть evt).
func (c *Client) callFrame(ctx context.Context, req map[string]any, label string, args any, out any) error {
	c.mu.Lock()
	conn := c.conn
	if conn == nil {
		c.mu.Unlock()
		return errors.New("нет соединения с Discord")
	}
	nonce := newNonce()
	ch := make(chan Payload, 1)
	c.pending[nonce] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, nonce)
		c.mu.Unlock()
	}()

	req["nonce"] = nonce
	if args != nil {
		req["args"] = args
	}
	if err := writeFrame(conn, OpFrame, req); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(rpcTimeout):
		return fmt.Errorf("таймаут ответа на %s", label)
	case p, ok := <-ch:
		if !ok {
			return errors.New("соединение с Discord закрыто")
		}
		if p.Evt == EvError {
			return parseRPCError(label, p.Data)
		}
		if out != nil && len(p.Data) > 0 {
			return json.Unmarshal(p.Data, out)
		}
		return nil
	}
}

func parseRPCError(label string, data []byte) error {
	var e struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &e); err != nil || e.Message == "" {
		return fmt.Errorf("%s: %s", label, string(data))
	}
	return fmt.Errorf("%s: %s (code %d)", label, e.Message, e.Code)
}

func (c *Client) subscribe(ctx context.Context, evt string, args map[string]any) error {
	return c.callFrame(ctx, map[string]any{"cmd": CmdSubscribe, "evt": evt}, "SUBSCRIBE "+evt, args, nil)
}

func (c *Client) unsubscribe(ctx context.Context, evt string, args map[string]any) error {
	return c.callFrame(ctx, map[string]any{"cmd": CmdUnsubscribe, "evt": evt}, "UNSUBSCRIBE "+evt, args, nil)
}

// VoiceSettings запрашивает текущее локальное состояние микрофона и звука.
func (c *Client) VoiceSettings(ctx context.Context) (*VoiceSettings, error) {
	var vs VoiceSettings
	if err := c.call(ctx, CmdGetVoiceSettings, nil, &vs); err != nil {
		return nil, err
	}
	return &vs, nil
}

// SelectedVoiceChannel возвращает текущий голосовой канал или nil, если мы не в канале.
func (c *Client) SelectedVoiceChannel(ctx context.Context) (*SelectedVoiceChannel, error) {
	var ch SelectedVoiceChannel
	if err := c.call(ctx, CmdGetSelectedVoice, nil, &ch); err != nil {
		return nil, err
	}
	if ch.ID == "" {
		return nil, nil
	}
	return &ch, nil
}
