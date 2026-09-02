// Package rules превращает состояние Discord в команду для HyperHDR.
package rules

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/hyperhdr"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/state"
)

// Длительности фаз мигания по умолчанию, если в правиле не заданы свои.
const (
	defaultBlinkOnMS  = 220
	defaultBlinkOffMS = 180
)

// Engine применяет правила к снимкам состояния.
type Engine struct {
	log *slog.Logger
	hdr *hyperhdr.Client

	mu       sync.Mutex
	cfg      config.Rules
	lastKey  string    // ключ последнего применённого действия
	lastSent time.Time // для debounce

	// Текущая фоновая анимация мигания: cancel прерывает её,
	// done закрывается, когда горутина действительно вышла.
	animCancel context.CancelFunc
	animDone   chan struct{}
}

func New(cfg config.Rules, hdr *hyperhdr.Client, log *slog.Logger) *Engine {
	return &Engine{cfg: cfg, hdr: hdr, log: log}
}

// SetConfig подменяет правила на лету (из веб-панели).
func (e *Engine) SetConfig(cfg config.Rules) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cfg = cfg
	e.lastKey = "" // заставить переприменить
}

// Config возвращает текущие правила.
func (e *Engine) Config() config.Rules {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg
}

// Match выбирает первое подходящее состояние согласно Order.
func Match(cfg config.Rules, snap state.Snapshot) (string, config.Action) {
	for _, name := range cfg.Order {
		if snap.Has(state.Kind(name)) {
			if act, ok := cfg.States[name]; ok {
				return name, act
			}
		}
	}
	return "idle", cfg.Idle
}

// Apply вычисляет и отправляет действие для снимка состояния.
func (e *Engine) Apply(ctx context.Context, snap state.Snapshot) error {
	e.mu.Lock()
	cfg := e.cfg
	if !cfg.Enabled {
		e.mu.Unlock()
		return nil
	}
	name, act := Match(cfg, snap)
	key := actionKey(name, act)
	if key == e.lastKey {
		e.mu.Unlock()
		return nil
	}
	if d := time.Duration(cfg.DebounceMS) * time.Millisecond; d > 0 {
		if wait := d - time.Since(e.lastSent); wait > 0 {
			e.mu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
			e.mu.Lock()
		}
	}
	e.lastKey = key
	e.lastSent = time.Now()
	e.mu.Unlock()

	// Состояние сменилось — недомигавшая анимация больше не актуальна.
	e.stopBlink()

	e.log.Info("применяю правило", "state", name, "action", act.Type)
	return e.run(ctx, act)
}

func (e *Engine) run(ctx context.Context, act config.Action) error {
	switch act.Type {
	case "color":
		if len(act.Color) != 3 {
			return fmt.Errorf("правило color: нужен массив [R,G,B]")
		}
		r, g, b := scale(act.Color[0], act.Brightness),
			scale(act.Color[1], act.Brightness),
			scale(act.Color[2], act.Brightness)
		if act.BlinkCount > 0 {
			e.startBlink(ctx, act, r, g, b)
			return nil
		}
		return e.hdr.SetColor(ctx, r, g, b, act.DurationMS)
	case "effect":
		return e.hdr.SetEffect(ctx, act.Effect, act.DurationMS)
	case "clear":
		return e.hdr.Clear(ctx)
	case "none", "":
		return nil
	default:
		return fmt.Errorf("неизвестный тип действия: %q", act.Type)
	}
}

// startBlink запускает анимацию в фоне: цикл правил не должен ждать,
// пока она домигает, иначе события Discord встанут в очередь.
func (e *Engine) startBlink(ctx context.Context, act config.Action, r, g, b int) {
	actx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	e.mu.Lock()
	e.animCancel, e.animDone = cancel, done
	e.mu.Unlock()

	go func() {
		defer close(done)
		defer cancel()
		if err := e.blink(actx, act, r, g, b); err != nil && actx.Err() == nil {
			e.log.Error("анимация мигания", "err", err)
		}
	}()
}

// stopBlink прерывает текущую анимацию и дожидается её выхода, чтобы
// команды старого и нового действия не перемешались на ленте.
func (e *Engine) stopBlink() {
	e.mu.Lock()
	cancel, done := e.animCancel, e.animDone
	e.animCancel, e.animDone = nil, nil
	e.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

// blink мигает цветом BlinkCount раз и оставляет его гореть (Hold).
// В паузах приоритет снимается, поэтому сквозь мигание видно обычный
// захват экрана — как при коротком duration.
func (e *Engine) blink(ctx context.Context, act config.Action, r, g, b int) error {
	on := blinkPhase(act.BlinkOnMS, defaultBlinkOnMS)
	off := blinkPhase(act.BlinkOffMS, defaultBlinkOffMS)

	for i := 0; i < act.BlinkCount; i++ {
		// duration = длительность вспышки: если нас прервут посередине,
		// цвет погаснет сам, а не залипнет на ленте.
		if err := e.hdr.SetColor(ctx, r, g, b, int(on/time.Millisecond)); err != nil {
			return err
		}
		if err := sleep(ctx, on); err != nil {
			return err
		}
		if err := e.hdr.Clear(ctx); err != nil {
			return err
		}
		if err := sleep(ctx, off); err != nil {
			return err
		}
	}
	if act.Hold != nil && !*act.Hold {
		// Только мигнуть и вернуть подсветку обычному источнику.
		return e.hdr.Clear(ctx)
	}
	return e.hdr.SetColor(ctx, r, g, b, act.DurationMS)
}

func blinkPhase(ms, def int) time.Duration {
	if ms <= 0 {
		ms = def
	}
	return time.Duration(ms) * time.Millisecond
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// scale применяет яркость 1..100; 0 означает «не менять».
func scale(v, brightness int) int {
	if brightness <= 0 || brightness >= 100 {
		return clamp(v)
	}
	return clamp(v * brightness / 100)
}

func clamp(v int) int {
	switch {
	case v < 0:
		return 0
	case v > 255:
		return 255
	default:
		return v
	}
}

func actionKey(name string, a config.Action) string {
	hold := true
	if a.Hold != nil {
		hold = *a.Hold
	}
	return fmt.Sprintf("%s|%s|%v|%s|%d|%d|%d/%d/%d|%t",
		name, a.Type, a.Color, a.Effect, a.DurationMS, a.Brightness,
		a.BlinkCount, a.BlinkOnMS, a.BlinkOffMS, hold)
}
