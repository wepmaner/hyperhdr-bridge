package config

import (
	"reflect"
	"testing"
)

// Правило старого формата (blink_*) должно давать ту же картину на ленте, что
// и раньше: вспышка, пауза, и так blink_count раз.
func TestSequenceFromLegacyBlink(t *testing.T) {
	no := false
	act := Action{Type: "color", Color: []int{255, 140, 0}, BlinkCount: 2, BlinkOnMS: 150, BlinkOffMS: 100, Hold: &no}
	seq, ok := act.Sequence()
	if !ok {
		t.Fatal("у правила color должна быть последовательность")
	}
	on := Step{Type: StepColor, MS: 150, Color: "#FF8C00"}
	off := Step{Type: StepPause, MS: 100}
	want := Sequence{End: EndReturn, Steps: []Step{on, off, on, off}}
	if !reflect.DeepEqual(seq, want) {
		t.Fatalf("получили %+v, ожидали %+v", seq, want)
	}
}

func TestSequenceFromLegacyShapes(t *testing.T) {
	cases := []struct {
		name  string
		act   Action
		end   string
		steps int
		ok    bool
	}{
		{"clear", Action{Type: "clear"}, EndReturn, 0, true},
		{"ровный цвет", Action{Type: "color", Color: []int{1, 2, 3}}, EndHold, 1, true},
		{"цикл", Action{Type: "color", Color: []int{1, 2, 3}, Loop: true}, EndLoop, 2, true},
		{"эффект", Action{Type: "effect", Effect: "Rainbow"}, "", 0, false},
		{"цвет без компонент", Action{Type: "color"}, "", 0, false},
	}
	for _, c := range cases {
		seq, ok := c.act.Sequence()
		if ok != c.ok || seq.End != c.end || len(seq.Steps) != c.steps {
			t.Errorf("%s: ok=%v end=%q шагов=%d", c.name, ok, seq.End, len(seq.Steps))
		}
	}
}

// Последовательность из редактора заменяет поля старого формата целиком и
// сохраняет выключатель события.
func TestWithSequenceDropsLegacyFields(t *testing.T) {
	off := false
	act := Action{Enabled: &off, Type: "color", Color: []int{255, 0, 0}, BlinkCount: 3}
	seq := Sequence{End: EndLoop, Steps: []Step{{Type: StepPause, MS: 100}}}
	got := act.WithSequence(seq)
	if got.On() || got.Type != "seq" || got.Color != nil || got.BlinkCount != 0 {
		t.Fatalf("правило после замены: %+v", got)
	}
	if back, ok := got.Sequence(); !ok || !reflect.DeepEqual(back, seq) {
		t.Fatalf("последовательность не сохранилась: %+v", back)
	}
}

func TestNormalize(t *testing.T) {
	seq, err := Sequence{End: EndHold, Steps: []Step{
		{Type: StepColor, MS: 200, Color: "ff7a00"},
		{Type: StepPause, MS: 50, Color: "#123456", Brightness: 40},
		{Type: StepColor, MS: 200, Color: "#00FF00", Brightness: 1},
	}}.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	want := []Step{
		{Type: StepColor, MS: 200, Color: "#FF7A00", Brightness: 100},
		{Type: StepPause, MS: 50},
		{Type: StepColor, MS: 200, Color: "#00FF00", Brightness: 5},
	}
	if !reflect.DeepEqual(seq.Steps, want) {
		t.Fatalf("получили %+v", seq.Steps)
	}

	bad := []Sequence{
		{End: "forever"},
		{End: EndHold, Steps: []Step{{Type: StepColor, MS: 5, Color: "#FFFFFF"}}},
		{End: EndHold, Steps: []Step{{Type: StepColor, MS: 100, Color: "red"}}},
		{End: EndHold, Steps: []Step{{Type: "strobe", MS: 100}}},
		{End: EndHold, Steps: make([]Step, MaxSteps+1)},
	}
	for i, s := range bad {
		if _, err := s.Normalize(); err == nil {
			t.Errorf("случай %d: ожидалась ошибка", i)
		}
	}
}

func TestStepRGBAppliesBrightness(t *testing.T) {
	r, g, b, err := Step{Type: StepColor, Color: "#FF8000", Brightness: 50}.RGB()
	if err != nil || r != 127 || g != 64 || b != 0 {
		t.Fatalf("получили %d,%d,%d (%v)", r, g, b, err)
	}
}
