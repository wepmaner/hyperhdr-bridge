// Package web — локальная панель управления на 127.0.0.1.
package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/app"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/autostart"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
)

//go:embed static
var staticFS embed.FS

// Server — HTTP-сервер панели.
type Server struct {
	app *app.App
	log *slog.Logger
	srv *http.Server
}

func New(a *app.App, log *slog.Logger) *Server {
	s := &Server{app: a, log: log}
	mux := http.NewServeMux()

	sub, _ := fs.Sub(staticFS, "static")
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/rules", s.handleRules)
	mux.HandleFunc("/api/test", s.handleTest)
	mux.HandleFunc("/api/hyperhdr/info", s.handleHDRInfo)
	mux.HandleFunc("/api/autostart", s.handleAutostart)
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
	s.log.Info("веб-панель доступна", "url", "http://"+s.srv.Addr)
	if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// localOnly отсекает запросы не с петлевого интерфейса.
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// TODO: сверять r.RemoteAddr с 127.0.0.1/::1 и проверять Origin (CSRF).
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.app.State.Get())
}

// handleStatus — состояние подключения к Discord (для индикатора в панели).
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status, user, lastErr := s.app.Discord.Info()
	writeJSON(w, http.StatusOK, map[string]any{
		"discord": map[string]any{
			"status": status,
			"user":   user,
			"error":  lastErr,
		},
		"rules_enabled": s.app.Rules.Config().Enabled,
		"autostart":     autostart.Get(s.app.CfgPath),
	})
}

// handleAutostart: GET — состояние автозапуска, POST {"enabled":bool} — переключить.
func (s *Server) handleAutostart(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, autostart.Get(s.app.CfgPath))
	case http.MethodPost:
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := autostart.Set(s.app.CfgPath, body.Enabled); err != nil {
			code := http.StatusInternalServerError
			if err == autostart.ErrUnsupported {
				code = http.StatusNotImplemented
			}
			http.Error(w, err.Error(), code)
			return
		}
		s.log.Info("автозапуск", "enabled", body.Enabled)
		writeJSON(w, http.StatusOK, autostart.Get(s.app.CfgPath))
	default:
		http.Error(w, "метод не поддерживается", http.StatusMethodNotAllowed)
	}
}

// handleEvents — поток обновлений состояния через Server-Sent Events.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming не поддерживается", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	updates, unsub := s.app.State.Subscribe()
	defer unsub()

	send := func(v any) {
		raw, _ := json.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", raw)
		flusher.Flush()
	}
	send(s.app.State.Get())

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case snap, ok := <-updates:
			if !ok {
				return
			}
			send(snap)
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// handleRules: GET — текущие правила, PUT — заменить и сохранить.
func (s *Server) handleRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.app.Rules.Config())
	case http.MethodPut:
		var rules config.Rules
		if err := json.NewDecoder(r.Body).Decode(&rules); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.app.Rules.SetConfig(rules)
		if err := s.app.SaveConfig(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Применяем сразу к текущему состоянию.
		if err := s.app.Rules.Apply(r.Context(), s.app.State.Get()); err != nil {
			s.log.Warn("не удалось применить новые правила", "err", err)
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		http.Error(w, "метод не поддерживается", http.StatusMethodNotAllowed)
	}
}

// handleTest подсвечивает переданный цвет — «примерка» из панели.
func (s *Server) handleTest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Color      []int `json:"color"`
		DurationMS int   `json:"duration_ms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Color) != 3 {
		http.Error(w, "ожидается {\"color\":[R,G,B]}", http.StatusBadRequest)
		return
	}
	if body.DurationMS == 0 {
		body.DurationMS = 1500
	}
	if err := s.app.HDR.SetColor(r.Context(), body.Color[0], body.Color[1], body.Color[2], body.DurationMS); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleHDRInfo(w http.ResponseWriter, r *http.Request) {
	info, err := s.app.HDR.ServerInfo(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(info)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
