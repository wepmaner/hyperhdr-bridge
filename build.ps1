# Сборка релизных бинарников в bin\.
# Сначала собирается веб-панель (web\ -> internal\web\static): её встраивает в exe go:embed.
#   .\build.ps1 -SkipWeb         — не пересобирать панель (нет Node или правился только Go)
# -H=windowsgui — приложение без консольного окна: только трей и веб-панель.
# Логи в этом режиме идут в bin\bridge.log (пункт трея «Открыть лог»).
#   .\build.ps1                  — локальная сборка, версия "dev"
#   .\build.ps1 -Version 0.2.0   — релизная сборка с версией
param([string]$Version = 'dev', [switch]$SkipWeb)
$ErrorActionPreference = 'Stop'
$root = $PSScriptRoot
New-Item -ItemType Directory -Force (Join-Path $root 'bin') | Out-Null
$Version = $Version.TrimStart('v')

if (-not $SkipWeb) {
  Push-Location (Join-Path $root 'web')
  try {
    npm ci
    if (-not $?) { throw 'npm ci не удался' }
    npm run build
    if (-not $?) { throw 'сборка панели не удалась' }
  } finally { Pop-Location }
}

go build -ldflags "-H=windowsgui -s -w -X main.version=$Version" -o (Join-Path $root 'bin\bridge.exe') ./cmd/bridge
if (-not $?) { throw 'сборка bridge не удалась' }
# rpcprobe — диагностика, ей консоль нужна. -s -w убирает отладочную информацию.
go build -ldflags '-s -w' -o (Join-Path $root 'bin\rpcprobe.exe') ./cmd/rpcprobe
if (-not $?) { throw 'сборка rpcprobe не удалась' }
# gsiprobe — диагностика CS2: печатает поток Game State Integration.
go build -ldflags '-s -w' -o (Join-Path $root 'bin\gsiprobe.exe') ./cmd/gsiprobe
if (-not $?) { throw 'сборка gsiprobe не удалась' }

if (-not (Test-Path (Join-Path $root 'bin\config.yaml'))) {
  Copy-Item (Join-Path $root 'config.yaml') (Join-Path $root 'bin\config.yaml')
}
Get-ChildItem (Join-Path $root 'bin') -Filter *.exe | Select-Object Name, Length, LastWriteTime
