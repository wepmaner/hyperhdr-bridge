package cs2

import (
	"time"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/config"
)

// Signals — производное состояние игры, уже приведённое к дискретному виду.
//
// Квантование живёт здесь, а не в правилах, по причине из state.Store: снимок
// сравнивается через ==, и сырые величины (flashed 0..255, секунды до взрыва)
// меняли бы его на каждом кадре. Дедупликация в движке правил перестала бы
// работать, и лента захлебнулась бы командами.
type Signals struct {
	// Live — идёт матч и данные про нас.
	Live bool
	// Dead — признак смерти активен; гаснет сам через death_hold_ms.
	Dead bool
	// Died — смерть произошла именно сейчас. Край, а не состояние: нужен для
	// логов и тестов, в состояние моста не попадает.
	Died bool
	// Flashed — ослепило вспышкой.
	Flashed bool
	// BombPhase: 0 нет бомбы, 1 тикает, 2 спешить, 3 паника.
	BombPhase int
	// Exploded — бомба только что взорвалась; гаснет через bomb_exploded_hold_ms.
	Exploded bool
}

// Detector выводит события из потока снимков.
//
// Чистая логика без ввода-вывода и горутин: на вход кадр и время, на выход
// признаки. Поэтому его удаётся прогонять записанной сессией без запуска игры
// (см. detect_test.go и testdata/session-casual.jsonl).
type Detector struct {
	cfg config.CS2

	// haveHealth и lastHealth — история здоровья. Обновляется только кадрами
	// про нас самих: в наблюдении блок player описывает тиммейта.
	haveHealth bool
	lastHealth int

	deadUntil time.Time
	// diedNow — край смерти: выставляется feedHealth и снимается в Feed.
	// Поле, а не возвращаемое значение, чтобы derive остался общим для Feed и Tick.
	diedNow bool

	planted   bool
	plantedAt time.Time
	// explodedUntil — до какого момента показывать взрыв. Игра о взрыве не
	// сообщает: ключ round.bomb исчезает и при взрыве, и при разминировании.
	// Отличаем по своим часам — досидела до конца фитиля, значит взорвалась.
	explodedUntil time.Time

	live    bool
	flashed bool
}

func NewDetector(cfg config.CS2) *Detector {
	return &Detector{cfg: cfg}
}

// Feed принимает очередной снимок состояния.
func (d *Detector) Feed(p *Payload, now time.Time) Signals {
	own := p.ownState()

	d.live = own != nil && p.Player.Activity == "playing" &&
		p.Map != nil && p.Map.Phase == "live"

	switch {
	case own != nil:
		d.flashed = own.Flashed >= d.cfg.FlashThreshold
		d.feedHealth(own.Health, now)
	case p.Player != nil && p.Player.State != nil:
		// Кадр про другого игрока: мы за ним наблюдаем. Историю здоровья не
		// трогаем вовсе — иначе его респавн прочитается как наша жизнь, а его
		// смерть как наша.
	default:
		// Ни своего состояния, ни чужого — мы вышли в меню. Всё забываем,
		// иначе лента залипнет на последнем игровом цвете.
		d.reset()
	}

	d.feedBomb(p, now)
	sig := d.derive(now)
	sig.Died = d.diedNow
	d.diedNow = false
	return sig
}

func (d *Detector) feedHealth(health int, now time.Time) {
	// Смерть — переход в ноль, а не факт нулевого здоровья: иначе каждый кадр
	// мёртвого игрока порождал бы новое событие.
	if d.haveHealth && d.lastHealth > 0 && health == 0 {
		if !d.cfg.OnlyWhenLive || d.live {
			d.deadUntil = now.Add(time.Duration(d.cfg.DeathHoldMS) * time.Millisecond)
			d.diedNow = true
		}
	}
	d.haveHealth, d.lastHealth = true, health
}

// feedBomb следит за закладкой. Таймер до взрыва игра не присылает (проверено
// записью: блок bomb не приходит ни разу), поэтому отсчёт ведём сами от момента,
// когда впервые увидели round.bomb = "planted".
func (d *Detector) feedBomb(p *Payload, now time.Time) {
	planted := p.Round != nil && p.Round.Bomb == "planted"
	switch {
	case planted && !d.planted:
		d.planted, d.plantedAt = true, now
	case !planted:
		// Конец жизни бомбы виден не значением, а исчезновением ключа.
		d.planted = false
	}
}

// Tick пересчитывает признаки по времени, без нового кадра.
//
// Нужен потому, что игра шлёт кадр только при изменении состояния: пока бомба
// тихо тикает, изменений нет, и без собственного тикера фазы ускорения никогда
// бы не сменились. Он же гасит признак смерти по истечении death_hold_ms.
func (d *Detector) Tick(now time.Time) Signals {
	return d.derive(now)
}

func (d *Detector) derive(now time.Time) Signals {
	d.advance(now)
	return Signals{
		Live:      d.live,
		Dead:      now.Before(d.deadUntil),
		Flashed:   d.flashed,
		BombPhase: d.bombPhase(now),
		Exploded:  now.Before(d.explodedUntil),
	}
}

// advance переводит догоревшую бомбу во взрыв. Вынесено из bombPhase, потому
// что это переход состояния, а не вычисление: сработать он должен один раз.
func (d *Detector) advance(now time.Time) {
	if !d.planted {
		return
	}
	if now.Sub(d.plantedAt) < time.Duration(d.cfg.BombFuseS)*time.Second {
		return
	}
	d.planted = false
	d.explodedUntil = now.Add(time.Duration(d.cfg.BombExplodedHoldMS) * time.Millisecond)
}

func (d *Detector) bombPhase(now time.Time) int {
	if !d.planted {
		return 0
	}
	left := time.Duration(d.cfg.BombFuseS)*time.Second - now.Sub(d.plantedAt)
	switch {
	case left <= 0:
		// Взорвалась. Отдельного кадра об этом может не быть вовсе.
		return 0
	case left <= time.Duration(d.cfg.BombPanicS)*time.Second:
		return 3
	case left <= time.Duration(d.cfg.BombHurryS)*time.Second:
		return 2
	default:
		return 1
	}
}

func (d *Detector) reset() {
	d.haveHealth, d.lastHealth = false, 0
	d.deadUntil = time.Time{}
	d.planted = false
	d.explodedUntil = time.Time{}
	d.live, d.flashed = false, false
}
