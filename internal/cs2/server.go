package cs2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
)

// maxFrameBytes — потолок на размер снимка состояния. Блоки allplayers_* мы не
// запрашиваем, так что реальные кадры на порядки меньше; ограничение защищает
// от постороннего процесса, который вздумает залить в порт мегабайты.
const maxFrameBytes = 1 << 20

// badTokenWarnEvery — как часто жаловаться в лог на чужие запросы. Без этого
// один зациклившийся процесс забьёт лог полностью.
const badTokenWarnEvery = time.Minute

// Server принимает снимки состояния от CS2 по HTTP.
//
// Игра шлёт не события, а полный снимок с заданной периодичностью, поэтому
// сервер ничего не интерпретирует: он проверяет токен и отдаёт сырой кадр
// дальше. Разбор и вывод событий — дело детектора.
type Server struct {
	log *slog.Logger

	// OnFrame получает сырой кадр. Устанавливать до Run.
	OnFrame func([]byte)
	// OnIdle вызывается, когда игра перестала слать данные. Устанавливать до Run.
	OnIdle func()
	// Persist сохраняет изменённую секцию конфига — нужен, когда при установке
	// cfg генерируется новый токен. Устанавливать до Run.
	Persist func(config.CS2) error

	idleTimeout time.Duration

	mu          sync.Mutex
	cfg         config.CS2
	listening   bool
	seenData    bool
	lastFrame   time.Time
	idleFired   bool
	lastErr     string
	lastBadWarn time.Time
	// installedAt — когда cfg записан в этой сессии, и sawStopped — видели ли
	// мы после этого игру незапущенной. Вместе они заменяют время старта
	// процесса игры, к которому мы принципиально не обращаемся.
	installedAt time.Time
	sawStopped  bool
}

func New(cfg config.CS2, log *slog.Logger) *Server {
	timeout := time.Duration(cfg.IdleTimeoutS) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Server{cfg: cfg, log: log, idleTimeout: timeout}
}

// Run поднимает слушатель и работает до отмены контекста.
//
// Ошибка здесь не должна ронять мост: Discord-часть обязана работать и когда
// игра не установлена или порт занят. Причина складывается в Status.Error,
// чтобы её было видно в панели.
func (s *Server) Run(ctx context.Context) error {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()
	if !cfg.Enabled {
		return nil
	}

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		err = fmt.Errorf("слушатель CS2 на %s: %w", cfg.Addr, err)
		s.setError(err.Error())
		return err
	}
	s.mu.Lock()
	s.listening, s.lastErr = true, ""
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.listening = false
		s.mu.Unlock()
	}()

	srv := &http.Server{Handler: s.handler(), ReadHeaderTimeout: 5 * time.Second}
	go s.watchIdle(ctx)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	// Числа таймингов печатаем при старте: отсчёт до взрыва мост ведёт сам, и
	// когда подсветка ведёт себя не так, первый вопрос — с какими значениями
	// она вообще работает.
	s.log.Info("слушаю CS2", "addr", cfg.Addr,
		"fuse_s", cfg.BombFuseS, "hurry_s", cfg.BombHurryS, "panic_s", cfg.BombPanicS,
		"death_hold_ms", cfg.DeathHoldMS, "exploded_hold_ms", cfg.BombExplodedHoldMS)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.setError(err.Error())
		return err
	}
	return nil
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleFrame)
	return mux
}

// authEnvelope — единственное, что сервер читает из кадра сам.
type authEnvelope struct {
	Auth struct {
		Token string `json:"token"`
	} `json:"auth"`
}

func (s *Server) handleFrame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxFrameBytes))
	if err != nil {
		http.Error(w, "кадр не прочитан", http.StatusBadRequest)
		return
	}
	var env authEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		s.log.Debug("кадр CS2 не разобран", "err", err)
		http.Error(w, "не JSON", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	want := s.cfg.Token
	s.mu.Unlock()
	// Пустой токен означает, что cfg ставили руками: проверять нечего, но и
	// отбрасывать данные игры не за что.
	if want != "" && env.Auth.Token != want {
		s.warnBadToken(r.RemoteAddr)
		http.Error(w, "неверный токен", http.StatusForbidden)
		return
	}

	s.mu.Lock()
	s.seenData, s.lastFrame, s.idleFired = true, time.Now(), false
	onFrame := s.OnFrame
	s.mu.Unlock()

	if onFrame != nil {
		onFrame(raw)
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) warnBadToken(remote string) {
	s.mu.Lock()
	quiet := time.Since(s.lastBadWarn) < badTokenWarnEvery
	if !quiet {
		s.lastBadWarn = time.Now()
	}
	s.mu.Unlock()
	if !quiet {
		s.log.Warn("отброшен кадр с неверным токеном", "remote", remote)
	}
}

// watchIdle гасит игровое состояние, когда игра перестала слать данные: она
// закрывается молча, и без сторожа лента залипнет на последнем цвете навсегда.
// Срабатывает один раз на период тишины и только если данные вообще были.
func (s *Server) watchIdle(ctx context.Context) {
	interval := s.idleTimeout / 3
	if interval < 10*time.Millisecond {
		interval = 10 * time.Millisecond
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.mu.Lock()
			idle := s.seenData && !s.idleFired && time.Since(s.lastFrame) > s.idleTimeout
			if idle {
				s.idleFired = true
			}
			onIdle := s.OnIdle
			s.mu.Unlock()
			if idle && onIdle != nil {
				s.log.Info("данные от CS2 прекратились, гашу игровое состояние")
				onIdle()
			}
		}
	}
}

func (s *Server) setError(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastErr = msg
}

// Config возвращает текущую секцию конфига (токен мог смениться при установке).
func (s *Server) Config() config.CS2 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// Status собирает полную картину для панели: слушатель, файл конфигурации игры
// и запущена ли сама игра.
func (s *Server) Status() Status {
	s.mu.Lock()
	cfg := s.cfg
	st := Status{
		Supported: Supported(),
		Addr:      cfg.Addr,
		Listening: s.listening,
		SeenData:  s.seenData,
		Error:     s.lastErr,
	}
	installedAt := s.installedAt
	s.mu.Unlock()

	if !cfg.Enabled {
		return st
	}

	st.Running = Running()
	if !st.Running && !installedAt.IsZero() {
		// Игру видели выключенной уже после записи cfg — значит, перезапуск
		// состоялся и напоминать о нём больше не нужно.
		s.mu.Lock()
		s.sawStopped = true
		s.mu.Unlock()
	}

	dir, err := CfgDir(cfg.CfgPath)
	if err != nil {
		if st.Error == "" {
			st.Error = err.Error()
		}
	} else {
		ins := inspect(dir, cfg.Addr, cfg.Token)
		st.Dir, st.Path = ins.Dir, ins.Path
		st.Installed, st.Stale = ins.Installed, ins.Stale
		if ins.Error != "" && st.Error == "" {
			st.Error = ins.Error
		}
	}

	s.mu.Lock()
	pendingRestart := !installedAt.IsZero() && !s.sawStopped
	s.mu.Unlock()
	st.NeedsRestart = st.Running && (!st.Installed || st.Stale || pendingRestart)
	return st
}

// Install записывает cfg в каталог игры, при необходимости сгенерировав токен.
//
// cfg читается игрой только при запуске, поэтому после установки CS2 нужно
// перезапустить — об этом сообщает Status.NeedsRestart.
func (s *Server) Install() (Status, error) {
	cfg := s.Config()
	dir, err := CfgDir(cfg.CfgPath)
	if err != nil {
		return s.Status(), err
	}
	token := cfg.Token
	if token == "" {
		if token, err = NewToken(); err != nil {
			return s.Status(), err
		}
	}
	path, err := install(dir, cfg.Addr, token)
	if err != nil {
		return s.Status(), err
	}

	s.mu.Lock()
	s.cfg.Token = token
	s.installedAt = time.Now()
	s.sawStopped = false
	changed := s.cfg
	persist := s.Persist
	s.mu.Unlock()

	if persist != nil {
		if err := persist(changed); err != nil {
			// Файл игре уже записан; несохранённый токен — повод сказать об
			// этом, но не повод считать установку несостоявшейся.
			s.log.Error("токен CS2 не сохранён в конфиг", "err", err)
			st := s.Status()
			st.Error = "конфиг игры записан, но токен не сохранён: " + err.Error()
			return st, nil
		}
	}
	s.log.Info("конфиг GSI установлен", "path", path)
	return s.Status(), nil
}
