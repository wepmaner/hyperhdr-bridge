// Package autostart управляет автозапуском моста при входе пользователя в систему.
//
// Реализация зависит от ОС: на Windows это значение в HKCU\...\Run,
// на остальных платформах автозапуск пока не поддержан (см. autostart_other.go).
package autostart

import (
	"errors"
	"strings"
)

// ErrUnsupported возвращается на платформах без реализации автозапуска.
var ErrUnsupported = errors.New("автозапуск не поддержан на этой платформе")

// Status — текущее состояние автозапуска для панели и трея.
type Status struct {
	// Supported — умеет ли текущая ОС включать автозапуск.
	Supported bool `json:"supported"`
	// Enabled — прописан ли автозапуск сейчас.
	Enabled bool `json:"enabled"`
	// Command — что именно записано в автозапуск (пусто, если выключен).
	Command string `json:"command,omitempty"`
	// Stale — автозапуск прописан, но ведёт на другой exe (старая копия).
	Stale bool `json:"stale,omitempty"`
	// Error — почему состояние не удалось прочитать.
	Error string `json:"error,omitempty"`
}

// Get собирает Status, пряча ошибки в поле Error: панели нужен ответ, а не 500.
func Get(cfgPath string) Status {
	if !Supported() {
		return Status{}
	}
	enabled, cmd, err := state()
	st := Status{Supported: true, Enabled: enabled, Command: cmd}
	if err != nil {
		st.Error = err.Error()
		return st
	}
	if enabled {
		want, err := Command(cfgPath)
		st.Stale = err == nil && !sameCommand(cmd, want)
	}
	return st
}

// Refresh чинит устаревшую запись автозапуска: та же программа, но старая
// строка запуска (например, из версии без -silent). Переписываем только записи,
// ведущие на этот же exe, — чужую копию программы пользователь пусть решает сам
// (в UI это видно как Stale). Возвращает true, если запись была переписана.
func Refresh(cfgPath string) (bool, error) {
	if !Supported() {
		return false, nil
	}
	enabled, cur, err := state()
	if err != nil || !enabled {
		return false, err
	}
	want, err := Command(cfgPath)
	if err != nil {
		return false, err
	}
	if sameCommand(cur, want) {
		return false, nil
	}
	if !sameCommand(commandExe(cur), commandExe(want)) {
		return false, nil
	}
	if err := enable(cfgPath); err != nil {
		return false, err
	}
	return true, nil
}

// commandExe вырезает из строки запуска путь к программе: он либо в кавычках,
// либо (без пробелов в пути) занимает всё до первого пробела.
func commandExe(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if strings.HasPrefix(cmd, `"`) {
		if end := strings.Index(cmd[1:], `"`); end >= 0 {
			return cmd[1 : end+1]
		}
		return cmd[1:]
	}
	if sp := strings.Index(cmd, " "); sp >= 0 {
		return cmd[:sp]
	}
	return cmd
}

// Set включает или выключает автозапуск.
func Set(cfgPath string, enabled bool) error {
	if !Supported() {
		return ErrUnsupported
	}
	if enabled {
		return enable(cfgPath)
	}
	return disable()
}
