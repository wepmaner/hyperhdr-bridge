// Package presets хранит библиотеку световых последовательностей: встроенные
// пресеты и те, что человек сохранил сам или открыл из файла.
package presets

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
)

// maxNameLen — потолок на длину названия, тот же, что в поле ввода панели.
const maxNameLen = 60

// ErrNotFound — пресета с таким id нет среди своих (встроенные не удаляются).
var ErrNotFound = errors.New("пресет не найден")

// Preset — именованная световая последовательность.
type Preset struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Own — сохранён человеком; встроенные удалить нельзя.
	Own bool `json:"own"`
	// From — откуда взялся: название события или имя файла. Только для показа.
	From string          `json:"from,omitempty"`
	Seq  config.Sequence `json:"seq"`
}

// Store — свои пресеты на диске плюс встроенные из кода.
type Store struct {
	path string
	log  *slog.Logger

	mu  sync.Mutex
	own []Preset
}

// Open читает библиотеку. Отсутствующий или испорченный файл — не повод не
// запускать мост: остаются встроенные пресеты, причина уходит в лог.
func Open(path string, log *slog.Logger) *Store {
	s := &Store{path: path, log: log}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s
	}
	if err == nil {
		err = json.Unmarshal(raw, &s.own)
	}
	if err != nil {
		log.Warn("библиотека пресетов не прочитана", "path", path, "err", err)
		s.own = nil
	}
	for i := range s.own {
		s.own[i].Own = true
	}
	return s
}

// List возвращает встроенные пресеты, а за ними свои.
func (s *Store) List() []Preset {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append(builtin(), s.own...)
}

// Add сохраняет новый пресет и возвращает его с присвоенным id.
func (s *Store) Add(name, from string, seq config.Sequence) (Preset, error) {
	name = clip(strings.TrimSpace(name))
	if name == "" {
		return Preset{}, errors.New("у пресета должно быть название")
	}
	seq, err := seq.Normalize()
	if err != nil {
		return Preset{}, err
	}
	if len(seq.Steps) == 0 {
		return Preset{}, errors.New("в пресете нет ни одного шага")
	}
	id, err := newID()
	if err != nil {
		return Preset{}, err
	}
	p := Preset{ID: id, Name: name, Own: true, From: clip(strings.TrimSpace(from)), Seq: seq}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.own = append(s.own, p)
	if err := s.saveLocked(); err != nil {
		s.own = s.own[:len(s.own)-1]
		return Preset{}, err
	}
	return p, nil
}

// Delete удаляет свой пресет.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.own {
		if p.ID != id {
			continue
		}
		prev := s.own
		s.own = append(append([]Preset{}, s.own[:i]...), s.own[i+1:]...)
		if err := s.saveLocked(); err != nil {
			s.own = prev
			return err
		}
		return nil
	}
	return ErrNotFound
}

// saveLocked атомарно пишет свои пресеты на диск. Требует s.mu.
func (s *Store) saveLocked() error {
	raw, err := json.MarshalIndent(s.own, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("запись пресетов: %w", err)
	}
	return os.Rename(tmp, s.path)
}

func clip(s string) string {
	if r := []rune(s); len(r) > maxNameLen {
		return string(r[:maxNameLen])
	}
	return s
}

func newID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "u-" + hex.EncodeToString(b[:]), nil
}

// builtin — пресеты, которые есть у всех. Собираются заново на каждый вызов:
// срезы шагов нельзя отдавать наружу общими.
func builtin() []Preset {
	const red, orange, white = "#FF2A2A", "#FF7A00", "#FFFFFF"
	c := func(color string, ms, bright int) config.Step {
		return config.Step{Type: config.StepColor, Color: color, MS: ms, Brightness: bright}
	}
	p := func(ms int) config.Step { return config.Step{Type: config.StepPause, MS: ms} }
	seq := func(end string, steps ...config.Step) config.Sequence {
		return config.Sequence{End: end, Steps: steps}
	}
	return []Preset{
		{ID: "b-double", Name: "Двойная вспышка",
			Seq: seq(config.EndHold, c(orange, 150, 100), p(150), c(orange, 150, 100), p(150))},
		{ID: "b-alarm", Name: "Тревога",
			Seq: seq(config.EndLoop, c(red, 200, 100), p(200))},
		{ID: "b-single", Name: "Одна вспышка",
			Seq: seq(config.EndReturn, c(white, 150, 100))},
		{ID: "b-pulse", Name: "Пульс",
			Seq: seq(config.EndLoop, c(red, 400, 50), p(400))},
		{ID: "b-hit", Name: "Удар",
			Seq: seq(config.EndReturn, c(red, 120, 100), p(80), c(red, 700, 60))},
	}
}
