// Команда bridge — трей-приложение, связывающее состояние Discord с подсветкой HyperHDR.
package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/app"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/autostart"
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
	silent := flag.Bool("silent", false, "не открывать веб-панель при запуске (режим автозапуска)")
	flag.Parse()

	*cfgPath = resolveConfigPath(*cfgPath)

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
	log.Info("запуск", "version", version)

	// Запись автозапуска могла остаться от прошлой версии (без -silent) —
	// приводим её к текущей строке запуска, иначе окно панели будет
	// открываться при каждом входе в систему.
	if updated, err := autostart.Refresh(*cfgPath); err != nil {
		log.Warn("обновление записи автозапуска", "err", err)
	} else if updated {
		log.Info("запись автозапуска обновлена")
	}

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
		if cfg.Web.OpenOnStart && !*silent {
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

// resolveConfigPath ищет конфиг рядом с программой, если относительный путь не
// нашёлся в рабочем каталоге. При автозапуске рабочий каталог — System32,
// и без этого мост читал бы (и создавал) конфиг не там, где ожидает человек.
func resolveConfigPath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	if _, err := os.Stat(path); err == nil {
		return path
	}
	exe, err := os.Executable()
	if err != nil {
		return path
	}
	return filepath.Join(filepath.Dir(exe), path)
}

func newLogger(level string, out io.Writer) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	h := slog.NewTextHandler(out, &slog.HandlerOptions{Level: lvl})
	return slog.New(h)
}
