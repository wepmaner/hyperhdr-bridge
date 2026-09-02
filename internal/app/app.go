// Package app связывает Discord, правила, HyperHDR, веб-панель и трей.
package app

import (
	"context"
	"log/slog"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
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
	Rules   *rules.Engine
	State   *state.Store
}

func New(cfg *config.Config, cfgPath string, log *slog.Logger) *App {
	hdr := hyperhdr.New(cfg.HyperHDR, log.With("mod", "hyperhdr"))
	return &App{
		Cfg:     cfg,
		CfgPath: cfgPath,
		Log:     log,
		Discord: discord.NewClient(cfg.Discord, log.With("mod", "discord")),
		HDR:     hdr,
		Rules:   rules.New(cfg.Rules, hdr, log.With("mod", "rules")),
		State:   state.New(),
	}
}

// Run запускает клиент Discord и обработчики до отмены контекста.
func (a *App) Run(ctx context.Context) error {
	go func() {
		if err := a.Discord.Run(ctx); err != nil && ctx.Err() == nil {
			a.Log.Error("клиент Discord остановлен", "err", err)
		}
	}()
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
	return config.Save(a.CfgPath, a.Cfg)
}
