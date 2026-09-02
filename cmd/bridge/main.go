// Команда bridge — трей-приложение, связывающее состояние Discord с подсветкой HyperHDR.
package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/app"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/console"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/tray"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/web"
)

func main() {
	console.Setup()

	cfgPath := flag.String("config", "config.yaml", "путь к файлу конфигурации")
	noTray := flag.Bool("no-tray", false, "запуск без иконки в трее (консольный режим)")
	logPath := flag.String("log", "", "файл лога (по умолчанию bridge.log рядом с exe)")
	flag.Parse()

	logOut, logFile, closer := openLog(*logPath)
	if closer != nil {
		defer closer.Close()
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("конфигурация", "err", err)
		os.Exit(1)
	}
	log := newLogger(cfg.LogLevel, logOut)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() { <-sig; log.Info("получен сигнал завершения"); cancel() }()

	a := app.New(cfg, *cfgPath, log)
	a.LogPath = logFile

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := a.Run(ctx); err != nil && ctx.Err() == nil {
			log.Error("приложение остановлено", "err", err)
		}
	}()

	if cfg.Web.Enabled {
		srv := web.New(a, log.With("mod", "web"))
		go func() {
			if err := srv.Run(ctx); err != nil {
				log.Error("веб-панель", "err", err)
			}
		}()
		if cfg.Web.OpenOnStart {
			tray.OpenBrowser("http://" + cfg.Web.Addr)
		}
	}

	if *noTray {
		<-ctx.Done()
	} else {
		// systray.Run захватывает главный поток ОС — вызываем в main.
		tray.Run(ctx, a, cancel, log.With("mod", "tray"))
	}
	<-done
	log.Info("завершено")
}

func newLogger(level string, out io.Writer) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	h := slog.NewTextHandler(out, &slog.HandlerOptions{Level: lvl})
	return slog.New(h)
}
