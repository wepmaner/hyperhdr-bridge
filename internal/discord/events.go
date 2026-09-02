package discord

// ReadyData — данные события READY после HANDSHAKE.
type ReadyData struct {
	V    int `json:"v"`
	User struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"user"`
	Config struct {
		APIEndpoint string `json:"api_endpoint"`
		CDNHost     string `json:"cdn_host"`
	} `json:"config"`
}

// VoiceSettings — ответ GET_VOICE_SETTINGS и тело VOICE_SETTINGS_UPDATE.
// mute/deaf здесь — ЛОКАЛЬНЫЕ (self) состояния пользователя.
type VoiceSettings struct {
	Mute  bool `json:"mute"`
	Deaf  bool `json:"deaf"`
	Input struct {
		DeviceID string  `json:"device_id"`
		Volume   float64 `json:"volume"`
	} `json:"input"`
	Output struct {
		DeviceID string  `json:"device_id"`
		Volume   float64 `json:"volume"`
	} `json:"output"`
	Mode struct {
		Type      string  `json:"type"` // PUSH_TO_TALK | VOICE_ACTIVITY
		Threshold float64 `json:"threshold"`
	} `json:"mode"`
}

// VoiceStateUpdate — состояние конкретного участника в канале.
// Здесь mute/deaf — СЕРВЕРНЫЕ, self_* — локальные.
type VoiceStateUpdate struct {
	User struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"user"`
	Nick       string `json:"nick"`
	Mute       bool   `json:"mute"`
	VoiceState struct {
		Mute     bool `json:"mute"`
		Deaf     bool `json:"deaf"`
		SelfMute bool `json:"self_mute"`
		SelfDeaf bool `json:"self_deaf"`
		Suppress bool `json:"suppress"`
	} `json:"voice_state"`
}

// VoiceChannelSelect — смена/выход из голосового канала (channel_id == "" => выход).
//
// Name в самом событии Discord отсутствует: клиент дополняет его результатом
// GET_SELECTED_VOICE_CHANNEL, чтобы UI мог показать название канала.
type VoiceChannelSelect struct {
	ChannelID string `json:"channel_id"`
	GuildID   string `json:"guild_id"`
	Name      string `json:"name,omitempty"`
}

// SelectedVoiceChannel — ответ GET_SELECTED_VOICE_CHANNEL.
type SelectedVoiceChannel struct {
	ID      string `json:"id"`
	GuildID string `json:"guild_id"`
	Name    string `json:"name"`
	Type    int    `json:"type"`
}

// VoiceConnectionStatus — состояние соединения: DISCONNECTED, CONNECTING,
// AUTHENTICATING, CONNECTED, VOICE_CONNECTED, NO_ROUTE и т.д.
type VoiceConnectionStatus struct {
	State    string  `json:"state"`
	Hostname string  `json:"hostname"`
	Ping     float64 `json:"average_ping"`
}

// Speaking — SPEAKING_START / SPEAKING_STOP.
type Speaking struct {
	UserID    string `json:"user_id"`
	ChannelID string `json:"channel_id"`
}

// Event — то, что клиент отдаёт наверх (в app).
type Event struct {
	Name string // одна из Ev* констант
	Data []byte // сырое поле data
}
