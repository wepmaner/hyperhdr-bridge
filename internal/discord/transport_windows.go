//go:build windows

package discord

import (
	"fmt"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// dialIPC перебирает \\.\pipe\discord-ipc-0..N и возвращает первый живой канал.
// Индексов несколько, если одновременно запущены stable / PTB / Canary.
func dialIPC(attempts int) (net.Conn, string, error) {
	if attempts <= 0 {
		attempts = 10
	}
	timeout := 2 * time.Second
	var lastErr error
	for i := 0; i < attempts; i++ {
		path := fmt.Sprintf(`\\.\pipe\discord-ipc-%d`, i)
		conn, err := winio.DialPipe(path, &timeout)
		if err == nil {
			return conn, path, nil
		}
		lastErr = err
	}
	return nil, "", fmt.Errorf("не найден ни один discord-ipc сокет из %d (Discord запущен?): %w",
		attempts, lastErr)
}
