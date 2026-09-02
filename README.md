# discord-hyperhdr-bridge

Трей-приложение на Go: следит за состоянием пользователя в Discord
(мут, глушение, речь, вход/выход из голосового канала) и меняет цвет подсветки
через JSON-API HyperHDR. Управление — иконка в трее + локальная веб-панель.

Статус: **работает end-to-end**. Discord → состояние → правило → цвет на ленте,
трей и веб-панель на месте. Проверено на живой связке Discord + HyperHDR.

## Структура

```
cmd/bridge/main.go            точка входа: конфиг, логгер, запуск app/web/tray
cmd/rpcprobe/main.go          диагностика Discord без HyperHDR: печатает состояния
internal/config/              config.yaml: загрузка, значения по умолчанию, валидация
internal/discord/
  protocol.go                 кадры IPC, опкоды, константы команд и событий
  transport_windows.go        named pipe \\.\pipe\discord-ipc-N (go-winio)
  transport_unix.go           unix-сокет discord-ipc-N
  client.go                   handshake, READY, nonce-RPC, подписки, реконнект
  oauth.go                    AUTHORIZE → token, refresh, кэш token.json
  events.go                   типы полезной нагрузки событий
internal/state/               потокобезопасный Snapshot + подписки
internal/rules/               Snapshot → действие HyperHDR, приоритет + debounce
internal/hyperhdr/            TCP JSON-клиент (19444): color / effect / clear
internal/web/                 HTTP-панель на 127.0.0.1, SSE-поток состояния
internal/tray/                иконка в трее (fyne.io/systray); ICO собирается
                              из assets/icon.png + цветная точка состояния
internal/autostart/           автозапуск при входе: запись в HKCU\...\Run
internal/app/                 связывание всего вместе; events.go — RPC → Snapshot
configs/config.example.yaml   пример конфигурации
docs/discord-integration.md   ЧТО НУЖНО ДЛЯ ИНТЕГРАЦИИ С DISCORD ← читать первым
```

## Поток данных

```
Discord Desktop
   │  named pipe / unix socket (IPC, кадры opcode+len+json)
   ▼
discord.Client ── Event ──► app.handleEvent ──► state.Store (Snapshot)
                                                    │ подписки
                                    ┌───────────────┼───────────────┐
                                    ▼               ▼               ▼
                              rules.Engine     web (SSE)        tray
                                    │
                                    ▼ TCP 19444 JSON
                              hyperhdr.Client ──► HyperHDR ──► лента
```

## Правила по умолчанию

Первое совпавшее состояние выигрывает (`rules.order`):

| Состояние | Что делает подсветка |
|---|---|
| `deafened` — звук выключен | красный `255,0,0`: мигнуть 1 раз и гореть, пока состояние активно |
| `muted` — микрофон выключен (self или server) | оранжевый `255,140,0`: мигнуть 1 раз и гореть, пока состояние активно |
| `connected` — просто в канале | `clear` — HyperHDR работает как обычно (захват экрана) |
| `idle` — не в канале | `clear` (вернуть обычный источник) |

Подсветку мост занимает только на время мута/глушения. Мигание настраивается
на любом правиле `type: color`:

```yaml
muted:
  type: color
  color: [255, 140, 0]
  blink_count: 1      # 0 = не мигать, сразу зажечь
  blink_on_ms: 220    # длительность вспышки
  blink_off_ms: 180   # пауза, в неё видно обычный захват экрана
  # hold: false       # только мигнуть и сразу вернуть подсветку HyperHDR
```

В паузах между вспышками и после `clear` приоритет снимается, поэтому сквозь
анимацию видно обычную работу HyperHDR. Смена состояния посреди мигания
прерывает анимацию: новое правило применяется сразу.

Речь (`speaking`) сознательно не используется: Discord шлёт `SPEAKING_START/STOP`
на каждой паузе в фразе, и подсветка от этого мигает. Состояние по-прежнему
читается и видно в `/api/state`, но правила на него не навешаны.

Мост занимает один приоритет HyperHDR (`hyperhdr.priority: 50`) и снимает его
при выходе — обычный захват экрана продолжает работать под ним.

## Что нужно сделать, чтобы это поехало

1. Go 1.23+ (проверено на 1.27) и `go mod tidy` — зависимости подтянутся сами.
2. Заполнить `config.yaml`: `client_secret` из Developer Portal → OAuth2
   (подробности и остальные шаги — `docs/discord-integration.md`).
3. Проверить, что Discord виден:
   ```
   go run ./cmd/rpcprobe -config config.yaml
   ```
   При первом запуске в клиенте Discord появится окно согласия.
4. В HyperHDR: Settings → General → **JSON server** включён, порт 19444;
   если включена авторизация — создать токен и вписать в `hyperhdr.token`.
5. Сборка:
   ```
   .\build.ps1                                # релиз: без консоли, в bin\
   go build -o bin/bridge.exe ./cmd/bridge    # с консолью (для отладки)
   ```
   `build.ps1` собирает с `-ldflags "-H=windowsgui -s -w"`: окна консоли нет,
   приложение живёт в трее. Логи в этом режиме идут в `bin\bridge.log` — пункт
   трея «Открыть лог»; путь переопределяется флагом `-log`, файл начинается
   заново, когда перевалит за 1 МБ.
   Отладочный запуск без трея: `bin\bridge.exe -no-tray -config config.yaml`

## Автозапуск при входе в Windows

Переключатель есть в двух местах: в веб-панели (раздел «Настройки») и в меню трея
(«Запускать при входе в систему»). Оба пишут одно и то же значение
`DiscordHyperHDRBridge` в `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` — права
администратора не нужны, автозапуск действует только для текущего пользователя.

В реестр попадает полный путь к exe и абсолютный путь к конфигу
(`"...\bin\bridge.exe" -config "...\bin\config.yaml"`), потому что автозапуск стартует
с произвольным рабочим каталогом. Если exe переехал, панель отметит запись как чужую
и предложит переключить тумблер туда-обратно, чтобы обновить путь.

API: `GET /api/autostart` — состояние, `POST /api/autostart {"enabled":true}` —
переключить. То же состояние приходит полем `autostart` в `GET /api/status`.

## Что уже сделано в слое Discord

- named pipe / unix-сокет, перебор `discord-ipc-0..9`, реконнект с backoff;
- HANDSHAKE → ожидание `READY` → OAuth2 (`AUTHORIZE` + обмен на токен, refresh,
  кэш `token.json`, перевыпуск при отказе) → `AUTHENTICATE`;
- подписки на `VOICE_SETTINGS_UPDATE`, `VOICE_CHANNEL_SELECT`,
  `VOICE_CONNECTION_STATUS`, а также `SPEAKING_START/STOP` и `VOICE_STATE_UPDATE`
  с переподпиской при смене канала;
- начальный снимок (`GET_VOICE_SETTINGS`, `GET_SELECTED_VOICE_CHANNEL`) — состояние
  известно сразу при старте, а не после первого события;
- фильтрация чужих событий по своему `user_id`, ответы на `PING`.

## Оставшиеся TODO по коду

- `web/static/index.html` — редактор правил (цвет на состояние), «примерить цвет»,
  индикатор связи с HyperHDR.
- `web/server.go` — проверка `RemoteAddr`/`Origin` (защита от CSRF из браузера).
- Плавные переходы цвета (fade) — по желанию.
- Автозапуск для macOS/Linux: сейчас `internal/autostart` умеет только Windows,
  панель и трей в остальных ОС просто прячут переключатель.

## Что осознанно НЕ делается

- Бот на сервере Discord: локальный мут ботам не виден (см. docs).
- Работа с браузерной версией Discord: у неё нет IPC-сокета.
