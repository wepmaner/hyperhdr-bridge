// Команда rpcprobe — диагностика интеграции с Discord без HyperHDR.
//
// Подключается к локальному Discord RPC, авторизуется и печатает состояние
// пользователя при каждом изменении. Нужна, чтобы убедиться, что мост
// действительно «видит» мут, глушение и речь.
//
//	go run ./cmd/rpcprobe -config config.yaml
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/app"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/console"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/discord"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/state"
)

func main() {
	console.Setup()

	cfgPath := flag.String("config", "config.yaml", "путь к файлу конфигурации")
	verbose := flag.Bool("v", false, "печатать каждый кадр RPC")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "конфигурация:", err)
		os.Exit(1)
	}
	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() { <-sig; cancel() }()

	client := discord.NewClient(cfg.Discord, log.With("mod", "discord"))
	store := state.New()

	go func() {
		if err := client.Run(ctx); err != nil && ctx.Err() == nil {
			log.Error("клиент Discord остановлен", "err", err)
		}
	}()

	updates, unsub := store.Subscribe()
	defer unsub()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-client.Events():
				if !ok {
					return
				}
				app.ApplyEvent(log.With("mod", "events"), store, client.UserID(), ev)
			}
		}
	}()

	fmt.Println("Ожидаю событий Discord. Попробуйте: зайти в голосовой канал,")
	fmt.Println("нажать мут, нажать глушение, что-нибудь сказать. Ctrl+C — выход.")
	fmt.Println()

	for {
		select {
		case <-ctx.Done():
			return
		case snap, ok := <-updates:
			if !ok {
				return
			}
			printSnapshot(snap)
		}
	}
}

func printSnapshot(s state.Snapshot) {
	channel := "—"
	if s.Connected {
		channel = s.ChannelName
		if channel == "" {
			channel = s.ChannelID
		}
	}
	fmt.Printf("%s  канал=%-20s говорит=%-5s мут=%-5s глушение=%-5s (self m/d=%v/%v, server m/d=%v/%v)\n",
		s.UpdatedAt.Format("15:04:05.000"),
		channel,
		yn(s.Speaking), yn(s.Muted()), yn(s.Deafened()),
		s.SelfMute, s.SelfDeaf, s.ServerMute, s.ServerDeaf,
	)
}

func yn(b bool) string {
	if b {
		return "да"
	}
	return "нет"
}
