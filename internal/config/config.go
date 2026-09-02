// Package config загружает и сохраняет настройки моста.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config — корневая структура настроек (config.yaml).
type Config struct {
	Discord  Discord  `yaml:"discord"`
	HyperHDR HyperHDR `yaml:"hyperhdr"`
	Web      Web      `yaml:"web"`
	Rules    Rules    `yaml:"rules"`
	LogLevel string   `yaml:"log_level"` // debug|info|warn|error
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
	TokenFile string `yaml:"token_file"`
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
	Enabled bool `yaml:"enabled"`
	// Order — порядок проверки состояний, первое совпавшее выигрывает.
	Order []string `yaml:"order"`
	// States — действие для каждого состояния (ключи см. state.Kind).
	States map[string]Action `yaml:"states"`
	// Idle — что делать, когда ни одно состояние не активно.
	Idle Action `yaml:"idle"`
	// Debounce — минимальный интервал между командами, мс.
	DebounceMS int `yaml:"debounce_ms"`
}

// Action — что отправить в HyperHDR.
type Action struct {
	// Type: color | effect | clear | none
	Type string `yaml:"type"`
	// Color в формате [R,G,B] для type=color.
	Color []int `yaml:"color,omitempty"`
	// Effect — имя эффекта HyperHDR для type=effect.
	Effect string `yaml:"effect,omitempty"`
	// DurationMS — 0 = бессрочно (до следующей команды).
	DurationMS int `yaml:"duration_ms,omitempty"`
	// Brightness 0..100, применяется как множитель к цвету. 0 = не менять.
	Brightness int `yaml:"brightness,omitempty"`
	// BlinkCount — сколько раз мигнуть цветом перед тем, как зафиксировать его.
	// 0 = не мигать, сразу зажечь. В паузах между вспышками приоритет
	// снимается, поэтому видно обычную работу HyperHDR (захват экрана).
	BlinkCount int `yaml:"blink_count,omitempty"`
	// BlinkOnMS / BlinkOffMS — длительность вспышки и паузы, мс (0 = по умолчанию).
	BlinkOnMS  int `yaml:"blink_on_ms,omitempty"`
	BlinkOffMS int `yaml:"blink_off_ms,omitempty"`
	// Hold — оставить цвет гореть после миганий. nil = true (держим).
	// false — только мигнуть и вернуть подсветку обычному источнику.
	Hold *bool `yaml:"hold,omitempty"`
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
		Web: Web{Enabled: true, Addr: "127.0.0.1:7788"},
		Rules: Rules{
			Enabled:    true,
			DebounceMS: 120,
			Order:      []string{"deafened", "muted", "connected"},
			States: map[string]Action{
				"deafened": {Type: "color", Color: []int{255, 0, 0}, BlinkCount: 1},
				"muted":    {Type: "color", Color: []int{255, 140, 0}, BlinkCount: 1},
				// В канале, но без мута — не мешаем HyperHDR делать своё дело.
				"connected": {Type: "clear"},
			},
			Idle: Action{Type: "clear"},
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
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("разбор %s: %w", path, err)
	}
	return cfg, cfg.Validate()
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
	return nil
}
