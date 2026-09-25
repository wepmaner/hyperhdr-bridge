package cs2

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
)

type sigSink struct {
	mu  sync.Mutex
	got []Signals
}

func (s *sigSink) add(sig Signals) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, sig)
}

func (s *sigSink) snapshot() []Signals {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Signals(nil), s.got...)
}

func newTestTracker() (*Tracker, *sigSink) {
	t := NewTracker(config.Default().CS2, slog.New(slog.NewTextHandler(io.Discard, nil)))
	sink := &sigSink{}
	t.OnChange = sink.add
	return t, sink
}

func mustJSON(t *testing.T, p *Payload) []byte {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("сериализация кадра: %v", err)
	}
	return raw
}

// Наружу уходят только изменения: игра шлёт heartbeat каждые 10 секунд, и
// одинаковые кадры не должны дёргать правила.
func TestTrackerEmitsOnlyChanges(t *testing.T) {
	tr, sink := newTestTracker()
	frame := mustJSON(t, livePayload(100, 0, ""))

	tr.Frame(frame)
	tr.Frame(frame)
	tr.Frame(frame)

	if n := len(sink.snapshot()); n != 1 {
		t.Fatalf("вызовов OnChange %d, ожидался 1", n)
	}
}

func TestTrackerReportsDeathAndBomb(t *testing.T) {
	tr, sink := newTestTracker()
	tr.Frame(mustJSON(t, livePayload(100, 0, "")))
	tr.Frame(mustJSON(t, livePayload(0, 0, "")))
	tr.Frame(mustJSON(t, livePayload(100, 0, "planted")))

	got := sink.snapshot()
	if len(got) < 3 {
		t.Fatalf("вызовов OnChange %d, ожидалось не меньше 3: %+v", len(got), got)
	}
	if !got[1].Dead {
		t.Errorf("второе изменение без признака смерти: %+v", got[1])
	}
	// Край Died наружу не отдаём: он живёт один кадр.
	if got[1].Died {
		t.Error("край Died просочился в отданные признаки")
	}
	if got[2].BombPhase != 1 {
		t.Errorf("фаза бомбы = %d, ожидалась 1", got[2].BombPhase)
	}
}

// Прекращение данных обнуляет состояние.
func TestTrackerResetClearsState(t *testing.T) {
	tr, sink := newTestTracker()
	tr.Frame(mustJSON(t, livePayload(100, 0, "planted")))
	tr.Reset()

	got := sink.snapshot()
	last := got[len(got)-1]
	if last.Live || last.Dead || last.BombPhase != 0 {
		t.Errorf("после Reset состояние не пустое: %+v", last)
	}
}

// Фазы бомбы двигает тикер, а не входящие кадры: пока бомба тикает, игра
// молчит, и без тикера ускорение никогда бы не наступило.
func TestTrackerTickerAdvancesBombWithoutFrames(t *testing.T) {
	cfg := config.Default().CS2
	// Сжимаем шкалу, чтобы тест не ждал 40 секунд реального времени.
	cfg.BombFuseS, cfg.BombHurryS, cfg.BombPanicS = 3, 2, 1
	tr := NewTracker(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	tr.interval = 20 * time.Millisecond
	sink := &sigSink{}
	tr.OnChange = sink.add

	tr.Frame(mustJSON(t, livePayload(100, 0, "planted")))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tr.Run(ctx)

	deadline := time.After(4 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("фазы не сменились без новых кадров: %+v", sink.snapshot())
		case <-time.After(50 * time.Millisecond):
		}
		phases := map[int]bool{}
		for _, s := range sink.snapshot() {
			phases[s.BombPhase] = true
		}
		if phases[1] && phases[2] && phases[3] && phases[0] {
			return // прошли 1 → 2 → 3 → взрыв, ни одного нового кадра не понадобилось
		}
	}
}
