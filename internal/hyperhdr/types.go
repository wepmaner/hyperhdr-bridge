package hyperhdr

import "encoding/json"

// Request — конверт JSON-API HyperHDR.
type Request struct {
	Command    string  `json:"command"`
	Subcommand string  `json:"subcommand,omitempty"`
	Token      string  `json:"token,omitempty"`
	Color      []int   `json:"color,omitempty"`
	Effect     *Effect `json:"effect,omitempty"`
	// Priority: 1..254, меньше = важнее. Наш мост занимает один приоритет.
	Priority int    `json:"priority,omitempty"`
	Duration int    `json:"duration,omitempty"` // мс, 0 = бессрочно
	Origin   string `json:"origin,omitempty"`
	Tan      int    `json:"tan,omitempty"`
}

// Effect — тело команды effect.
type Effect struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

// Response — ответ HyperHDR.
type Response struct {
	Success bool            `json:"success"`
	Command string          `json:"command"`
	Error   string          `json:"error,omitempty"`
	Tan     int             `json:"tan,omitempty"`
	Info    json.RawMessage `json:"info,omitempty"`
}
