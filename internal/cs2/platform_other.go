//go:build !windows

package cs2

// Вне Windows поиск Steam и определение запущенной игры не реализованы.
// Сам слушатель GSI при этом работает: cfg можно положить руками, указав
// каталог в cs2.cfg_path.

func Supported() bool { return false }

func steamPath() (string, error) { return "", ErrUnsupported }

func Running() bool { return false }
