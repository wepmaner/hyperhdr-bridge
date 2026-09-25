// Команда gsiprobe — диагностика Game State Integration CS2 без HyperHDR.
//
// Поднимает тот же слушатель, что и мост, печатает каждый кадр в читаемом виде
// и складывает сырой поток в файл. Нужна, потому что состав полей GSI в CS2 не
// документирован достоверно: детектор событий пишется по этой записи, а тесты
// потом гоняют её без запуска игры.
//
//	go run ./cmd/gsiprobe -install      # положить cfg в каталог игры
//	go run ./cmd/gsiprobe -out dump.jsonl
//
// После -install игру нужно перезапустить: cfg читается только при старте.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/console"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/cs2"
)

func main() {
	console.Setup()

	cfgPath := flag.String("config", "config.yaml", "путь к файлу конфигурации")
	out := flag.String("out", "gsi-dump.jsonl", "куда писать сырые кадры (пусто — не писать)")
	doInstall := flag.Bool("install", false, "положить gamestate_integration в каталог игры и выйти")
	quiet := flag.Bool("quiet", false, "не печатать каждый кадр")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "конфигурация:", err)
		os.Exit(1)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Слушатель должен работать и при cs2.enabled: false — утилита диагностическая.
	cs2cfg := cfg.CS2
	cs2cfg.Enabled = true
	srv := cs2.New(cs2cfg, log)
	srv.Persist = func(changed config.CS2) error {
		cfg.CS2 = changed
		return config.Save(*cfgPath, cfg)
	}

	if *doInstall {
		st, err := srv.Install()
		if err != nil {
			fmt.Fprintln(os.Stderr, "установка cfg:", err)
			os.Exit(1)
		}
		fmt.Println("cfg записан:", st.Path)
		if st.Running {
			fmt.Println("CS2 сейчас запущен — выйдите из игры и запустите заново,")
			fmt.Println("иначе конфиг не подействует.")
		}
		return
	}

	printStatus(srv.Status())

	rec := &recorder{quiet: *quiet, path: *out}
	defer rec.close()
	if *out != "" {
		fmt.Println("Сырые кадры будут писаться в", *out)
	}
	srv.OnFrame = rec.frame
	srv.OnIdle = func() { fmt.Println("— данные от игры прекратились —") }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() { <-sig; cancel() }()

	fmt.Println()
	fmt.Println("Жду данные от CS2. Зайдите в оффлайн-игру с ботами и постарайтесь")
	fmt.Println("умереть, поймать флешку, заложить бомбу и дать ей взорваться.")
	fmt.Println("Ctrl+C — выход и итоговая сводка.")
	fmt.Println()

	if err := srv.Run(ctx); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "слушатель:", err)
		if addrInUse(err) {
			// Самая частая причина: мост уже висит в трее и держит тот же порт.
			fmt.Fprintln(os.Stderr, "Порт занят. Скорее всего, запущен сам мост —")
			fmt.Fprintln(os.Stderr, "закройте bridge.exe через иконку в трее и повторите.")
		}
		os.Exit(1)
	}
	rec.report()
}

// wsaEAddrInUse — WSAEADDRINUSE. Winsock отдаёт свой код, а не POSIX-овый
// syscall.EADDRINUSE, поэтому проверяем оба. Сравнивать текст ошибки нельзя:
// на русской Windows он локализован.
const wsaEAddrInUse = syscall.Errno(10048)

func addrInUse(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && (errno == syscall.EADDRINUSE || errno == wsaEAddrInUse)
}

func printStatus(st cs2.Status) {
	fmt.Println("Слушатель:   ", st.Addr)
	if st.Dir != "" {
		fmt.Println("Каталог игры:", st.Dir)
	}
	switch {
	case st.Error != "":
		fmt.Println("Конфиг GSI:   не определён —", st.Error)
	case !st.Installed:
		fmt.Println("Конфиг GSI:   не установлен (запустите с -install)")
	case st.Stale:
		fmt.Println("Конфиг GSI:   устарел, адрес или токен изменились (-install)")
	default:
		fmt.Println("Конфиг GSI:   установлен")
	}
	fmt.Println("CS2 запущен: ", yn(st.Running))
}

// recorder пишет сырые кадры в файл и попутно собирает то, ради чего утилита и
// написана: какие блоки данных игра реально присылает игроку в матче.
type recorder struct {
	quiet bool
	// path открывается лениво, по первому кадру: иначе неудачный запуск
	// (занятый порт, не установленный cfg) оставлял бы пустой файл дампа,
	// который легко спутать с неудачной записью игровой сессии.
	path string

	mu     sync.Mutex
	file   *os.File
	frames int
	blocks map[string]int
	// bombCountdown — приходил ли таймер бомбы. Главный открытый вопрос: в
	// CS:GO блок bomb доставался только наблюдателю.
	bombCountdown bool
	first, last   time.Time
}

func (r *recorder) frame(raw []byte) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		buf.Write(raw) // не разобралось — пишем как есть, разберёмся глазами
	}

	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)

	r.mu.Lock()
	if r.blocks == nil {
		r.blocks = map[string]int{}
		r.first = time.Now()
	}
	r.frames++
	r.last = time.Now()
	for k := range doc {
		r.blocks[k]++
	}
	if _, ok := dig(doc, "bomb", "countdown"); ok {
		r.bombCountdown = true
	}
	if r.file == nil && r.path != "" {
		f, err := os.Create(r.path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "создание файла дампа:", err)
			r.path = "" // больше не пытаемся: сообщение раз в кадр никому не нужно
		} else {
			r.file = f
		}
	}
	file, n := r.file, r.frames
	r.mu.Unlock()

	if file != nil {
		buf.WriteByte('\n')
		if _, err := file.Write(buf.Bytes()); err != nil {
			fmt.Fprintln(os.Stderr, "запись дампа:", err)
		}
	}
	if !r.quiet {
		fmt.Printf("%4d  %s\n", n, summarize(doc))
	}
}

// summarize вытаскивает из кадра то немногое, что нужно видеть глазами во
// время записи. Типы GSI сознательно не заводятся: их состав и есть предмет
// исследования, а строить их на догадках — то, чего эта утилита избегает.
func summarize(doc map[string]any) string {
	get := func(path ...string) string {
		v, ok := dig(doc, path...)
		if !ok {
			return "—"
		}
		return fmt.Sprint(v)
	}
	mine := "?"
	if p, ok := dig(doc, "player", "steamid"); ok {
		if pr, ok := dig(doc, "provider", "steamid"); ok {
			mine = yn(fmt.Sprint(p) == fmt.Sprint(pr))
		}
	}
	return fmt.Sprintf("свой=%-3s hp=%-4s flash=%-4s карта=%-10s раунд=%-9s бомба=%-9s таймер=%s",
		mine,
		get("player", "state", "health"),
		get("player", "state", "flashed"),
		get("map", "phase"),
		get("round", "phase"),
		get("round", "bomb"),
		get("bomb", "countdown"),
	)
}

// dig достаёт значение по пути во вложенных map[string]any.
func dig(doc map[string]any, path ...string) (any, bool) {
	var cur any = doc
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[key]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func (r *recorder) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file != nil {
		_ = r.file.Close()
		r.file = nil
	}
}

func (r *recorder) report() {
	r.mu.Lock()
	defer r.mu.Unlock()

	fmt.Println()
	fmt.Println("── Итог ──")
	if r.frames == 0 {
		fmt.Println("Кадров не получено. Проверьте, что cfg установлен (-install)")
		fmt.Println("и что CS2 перезапущен после установки.")
		return
	}
	fmt.Printf("Кадров: %d за %s\n", r.frames, r.last.Sub(r.first).Round(time.Second))

	names := make([]string, 0, len(r.blocks))
	for k := range r.blocks {
		names = append(names, k)
	}
	sort.Strings(names)
	fmt.Println("Присланные блоки:")
	for _, k := range names {
		fmt.Printf("  %-20s %d\n", k, r.blocks[k])
	}
	fmt.Println()
	fmt.Println("Таймер бомбы (bomb.countdown):", yn(r.bombCountdown))
	if !r.bombCountdown {
		fmt.Println("  Значит, отсчёт до взрыва мост будет вести сам от момента закладки.")
	}
}

func yn(b bool) string {
	if b {
		return "да"
	}
	return "нет"
}
