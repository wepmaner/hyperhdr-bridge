// Package state хранит текущее состояние пользователя в Discord
// и рассылает изменения подписчикам (правила, веб-панель, трей).
package state

import (
	"sync"
	"time"
)

// Kind — атомарное состояние, на которое можно навесить правило.
type Kind string

const (
	KindConnected Kind = "connected" // подключён к голосовому каналу
	KindSpeaking  Kind = "speaking"  // говорит прямо сейчас
	KindMuted     Kind = "muted"     // микрофон выключен (self или server)
	KindDeafened  Kind = "deafened"  // звук выключен (self или server)
	KindStreaming Kind = "streaming" // TODO: демонстрация экрана
)

// Snapshot — полный слепок состояния на момент времени.
type Snapshot struct {
	Connected   bool      `json:"connected"`
	ChannelID   string    `json:"channel_id"`
	ChannelName string    `json:"channel_name"`
	GuildID     string    `json:"guild_id"`
	SelfMute    bool      `json:"self_mute"`
	SelfDeaf    bool      `json:"self_deaf"`
	ServerMute  bool      `json:"server_mute"`
	ServerDeaf  bool      `json:"server_deaf"`
	Speaking    bool      `json:"speaking"`
	Streaming   bool      `json:"streaming"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Muted — микрофон выключен любым способом.
func (s Snapshot) Muted() bool { return s.SelfMute || s.ServerMute }

// Deafened — звук выключен любым способом.
func (s Snapshot) Deafened() bool { return s.SelfDeaf || s.ServerDeaf }

// Has сообщает, активно ли состояние.
func (s Snapshot) Has(k Kind) bool {
	switch k {
	case KindConnected:
		return s.Connected
	case KindSpeaking:
		return s.Speaking
	case KindMuted:
		return s.Muted()
	case KindDeafened:
		return s.Deafened()
	case KindStreaming:
		return s.Streaming
	}
	return false
}

// Store — потокобезопасное хранилище с широковещательной рассылкой.
type Store struct {
	mu   sync.RWMutex
	cur  Snapshot
	subs map[int]chan Snapshot
	next int
}

func New() *Store {
	return &Store{subs: make(map[int]chan Snapshot)}
}

// Get возвращает текущий слепок.
func (s *Store) Get() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

// Update применяет мутацию и уведомляет подписчиков, если что-то изменилось.
func (s *Store) Update(mut func(*Snapshot)) Snapshot {
	s.mu.Lock()
	prev := s.cur
	mut(&s.cur)
	if s.cur == prev {
		s.mu.Unlock()
		return s.cur
	}
	s.cur.UpdatedAt = time.Now()
	snap := s.cur
	subs := make([]chan Snapshot, 0, len(s.subs))
	for _, ch := range s.subs {
		subs = append(subs, ch)
	}
	s.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- snap:
		default: // медленный подписчик — пропускаем кадр
		}
	}
	return snap
}

// Subscribe возвращает канал обновлений и функцию отписки.
func (s *Store) Subscribe() (<-chan Snapshot, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.next
	s.next++
	ch := make(chan Snapshot, 8)
	s.subs[id] = ch
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if c, ok := s.subs[id]; ok {
			delete(s.subs, id)
			close(c)
		}
	}
}
