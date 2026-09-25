//go:build windows

package cs2

import (
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// processName — имя процесса игры. Нужно только чтобы отличить «игра запущена»
// от «выключена» и подсказать в панели, что cfg подхватится после перезапуска.
const processName = "cs2.exe"

// Supported — на Windows умеем и искать Steam, и видеть запущенную игру.
func Supported() bool { return true }

// steamPath читает каталог Steam из реестра. HKCU достаточно и не требует прав
// администратора; HKLM — запасной вариант для установок «на всех пользователей».
func steamPath() (string, error) {
	if p, err := regString(registry.CURRENT_USER, `Software\Valve\Steam`, "SteamPath"); err == nil {
		return filepath.FromSlash(p), nil
	}
	// В HKLM ключ 32-битный, поэтому WOW6432Node.
	p, err := regString(registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Valve\Steam`, "InstallPath")
	if err != nil {
		return "", fmt.Errorf("Steam не найден в реестре: %w", err)
	}
	return filepath.FromSlash(p), nil
}

func regString(root registry.Key, path, name string) (string, error) {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer key.Close()
	val, _, err := key.GetStringValue(name)
	if err != nil {
		return "", err
	}
	if val == "" {
		return "", fmt.Errorf("%s\\%s пуст", path, name)
	}
	return val, nil
}

// Running сообщает, запущена ли игра.
//
// Сознательно используется только перечисление процессов: дескриптор процесса
// игры не открывается никогда. Время старта процесса через OpenProcess дало бы
// более точный ответ «читала ли запущенная игра свежий cfg», но обращение к
// дескриптору чужого игрового процесса — ровно то поведение, которое выглядит
// как чит. Вся ценность GSI в том, что игру мы не трогаем; ломать это ради
// подсказки в панели нельзя.
func Running() bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snap, &entry); err != nil {
		return false
	}
	for {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), processName) {
			return true
		}
		if err := windows.Process32Next(snap, &entry); err != nil {
			return false
		}
	}
}
