// Package discord реализует клиент локального Discord RPC (IPC) —
// именованный канал/unix-сокет, который открывает десктопный клиент Discord.
package discord

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// Opcode — тип IPC-кадра.
type Opcode uint32

const (
	OpHandshake Opcode = 0
	OpFrame     Opcode = 1
	OpClose     Opcode = 2
	OpPing      Opcode = 3
	OpPong      Opcode = 4
)

// Максимальный размер полезной нагрузки кадра (защита от мусора в сокете).
const maxFrameSize = 1 << 20

// Команды RPC.
const (
	CmdDispatch         = "DISPATCH"
	CmdAuthorize        = "AUTHORIZE"
	CmdAuthenticate     = "AUTHENTICATE"
	CmdSubscribe        = "SUBSCRIBE"
	CmdUnsubscribe      = "UNSUBSCRIBE"
	CmdGetVoiceSettings = "GET_VOICE_SETTINGS"
	CmdGetSelectedVoice = "GET_SELECTED_VOICE_CHANNEL"
	CmdSetVoiceSettings = "SET_VOICE_SETTINGS"
)

// События, на которые подписывается мост.
const (
	EvReady                 = "READY"
	EvError                 = "ERROR"
	EvVoiceSettingsUpdate   = "VOICE_SETTINGS_UPDATE"
	EvVoiceStateUpdate      = "VOICE_STATE_UPDATE"
	EvVoiceConnectionStatus = "VOICE_CONNECTION_STATUS"
	EvVoiceChannelSelect    = "VOICE_CHANNEL_SELECT"
	EvSpeakingStart         = "SPEAKING_START"
	EvSpeakingStop          = "SPEAKING_STOP"
)

// Payload — универсальный конверт кадра RPC.
type Payload struct {
	Cmd   string          `json:"cmd,omitempty"`
	Nonce string          `json:"nonce,omitempty"`
	Evt   string          `json:"evt,omitempty"`
	Args  json.RawMessage `json:"args,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// writeFrame кодирует кадр: [opcode uint32 LE][len uint32 LE][json].
func writeFrame(w io.Writer, op Opcode, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	head := make([]byte, 8)
	binary.LittleEndian.PutUint32(head[0:4], uint32(op))
	binary.LittleEndian.PutUint32(head[4:8], uint32(len(raw)))
	if _, err := w.Write(head); err != nil {
		return err
	}
	_, err = w.Write(raw)
	return err
}

// readFrame читает один кадр из сокета.
func readFrame(r io.Reader) (Opcode, []byte, error) {
	head := make([]byte, 8)
	if _, err := io.ReadFull(r, head); err != nil {
		return 0, nil, err
	}
	op := Opcode(binary.LittleEndian.Uint32(head[0:4]))
	size := binary.LittleEndian.Uint32(head[4:8])
	if size > maxFrameSize {
		return 0, nil, fmt.Errorf("кадр слишком большой: %d байт", size)
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, nil, err
	}
	return op, body, nil
}
