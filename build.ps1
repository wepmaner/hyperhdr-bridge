# Сборка релизных бинарников в bin\.
# -H=windowsgui — приложение без консольного окна: только трей и веб-панель.
# Логи в этом режиме идут в bin\bridge.log (пункт трея «Открыть лог»).
$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
New-Item -ItemType Directory -Force (Join-Path $root 'bin') | Out-Null

go build -ldflags '-H=windowsgui -s -w' -o (Join-Path $root 'bin\bridge.exe') ./cmd/bridge
if (-not $?) { throw 'сборка bridge не удалась' }
# rpcprobe — диагностика, ей консоль нужна.
go build -o (Join-Path $root 'bin\rpcprobe.exe') ./cmd/rpcprobe
if (-not $?) { throw 'сборка rpcprobe не удалась' }

if (-not (Test-Path (Join-Path $root 'bin\config.yaml'))) {
  Copy-Item (Join-Path $root 'config.yaml') (Join-Path $root 'bin\config.yaml')
}
Get-ChildItem (Join-Path $root 'bin') -Filter *.exe | Select-Object Name, Length, LastWriteTime
