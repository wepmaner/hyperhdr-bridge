//go:build windows

package autostart

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// runKey — ключ автозапуска текущего пользователя: права администратора не нужны.
const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// valueName — имя записи в Run. Менять нельзя: по нему находим свою старую запись.
const valueName = "DiscordHyperHDRBridge"

// Supported — на Windows автозапуск умеем.
func Supported() bool { return true }

// Command собирает строку запуска: полный путь к exe и абсолютный путь к конфигу.
// Абсолютный — потому что автозапуск стартует с произвольным рабочим каталогом.
func Command(cfgPath string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("путь к программе: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	cmd := quote(exe)
	if cfgPath != "" {
		abs, err := filepath.Abs(cfgPath)
		if err != nil {
			return "", fmt.Errorf("путь к конфигу: %w", err)
		}
		cmd += " -config " + quote(abs)
	}
	return cmd, nil
}

// state читает текущее значение из реестра.
func state() (bool, string, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return false, "", nil
		}
		return false, "", fmt.Errorf("открытие ключа автозапуска: %w", err)
	}
	defer key.Close()

	cmd, _, err := key.GetStringValue(valueName)
	if err == registry.ErrNotExist {
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("чтение записи автозапуска: %w", err)
	}
	return true, cmd, nil
}

func enable(cfgPath string) error {
	cmd, err := Command(cfgPath)
	if err != nil {
		return err
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("открытие ключа автозапуска: %w", err)
	}
	defer key.Close()
	if err := key.SetStringValue(valueName, cmd); err != nil {
		return fmt.Errorf("запись автозапуска: %w", err)
	}
	return nil
}

func disable() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return fmt.Errorf("открытие ключа автозапуска: %w", err)
	}
	defer key.Close()
	if err := key.DeleteValue(valueName); err != nil && err != registry.ErrNotExist {
		return fmt.Errorf("удаление записи автозапуска: %w", err)
	}
	return nil
}

// sameCommand сравнивает строки запуска: пути в Windows регистронезависимы.
func sameCommand(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// quote оборачивает путь в кавычки — в нём почти наверняка есть пробелы.
func quote(s string) string { return `"` + s + `"` }
