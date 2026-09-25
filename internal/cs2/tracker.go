package cs2

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
)

// tickInterval — как часто пересчитывать признаки без новых кадров.
//
// Игра шлёт кадр только при изменении состояния, а тикающая бомба состояние не
// меняет: без собственного тикера фазы ускорения не наступили бы никогда.
// 200 мс достаточно мелко для границ фаз и ничего не стоит — наружу уходят
// только изменения.
const tickInterval = 200 * time.Millisecond

// Tracker связывает поток кадров с детектором и отдаёт наружу только изменения.
//
// Про state.Store здесь сознательно ничего не известно: перевод Signals в снимок
// состояния делает app. Так пакет cs2 остаётся замкнутым на своей задаче.
type Tracker struct {
	det *Detector
	log *slog.Logger

	// OnChange вызывается при изменении признаков. Устанавливать до Run.
	OnChange func(Signals)

	interval time.Duration

	mu   sync.Mutex
	last Signals
	seen bool
}

func NewTracker(cfg config.CS2, log *slog.Logger) *Tracker {
	return &Tracker{det: NewDetector(cfg), log: log, interval: tickInterval}
}

// Frame разбирает и скармливает детектору очередной снимок.
func (t *Tracker) Frame(raw []byte) {
	var p Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.log.Debug("кадр CS2 не разобран", "err", err)
		return
	}
	t.mu.Lock()
	sig := t.det.Feed(&p, time.Now())
	t.mu.Unlock()

	if sig.Died {
		t.log.Info("CS2: смерть")
	}
	t.emit(sig)
}

// Reset забывает игровое состояние — вызывается, когда игра перестала слать
// данные. Без этого лента залипнет на последнем игровом цвете навсегда.
func (t *Tracker) Reset() {
	t.mu.Lock()
	t.det.reset()
	sig := t.det.Tick(time.Now())
	t.mu.Unlock()
	t.emit(sig)
}

// Run пересчитывает признаки по времени до отмены контекста.
func (t *Tracker) Run(ctx context.Context) {
	tick := time.NewTicker(t.interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			t.mu.Lock()
			sig := t.det.Tick(now)
			t.mu.Unlock()
			t.emit(sig)
		}
	}
}

// emit отдаёт признаки наружу, только если они изменились. Край Died в
// сравнении не участвует: он живёт один кадр и иначе давал бы лишнее событие.
func (t *Tracker) emit(sig Signals) {
	cmp := sig
	cmp.Died = false

	t.mu.Lock()
	changed := !t.seen || cmp != t.last
	t.last, t.seen = cmp, true
	cb := t.OnChange
	t.mu.Unlock()

	if changed && cb != nil {
		cb(cmp)
	}
}
