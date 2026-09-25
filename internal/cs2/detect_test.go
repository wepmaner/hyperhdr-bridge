package cs2

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
)

// loadSession читает записанную сессию GSI. Время берём из самого кадра
// (provider.timestamp), поэтому тест воспроизводит настоящий ход времени и
// проверяет фазы бомбы без единого sleep.
func loadSession(t *testing.T, path string) []Payload {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("открытие записи: %v", err)
	}
	defer f.Close()

	var out []Payload
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var p Payload
		if err := json.Unmarshal(line, &p); err != nil {
			t.Fatalf("разбор кадра %d: %v", len(out)+1, err)
		}
		out = append(out, p)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("чтение записи: %v", err)
	}
	return out
}

func frameTime(p Payload) time.Time {
	if p.Provider == nil {
		return time.Time{}
	}
	return time.Unix(p.Provider.Timestamp, 0)
}

// Опорный тест: прогон настоящей записи целиком. В ней ровно одна смерть
// (кадр 52, здоровье 100 → 0) и две закладки бомбы (кадры 45 и 60).
func TestDetectorOnRecordedSession(t *testing.T) {
	frames := loadSession(t, "testdata/session-casual.jsonl")
	if len(frames) != 65 {
		t.Fatalf("в записи %d кадров, ожидалось 65", len(frames))
	}

	d := NewDetector(config.Default().CS2)
	deaths, plants, flashes := 0, 0, 0
	prevBomb := 0
	for i, p := range frames {
		sig := d.Feed(&p, frameTime(p))
		if sig.Died {
			deaths++
			t.Logf("смерть на кадре %d", i+1)
		}
		if sig.BombPhase > 0 && prevBomb == 0 {
			plants++
			t.Logf("закладка бомбы на кадре %d", i+1)
		}
		if sig.Flashed {
			flashes++
		}
		prevBomb = sig.BombPhase
	}

	if deaths != 1 {
		t.Errorf("смертей найдено %d, в записи ровно одна", deaths)
	}
	if plants != 2 {
		t.Errorf("закладок бомбы найдено %d, в записи две", plants)
	}
	// Флешку в записи поймать не удалось: flashed везде 0. Тест фиксирует это,
	// чтобы ложное срабатывание порога сразу бросалось в глаза.
	if flashes != 0 {
		t.Errorf("сработала флешка %d раз, хотя flashed в записи всегда 0", flashes)
	}
}

// Смерть считается по переходу здоровья в ноль, а не по факту health == 0:
// иначе каждый кадр мёртвого игрока порождал бы новое событие.
func TestDetectorDeathIsEdgeNotLevel(t *testing.T) {
	d := NewDetector(config.Default().CS2)
	now := time.Now()
	feed := func(hp int) Signals {
		return d.Feed(livePayload(hp, 0, ""), now)
	}
	feed(100)
	if !feed(0).Died {
		t.Fatal("переход 100 → 0 не распознан как смерть")
	}
	if feed(0).Died {
		t.Error("повторный кадр с нулевым здоровьем снова считается смертью")
	}
	if feed(100).Died {
		t.Error("респавн посчитан смертью")
	}
}

// После смерти игра переключает блок player на тиммейта, за которым мы
// наблюдаем. Его здоровье не должно попадать в нашу историю: иначе чужой
// респавн прочитается как своя жизнь, а чужая смерть — как своя.
func TestDetectorIgnoresSpectatedPlayer(t *testing.T) {
	d := NewDetector(config.Default().CS2)
	now := time.Now()
	d.Feed(livePayload(100, 0, ""), now)

	other := livePayload(100, 0, "")
	other.Player.SteamID = "76561190000000999" // тиммейт, за которым смотрим
	if d.Feed(other, now).Died {
		t.Fatal("кадр наблюдения посчитан своим")
	}
	// Чужая смерть тоже не наша.
	other.Player.State.Health = 0
	if d.Feed(other, now).Died {
		t.Fatal("смерть наблюдаемого игрока посчитана своей")
	}
	// А вот собственная — наша.
	if !d.Feed(livePayload(0, 0, ""), now).Died {
		t.Error("своя смерть после наблюдения не распознана")
	}
}

// На разминке респавны идут потоком, и мигать на каждом нельзя.
func TestDetectorSkipsDeathInWarmup(t *testing.T) {
	d := NewDetector(config.Default().CS2)
	now := time.Now()
	warm := livePayload(100, 0, "")
	warm.Map.Phase = "warmup"
	d.Feed(warm, now)
	warm.Player.State.Health = 0
	if d.Feed(warm, now).Died {
		t.Error("смерть на разминке засчитана, хотя only_when_live включён")
	}
}

// Признак смерти гаснет сам: иначе, умерев при заложенной бомбе, игрок
// отмигает красным и до конца раунда смотрит на погашенную ленту.
func TestDetectorDeadExpiresByTimer(t *testing.T) {
	cfg := config.Default().CS2
	d := NewDetector(cfg)
	now := time.Now()
	d.Feed(livePayload(100, 0, ""), now)
	if !d.Feed(livePayload(0, 0, ""), now).Dead {
		t.Fatal("сразу после смерти признак не активен")
	}
	hold := time.Duration(cfg.DeathHoldMS) * time.Millisecond
	if !d.Tick(now.Add(hold / 2)).Dead {
		t.Error("признак смерти погас раньше death_hold_ms")
	}
	if d.Tick(now.Add(hold + time.Millisecond)).Dead {
		t.Error("признак смерти не погас после death_hold_ms")
	}
}

// Фазы ускорения считаются от момента закладки: таймера в данных нет.
func TestDetectorBombPhasesByOwnClock(t *testing.T) {
	cfg := config.Default().CS2
	d := NewDetector(cfg)
	start := time.Now()
	d.Feed(livePayload(100, 0, "planted"), start)

	at := func(sec int) int { return d.Tick(start.Add(time.Duration(sec) * time.Second)).BombPhase }
	cases := []struct{ sec, want int }{
		{0, 1},   // 40 с до взрыва
		{15, 1},  // 25 с — ещё спокойно
		{25, 2},  // 15 с — спешить
		{36, 3},  // 4 с — паника
		{41, 0},  // взорвалась, кадра об этом не будет
	}
	for _, c := range cases {
		if got := at(c.sec); got != c.want {
			t.Errorf("через %d с фаза = %d, ожидалась %d", c.sec, got, c.want)
		}
	}
}

// Конец жизни бомбы виден не значением, а исчезновением ключа round.bomb.
func TestDetectorBombEndsWhenKeyDisappears(t *testing.T) {
	d := NewDetector(config.Default().CS2)
	now := time.Now()
	if d.Feed(livePayload(100, 0, "planted"), now).BombPhase == 0 {
		t.Fatal("закладка не распознана")
	}
	if d.Feed(livePayload(100, 0, ""), now).BombPhase != 0 {
		t.Error("бомба осталась активной после исчезновения round.bomb")
	}
}

// Выход в меню обнуляет всё: иначе лента залипнет на последнем игровом цвете.
func TestDetectorResetsOutsideMatch(t *testing.T) {
	d := NewDetector(config.Default().CS2)
	now := time.Now()
	d.Feed(livePayload(100, 0, "planted"), now)

	menu := &Payload{
		Provider: &Provider{SteamID: "76561190000000001"},
		Player:   &Player{SteamID: "76561190000000001", Activity: "menu"},
	}
	sig := d.Feed(menu, now)
	if sig.Live || sig.BombPhase != 0 || sig.Dead {
		t.Errorf("в меню состояние не сброшено: %+v", sig)
	}
}

// livePayload собирает кадр живого матча: мы сами, карта live, раунд идёт.
func livePayload(health, flashed int, bomb string) *Payload {
	const id = "76561190000000001"
	p := &Payload{
		Provider: &Provider{SteamID: id, Name: "Counter-Strike: Global Offensive"},
		Player: &Player{
			SteamID:  id,
			Activity: "playing",
			Team:     "T",
			State:    &PlayerState{Health: health, Flashed: flashed},
		},
		Map:   &MapInfo{Name: "de_dust2", Phase: "live"},
		Round: &Round{Phase: "live", Bomb: bomb},
	}
	return p
}

// Взрыв виден только по собственным часам: игра о нём не сообщает, ключ
// round.bomb просто исчезает — ровно так же, как при разминировании.
// Различаем по времени: досидела до конца фитиля — взорвалась.
func TestDetectorExplosionAfterFuse(t *testing.T) {
	cfg := config.Default().CS2
	d := NewDetector(cfg)
	start := time.Now()
	d.Feed(livePayload(100, 0, "planted"), start)

	fuse := time.Duration(cfg.BombFuseS) * time.Second
	if d.Tick(start.Add(fuse - time.Second)).Exploded {
		t.Error("взрыв засчитан до конца отсчёта")
	}
	sig := d.Tick(start.Add(fuse + 10*time.Millisecond))
	if !sig.Exploded {
		t.Fatal("взрыв не распознан по истечении фитиля")
	}
	if sig.BombPhase != 0 {
		t.Errorf("после взрыва фаза = %d, ожидался 0", sig.BombPhase)
	}

	hold := time.Duration(cfg.BombExplodedHoldMS) * time.Millisecond
	if !d.Tick(start.Add(fuse + hold/2)).Exploded {
		t.Error("признак взрыва погас раньше времени")
	}
	if d.Tick(start.Add(fuse + hold + time.Second)).Exploded {
		t.Error("признак взрыва не погас")
	}
}

// Разминирование — тот же исчезнувший ключ, но до конца отсчёта. Взрыва нет.
func TestDetectorNoExplosionWhenDefused(t *testing.T) {
	cfg := config.Default().CS2
	d := NewDetector(cfg)
	start := time.Now()
	d.Feed(livePayload(100, 0, "planted"), start)

	// Сапёр успел за 10 секунд до конца.
	defused := start.Add(time.Duration(cfg.BombFuseS-10) * time.Second)
	d.Feed(livePayload(100, 0, ""), defused)

	after := start.Add(time.Duration(cfg.BombFuseS+5) * time.Second)
	if sig := d.Tick(after); sig.Exploded {
		t.Error("разминированная бомба посчитана взорвавшейся")
	}
}

// Выход в меню гасит и признак взрыва.
func TestDetectorResetClearsExplosion(t *testing.T) {
	cfg := config.Default().CS2
	d := NewDetector(cfg)
	start := time.Now()
	d.Feed(livePayload(100, 0, "planted"), start)
	d.Tick(start.Add(time.Duration(cfg.BombFuseS)*time.Second + time.Millisecond))

	d.reset()
	if d.Tick(start).Exploded {
		t.Error("после сброса остался признак взрыва")
	}
}
