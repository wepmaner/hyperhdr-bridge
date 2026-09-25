package rules

import (
	"testing"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/state"
)

func ptr(b bool) *bool { return &b }

// Выключенное правило пропускается, и выигрывает следующее подходящее —
// иначе выключатель в панели просто гасил бы подсветку вместо передачи
// хода менее приоритетному состоянию.
func TestMatchSkipsDisabledState(t *testing.T) {
	cfg := config.Rules{
		Order: []string{"deafened", "muted", "connected"},
		States: map[string]config.Action{
			"deafened":  {Type: "color", Color: []int{255, 0, 0}, Enabled: ptr(false)},
			"muted":     {Type: "color", Color: []int{255, 140, 0}},
			"connected": {Type: "clear"},
		},
		Idle: config.Action{Type: "clear"},
	}
	snap := state.Snapshot{Connected: true, SelfDeaf: true, SelfMute: true}

	name, act := Match(cfg, snap)
	if name != "muted" {
		t.Fatalf("выбрано %q, ожидалось muted", name)
	}
	if act.Type != "color" {
		t.Errorf("действие = %+v", act)
	}
}

// Если выключены все подходящие состояния, работает idle: лента возвращается
// обычному источнику, а не застревает на последнем цвете.
func TestMatchFallsBackToIdleWhenAllDisabled(t *testing.T) {
	cfg := config.Rules{
		Order: []string{"deafened", "muted"},
		States: map[string]config.Action{
			"deafened": {Type: "color", Enabled: ptr(false)},
			"muted":    {Type: "color", Enabled: ptr(false)},
		},
		Idle: config.Action{Type: "clear"},
	}
	name, act := Match(cfg, state.Snapshot{SelfDeaf: true, SelfMute: true})
	if name != "idle" || act.Type != "clear" {
		t.Fatalf("выбрано %q / %+v, ожидался idle/clear", name, act)
	}
}

// Отсутствующий Enabled означает «включено»: старые конфиги не должны
// внезапно перестать работать после обновления моста.
func TestMatchTreatsMissingEnabledAsOn(t *testing.T) {
	cfg := config.Rules{
		Order:  []string{"muted"},
		States: map[string]config.Action{"muted": {Type: "color"}},
		Idle:   config.Action{Type: "clear"},
	}
	if name, _ := Match(cfg, state.Snapshot{SelfMute: true}); name != "muted" {
		t.Fatalf("выбрано %q, ожидалось muted", name)
	}
}
