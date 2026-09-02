package main

import (
	"io"
	"os"
	"path/filepath"
)

// maxLogBytes — при превышении лог начинается заново, чтобы файл не рос вечно.
const maxLogBytes = 1 << 20

// defaultLogPath — bridge.log рядом с исполняемым файлом.
func defaultLogPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "bridge.log"
	}
	return filepath.Join(filepath.Dir(exe), "bridge.log")
}

// openLog возвращает writer для логов и путь к файлу.
//
// В GUI-режиме (сборка с -H=windowsgui) консоли нет и os.Stdout ведёт в никуда,
// поэтому пишем в файл. Если консоль всё-таки есть (обычная сборка или запуск
// с перенаправлением) — дублируем туда же.
func openLog(path string) (io.Writer, string, *os.File) {
	if path == "" {
		path = defaultLogPath()
	}
	if st, err := os.Stat(path); err == nil && st.Size() > maxLogBytes {
		_ = os.Remove(path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		// Каталог только для чтения — не повод не запускаться.
		return os.Stdout, "", nil
	}
	if hasConsole() {
		// Файл первым: io.MultiWriter обрывается на первой ошибке, а stdout
		// в GUI-режиме может оказаться невалидным дескриптором.
		return io.MultiWriter(f, quiet{os.Stdout}), path, f
	}
	return f, path, f
}

// quiet глотает ошибки записи: недоступный stdout не должен ронять логгер.
type quiet struct{ w io.Writer }

func (q quiet) Write(p []byte) (int, error) {
	_, _ = q.w.Write(p)
	return len(p), nil
}

// hasConsole — есть ли куда писать в stdout. В GUI-режиме дескриптор невалиден.
func hasConsole() bool {
	_, err := os.Stdout.Stat()
	return err == nil
}
