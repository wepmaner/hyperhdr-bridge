package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Относительный token_file должен цепляться к каталогу конфига, а не к рабочему
// каталогу процесса: при автозапуске он равен System32.
func TestResolvePathsRelativeToConfigDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := Save(path, Default()); err != nil {
		t.Fatalf("сохранение конфига: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("загрузка конфига: %v", err)
	}
	want := filepath.Join(dir, "token.json")
	if cfg.Discord.TokenPath != want {
		t.Errorf("TokenPath = %q, ожидалось %q", cfg.Discord.TokenPath, want)
	}
	if cfg.Discord.TokenFile != "token.json" {
		t.Errorf("TokenFile изменился: %q", cfg.Discord.TokenFile)
	}
}

// Абсолютный token_file берём как есть.
func TestResolvePathsKeepsAbsolute(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "sub", "token.json")
	cfg := Default()
	cfg.Discord.TokenFile = abs
	cfg.resolvePaths(filepath.Join(dir, "config.yaml"))
	if cfg.Discord.TokenPath != abs {
		t.Errorf("TokenPath = %q, ожидалось %q", cfg.Discord.TokenPath, abs)
	}
}

// В YAML не должно попадать вычисленное поле — на диске остаётся token_file.
func TestSaveDoesNotWriteTokenPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := Default()
	cfg.resolvePaths(path)
	if err := Save(path, cfg); err != nil {
		t.Fatalf("сохранение конфига: %v", err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatalf("загрузка конфига: %v", err)
	}
	if again.Discord.TokenFile != "token.json" {
		t.Errorf("token_file на диске = %q", again.Discord.TokenFile)
	}
}

// Старый config.yaml без секции cs2 должен получить рабочие значения по
// умолчанию, а не нули: иначе после обновления моста интеграция окажется с
// пустым адресом слушателя и нулевыми порогами.
func TestLoadFillsCS2DefaultsForOldConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	old := "discord:\n  client_id: \"1\"\nhyperhdr:\n  host: 127.0.0.1\n  priority: 50\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatalf("подготовка конфига: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("загрузка конфига: %v", err)
	}
	if cfg.CS2.Addr != "127.0.0.1:7799" {
		t.Errorf("cs2.addr = %q, ожидался адрес по умолчанию", cfg.CS2.Addr)
	}
	if !cfg.CS2.OnlyWhenLive {
		t.Error("cs2.only_when_live должен быть включён по умолчанию")
	}
	if cfg.CS2.BombPanicS != 5 || cfg.CS2.BombHurryS != 20 {
		t.Errorf("пороги бомбы = %d/%d", cfg.CS2.BombPanicS, cfg.CS2.BombHurryS)
	}
}

// Фаза паники должна начинаться позже фазы спешки: иначе она недостижима и
// ускорение молча не работает.
func TestValidateRejectsBombThresholdOrder(t *testing.T) {
	cfg := Default()
	cfg.CS2.BombPanicS = 25
	cfg.CS2.BombHurryS = 20
	if err := cfg.Validate(); err == nil {
		t.Fatal("ожидалась ошибка при panic_s >= hurry_s")
	}
}

// Выключенная интеграция не должна валить валидацию своими полями.
func TestValidateSkipsDisabledCS2(t *testing.T) {
	cfg := Default()
	cfg.CS2.Enabled = false
	cfg.CS2.Addr = ""
	cfg.CS2.BombPanicS = 999
	if err := cfg.Validate(); err != nil {
		t.Fatalf("выключенная секция cs2 не должна валидироваться: %v", err)
	}
}

// В YAML список заменяется целиком, поэтому rules.order из старого конфига
// вытеснит события CS2, и они молча перестанут срабатывать. Load обязан их
// вернуть — иначе интеграция «не работает» без единой ошибки в логе.
func TestLoadAddsCS2RulesToOldConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	old := "discord:\n  client_id: \"1\"\n" +
		"hyperhdr:\n  host: 127.0.0.1\n  priority: 50\n" +
		"rules:\n  order: [deafened, muted, connected]\n" +
		"  states:\n    muted:\n      type: color\n      color: [1, 2, 3]\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatalf("подготовка конфига: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("загрузка конфига: %v", err)
	}

	for _, name := range cs2RuleOrder {
		if _, ok := cfg.Rules.States[name]; !ok {
			t.Errorf("правило %q не добавлено", name)
		}
		if !slices.Contains(cfg.Rules.Order, name) {
			t.Errorf("%q отсутствует в order", name)
		}
	}
	// События CS2 должны стоять выше состояний Discord: играя, человек хочет
	// видеть игру, а мут — только в паузах.
	if idx := slices.Index(cfg.Rules.Order, "cs2_dead"); idx > slices.Index(cfg.Rules.Order, "muted") {
		t.Errorf("cs2_dead ниже muted: %v", cfg.Rules.Order)
	}
	// Настройки пользователя не затираются.
	if got := cfg.Rules.States["muted"].Color; len(got) != 3 || got[0] != 1 {
		t.Errorf("правило muted перезаписано: %+v", got)
	}
}

// Повторная загрузка не должна плодить дубликаты в order.
func TestLoadDoesNotDuplicateCS2Rules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := Save(path, Default()); err != nil {
		t.Fatalf("сохранение конфига: %v", err)
	}
	for i := 0; i < 3; i++ {
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("загрузка конфига: %v", err)
		}
		if err := Save(path, cfg); err != nil {
			t.Fatalf("сохранение конфига: %v", err)
		}
	}
	cfg, _ := Load(path)
	seen := map[string]int{}
	for _, name := range cfg.Rules.Order {
		seen[name]++
	}
	for name, n := range seen {
		if n > 1 {
			t.Errorf("%q встречается в order %d раз", name, n)
		}
	}
}

// Признак смерти обязан жить дольше собственной анимации, иначе смена
// состояния оборвёт мигание на середине.
func TestValidateRejectsTooShortDeathHold(t *testing.T) {
	cfg := Default()
	cfg.CS2.DeathHoldMS = 100
	if err := cfg.Validate(); err == nil {
		t.Fatal("ожидалась ошибка: death_hold_ms короче анимации cs2_dead")
	}
	// Значения по умолчанию должны быть согласованы между собой.
	if err := Default().Validate(); err != nil {
		t.Fatalf("конфиг по умолчанию не проходит валидацию: %v", err)
	}
}

// Новое событие должно вставать на своё место относительно уже имеющихся,
// а не в начало списка. Иначе взрыв перебивал бы мигание смерти, а каждое
// следующее добавленное событие молча оказывалось бы самым приоритетным.
func TestEnsureCS2RulesInsertsAtCanonicalPosition(t *testing.T) {
	cfg := Default()
	// Конфиг, где cs2_bomb_exploded ещё не знали.
	cfg.Rules.Order = []string{"cs2_dead", "cs2_flashed", "cs2_bomb", "deafened", "muted"}
	delete(cfg.Rules.States, "cs2_bomb_exploded")

	cfg.ensureCS2Rules()

	dead := slices.Index(cfg.Rules.Order, "cs2_dead")
	exploded := slices.Index(cfg.Rules.Order, "cs2_bomb_exploded")
	flashed := slices.Index(cfg.Rules.Order, "cs2_flashed")
	if exploded < 0 {
		t.Fatalf("событие не добавлено: %v", cfg.Rules.Order)
	}
	if !(dead < exploded && exploded < flashed) {
		t.Errorf("порядок = %v, ожидалось cs2_dead → cs2_bomb_exploded → cs2_flashed",
			cfg.Rules.Order)
	}
	// Пользовательский порядок остальных не переставляем.
	if slices.Index(cfg.Rules.Order, "deafened") >= slices.Index(cfg.Rules.Order, "muted") {
		t.Errorf("порядок состояний Discord нарушен: %v", cfg.Rules.Order)
	}
}
