// Package tray — иконка в системном трее.
package tray

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"

	"fyne.io/systray"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/app"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/autostart"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/discord"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/rules"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/state"
)

// Run блокирует горутину до выхода из трея; cancel вызывается при «Выход».
// ВАЖНО: systray должен работать в главной горутине ОС (см. cmd/bridge/main.go).
func Run(ctx context.Context, a *app.App, cancel context.CancelFunc, log *slog.Logger) {
	onReady := func() {
		systray.SetIcon(IconPlain())
		systray.SetTitle("Discord → HyperHDR")
		systray.SetTooltip("Discord → HyperHDR")

		mOpen := systray.AddMenuItem("Открыть панель", "Локальный веб-интерфейс")
		// В GUI-сборке консоли нет, лог — единственный способ увидеть ошибки.
		var logClicked chan struct{}
		if a.LogPath != "" {
			logClicked = systray.AddMenuItem("Открыть лог", a.LogPath).ClickedCh
		}
		mToggle := systray.AddMenuItemCheckbox("Реакция включена",
			"Приостановить реакцию на события", a.Rules.Config().Enabled)

		// Автозапуск: пункт добавляем только там, где умеем его прописывать.
		var mAuto *systray.MenuItem
		var autoClicked chan struct{}
		if st := autostart.Get(a.CfgPath); st.Supported {
			mAuto = systray.AddMenuItemCheckbox("Запускать при входе в систему",
				"Добавить программу в автозапуск Windows", st.Enabled)
			autoClicked = mAuto.ClickedCh
			if st.Error != "" {
				log.Warn("состояние автозапуска", "err", st.Error)
			}
		}
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Выход", "Завершить работу")

		go watchState(ctx, a)

		go func() {
			for {
				select {
				case <-ctx.Done():
					systray.Quit()
					return
				case <-mOpen.ClickedCh:
					OpenBrowser("http://" + a.Cfg.Web.Addr)
				case <-logClicked: // nil-канал, если лога нет: ветка не сработает
					OpenFile(a.LogPath)
				case <-mToggle.ClickedCh:
					cfg := a.Rules.Config()
					cfg.Enabled = !cfg.Enabled
					a.Rules.SetConfig(cfg)
					if cfg.Enabled {
						mToggle.Check()
						if err := a.Rules.Apply(ctx, a.State.Get()); err != nil {
							log.Warn("применение правил", "err", err)
						}
					} else {
						mToggle.Uncheck()
						if err := a.HDR.Clear(ctx); err != nil {
							log.Warn("снятие приоритета", "err", err)
						}
					}
					if err := a.SaveConfig(); err != nil {
						log.Warn("сохранение конфига", "err", err)
					}
				case <-autoClicked: // nil-канал вне Windows: ветка не сработает
					enable := !mAuto.Checked()
					if err := autostart.Set(a.CfgPath, enable); err != nil {
						log.Warn("автозапуск", "enabled", enable, "err", err)
						break
					}
					if enable {
						mAuto.Check()
					} else {
						mAuto.Uncheck()
					}
				case <-mQuit.ClickedCh:
					cancel()
					systray.Quit()
					return
				}
			}
		}()
	}

	systray.Run(onReady, func() { cancel() })
}

// watchState держит иконку и подсказку в трее в согласии с текущим состоянием.
func watchState(ctx context.Context, a *app.App) {
	updates, unsub := a.State.Subscribe()
	defer unsub()

	render := func(snap state.Snapshot) {
		name, act := rules.Match(a.Rules.Config(), snap)
		if act.Type == "color" && len(act.Color) == 3 {
			systray.SetIcon(Icon(uint8(act.Color[0]), uint8(act.Color[1]), uint8(act.Color[2])))
		} else {
			systray.SetIcon(Icon(120, 124, 134))
		}
		systray.SetTooltip(tooltip(a, snap, name))
	}
	render(a.State.Get())

	for {
		select {
		case <-ctx.Done():
			return
		case snap, ok := <-updates:
			if !ok {
				return
			}
			render(snap)
		}
	}
}

func tooltip(a *app.App, snap state.Snapshot, ruleName string) string {
	status, user, lastErr := a.Discord.Info()
	if status != discord.StatusAuthenticated {
		if lastErr != "" {
			return fmt.Sprintf("Discord: %s\n%s", status, lastErr)
		}
		return fmt.Sprintf("Discord: %s", status)
	}
	where := "вне голосового канала"
	if snap.Connected {
		where = snap.ChannelName
		if where == "" {
			where = snap.ChannelID
		}
	}
	return fmt.Sprintf("%s\n%s\nсостояние: %s", user, where, ruleName)
}

// OpenFile открывает файл в программе по умолчанию (лог — в блокноте).
func OpenFile(path string) {
	switch runtime.GOOS {
	case "windows":
		_ = exec.Command("notepad", path).Start()
	case "darwin":
		_ = exec.Command("open", path).Start()
	default:
		_ = exec.Command("xdg-open", path).Start()
	}
}

// OpenBrowser открывает URL в браузере по умолчанию.
func OpenBrowser(url string) {
	switch runtime.GOOS {
	case "windows":
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		_ = exec.Command("open", url).Start()
	default:
		_ = exec.Command("xdg-open", url).Start()
	}
}
