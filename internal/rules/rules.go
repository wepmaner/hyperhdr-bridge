// Package rules превращает состояние Discord в команду для HyperHDR.
package rules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/hyperhdr"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/state"
)

const (
	// colorOverlapMS — запас к duration цветного шага, за которым сразу идёт
	// следующий цвет: без него лента успевает моргнуть захватом экрана, пока
	// новая команда в пути.
	colorOverlapMS = 200
	// previewMax — потолок на проверку на ленте для концовок hold и loop. Своего
	// конца у них нет, её останавливает человек; потолок нужен на случай, если
	// вкладку с панелью закрыли, не нажав «Остановить».
	previewMax = time.Minute
	// commandTimeout — потолок на команду, отправляемую вне основного цикла.
	commandTimeout = 3 * time.Second
)

// Engine применяет правила к снимкам состояния.
type Engine struct {
	log *slog.Logger
	hdr *hyperhdr.Client

	mu       sync.Mutex
	cfg      config.Rules
	lastKey  string         // ключ последнего применённого действия
	lastSent time.Time      // для debounce
	lastSnap state.Snapshot // чтобы вернуть ленту состоянию после проверки

	// Текущая фоновая анимация: cancel прерывает её, done закрывается, когда
	// горутина действительно вышла.
	animCancel context.CancelFunc
	animDone   chan struct{}
	// previewing — текущая анимация запущена проверкой на ленте, а не событием.
	previewing bool
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
			// Выключенное правило не «гасит» подсветку, а уступает ход
			// следующему в Order — иначе выключатель в панели вместо отказа
			// от события просто оставлял бы ленту тёмной.
			if act, ok := cfg.States[name]; ok && act.On() {
				return name, act
			}
		}
	}
	return "idle", cfg.Idle
}

// Apply вычисляет и отправляет действие для снимка состояния.
func (e *Engine) Apply(ctx context.Context, snap state.Snapshot) error {
	e.mu.Lock()
	e.lastSnap = snap
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

	// Состояние сменилось — недоигравшая анимация больше не актуальна.
	e.stopAnim()

	e.log.Info("применяю правило", "state", name, "action", act.Type)
	return e.run(ctx, act)
}

// Reapply заново применяет правило к последнему известному состоянию —
// после правки правил в панели.
func (e *Engine) Reapply(ctx context.Context) error {
	e.mu.Lock()
	snap := e.lastSnap
	e.lastKey = ""
	e.mu.Unlock()
	return e.Apply(ctx, snap)
}

// Stop прерывает анимацию при завершении моста, чтобы она не успела занять
// ленту уже после того, как приоритет снят.
func (e *Engine) Stop() { e.stopAnim() }

func (e *Engine) run(ctx context.Context, act config.Action) error {
	if seq, ok := act.Sequence(); ok {
		return e.runSeq(ctx, seq)
	}
	switch act.Type {
	case "effect":
		return e.hdr.SetEffect(ctx, act.Effect, act.DurationMS)
	case "none", "":
		return nil
	case "color":
		return fmt.Errorf("правило color: нужен массив [R,G,B]")
	default:
		return fmt.Errorf("неизвестный тип действия: %q", act.Type)
	}
}

// runSeq исполняет последовательность. То, что не требует ожидания, уходит
// сразу; остальное играет в фоне: цикл правил не должен ждать анимацию, иначе
// события встанут в очередь.
func (e *Engine) runSeq(ctx context.Context, seq config.Sequence) error {
	if seq.Total() <= 0 {
		return e.finish(ctx, seq)
	}
	if len(seq.Steps) == 1 && seq.End == config.EndHold && seq.Steps[0].Type == config.StepColor {
		// Один цвет и удержание — просто зажечь, анимировать нечего.
		return e.finish(ctx, seq)
	}
	e.startAnim(func(actx context.Context) error {
		return e.play(actx, seq)
	})
	return nil
}

// startAnim запускает фоновую анимацию. Контекст у неё свой, а не вызывающего:
// правило может быть применено из HTTP-запроса панели, и анимация не должна
// обрываться вместе с ним. Останавливает её только stopAnim.
func (e *Engine) startAnim(fn func(context.Context) error) {
	actx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	e.mu.Lock()
	e.animCancel, e.animDone = cancel, done
	e.mu.Unlock()

	go func() {
		defer close(done)
		defer cancel()
		if err := fn(actx); err != nil && actx.Err() == nil {
			e.log.Error("световая последовательность", "err", err)
		}
	}()
}

// stopAnim прерывает текущую анимацию и дожидается её выхода, чтобы команды
// старого и нового действия не перемешались на ленте.
func (e *Engine) stopAnim() {
	e.mu.Lock()
	cancel, done := e.animCancel, e.animDone
	e.animCancel, e.animDone = nil, nil
	e.previewing = false
	e.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

// play проигрывает шаги и концовку. При концовке loop не возвращается, пока
// контекст не отменят.
func (e *Engine) play(ctx context.Context, seq config.Sequence) error {
	for {
		if err := e.steps(ctx, seq); err != nil {
			return err
		}
		if seq.End != config.EndLoop {
			return e.finish(ctx, seq)
		}
	}
}

// steps проигрывает шаги один раз.
func (e *Engine) steps(ctx context.Context, seq config.Sequence) error {
	for i, st := range seq.Steps {
		d := time.Duration(st.MS) * time.Millisecond
		if st.Type == config.StepPause {
			// В паузе приоритет снимается, поэтому сквозь мигание видно
			// обычный захват экрана.
			if err := e.hdr.Clear(ctx); err != nil {
				return err
			}
		} else {
			r, g, b, err := st.RGB()
			if err != nil {
				return err
			}
			// duration = длительность шага: если нас прервут посередине,
			// цвет погаснет сам, а не залипнет на ленте.
			dur := st.MS
			if nextIsColor(seq, i) {
				dur += colorOverlapMS
			}
			if err := e.hdr.SetColor(ctx, r, g, b, dur); err != nil {
				return err
			}
		}
		if err := sleep(ctx, d); err != nil {
			return err
		}
	}
	return nil
}

// nextIsColor сообщает, продолжится ли свет сразу после шага i — следующим
// цветным шагом, удержанием или началом нового круга.
func nextIsColor(seq config.Sequence, i int) bool {
	if i+1 < len(seq.Steps) {
		return seq.Steps[i+1].Type == config.StepColor
	}
	switch seq.End {
	case config.EndHold:
		return true
	case config.EndLoop:
		return seq.Steps[0].Type == config.StepColor
	}
	return false
}

// finish исполняет концовку: удержать последний цвет или вернуть ленту.
func (e *Engine) finish(ctx context.Context, seq config.Sequence) error {
	if seq.End == config.EndHold {
		if last, ok := seq.LastColor(); ok {
			r, g, b, err := last.RGB()
			if err != nil {
				return err
			}
			return e.hdr.SetColor(ctx, r, g, b, 0)
		}
	}
	return e.hdr.Clear(ctx)
}

// Preview проигрывает последовательность на ленте вне очереди — проверка из
// редактора. Возвращает, сколько это займёт; 0 — пока не остановят.
//
// Концовка return заканчивается сама. У hold и loop своего конца нет: цвет
// горит, а круг повторяется, пока не вызовут StopPreview (или не истечёт
// previewMax). После проверки лента возвращается к тому, что положено текущему
// состоянию. Настоящее событие, случившееся во время проверки, её прерывает.
func (e *Engine) Preview(seq config.Sequence) time.Duration {
	total := time.Duration(seq.Total()) * time.Millisecond
	limit, reported := total, total
	if seq.End != config.EndReturn {
		limit, reported = previewMax, 0
	}

	e.stopAnim()
	e.log.Info("проверка на ленте", "steps", len(seq.Steps), "end", seq.End)
	e.mu.Lock()
	e.previewing = true
	e.mu.Unlock()
	e.startAnim(func(actx context.Context) error {
		pctx, cancel := context.WithTimeout(actx, limit+100*time.Millisecond)
		defer cancel()
		err := e.play(pctx, seq)
		if err == nil && seq.End == config.EndHold {
			err = sleep(pctx, limit-total)
		}
		if actx.Err() != nil {
			// Прервали снаружи: лентой уже распоряжается новое действие.
			return nil
		}
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		e.mu.Lock()
		e.previewing = false
		e.mu.Unlock()
		e.restore()
		return nil
	})
	return reported
}

// StopPreview останавливает проверку на ленте и возвращает ленту текущему
// состоянию. Если проверка не идёт, ничего не делает: анимацию настоящего
// события кнопка в панели трогать не должна.
func (e *Engine) StopPreview() {
	e.mu.Lock()
	active := e.previewing
	e.mu.Unlock()
	if !active {
		return
	}
	e.stopAnim()
	e.restore()
}

// restore возвращает ленте действие текущего состояния. Вызывается из
// горутины анимации, поэтому stopAnim здесь звать нельзя — она ждала бы
// саму себя; новая анимация запускается поверх и подменяет запись о текущей.
func (e *Engine) restore() {
	e.mu.Lock()
	cfg, snap := e.cfg, e.lastSnap
	e.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	if !cfg.Enabled {
		_ = e.hdr.Clear(ctx)
		return
	}
	name, act := Match(cfg, snap)
	e.mu.Lock()
	e.lastKey = actionKey(name, act)
	e.lastSent = time.Now()
	e.mu.Unlock()
	if err := e.run(ctx, act); err != nil {
		e.log.Error("возврат ленты после проверки", "err", err)
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func actionKey(name string, a config.Action) string {
	if seq, ok := a.Sequence(); ok {
		raw, _ := json.Marshal(seq)
		return name + "|" + string(raw)
	}
	return fmt.Sprintf("%s|%s|%s|%d", name, a.Type, a.Effect, a.DurationMS)
}
