package app

import "time"

// defaultShutdown — сколько ждём на корректное завершение (снятие приоритета и т.п.).
const defaultShutdown = 3 * time.Second
