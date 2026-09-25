// Package config загружает и сохраняет настройки моста.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"
)

// Config — корневая структура настроек (config.yaml).
type Config struct {
	Discord  Discord  `yaml:"discord"`
	HyperHDR HyperHDR `yaml:"hyperhdr"`
	CS2      CS2      `yaml:"cs2"`
	Web      Web      `yaml:"web"`
	Rules    Rules    `yaml:"rules"`
	LogLevel string   `yaml:"log_level"` // debug|info|warn|error
}

// CS2 — интеграция с Counter-Strike 2 через Game State Integration.
//
// GSI — штатный механизм Valve: игре кладут cfg-файл, и она сама шлёт HTTP POST
// с полным снимком своего состояния. Процесс игры мы не трогаем вообще.
type CS2 struct {
	Enabled bool `yaml:"enabled"`
	// Addr — где слушаем POST от игры. Только localhost: адрес попадёт в cfg,
	// который читает игра на этой же машине.
	Addr string `yaml:"addr"`
	// Token — общий секрет, который игра шлёт в поле auth.token. Защищает от
	// постороннего локального процесса, притворяющегося игрой. Генерируется
	// при установке cfg.
	Token string `yaml:"token"`
	// CfgPath — каталог cfg игры. Пусто = искать Steam автоматически.
	CfgPath string `yaml:"cfg_path"`
	// IdleTimeoutS — сколько ждать данных, прежде чем считать игру закрытой.
	// Без этого лента залипнет на последнем игровом цвете навсегда.
	IdleTimeoutS int `yaml:"idle_timeout_s"`
	// OnlyWhenLive — засчитывать смерть только в живом раунде. На разминке
	// с респавнами иначе получается непрерывное мигание.
	OnlyWhenLive bool `yaml:"only_when_live"`
	// DeathHoldMS — сколько держится признак смерти. Не «до респавна»: иначе,
	// умерев при заложенной бомбе, игрок отмигает красным и следующие 40 секунд
	// смотрит на погашенную ленту вместо тикающей бомбы.
	DeathHoldMS int `yaml:"death_hold_ms"`
	// FlashThreshold — с какого значения flashed считаем, что ослепило.
	FlashThreshold int `yaml:"flash_threshold"`
	// BombFuseS — сколько тикает бомба. Записью подтверждено, что таймер
	// bomb.countdown игроку не приходит, поэтому отсчёт мост ведёт сам, и это
	// число обязано быть верным. 40 секунд — обычные режимы.
	BombFuseS int `yaml:"bomb_fuse_s"`
	// BombHurryS / BombPanicS — границы фаз ускорения пульса бомбы, секунды
	// до взрыва. Должно выполняться BombPanicS < BombHurryS < BombFuseS.
	BombHurryS int `yaml:"bomb_hurry_s"`
	BombPanicS int `yaml:"bomb_panic_s"`
	// BombExplodedHoldMS — сколько держать цвет взрыва. Взрыв игра тоже не
	// сообщает: его вычисляет детектор, когда фитиль догорел до конца.
	BombExplodedHoldMS int `yaml:"bomb_exploded_hold_ms"`
}

// Discord — параметры OAuth2/RPC приложения Discord.
type Discord struct {
	// ClientID приложения из Developer Portal.
	ClientID string `yaml:"client_id"`
	// ClientSecret нужен для обмена authorization code на access token.
	ClientSecret string `yaml:"client_secret"`
	// RedirectURI обязан совпадать с одним из зарегистрированных в портале.
	RedirectURI string `yaml:"redirect_uri"`
	// Scopes для AUTHORIZE. Минимум: rpc, rpc.voice.read.
	Scopes []string `yaml:"scopes"`
	// TokenFile — куда кэшировать access/refresh token.
	// Относительный путь считается от каталога config.yaml (см. Load).
	TokenFile string `yaml:"token_file"`
	// TokenPath — TokenFile, приведённый к абсолютному пути. Заполняется в Load
	// и не пишется на диск: в конфиге должен остаться путь как его задал человек.
	// Нужен потому, что при автозапуске рабочий каталог — не каталог программы,
	// и относительный token.json уходит в System32, где записать его нельзя.
	TokenPath string `yaml:"-" json:"-"`
	// PipeRange — сколько IPC-сокетов перебирать (discord-ipc-0..N).
	PipeRange int `yaml:"pipe_range"`
}

// HyperHDR — как достучаться до HyperHDR.
type HyperHDR struct {
	Host string `yaml:"host"`
	// JSONPort — «сырой» JSON-сервер HyperHDR (по умолчанию 19444).
	JSONPort int `yaml:"json_port"`
	// Token — API-токен, если в HyperHDR включена авторизация.
	Token string `yaml:"token"`
	// Priority — приоритет наших команд (меньше число = выше приоритет).
	Priority int `yaml:"priority"`
	// Origin — как мост подписывается в списке источников HyperHDR.
	Origin string `yaml:"origin"`
	// RestoreOnExit — снимать наш приоритет при выходе.
	RestoreOnExit bool `yaml:"restore_on_exit"`
}

// Web — локальная панель управления.
type Web struct {
	Enabled bool   `yaml:"enabled"`
	Addr    string `yaml:"addr"` // например 127.0.0.1:7788
	// OpenOnStart — открывать браузер при запуске.
	OpenOnStart bool `yaml:"open_on_start"`
}

// Rules — сопоставление состояний Discord и действий подсветки.
type Rules struct {
	// Enabled — общий выключатель реакции на события.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Order — порядок проверки состояний, первое совпавшее выигрывает.
	Order []string `yaml:"order" json:"order"`
	// States — действие для каждого состояния (ключи см. state.Kind).
	States map[string]Action `yaml:"states" json:"states"`
	// Idle — что делать, когда ни одно состояние не активно.
	Idle Action `yaml:"idle" json:"idle"`
	// Debounce — минимальный интервал между командами, мс.
	DebounceMS int `yaml:"debounce_ms" json:"debounce_ms"`
}

// Action — что отправить в HyperHDR.
type Action struct {
	// Enabled — выключатель события из панели. nil = включено: иначе старые
	// конфиги, где поля нет, перестали бы работать после обновления моста.
	// Выключенное состояние пропускается, и ход переходит следующему в Order.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled"`
	// Type: color | effect | clear | none
	Type string `yaml:"type" json:"type"`
	// Color в формате [R,G,B] для type=color.
	Color []int `yaml:"color,omitempty" json:"color,omitempty"`
	// Effect — имя эффекта HyperHDR для type=effect.
	Effect string `yaml:"effect,omitempty" json:"effect,omitempty"`
	// DurationMS — 0 = бессрочно (до следующей команды).
	DurationMS int `yaml:"duration_ms,omitempty" json:"duration_ms,omitempty"`
	// Brightness 0..100, применяется как множитель к цвету. 0 = не менять.
	Brightness int `yaml:"brightness,omitempty" json:"brightness,omitempty"`
	// BlinkCount — сколько раз мигнуть цветом перед тем, как зафиксировать его.
	// 0 = не мигать, сразу зажечь. В паузах между вспышками приоритет
	// снимается, поэтому видно обычную работу HyperHDR (захват экрана).
	BlinkCount int `yaml:"blink_count,omitempty" json:"blink_count,omitempty"`
	// BlinkOnMS / BlinkOffMS — длительность вспышки и паузы, мс (0 = по умолчанию).
	BlinkOnMS  int `yaml:"blink_on_ms,omitempty" json:"blink_on_ms,omitempty"`
	BlinkOffMS int `yaml:"blink_off_ms,omitempty" json:"blink_off_ms,omitempty"`
	// Hold — оставить цвет гореть после миганий. nil = true (держим).
	// false — только мигнуть и вернуть подсветку обычному источнику.
	Hold *bool `yaml:"hold,omitempty" json:"hold,omitempty"`
	// Loop — мигать без остановки, пока состояние активно. Нужно тикающей
	// бомбе: BlinkCount там неизвестен заранее, анимация живёт до смены
	// состояния и обрывается отменой контекста.
	Loop bool `yaml:"loop,omitempty" json:"loop,omitempty"`
}

// On сообщает, включено ли правило. Отсутствующий Enabled означает «включено».
func (a Action) On() bool { return a.Enabled == nil || *a.Enabled }

func boolPtr(b bool) *bool { return &b }

// cs2RuleOrder — события CS2 в порядке приоритета. Стоят выше состояний
// Discord: пока идёт матч, человек хочет видеть игру, а мут и глушение —
// в паузах, когда игра ничего не шлёт.
var cs2RuleOrder = []string{
	"cs2_dead", "cs2_bomb_exploded", "cs2_flashed",
	"cs2_bomb_panic", "cs2_bomb_hurry", "cs2_bomb",
}

// cs2Rules возвращает правила CS2 по умолчанию.
//
// Фазы бомбы — отдельные правила, а не параметр одного: движок правил не знает
// про CS2 и просто берёт первое совпавшее имя из Order, а смена фазы меняет
// ключ действия и сама перезапускает цикл с новым периодом.
func cs2Rules() map[string]Action {
	const on, off = 255, 90
	pulse := func(ms int) Action {
		return Action{
			Type: "color", Color: []int{on, off, 0},
			Loop: true, BlinkOnMS: ms, BlinkOffMS: ms,
		}
	}
	return map[string]Action{
		// Смерть: два красных мигания и сразу вернуть ленту HyperHDR.
		//
		// 500 мс на вспышку — не на глаз, а по замеру. Пульс бомбы длиной
		// 512 мс человек замечает уверенно, вспышки в 250 мс пропускает:
		// смерть случается в момент, когда взгляд в игре, а экран резко
		// меняется на камеру убийцы. Поэтому вспышка смерти равняется на
		// длительность, которая заведомо читается.
		//
		// Полная анимация 2 x (500 + 250) = 1500 мс, поэтому DeathHoldMS
		// не меньше 1800 — иначе смена состояния оборвёт мигание.
		"cs2_dead": {
			Type: "color", Color: []int{255, 0, 0},
			BlinkCount: 2, BlinkOnMS: 500, BlinkOffMS: 250, Hold: boolPtr(false),
		},
		// Флешка выключена по умолчанию: в записанной сессии поймать её не
		// удалось, порог не проверен, а белый экран HyperHDR и так отдаёт
		// через обычный захват.
		"cs2_flashed": {
			Type: "color", Color: []int{255, 255, 255}, Enabled: boolPtr(false),
		},
		// Взрыв: ровный цвет без мигания на пару секунд. Мигание здесь было бы
		// неуместно — пульс означал «бомба тикает», а тикать уже нечему.
		"cs2_bomb_exploded": {Type: "color", Color: []int{255, 60, 0}},
		"cs2_bomb":          pulse(500),
		"cs2_bomb_hurry":    pulse(250),
		"cs2_bomb_panic":    pulse(90),
	}
}

// ensureCS2Rules добавляет недостающие правила CS2 в уже существующий конфиг.
//
// Нужно потому, что в YAML список заменяется целиком: rules.order из конфига,
// написанного до появления интеграции, вытеснит события CS2, и они перестанут
// срабатывать без единой ошибки в логе. Уже настроенные пользователем правила
// не трогаем — выключение делается флагом enabled, а не удалением из order.
func (c *Config) ensureCS2Rules() {
	if c.Rules.States == nil {
		c.Rules.States = map[string]Action{}
	}
	defaults := cs2Rules()
	for _, name := range cs2RuleOrder {
		if _, ok := c.Rules.States[name]; !ok {
			c.Rules.States[name] = defaults[name]
		}
	}
	// Недостающее имя встаёт сразу после ближайшего предыдущего события CS2,
	// которое в списке уже есть. Так новое событие занимает своё место в
	// каноническом порядке, а расстановка, сделанная человеком, не ломается.
	// Простое добавление в начало списка сделало бы каждое новое событие
	// самым приоритетным — взрыв перебивал бы мигание смерти.
	for i, name := range cs2RuleOrder {
		if slices.Contains(c.Rules.Order, name) {
			continue
		}
		pos := 0
		for j := i - 1; j >= 0; j-- {
			if at := slices.Index(c.Rules.Order, cs2RuleOrder[j]); at >= 0 {
				pos = at + 1
				break
			}
		}
		c.Rules.Order = slices.Insert(c.Rules.Order, pos, name)
	}
}

// Default возвращает конфиг со значениями по умолчанию.
func Default() *Config {
	return &Config{
		LogLevel: "info",
		Discord: Discord{
			ClientID:    "1544770992914563253",
			RedirectURI: "http://localhost:7788/oauth/callback",
			Scopes:      []string{"rpc", "rpc.voice.read"},
			TokenFile:   "token.json",
			PipeRange:   10,
		},
		HyperHDR: HyperHDR{
			Host:          "127.0.0.1",
			JSONPort:      19444,
			Priority:      50,
			Origin:        "discord-bridge",
			RestoreOnExit: true,
		},
		CS2: CS2{
			Enabled:        true,
			Addr:           "127.0.0.1:7799",
			IdleTimeoutS:   30,
			OnlyWhenLive:   true,
			DeathHoldMS:    1800,
			FlashThreshold: 100,
			BombFuseS:      40,
			BombHurryS:     20,
			BombPanicS:     5,

			BombExplodedHoldMS: 2000,
		},
		Web: Web{Enabled: true, Addr: "127.0.0.1:7788"},
		Rules: Rules{
			Enabled:    true,
			DebounceMS: 120,
			Order:      append(slices.Clone(cs2RuleOrder), "deafened", "muted", "connected"),
			States:     defaultStates(),
			Idle:       Action{Type: "clear"},
		},
	}
}

// Load читает конфиг с диска; если файла нет — создаёт его со значениями по умолчанию.
func Load(path string) (*Config, error) {
	cfg := Default()
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if err := Save(path, cfg); err != nil {
			return nil, fmt.Errorf("создание конфига: %w", err)
		}
		cfg.resolvePaths(path)
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("разбор %s: %w", path, err)
	}
	cfg.ensureCS2Rules()
	cfg.resolvePaths(path)
	return cfg, cfg.Validate()
}

// resolvePaths привязывает относительные пути из конфига к каталогу самого
// конфига. Иначе они зависят от рабочего каталога процесса, а при автозапуске
// из HKCU\...\Run он равен C:\Windows\System32.
func (c *Config) resolvePaths(cfgPath string) {
	base := filepath.Dir(cfgPath)
	if abs, err := filepath.Abs(cfgPath); err == nil {
		base = filepath.Dir(abs)
	}
	c.Discord.TokenPath = c.Discord.TokenFile
	if c.Discord.TokenFile != "" && !filepath.IsAbs(c.Discord.TokenFile) {
		c.Discord.TokenPath = filepath.Join(base, c.Discord.TokenFile)
	}
}

// Save атомарно записывает конфиг.
func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Validate проверяет минимально необходимые поля.
func (c *Config) Validate() error {
	if c.Discord.ClientID == "" {
		return fmt.Errorf("discord.client_id не задан")
	}
	if c.HyperHDR.Host == "" {
		return fmt.Errorf("hyperhdr.host не задан")
	}
	if c.HyperHDR.Priority < 1 || c.HyperHDR.Priority > 254 {
		return fmt.Errorf("hyperhdr.priority должен быть 1..254")
	}
	if err := c.CS2.validate(); err != nil {
		return err
	}
	return c.validateDeathHold()
}

// validateDeathHold следит, чтобы признак смерти жил не меньше собственной
// анимации. Иначе состояние сменится посреди мигания, stopBlink оборвёт его,
// и вместо двух вспышек человек увидит одну с половиной.
func (c *Config) validateDeathHold() error {
	act, ok := c.Rules.States["cs2_dead"]
	if !ok || !c.CS2.Enabled || !act.On() || act.BlinkCount <= 0 {
		return nil
	}
	on, off := act.BlinkOnMS, act.BlinkOffMS
	if on <= 0 {
		on = 220 // те же значения по умолчанию, что в rules
	}
	if off <= 0 {
		off = 180
	}
	if need := act.BlinkCount * (on + off); c.CS2.DeathHoldMS < need {
		return fmt.Errorf(
			"cs2.death_hold_ms (%d) меньше длительности анимации cs2_dead (%d мс): "+
				"мигание оборвётся на середине", c.CS2.DeathHoldMS, need)
	}
	return nil
}

// validate проверяет секцию cs2. Выключенная интеграция не валидируется:
// её поля никого не касаются, и мост не должен из-за них не стартовать.
func (c CS2) validate() error {
	if !c.Enabled {
		return nil
	}
	if c.Addr == "" {
		return fmt.Errorf("cs2.addr не задан")
	}
	if c.FlashThreshold < 1 || c.FlashThreshold > 255 {
		return fmt.Errorf("cs2.flash_threshold должен быть 1..255")
	}
	// Фаза паники наступает позже фазы спешки. При обратном порядке паника
	// недостижима, и ускорение молча не работает.
	if c.BombPanicS >= c.BombHurryS {
		return fmt.Errorf("cs2.bomb_panic_s (%d) должен быть меньше cs2.bomb_hurry_s (%d)",
			c.BombPanicS, c.BombHurryS)
	}
	if c.BombHurryS >= c.BombFuseS {
		return fmt.Errorf("cs2.bomb_hurry_s (%d) должен быть меньше cs2.bomb_fuse_s (%d)",
			c.BombHurryS, c.BombFuseS)
	}
	if c.BombExplodedHoldMS < 1 {
		return fmt.Errorf("cs2.bomb_exploded_hold_ms должен быть положительным")
	}
	if c.IdleTimeoutS < 1 {
		return fmt.Errorf("cs2.idle_timeout_s должен быть положительным")
	}
	return nil
}

// defaultStates собирает правила по умолчанию: состояния Discord плюс события CS2.
func defaultStates() map[string]Action {
	states := map[string]Action{
		"deafened": {Type: "color", Color: []int{255, 0, 0}, BlinkCount: 1},
		"muted":    {Type: "color", Color: []int{255, 140, 0}, BlinkCount: 1},
		// В канале, но без мута — не мешаем HyperHDR делать своё дело.
		"connected": {Type: "clear"},
	}
	for name, act := range cs2Rules() {
		states[name] = act
	}
	return states
}
