# Интеграция с Discord: что нужно и как это работает

## 1. Главное ограничение

Discord **не даёт боту** узнать, что вы нажали «мут» у себя локально.
Bot Gateway видит только серверный мут (`GUILD_VOICE_STATES` → `VOICE_STATE_UPDATE`)
и то, только когда вы находитесь в голосовом канале гильдии.
Self-mute там тоже есть, но событие приходит с задержкой и только для гильдий,
где ваш бот присутствует.

Поэтому мост использует **локальный Discord RPC (IPC)** — интерфейс, который
десктопный клиент Discord открывает на машине пользователя. Именно так работают
оверлеи, Stream Deck и подобные утилиты.

Следствия:
- нужен **запущенный десктопный клиент Discord** (браузерная версия не подходит);
- всё работает локально, токен бота не нужен, бот на сервер не добавляется;
- права ограничены OAuth2-скоупами, которые пользователь подтверждает один раз.

## 2. Настройка приложения в Developer Portal

Приложение уже создано: **Client ID `1544770992914563253`**.

Что нужно доделать на https://discord.com/developers/applications:

1. **OAuth2 → Client Secret** — сгенерировать и вписать в `config.yaml`
   (`discord.client_secret`). Секрет нужен для обмена `code` → `access_token`.
2. **OAuth2 → Redirects** — добавить URI *точно* в том виде, в каком он попадёт
   в запрос: `http://localhost:7788/oauth/callback`.
   При RPC-флоу редирект физически не открывается, но Discord сверяет строку.
3. **Скоупы**: `rpc` и `rpc.voice.read`.
   Дополнительно по желанию: `identify` (имя/аватар в панели),
   `rpc.notifications.read` (реакция на пинги/упоминания),
   `rpc.activities.write` (менять свой статус — нам не нужно).
4. **Whitelist RPC.** Скоуп `rpc` приватный: по умолчанию его могут использовать
   только владелец приложения и участники его команды. Для личного использования
   этого достаточно — вы владелец. Если приложением будет пользоваться кто-то
   ещё, каждого нужно добавить в allowlist приложения (вкладка **App Testers**
   / RPC allowlist) либо подавать заявку на публичный доступ к RPC.
5. Бот-пользователь (вкладка Bot) и Privileged Intents **не нужны**.

## 3. Транспорт

| ОС | Путь |
|---|---|
| Windows | `\.\pipe\discord-ipc-0` … `discord-ipc-9` |
| Linux | `$XDG_RUNTIME_DIR/discord-ipc-N`, плюс `.../app/com.discordapp.Discord/`, `snap.discord/` |
| macOS | `$TMPDIR/discord-ipc-N` |

Перебираем индексы 0..9 — их несколько, если открыто несколько клиентов
(stable / PTB / canary).

Формат кадра — little-endian:

```
[opcode uint32][length uint32][json payload length байт]
```

Опкоды: `0 HANDSHAKE`, `1 FRAME`, `2 CLOSE`, `3 PING`, `4 PONG`.
На `PING` обязательно отвечать `PONG` тем же телом, иначе клиент рвёт соединение.

**На Windows named pipe нельзя открыть через `net.Dial`** — используется
[`github.com/Microsoft/go-winio`](https://github.com/Microsoft/go-winio):
`winio.DialPipe(path, &timeout)` (реализовано в `transport_windows.go`).

## 4. Последовательность подключения

```
1. dial IPC-сокет
2. → op=0 HANDSHAKE {"v":1,"client_id":"1544770992914563253"}
3. ← evt=READY {user:{id,username}, config:{...}}         # запоминаем свой user_id
4. если нет валидного токена:
   → cmd=AUTHORIZE {client_id, scopes:["rpc","rpc.voice.read"]}
     (в клиенте Discord всплывает окно согласия; пользователь жмёт «Authorize»)
   ← {code:"..."}
   → POST https://discord.com/api/v10/oauth2/token
     grant_type=authorization_code, code, redirect_uri
     Basic auth: client_id:client_secret
   ← {access_token, refresh_token, expires_in}            # кэшируем в token.json
5. → cmd=AUTHENTICATE {access_token}
   ← {application, user, scopes, expires}
6. → cmd=SUBSCRIBE evt=VOICE_SETTINGS_UPDATE
   → cmd=SUBSCRIBE evt=VOICE_CHANNEL_SELECT
   → cmd=SUBSCRIBE evt=VOICE_CONNECTION_STATUS
7. → cmd=GET_VOICE_SETTINGS            # начальный снимок, иначе до первого
   → cmd=GET_SELECTED_VOICE_CHANNEL    # события состояние неизвестно
8. при входе в канал (VOICE_CHANNEL_SELECT с channel_id != null):
   → SUBSCRIBE evt=SPEAKING_START      args={channel_id}
   → SUBSCRIBE evt=SPEAKING_STOP       args={channel_id}
   → SUBSCRIBE evt=VOICE_STATE_UPDATE  args={channel_id}
   при выходе — UNSUBSCRIBE тех же событий со старым channel_id
```

Каждой команде присваивается `nonce`; ответ приходит с тем же `nonce`.
Ошибка приходит как `evt: "ERROR"` с `data.code` / `data.message`.

## 5. Какие события за какое состояние отвечают

| Событие | Что даёт | Поле в `state.Snapshot` |
|---|---|---|
| `VOICE_SETTINGS_UPDATE` | локальные мут/глушение (`mute`, `deaf`), устройства, режим PTT | `SelfMute`, `SelfDeaf` |
| `VOICE_STATE_UPDATE` (args channel_id) | состояние участника: `voice_state.mute/deaf` — **серверные**, `self_mute/self_deaf` — локальные, `suppress` — «слушатель» в трибуне | `ServerMute`, `ServerDeaf` |
| `SPEAKING_START` / `SPEAKING_STOP` | кто говорит прямо сейчас (`user_id`) | `Speaking` |
| `VOICE_CHANNEL_SELECT` | вход/выход/переход между каналами (`channel_id == null` — вышел) | `Connected`, `ChannelID`, `GuildID` |
| `VOICE_CONNECTION_STATUS` | качество связи: `CONNECTING`, `VOICE_CONNECTED`, `NO_ROUTE`, ping | диагностика, можно мигать при обрыве |
| `NOTIFICATION_CREATE` | входящие уведомления/упоминания (нужен `rpc.notifications.read`) | будущее: вспышка при пинге |

Важно фильтровать события по своему `user_id` из `READY`: `VOICE_STATE_UPDATE`
и `SPEAKING_*` приходят для **всех** участников канала.

Кнопка мута в клиенте меняет и `VOICE_SETTINGS_UPDATE`, и `VOICE_STATE_UPDATE` —
поэтому self/server состояния хранятся раздельно, а правило `muted` срабатывает
на «или».

## 6. Токен и его жизненный цикл

- `access_token` живёт ~7 дней, обновляется через `grant_type=refresh_token`.
- Кэш — `token.json` рядом с бинарником, права `0600`, добавлен в `.gitignore`.
- При `401`/протухшем refresh — снова показываем окно `AUTHORIZE`.
- `client_secret` лежит в `config.yaml`. Для «взрослого» варианта — Windows DPAPI
  или Credential Manager (TODO).

## 7. Крайние случаи, которые надо обработать

- **Discord не запущен / перезапустился** — dial падает, `Client.Run` уходит в
  переподключение с экспоненциальной задержкой (уже реализовано).
- **Discord обновился** — пайп пересоздаётся, соединение рвётся; лечится тем же
  реконнектом.
- **Гонка HANDSHAKE → AUTHENTICATE**: команды нельзя слать до `READY`.
  Реализовано: `session()` блокируется на канале `ready` (таймаут 20 c).
- **Пользователь нажал «Cancel»** в окне согласия → `ERROR` code 4006/5000;
  показать это в трее, а не молча падать.
- **Переход между каналами** без выхода: приходит новый `VOICE_CHANNEL_SELECT`,
  старые подписки надо снять, иначе ловим события чужого канала. Реализовано в
  `syncChannelSubs`; обработка идёт одним воркером, чтобы не переставлять события местами.
- **Push-to-talk**: `SPEAKING_START` приходит только пока клавиша зажата — правило
  `speaking` даст мигание; для PTT лучше отдельный debounce (в конфиге есть).

## 8. Отладка

- Проверить, что пайп существует (PowerShell):
  `Get-ChildItem \.\pipe\ | Where-Object Name -like 'discord-ipc*'`
- `log_level: debug` в конфиге печатает каждый кадр RPC.
- Полезная референс-реализация протокола: `discord-rpc` (C++, архив),
  документация — https://discord.com/developers/docs/topics/rpc

## 9. Если AUTHORIZE отвечает ошибкой доступа

Скоуп `rpc` приватный. Владелец приложения и участники его команды получают
окно согласия сразу. Для всех остальных Discord вернёт `ERROR` с
`code: 4006` / сообщением про scope. Варианты:

1. Добавить пользователя в **App Testers / RPC allowlist** приложения — рабочий
   путь для «своих».
2. Флоу с `rpc_token` (для приложений, прошедших ревью RPC):
   ```
   POST /oauth2/token  grant_type=client_credentials&scope=rpc.api
   → {access_token}
   POST /oauth2/rpc/token  (Bearer access_token)
   → {rpc_token}
   → cmd=AUTHORIZE {client_id, scopes, rpc_token, username}
   ```
   В коде этот путь не реализован — для личного использования он не нужен.

## 10. Как проверить, что состояния видны

```
go run ./cmd/rpcprobe -config config.yaml      # -v печатает каждый кадр RPC
```

Утилита не трогает HyperHDR: она подключается к Discord, авторизуется и печатает
строку при каждом изменении состояния. Ожидаемый вывод при заходе в канал,
нажатии мута и речи:

```
19:41:02.118  канал=General    говорит=нет  мут=нет  глушение=нет  (self m/d=false/false, server m/d=false/false)
19:41:07.402  канал=General    говорит=нет  мут=да   глушение=нет  (self m/d=true/false,  server m/d=false/false)
19:41:11.930  канал=General    говорит=да   мут=нет  глушение=нет  (self m/d=false/false, server m/d=false/false)
```

Первый запуск покажет в клиенте Discord окно согласия — это `AUTHORIZE`.
Дальше используется `token.json`, окно больше не появляется.

## 11. Что уже проверено на этой машине

Прогон `bin\rpcprobe.exe -config config.yaml` (Go 1.27, Windows 11, Discord запущен):

```
INFO msg="подключено к Discord IPC" socket=\\.\pipe\discord-ipc-0
INFO msg="Discord READY" user=ruslan0735 id=399177627534884865
ERROR msg="Discord: нужна настройка" err="...заполните discord.client_secret в конфиге"
```

То есть транспорт, HANDSHAKE и READY работают, `client_id` Discord принимает.
Единственный оставшийся шаг — **Client Secret**: без него нельзя обменять `code`
из `AUTHORIZE` на access token, а без токена `AUTHENTICATE` не пройдёт и события
не поедут.

Что сделать (2 минуты, в браузере):

1. https://discord.com/developers/applications → приложение `1544770992914563253`
   → **OAuth2** → *Client Secret* → **Reset Secret** → скопировать.
2. Там же **Redirects** → добавить `http://localhost:7788/oauth/callback` → Save.
3. Вписать секрет в `config.yaml`:
   ```yaml
   discord:
     client_secret: "<вставить сюда>"
   ```
4. Запустить `bin\rpcprobe.exe` — в клиенте Discord появится окно согласия,
   после подтверждения пойдут строки состояния.

Секрет остаётся локально: `config.yaml` и `token.json` в `.gitignore`.
