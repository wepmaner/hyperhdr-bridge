package main

// version проставляется при сборке релиза: go build -ldflags "-X main.version=1.2.0".
// Локальная сборка без флага остаётся "dev" — такие сборки Stash не обновляет.
var version = "dev"
