// Package app связывает Discord, правила, HyperHDR, веб-панель и трей.
package app

import (
	"context"
	"log/slog"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/cs2"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/discord"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/hyperhdr"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/rules"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/state"
)

// App — корневой объект приложения.
type App struct {
	Cfg     *config.Config
	CfgPath string
	Log     *slog.Logger
	// LogPath — файл лога, если он ведётся (пункт трея «Открыть лог»).
	LogPath string

	Discord *discord.Client
	HDR     *hyperhdr.Client
	CS2     *cs2.Server
	// CS2Track превращает поток кадров GSI в признаки состояния.
	CS2Track *cs2.Tracker
	Rules    *rules.Engine
	State    *state.Store
}

func New(cfg *config.Config, cfgPath string, log *slog.Logger) *App {
	hdr := hyperhdr.New(cfg.HyperHDR, log.With("mod", "hyperhdr"))
	a := &App{
		Cfg:      cfg,
		CfgPath:  cfgPath,
		Log:      log,
		Discord:  discord.NewClient(cfg.Discord, log.With("mod", "discord")),
		HDR:      hdr,
		CS2:      cs2.New(cfg.CS2, log.With("mod", "cs2")),
		CS2Track: cs2.NewTracker(cfg.CS2, log.With("mod", "cs2")),
		Rules:    rules.New(cfg.Rules, hdr, log.With("mod", "rules")),
		State:    state.New(),
	}
	// Слушатель отдаёт сырые кадры трекеру, а тот — уже готовые признаки
	// в общий снимок состояния. Правила про CS2 по-прежнему ничего не знают.
	a.CS2.OnFrame = a.CS2Track.Frame
	a.CS2.OnIdle = a.CS2Track.Reset
	a.CS2Track.OnChange = func(sig cs2.Signals) {
		a.State.Update(func(s *state.Snapshot) {
			s.CS2Live = sig.Live
			s.CS2Dead = sig.Dead
			s.CS2Flashed = sig.Flashed
			s.CS2BombPhase = sig.BombPhase
			s.CS2BombExploded = sig.Exploded
		})
	}
	// При установке cfg игры может сгенерироваться токен — его нужно сохранить,
	// иначе после перезапуска моста игра будет стучаться с чужим секретом.
	a.CS2.Persist = func(config.CS2) error { return a.SaveConfig() }
	return a
}

// Run запускает клиент Discord и обработчики до отмены контекста.
func (a *App) Run(ctx context.Context) error {
	go func() {
		if err := a.Discord.Run(ctx); err != nil && ctx.Err() == nil {
			a.Log.Error("клиент Discord остановлен", "err", err)
		}
	}()
	// Слушатель CS2 не должен ронять мост: игра может быть не установлена,
	// а порт занят. Ошибка уходит в лог и в статус панели.
	go func() {
		if err := a.CS2.Run(ctx); err != nil && ctx.Err() == nil {
			a.Log.Error("слушатель CS2 остановлен", "err", err)
		}
	}()
	// Фазы бомбы двигает собственный тикер: игра шлёт кадры только при
	// изменении состояния, а тикающая бомба его не меняет.
	go a.CS2Track.Run(ctx)
	go a.consumeEvents(ctx)
	go a.applyLoop(ctx)

	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), defaultShutdown)
	defer cancel()
	return a.HDR.Close(shutdown)
}

// consumeEvents переводит события RPC в изменения состояния.
func (a *App) consumeEvents(ctx context.Context) {
	log := a.Log.With("mod", "events")
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-a.Discord.Events():
			if !ok {
				return
			}
			ApplyEvent(log, a.State, a.Discord.UserID(), ev)
		}
	}
}

// applyLoop гоняет каждое изменение состояния через движок правил.
func (a *App) applyLoop(ctx context.Context) {
	updates, unsub := a.State.Subscribe()
	defer unsub()
	for {
		select {
		case <-ctx.Done():
			return
		case snap, ok := <-updates:
			if !ok {
				return
			}
			if err := a.Rules.Apply(ctx, snap); err != nil && ctx.Err() == nil {
				a.Log.Error("применение правила", "err", err)
			}
		}
	}
}

// SaveConfig сохраняет текущий конфиг на диск (вызывается веб-панелью и треем).
func (a *App) SaveConfig() error {
	a.Cfg.Rules = a.Rules.Config()
	a.Cfg.CS2 = a.CS2.Config()
	return config.Save(a.CfgPath, a.Cfg)
}
