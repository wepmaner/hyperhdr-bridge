package config

import (
	"fmt"
	"strconv"
	"strings"
)

// Концовки последовательности: что делает лента, когда шаги отыграли.
const (
	EndHold   = "hold"   // последний цвет горит, пока событие активно
	EndReturn = "return" // лента возвращается обычному источнику HyperHDR
	EndLoop   = "loop"   // шаги идут по кругу, пока событие активно
)

// Типы шагов.
const (
	StepColor = "color"
	StepPause = "pause"
)

// Границы, общие для панели, файлов пресетов и конфига.
const (
	MinStepMS = 10
	MaxStepMS = 60000
	MaxSteps  = 200
)

// Значения мигания по умолчанию для правил старого формата (blink_*).
const (
	legacyBlinkOnMS  = 220
	legacyBlinkOffMS = 180
	// legacySolidMS — условная длина шага для правила «просто цвет»: концовка
	// hold держит цвет сколько нужно, а число нужно только таймлайну в панели.
	legacySolidMS = 500
)

// Step — один шаг световой последовательности.
type Step struct {
	// Type: color | pause. В паузе приоритет снимается, и на ленте видно
	// обычную работу HyperHDR (захват экрана).
	Type string `yaml:"type" json:"type"`
	// MS — длительность шага.
	MS int `yaml:"ms" json:"ms"`
	// Color — #RRGGBB, только для type=color.
	Color string `yaml:"color,omitempty" json:"color,omitempty"`
	// Brightness 5..100, множитель к цвету. 0 = 100.
	Brightness int `yaml:"brightness,omitempty" json:"brightness,omitempty"`
}

// Sequence — что происходит со светом после срабатывания события.
type Sequence struct {
	End   string `yaml:"end" json:"end"`
	Steps []Step `yaml:"steps" json:"steps"`
}

// Total — суммарная длительность шагов, мс.
func (s Sequence) Total() int {
	total := 0
	for _, st := range s.Steps {
		total += st.MS
	}
	return total
}

// LastColor возвращает последний цветной шаг — его держит концовка hold.
func (s Sequence) LastColor() (Step, bool) {
	for i := len(s.Steps) - 1; i >= 0; i-- {
		if s.Steps[i].Type == StepColor {
			return s.Steps[i], true
		}
	}
	return Step{}, false
}

// RGB разбирает цвет шага и применяет яркость.
func (st Step) RGB() (r, g, b int, err error) {
	r, g, b, err = ParseHex(st.Color)
	if err != nil {
		return 0, 0, 0, err
	}
	if br := st.Brightness; br > 0 && br < 100 {
		r, g, b = r*br/100, g*br/100, b*br/100
	}
	return r, g, b, nil
}

// ParseHex разбирает #RRGGBB.
func ParseHex(s string) (r, g, b int, err error) {
	h := strings.TrimPrefix(s, "#")
	if len(h) != 6 {
		return 0, 0, 0, fmt.Errorf("цвет %q: нужен формат #RRGGBB", s)
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("цвет %q: нужен формат #RRGGBB", s)
	}
	return int(v >> 16), int(v >> 8 & 0xFF), int(v & 0xFF), nil
}

// Hex собирает #RRGGBB из компонент, обрезая их до 0..255.
func Hex(r, g, b int) string {
	c := func(v int) int { return max(0, min(255, v)) }
	return fmt.Sprintf("#%02X%02X%02X", c(r), c(g), c(b))
}

// Normalize проверяет последовательность, пришедшую извне (панель, файл
// пресета), и приводит её к каноническому виду.
func (s Sequence) Normalize() (Sequence, error) {
	switch s.End {
	case EndHold, EndReturn, EndLoop:
	default:
		return s, fmt.Errorf("неизвестная концовка %q", s.End)
	}
	if len(s.Steps) > MaxSteps {
		return s, fmt.Errorf("слишком много шагов: %d, максимум %d", len(s.Steps), MaxSteps)
	}
	out := Sequence{End: s.End, Steps: make([]Step, 0, len(s.Steps))}
	for i, st := range s.Steps {
		if st.MS < MinStepMS || st.MS > MaxStepMS {
			return s, fmt.Errorf("шаг %d: длительность %d мс вне %d..%d", i+1, st.MS, MinStepMS, MaxStepMS)
		}
		switch st.Type {
		case StepPause:
			out.Steps = append(out.Steps, Step{Type: StepPause, MS: st.MS})
		case StepColor:
			r, g, b, err := ParseHex(st.Color)
			if err != nil {
				return s, fmt.Errorf("шаг %d: %w", i+1, err)
			}
			br := st.Brightness
			if br <= 0 || br > 100 {
				br = 100
			}
			out.Steps = append(out.Steps, Step{Type: StepColor, MS: st.MS, Color: Hex(r, g, b), Brightness: max(5, br)})
		default:
			return s, fmt.Errorf("шаг %d: неизвестный тип %q", i+1, st.Type)
		}
	}
	return out, nil
}

// Sequence возвращает световую последовательность правила.
//
// Правила старого формата (color + blink_*) переводятся на лету, поэтому
// написанные руками конфиги продолжают работать, а движок и панель имеют дело
// с одним представлением. ok == false — у правила нет последовательности
// (effect, none): его движок исполняет по-старому.
func (a Action) Sequence() (seq Sequence, ok bool) {
	if a.Seq != nil {
		return *a.Seq, true
	}
	switch a.Type {
	case "clear":
		return Sequence{End: EndReturn, Steps: []Step{}}, true
	case "color":
		if len(a.Color) != 3 {
			return Sequence{}, false
		}
	default:
		return Sequence{}, false
	}

	color := Step{Type: StepColor, Color: Hex(a.Color[0], a.Color[1], a.Color[2]), Brightness: a.Brightness}
	if a.BlinkCount <= 0 && !a.Loop {
		color.MS = legacySolidMS
		return Sequence{End: EndHold, Steps: []Step{color}}, true
	}
	on, off := a.BlinkOnMS, a.BlinkOffMS
	if on <= 0 {
		on = legacyBlinkOnMS
	}
	if off <= 0 {
		off = legacyBlinkOffMS
	}
	color.MS = on
	pause := Step{Type: StepPause, MS: off}
	if a.Loop {
		return Sequence{End: EndLoop, Steps: []Step{color, pause}}, true
	}
	seq = Sequence{End: EndHold}
	if a.Hold != nil && !*a.Hold {
		seq.End = EndReturn
	}
	for range a.BlinkCount {
		seq.Steps = append(seq.Steps, color, pause)
	}
	return seq, true
}

// WithSequence возвращает правило с новой последовательностью. Поля старого
// формата сбрасываются: два описания одного света в конфиге разойдутся при
// первой же ручной правке.
func (a Action) WithSequence(seq Sequence) Action {
	return Action{Enabled: a.Enabled, Type: "seq", Seq: &seq}
}
