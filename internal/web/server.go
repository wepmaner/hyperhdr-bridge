// Package web — локальная панель управления на 127.0.0.1.
package web

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/app"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/autostart"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/cs2"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/presets"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/state"
)

// static — собранная панель (исходники в web/, сборка: npm run build).
//
//go:embed all:static
var staticFS embed.FS

const (
	// healthEvery — как часто перепроверять связь с HyperHDR и состояние CS2.
	healthEvery = 3 * time.Second
	// maxBody — потолок на тело запроса панели: самое большое — последовательность
	// из 200 шагов, это единицы килобайт.
	maxBody = 1 << 20
)

// Server — HTTP-сервер панели.
type Server struct {
	app *app.App
	log *slog.Logger
	srv *http.Server

	mu     sync.Mutex
	health health
	subs   map[chan struct{}]struct{}
}

// health — то, что дорого узнавать на каждый запрос: связь с HyperHDR (сетевой
// вызов) и состояние CS2 (перечисление процессов, чтение файла в каталоге игры).
type health struct {
	hdrErr string
	cs2    cs2.Status
}

func New(a *app.App, log *slog.Logger) *Server {
	s := &Server{app: a, log: log, subs: make(map[chan struct{}]struct{})}
	mux := http.NewServeMux()

	sub, _ := fs.Sub(staticFS, "static")
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("GET /api/panel", s.handlePanel)
	mux.HandleFunc("GET /api/stream", s.handleStream)
	mux.HandleFunc("PUT /api/events/{id}", s.handleEvent)
	mux.HandleFunc("PUT /api/order", s.handleOrder)
	mux.HandleFunc("POST /api/test", s.handleTest)
	mux.HandleFunc("POST /api/test/stop", s.handleTestStop)
	mux.HandleFunc("POST /api/presets", s.handlePresetAdd)
	mux.HandleFunc("DELETE /api/presets/{id}", s.handlePresetDelete)
	mux.HandleFunc("PUT /api/settings", s.handleSettings)
	mux.HandleFunc("POST /api/hyperhdr/check", s.handleHDRCheck)
	mux.HandleFunc("POST /api/hyperhdr/blink", s.handleHDRBlink)
	mux.HandleFunc("POST /api/discord/reconnect", s.handleDiscordReconnect)
	mux.HandleFunc("POST /api/cs2/install", s.handleCS2Install)
	// TODO: /oauth/callback — если решим делать браузерный OAuth-флоу
	// вместо RPC AUTHORIZE.

	s.srv = &http.Server{
		Addr:              a.Cfg.Web.Addr,
		Handler:           localOnly(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

// Run поднимает сервер и гасит его по отмене контекста.
func (s *Server) Run(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(shutdown)
	}()
	go s.watchHealth(ctx)
	s.log.Info("веб-панель доступна", "url", "http://"+s.srv.Addr)
	if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// localOnly не даёт чужой странице в браузере управлять мостом: запрос,
// меняющий состояние, принимается только от самой панели.
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// TODO: сверять r.RemoteAddr с 127.0.0.1/::1.
		if origin := r.Header.Get("Origin"); origin != "" && r.Method != http.MethodGet {
			if u, err := url.Parse(origin); err != nil || !isLoopbackHost(u.Hostname()) {
				http.Error(w, "запрос с чужой страницы отклонён", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ── Состояние панели ─────────────────────────────────────────────────────────

// panel — всё, что показывает панель, одним объектом. Он же уходит в поток
// /api/stream и возвращается из каждого запроса, меняющего состояние, чтобы
// панель не собирала картину из нескольких ответов.
type panel struct {
	Status   panelStatus      `json:"status"`
	HyperHDR panelHDR         `json:"hyperhdr"`
	Discord  panelDiscord     `json:"discord"`
	CS2      panelCS2         `json:"cs2"`
	Events   []panelEvent     `json:"events"`
	Presets  []presets.Preset `json:"presets"`
	Settings panelSettings    `json:"settings"`
}

// panelStatus — сводка по интеграциям: ok | err | action | off.
type panelStatus struct {
	HyperHDR string `json:"hyperhdr"`
	Discord  string `json:"discord"`
	CS2      string `json:"cs2"`
}

type panelHDR struct {
	Host  string `json:"host"`
	Port  int    `json:"port"`
	Error string `json:"error,omitempty"`
}

type panelDiscord struct {
	User  string `json:"user"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
	Auto  bool   `json:"auto"`
}

type panelCS2 struct {
	cs2.Status
	Enabled bool `json:"enabled"`
	// Folder — каталог игры, заданный вручную; пусто — ищем Steam сами.
	Folder   string `json:"folder"`
	Spectate bool   `json:"spectate"`
}

// panelEvent — событие в порядке приоритета: первое активное выигрывает.
type panelEvent struct {
	ID      string          `json:"id"`
	Enabled bool            `json:"enabled"`
	Active  bool            `json:"active"`
	Seq     config.Sequence `json:"seq"`
}

type panelSettings struct {
	Autostart autostart.Status `json:"autostart"`
}

func (s *Server) panel() panel {
	s.mu.Lock()
	h := s.health
	s.mu.Unlock()

	hdr := s.app.HDR.Config()
	dStatus, dUser, dErr := s.app.Discord.Info()
	cs := s.app.CS2.Config()
	rules := s.app.Rules.Config()
	snap := s.app.State.Get()

	p := panel{
		HyperHDR: panelHDR{Host: hdr.Host, Port: hdr.JSONPort, Error: h.hdrErr},
		Discord:  panelDiscord{User: dUser, State: string(dStatus), Error: dErr, Auto: s.app.Discord.AutoConnect()},
		CS2:      panelCS2{Status: h.cs2, Enabled: cs.Enabled, Folder: cs.CfgPath, Spectate: cs.ReactWhenSpectating},
		Events:   []panelEvent{},
		Presets:  s.app.Presets.List(),
		Settings: panelSettings{Autostart: autostart.Get(s.app.CfgPath)},
	}

	p.Status.HyperHDR = "ok"
	if h.hdrErr != "" {
		p.Status.HyperHDR = "err"
	}
	switch {
	case dStatus == "authenticated":
		p.Status.Discord = "ok"
	case !p.Discord.Auto && dStatus == "disconnected":
		p.Status.Discord = "off"
	default:
		p.Status.Discord = "err"
	}
	switch st := h.cs2; {
	case !cs.Enabled:
		p.Status.CS2 = "off"
	case !st.Listening:
		p.Status.CS2 = "err"
	case !st.Installed || st.Stale || st.NeedsRestart || st.Error != "":
		p.Status.CS2 = "action"
	default:
		p.Status.CS2 = "ok"
	}

	for _, id := range rules.Order {
		act, ok := rules.States[id]
		if !ok {
			continue
		}
		seq, ok := act.Sequence()
		if !ok {
			seq = config.Sequence{End: config.EndReturn}
		}
		if seq.Steps == nil {
			seq.Steps = []config.Step{}
		}
		p.Events = append(p.Events, panelEvent{
			ID: id, Enabled: act.On(), Active: snap.Has(state.Kind(id)), Seq: seq,
		})
	}
	return p
}

// watchHealth держит health свежим и будит поток панели, когда что-то меняется.
// Тот же тикер доставляет в панель и смену статуса Discord: у неё нет своего
// события, а сравнение отправленного с новым делает сам поток.
func (s *Server) watchHealth(ctx context.Context) {
	s.refreshHealth(ctx)
	tick := time.NewTicker(healthEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.refreshHealth(ctx)
		}
	}
}

func (s *Server) refreshHealth(ctx context.Context) {
	pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	h := health{cs2: s.app.CS2.Status()}
	if err := s.app.HDR.Ping(pctx); err != nil {
		h.hdrErr = err.Error()
	}
	s.mu.Lock()
	s.health = h
	s.mu.Unlock()
	s.notify()
}

// notify будит все открытые потоки панели.
func (s *Server) notify() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- struct{}{}:
		default: // поток ещё не разобрал прошлый сигнал — этого достаточно
		}
	}
}

func (s *Server) subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}
}

// ── Чтение ───────────────────────────────────────────────────────────────────

// handleState отдаёт сырой снимок состояния — для отладки.
func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.app.State.Get())
}

func (s *Server) handlePanel(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.panel())
}

// handleStream — поток состояния панели через Server-Sent Events. Шлёт объект
// целиком и только когда он изменился.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming не поддерживается", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	updates, unsubState := s.app.State.Subscribe()
	defer unsubState()
	woken, unsub := s.subscribe()
	defer unsub()

	var last []byte
	send := func() {
		raw, _ := json.Marshal(s.panel())
		if bytes.Equal(raw, last) {
			return
		}
		last = raw
		fmt.Fprintf(w, "data: %s\n\n", raw)
		flusher.Flush()
	}
	send()

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case _, ok := <-updates:
			if !ok {
				return
			}
			send()
		case <-woken:
			send()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// ── События и порядок ────────────────────────────────────────────────────────

// handleEvent меняет одно событие: выключатель и/или световую последовательность.
func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled *bool            `json:"enabled"`
		Seq     *config.Sequence `json:"seq"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	id := r.PathValue("id")
	rules := cloneRules(s.app.Rules.Config())
	act, ok := rules.States[id]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("нет события %q", id))
		return
	}
	if body.Seq != nil {
		seq, err := body.Seq.Normalize()
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		act = act.WithSequence(seq)
	}
	if body.Enabled != nil {
		act.Enabled = body.Enabled
	}
	rules.States[id] = act
	s.commitRules(w, r, rules)
}

// handleOrder задаёт приоритет событий: первое активное в списке выигрывает.
func (s *Server) handleOrder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Order []string `json:"order"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	rules := cloneRules(s.app.Rules.Config())
	seen := make(map[string]bool, len(body.Order))
	order := make([]string, 0, len(rules.Order))
	for _, id := range body.Order {
		if _, ok := rules.States[id]; !ok {
			writeError(w, http.StatusBadRequest, fmt.Errorf("нет события %q", id))
			return
		}
		if !seen[id] {
			seen[id] = true
			order = append(order, id)
		}
	}
	// То, чего панель не прислала, остаётся в хвосте: потерянное имя в order
	// означало бы событие, которое молча перестало срабатывать.
	for _, id := range rules.Order {
		if !seen[id] {
			order = append(order, id)
		}
	}
	rules.Order = order
	s.commitRules(w, r, rules)
}

// commitRules применяет новые правила, сохраняет конфиг и отвечает панелью.
func (s *Server) commitRules(w http.ResponseWriter, r *http.Request, rules config.Rules) {
	s.app.Rules.SetConfig(rules)
	s.syncCS2Holds(rules)
	if err := s.app.SaveConfig(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	// Применяем сразу к текущему состоянию.
	if err := s.app.Rules.Reapply(r.Context()); err != nil {
		s.log.Warn("не удалось применить новые правила", "err", err)
	}
	s.respond(w)
}

// holdMarginMS — на сколько признак события переживает свою анимацию.
const holdMarginMS = 300

// syncCS2Holds подгоняет время жизни признаков «смерть» и «взрыв» под их
// анимации. Эти события мгновенные, и сколько они «активны», решает мост:
// если признак короче анимации, смена состояния оборвёт её посередине, а
// config.Validate при следующем запуске такой конфиг не примет вовсе.
func (s *Server) syncCS2Holds(rules config.Rules) {
	fit := func(id string, cur int) int {
		act, ok := rules.States[id]
		if !ok {
			return cur
		}
		seq, ok := act.Sequence()
		if !ok || seq.End == config.EndLoop {
			return cur
		}
		need := seq.Total() + holdMarginMS
		if seq.End == config.EndHold {
			// Удержание длится, пока жив признак: укорачивать его незачем.
			return max(cur, need)
		}
		return need
	}
	cur := s.app.CS2.Config()
	dead, exploded := fit("cs2_dead", cur.DeathHoldMS), fit("cs2_bomb_exploded", cur.BombExplodedHoldMS)
	if dead == cur.DeathHoldMS && exploded == cur.BombExplodedHoldMS {
		return
	}
	s.app.CS2Track.SetConfig(s.app.CS2.Update(func(c *config.CS2) {
		c.DeathHoldMS, c.BombExplodedHoldMS = dead, exploded
	}))
}

// ── Проверка на ленте ────────────────────────────────────────────────────────

// handleTest проигрывает последовательность на настоящей ленте.
func (s *Server) handleTest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Seq config.Sequence `json:"seq"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	seq, err := body.Seq.Normalize()
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.preview(w, r, seq)
}

// handleTestStop останавливает проверку на ленте.
func (s *Server) handleTestStop(w http.ResponseWriter, r *http.Request) {
	s.app.Rules.StopPreview()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleHDRBlink — «Мигнуть»: две белые вспышки, чтобы увидеть, что лента на связи.
func (s *Server) handleHDRBlink(w http.ResponseWriter, r *http.Request) {
	white := config.Step{Type: config.StepColor, Color: "#FFFFFF", MS: 250, Brightness: 100}
	pause := config.Step{Type: config.StepPause, MS: 200}
	s.preview(w, r, config.Sequence{End: config.EndReturn, Steps: []config.Step{white, pause, white}})
}

func (s *Server) preview(w http.ResponseWriter, r *http.Request, seq config.Sequence) {
	// Анимация играет в фоне и об ошибке сообщит только в лог, поэтому связь
	// проверяем заранее: панели нужен честный ответ.
	if err := s.app.HDR.Ping(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	d := s.app.Rules.Preview(seq)
	writeJSON(w, http.StatusOK, map[string]any{"ms": d.Milliseconds()})
}

// ── Пресеты ──────────────────────────────────────────────────────────────────

func (s *Server) handlePresetAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string          `json:"name"`
		From string          `json:"from"`
		Seq  config.Sequence `json:"seq"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if _, err := s.app.Presets.Add(body.Name, body.From, body.Seq); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.respond(w)
}

func (s *Server) handlePresetDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Presets.Delete(r.PathValue("id")); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, presets.ErrNotFound) {
			code = http.StatusNotFound
		}
		writeError(w, code, err)
		return
	}
	s.respond(w)
}

// ── Настройки и интеграции ───────────────────────────────────────────────────

// handleSettings меняет общие настройки и настройки интеграций. Каждое поле
// необязательно: панель присылает только то, что человек переключил.
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Autostart   *bool `json:"autostart"`
		DiscordAuto *bool `json:"discordAuto"`
		CS2Spectate *bool `json:"cs2Spectate"`
		HyperHDR    *struct {
			Host string `json:"host"`
			Port int    `json:"port"`
		} `json:"hyperhdr"`
		CS2Folder *string `json:"cs2Folder"`
	}
	if !readJSON(w, r, &body) {
		return
	}

	if body.Autostart != nil {
		if err := autostart.Set(s.app.CfgPath, *body.Autostart); err != nil {
			code := http.StatusInternalServerError
			if errors.Is(err, autostart.ErrUnsupported) {
				code = http.StatusNotImplemented
			}
			writeError(w, code, err)
			return
		}
		s.log.Info("автозапуск", "enabled", *body.Autostart)
	}
	if h := body.HyperHDR; h != nil {
		if h.Host == "" || h.Port < 1 || h.Port > 65535 {
			writeError(w, http.StatusBadRequest, errors.New("нужен адрес и порт 1..65535"))
			return
		}
		s.app.HDR.SetTarget(h.Host, h.Port)
		s.log.Info("адрес HyperHDR изменён", "host", h.Host, "port", h.Port)
	}
	if body.CS2Folder != nil {
		// Пустая строка возвращает автоматический поиск Steam.
		if *body.CS2Folder != "" {
			if _, err := cs2.CfgDir(*body.CS2Folder); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
		}
		s.app.CS2.Update(func(c *config.CS2) { c.CfgPath = *body.CS2Folder })
	}
	if body.CS2Spectate != nil {
		s.app.CS2Track.SetConfig(s.app.CS2.Update(func(c *config.CS2) {
			c.ReactWhenSpectating = *body.CS2Spectate
		}))
	}
	if body.DiscordAuto != nil {
		s.app.Discord.SetAutoConnect(*body.DiscordAuto)
	}

	if err := s.app.SaveConfig(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.refreshHealth(r.Context())
	s.respond(w)
}

// handleHDRCheck — «Повторить»: перепроверить связь сейчас, не дожидаясь тикера.
func (s *Server) handleHDRCheck(w http.ResponseWriter, r *http.Request) {
	s.refreshHealth(r.Context())
	s.respond(w)
}

func (s *Server) handleDiscordReconnect(w http.ResponseWriter, r *http.Request) {
	s.app.Discord.Reconnect()
	s.respond(w)
}

// handleCS2Install записывает cfg в каталог игры.
func (s *Server) handleCS2Install(w http.ResponseWriter, r *http.Request) {
	st, err := s.app.CS2.Install()
	if err != nil {
		s.log.Warn("установка конфига CS2", "err", err)
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.log.Info("конфиг CS2 установлен", "path", st.Path)
	s.refreshHealth(r.Context())
	s.respond(w)
}

// ── Вспомогательное ──────────────────────────────────────────────────────────

// respond отвечает свежей панелью и будит остальные открытые вкладки.
func (s *Server) respond(w http.ResponseWriter) {
	s.notify()
	writeJSON(w, http.StatusOK, s.panel())
}

// cloneRules копирует правила вместе с картой состояний: Config() отдаёт ту же
// карту, которую читает движок, и править её на месте нельзя.
func cloneRules(r config.Rules) config.Rules {
	states := make(map[string]config.Action, len(r.States))
	for k, v := range r.States {
		states[k] = v
	}
	r.States = states
	r.Order = append([]string(nil), r.Order...)
	return r
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("тело запроса: %w", err))
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]any{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
