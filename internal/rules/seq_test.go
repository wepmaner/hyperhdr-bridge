package rules

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/state"
)

func color(hex string, ms int) config.Step {
	return config.Step{Type: config.StepColor, Color: hex, MS: ms}
}

func pause(ms int) config.Step { return config.Step{Type: config.StepPause, MS: ms} }

func seqAction(end string, steps ...config.Step) config.Action {
	return config.Action{}.WithSequence(config.Sequence{End: end, Steps: steps})
}

// Два цвета подряд: первый получает запас по duration, чтобы между ними не
// проглянул захват экрана, а последний перед удержанием сразу сменяется
// бессрочным. Пауза в начале — «подождать перед тем, как загореться».
func TestSequenceColorsBackToBack(t *testing.T) {
	f := newFakeHDR(t)
	e := testEngine(t, f, seqAction(config.EndHold, pause(20), color("#FFFFFF", 20), color("#FF0000", 20)))
	if err := e.Apply(context.Background(), state.Snapshot{Connected: true, SelfMute: true}); err != nil {
		t.Fatal(err)
	}
	got := f.waitFor(t, 4)
	if want := "clear color(220) color(220) color(0)"; strings.Join(got[:4], " ") != want {
		t.Fatalf("получили %v, ожидали %q", got, want)
	}
}

// Цикл идёт, пока состояние активно, и обрывается его сменой.
func TestSequenceLoopUntilStateChanges(t *testing.T) {
	f := newFakeHDR(t)
	e := testEngine(t, f, seqAction(config.EndLoop, color("#FF0000", 15), pause(15)))
	ctx := context.Background()
	if err := e.Apply(ctx, state.Snapshot{Connected: true, SelfMute: true}); err != nil {
		t.Fatal(err)
	}
	got := f.waitFor(t, 6)
	if want := "color(15) clear color(15) clear color(15) clear"; strings.Join(got[:6], " ") != want {
		t.Fatalf("получили %v, ожидали %q", got, want)
	}
	if err := e.Apply(ctx, state.Snapshot{Connected: true}); err != nil {
		t.Fatal(err)
	}
	n := len(f.commands())
	time.Sleep(100 * time.Millisecond)
	if after := f.commands(); len(after) != n {
		t.Fatalf("цикл продолжился после смены состояния: %v", after[n:])
	}
}

// Проверка на ленте проигрывает последовательность вне очереди и потом
// возвращает ленте действие текущего состояния.
func TestPreviewRestoresCurrentState(t *testing.T) {
	f := newFakeHDR(t)
	e := testEngine(t, f, config.Action{Type: "color", Color: []int{255, 140, 0}})
	if err := e.Apply(context.Background(), state.Snapshot{Connected: true, SelfMute: true}); err != nil {
		t.Fatal(err)
	}
	f.waitFor(t, 1) // color(0): мут горит

	e.Preview(config.Sequence{End: config.EndReturn, Steps: []config.Step{color("#00FF00", 20), pause(20)}})
	got := f.waitFor(t, 5)
	// Вспышка, пауза, концовка return — и снова цвет мута.
	if want := "color(0) color(20) clear clear color(0)"; strings.Join(got[:5], " ") != want {
		t.Fatalf("получили %v, ожидали %q", got, want)
	}
}

// Круг при проверке повторяется, пока его не остановят; после остановки лента
// возвращается текущему состоянию, и больше команд нет.
func TestPreviewLoopsUntilStopped(t *testing.T) {
	f := newFakeHDR(t)
	e := testEngine(t, f, config.Action{Type: "color", Color: []int{255, 140, 0}})
	if err := e.Apply(context.Background(), state.Snapshot{Connected: true, SelfMute: true}); err != nil {
		t.Fatal(err)
	}
	f.waitFor(t, 1) // color(0): мут горит

	if d := e.Preview(config.Sequence{End: config.EndLoop, Steps: []config.Step{color("#00FF00", 15), pause(15)}}); d != 0 {
		t.Fatalf("у круга нет своей длительности, получили %v", d)
	}
	got := f.waitFor(t, 7)
	if want := "color(15) clear color(15) clear color(15) clear"; strings.Join(got[1:7], " ") != want {
		t.Fatalf("получили %v, ожидали три круга %q", got, want)
	}

	e.StopPreview()
	got = f.commands()
	if got[len(got)-1] != "color(0)" {
		t.Fatalf("после остановки должен вернуться цвет мута, получили %v", got)
	}
	time.Sleep(100 * time.Millisecond)
	if after := f.commands(); len(after) != len(got) {
		t.Fatalf("проверка продолжилась после остановки: %v", after[len(got):])
	}
	// Повторная остановка ничего не шлёт.
	e.StopPreview()
	if after := f.commands(); len(after) != len(got) {
		t.Fatalf("лишние команды после повторной остановки: %v", after[len(got):])
	}
}

// Настоящее событие важнее проверки: оно прерывает её, и проверка после этого
// ничего не шлёт на ленту.
func TestPreviewInterruptedByStateChange(t *testing.T) {
	f := newFakeHDR(t)
	e := testEngine(t, f, config.Action{Type: "color", Color: []int{255, 140, 0}})
	ctx := context.Background()
	if err := e.Apply(ctx, state.Snapshot{Connected: true}); err != nil {
		t.Fatal(err)
	}
	f.waitFor(t, 1) // clear

	e.Preview(config.Sequence{End: config.EndLoop, Steps: []config.Step{color("#00FF00", 30), pause(30)}})
	f.waitFor(t, 2)
	if err := e.Apply(ctx, state.Snapshot{Connected: true, SelfMute: true}); err != nil {
		t.Fatal(err)
	}
	got := f.commands()
	if got[len(got)-1] != "color(0)" {
		t.Fatalf("последней должна быть команда мута, получили %v", got)
	}
	time.Sleep(150 * time.Millisecond)
	if after := f.commands(); len(after) != len(got) {
		t.Fatalf("проверка продолжилась после события: %v", after[len(got):])
	}
}
