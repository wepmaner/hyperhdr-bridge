package presets

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func flash() config.Sequence {
	return config.Sequence{End: config.EndReturn, Steps: []config.Step{
		{Type: config.StepColor, MS: 150, Color: "#ffffff"},
	}}
}

// Свой пресет переживает перезапуск; встроенные идут первыми и не удаляются.
func TestAddPersistsAndDeleteRemoves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.json")
	s := Open(path, quiet())
	builtins := len(s.List())
	if builtins == 0 {
		t.Fatal("должны быть встроенные пресеты")
	}

	p, err := s.Add("  Мой  ", "Микрофон выключен", flash())
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Мой" || !p.Own || p.Seq.Steps[0].Color != "#FFFFFF" {
		t.Fatalf("сохранён как %+v", p)
	}

	again := Open(path, quiet()).List()
	if len(again) != builtins+1 || again[builtins].ID != p.ID || !again[builtins].Own {
		t.Fatalf("после перезапуска: %+v", again)
	}

	if err := s.Delete(again[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("встроенный пресет удалять нельзя, получили %v", err)
	}
	if err := s.Delete(p.ID); err != nil {
		t.Fatal(err)
	}
	if got := Open(path, quiet()).List(); len(got) != builtins {
		t.Fatalf("пресет не удалён: %+v", got)
	}
}

func TestAddRejectsBadInput(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "presets.json"), quiet())
	if _, err := s.Add("   ", "", flash()); err == nil {
		t.Error("пустое название должно отклоняться")
	}
	if _, err := s.Add("Пусто", "", config.Sequence{End: config.EndHold}); err == nil {
		t.Error("пресет без шагов должен отклоняться")
	}
	if _, err := s.Add("Плохой", "", config.Sequence{End: "x"}); err == nil {
		t.Error("неизвестная концовка должна отклоняться")
	}
}

// Испорченный файл библиотеки не должен мешать запуску моста.
func TestOpenSurvivesBrokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "presets.json")
	if err := os.WriteFile(path, []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Open(path, quiet())
	for _, p := range s.List() {
		if p.Own {
			t.Fatalf("из битого файла прочитан пресет: %+v", p)
		}
	}
}
