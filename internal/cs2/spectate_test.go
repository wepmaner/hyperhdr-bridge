package cs2

import (
	"testing"
	"time"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
)

// С выключенным react_when_spectating бомба светит, только пока играешь сам:
// в наблюдении сигнал гаснет, но отсчёт идёт, и после возвращения в игру фаза
// сразу верная.
func TestDetectorBombWhileSpectating(t *testing.T) {
	spectate := func() *Payload {
		p := livePayload(100, 0, "planted")
		p.Player.SteamID = "76561190000000999" // тиммейт, за которым смотрим
		return p
	}

	cfg := config.Default().CS2
	d := NewDetector(cfg)
	start := time.Now()
	d.Feed(livePayload(100, 0, "planted"), start)
	if got := d.Feed(spectate(), start).BombPhase; got != 1 {
		t.Fatalf("по умолчанию в наблюдении бомба видна, получили фазу %d", got)
	}

	cfg.ReactWhenSpectating = false
	d = NewDetector(cfg)
	d.Feed(livePayload(100, 0, "planted"), start)
	if got := d.Feed(spectate(), start).BombPhase; got != 0 {
		t.Fatalf("в наблюдении бомба должна молчать, получили фазу %d", got)
	}
	// Тикер без новых кадров тоже молчит.
	if got := d.Tick(start.Add(25 * time.Second)).BombPhase; got != 0 {
		t.Fatalf("тикер в наблюдении дал фазу %d", got)
	}
	// Новый кадр про себя: отсчёт не терялся, до взрыва 14 с — фаза «спешить».
	if got := d.Feed(livePayload(100, 0, "planted"), start.Add(26*time.Second)).BombPhase; got != 2 {
		t.Fatalf("после возвращения в игру фаза = %d, ожидалась 2", got)
	}
}
