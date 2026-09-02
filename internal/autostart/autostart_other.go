//go:build !windows

package autostart

// Supported — вне Windows автозапуск пока не реализован.
func Supported() bool { return false }

func Command(string) (string, error) { return "", ErrUnsupported }

func state() (bool, string, error) { return false, "", ErrUnsupported }

func enable(string) error { return ErrUnsupported }

func disable() error { return ErrUnsupported }

func sameCommand(a, b string) bool { return a == b }
