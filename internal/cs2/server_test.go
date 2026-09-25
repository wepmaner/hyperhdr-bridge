package cs2

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
)

func testServer(t *testing.T, token string) (*Server, *httptest.Server, *frameSink) {
	t.Helper()
	cfg := config.Default().CS2
	cfg.Token = token
	s := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	sink := &frameSink{}
	s.OnFrame = sink.add
	ts := httptest.NewServer(s.handler())
	t.Cleanup(ts.Close)
	return s, ts, sink
}

// frameSink копит принятые кадры: обработчик вызывается из горутин http.
type frameSink struct {
	mu  sync.Mutex
	got [][]byte
}

func (f *frameSink) add(b []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, append([]byte(nil), b...))
}

func (f *frameSink) len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.got)
}

func post(t *testing.T, ts *httptest.Server, body string) *http.Response {
	t.Helper()
	res, err := http.Post(ts.URL+"/", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func TestServerAcceptsFrameWithValidToken(t *testing.T) {
	_, ts, sink := testServer(t, "secret")
	res := post(t, ts, `{"auth":{"token":"secret"},"provider":{"name":"CS2"}}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("код ответа = %d", res.StatusCode)
	}
	if sink.len() != 1 {
		t.Fatalf("кадров получено %d, ожидался 1", sink.len())
	}
}

// Чужой процесс на localhost не должен уметь прислать «ты умер».
func TestServerRejectsWrongToken(t *testing.T) {
	_, ts, sink := testServer(t, "secret")
	res := post(t, ts, `{"auth":{"token":"нет"},"provider":{"name":"CS2"}}`)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("код ответа = %d, ожидался 403", res.StatusCode)
	}
	if sink.len() != 0 {
		t.Fatal("кадр с неверным токеном попал в обработку")
	}
}

// Пустой токен в конфиге означает, что cfg ставили руками: проверять нечего,
// но и отбрасывать данные игры нельзя.
func TestServerAcceptsAnyTokenWhenNotConfigured(t *testing.T) {
	_, ts, sink := testServer(t, "")
	res := post(t, ts, `{"provider":{"name":"CS2"}}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("код ответа = %d", res.StatusCode)
	}
	if sink.len() != 1 {
		t.Fatal("кадр без токена отброшен, хотя токен не настроен")
	}
}

func TestServerRejectsGarbageJSON(t *testing.T) {
	_, ts, sink := testServer(t, "secret")
	res := post(t, ts, `{это не json`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("код ответа = %d, ожидался 400", res.StatusCode)
	}
	if sink.len() != 0 {
		t.Fatal("битый кадр попал в обработку")
	}
}

func TestServerRejectsGET(t *testing.T) {
	_, ts, _ := testServer(t, "secret")
	res, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("код ответа = %d, ожидался 405", res.StatusCode)
	}
}

// Игра закрылась — данные просто перестали приходить. Без сторожа лента
// залипнет на последнем игровом цвете навсегда.
func TestIdleWatchdogFiresAfterSilence(t *testing.T) {
	s, ts, _ := testServer(t, "secret")
	s.idleTimeout = 40 * time.Millisecond

	fired := make(chan struct{})
	var once sync.Once
	s.OnIdle = func() { once.Do(func() { close(fired) }) }

	post(t, ts, `{"auth":{"token":"secret"}}`)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.watchIdle(ctx)

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("сторож простоя не сработал")
	}
}

// Пока кадры идут, сторож молчит: иначе подсветка гасла бы посреди матча.
func TestIdleWatchdogSilentWhileFramesArrive(t *testing.T) {
	s, ts, _ := testServer(t, "secret")
	s.idleTimeout = 80 * time.Millisecond

	var mu sync.Mutex
	fires := 0
	s.OnIdle = func() { mu.Lock(); fires++; mu.Unlock() }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.watchIdle(ctx)

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		post(t, ts, `{"auth":{"token":"secret"}}`)
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if fires != 0 {
		t.Fatalf("сторож сработал %d раз при идущих кадрах", fires)
	}
}

// До первого кадра сторожу срабатывать не на чем: игра могла просто не
// запускаться, и гасить состояние, которого не было, незачем.
func TestIdleWatchdogQuietBeforeFirstFrame(t *testing.T) {
	s, _, _ := testServer(t, "secret")
	s.idleTimeout = 30 * time.Millisecond

	var mu sync.Mutex
	fires := 0
	s.OnIdle = func() { mu.Lock(); fires++; mu.Unlock() }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.watchIdle(ctx)
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if fires != 0 {
		t.Fatalf("сторож сработал %d раз, хотя кадров не было вовсе", fires)
	}
}
