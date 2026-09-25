package cs2

// Типы снимка Game State Integration.
//
// Состав полей взят не из документации, а из реальной записи с машины
// разработчика (testdata/session-casual.jsonl): в CS2 набор блоков отличается
// от CS:GO, и часть из них игроку в матче не достаётся вовсе.
//
// Проверено записью: блоки bomb и phase_countdowns не приходят ни разу — они
// остаются привилегией наблюдателя и GOTV. Поэтому таймер до взрыва мост ведёт
// сам, от момента появления round.bomb = "planted" (см. detect.go).
//
// Игра шлёт полный снимок при каждом изменении состояния плюс heartbeat, а
// старые значения изменившихся полей кладёт в блок previously. Мы им не
// пользуемся: сравнивать соседние кадры самим надёжнее — previously не переживает
// переподключение и не заполняется на первом кадре.

// Payload — снимок состояния целиком.
type Payload struct {
	Provider *Provider `json:"provider"`
	Player   *Player   `json:"player"`
	Map      *MapInfo  `json:"map"`
	Round    *Round    `json:"round"`
}

// Provider — кто прислал кадр. steamid здесь — владелец игры, то есть мы сами.
type Provider struct {
	Name      string `json:"name"`
	AppID     int    `json:"appid"`
	SteamID   string `json:"steamid"`
	Timestamp int64  `json:"timestamp"`
}

// Player описывает игрока, за которым сейчас «смотрит» игра. Это не всегда мы:
// в наблюдении блок начинает описывать другого игрока, и отличить можно только
// сравнением SteamID с Provider.SteamID.
type Player struct {
	SteamID string `json:"steamid"`
	Name    string `json:"name"`
	// Activity: menu | playing | textinput.
	Activity string       `json:"activity"`
	Team     string       `json:"team"`
	State    *PlayerState `json:"state"`
}

// PlayerState приходит только в матче; в меню блока нет вовсе.
type PlayerState struct {
	Health int `json:"health"`
	Armor  int `json:"armor"`
	// Flashed 0..255. По записи всегда был 0 — порог остаётся непроверенным.
	Flashed    int `json:"flashed"`
	Smoked     int `json:"smoked"`
	Burning    int `json:"burning"`
	RoundKills int `json:"round_kills"`
}

// MapInfo — состояние карты. Phase: warmup | live | intermission | gameover.
type MapInfo struct {
	Name  string `json:"name"`
	Mode  string `json:"mode"`
	Phase string `json:"phase"`
	Round int    `json:"round"`
}

// Round — состояние раунда.
type Round struct {
	// Phase: freezetime | live | over.
	Phase string `json:"phase"`
	// Bomb: "planted". Других значений в записи не встретилось; конец жизни
	// бомбы виден не значением, а исчезновением самого ключа.
	Bomb    string `json:"bomb"`
	WinTeam string `json:"win_team"`
}

// mine сообщает, описывает ли блок player нас самих.
func (p *Payload) mine() bool {
	return p.Player != nil && p.Provider != nil &&
		p.Player.SteamID != "" && p.Player.SteamID == p.Provider.SteamID
}

// ownState возвращает наше состояние или nil, если кадр описывает чужого
// игрока либо мы в меню.
func (p *Payload) ownState() *PlayerState {
	if !p.mine() {
		return nil
	}
	return p.Player.State
}
