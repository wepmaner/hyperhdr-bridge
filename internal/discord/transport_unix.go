//go:build !windows

package discord

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// dialIPC ищет сокет discord-ipc-N во временных каталогах пользователя.
func dialIPC(attempts int) (net.Conn, string, error) {
	for _, base := range ipcDirs() {
		for i := 0; i < attempts; i++ {
			path := filepath.Join(base, fmt.Sprintf("discord-ipc-%d", i))
			if conn, err := net.Dial("unix", path); err == nil {
				return conn, path, nil
			}
		}
	}
	return nil, "", fmt.Errorf("не найден ни один discord-ipc сокет (Discord запущен?)")
}

func ipcDirs() []string {
	dirs := []string{}
	for _, env := range []string{"XDG_RUNTIME_DIR", "TMPDIR", "TMP", "TEMP"} {
		if v := os.Getenv(env); v != "" {
			dirs = append(dirs, v)
		}
	}
	dirs = append(dirs, "/tmp")
	// Flatpak/Snap-варианты клиента Discord.
	extra := []string{"app/com.discordapp.Discord", "snap.discord"}
	for _, d := range append([]string{}, dirs...) {
		for _, e := range extra {
			dirs = append(dirs, filepath.Join(d, e))
		}
	}
	return dirs
}
