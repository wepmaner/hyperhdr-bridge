//go:build windows

// Package console приводит вывод консоли к UTF-8.
package console

import "syscall"

// Setup переключает кодовую страницу консоли на UTF-8 (65001).
// Без этого русский текст в cmd.exe выводится кракозябрами: Go пишет UTF-8,
// а консоль по умолчанию ждёт cp866. Ошибку игнорируем — при запуске
// без консоли (GUI-режим трея) вызов просто не сработает.
func Setup() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	_, _, _ = kernel32.NewProc("SetConsoleOutputCP").Call(65001)
	_, _, _ = kernel32.NewProc("SetConsoleCP").Call(65001)
}
