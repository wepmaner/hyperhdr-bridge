package rules

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/hyperhdr"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/state"
)

// fakeHDR — минимальный JSON-сервер HyperHDR: пишет в лог команды и отвечает ok.
type fakeHDR struct {
	ln  net.Listener
	mu  sync.Mutex
	got []string
}

func newFakeHDR(t *testing.T) *fakeHDR {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeHDR{ln: ln}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return f
}

func (f *fakeHDR) serve(conn net.Conn) {
	defer conn.Close()
	rd := bufio.NewReader(conn)
	for {
		line, err := rd.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				return
			}
			return
		}
		var req struct {
			Command  string `json:"command"`
			Color    []int  `json:"color"`
			Duration int    `json:"duration"`
		}
		_ = json.Unmarshal(line, &req)
		desc := req.Command
		if req.Command == "color" {
			desc += "(" + strconv.Itoa(req.Duration) + ")"
		}
		f.mu.Lock()
		f.got = append(f.got, desc)
		f.mu.Unlock()
		_, _ = conn.Write([]byte(`{"success":true,"command":"` + req.Command + `"}` + "\n"))
	}
}

func (f *fakeHDR) port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *fakeHDR) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.got...)
}

// waitFor ждёт, пока накопится n команд.
func (f *fakeHDR) waitFor(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := f.commands(); len(got) >= n {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("не дождались %d команд, получили %v", n, f.commands())
	return nil
}

func testEngine(t *testing.T, f *fakeHDR, muted config.Action) *Engine {
	hdr := hyperhdr.New(config.HyperHDR{
		Host: "127.0.0.1", JSONPort: f.port(), Priority: 50, Origin: "test",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	cfg := config.Rules{
		Enabled: true,
		Order:   []string{"muted", "connected"},
		States: map[string]config.Action{
			"muted":     muted,
			"connected": {Type: "clear"},
		},
		Idle: config.Action{Type: "clear"},
	}
	return New(cfg, hdr, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// Мут: две вспышки с паузами, затем цвет остаётся гореть (duration 0).
func TestBlinkThenHold(t *testing.T) {
	f := newFakeHDR(t)
	e := testEngine(t, f, config.Action{
		Type: "color", Color: []int{255, 140, 0},
		BlinkCount: 2, BlinkOnMS: 20, BlinkOffMS: 20,
	})
	ctx := context.Background()

	if err := e.Apply(ctx, state.Snapshot{Connected: true, SelfMute: true}); err != nil {
		t.Fatal(err)
	}
	got := f.waitFor(t, 5)
	want := "color(20) clear color(20) clear color(0)"
	if strings.Join(got[:5], " ") != want {
		t.Fatalf("последовательность мигания: %v, ожидали %q", got, want)
	}

	// Размут: возвращаем подсветку HyperHDR.
	if err := e.Apply(ctx, state.Snapshot{Connected: true}); err != nil {
		t.Fatal(err)
	}
	got = f.waitFor(t, 6)
	if got[5] != "clear" {
		t.Fatalf("после размута ожидали clear, получили %v", got)
	}
}

// Смена состояния посреди анимации не должна оставлять недомигавшую горутину.
func TestBlinkInterrupted(t *testing.T) {
	f := newFakeHDR(t)
	e := testEngine(t, f, config.Action{
		Type: "color", Color: []int{255, 140, 0},
		BlinkCount: 5, BlinkOnMS: 40, BlinkOffMS: 40,
	})
	ctx := context.Background()

	if err := e.Apply(ctx, state.Snapshot{Connected: true, SelfMute: true}); err != nil {
		t.Fatal(err)
	}
	f.waitFor(t, 1)
	if err := e.Apply(ctx, state.Snapshot{Connected: true}); err != nil {
		t.Fatal(err)
	}
	got := f.commands()
	if got[len(got)-1] != "clear" {
		t.Fatalf("последней командой должен быть clear, получили %v", got)
	}
	time.Sleep(200 * time.Millisecond)
	if after := f.commands(); len(after) != len(got) {
		t.Fatalf("анимация продолжилась после отмены: %v", after)
	}
}

// hold: false — мигнуть и вернуть подсветку обычному источнику.
func TestBlinkWithoutHold(t *testing.T) {
	f := newFakeHDR(t)
	no := false
	e := testEngine(t, f, config.Action{
		Type: "color", Color: []int{255, 140, 0},
		BlinkCount: 1, BlinkOnMS: 20, BlinkOffMS: 20, Hold: &no,
	})
	if err := e.Apply(context.Background(), state.Snapshot{Connected: true, SelfMute: true}); err != nil {
		t.Fatal(err)
	}
	got := f.waitFor(t, 3)
	if want := "color(20) clear clear"; strings.Join(got[:3], " ") != want {
		t.Fatalf("получили %v, ожидали %q", got, want)
	}
}
