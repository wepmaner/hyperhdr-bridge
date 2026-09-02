package app

import (
	"encoding/json"
	"log/slog"

	"github.com/ruslan/discord-hyperhdr-bridge/internal/discord"
	"github.com/ruslan/discord-hyperhdr-bridge/internal/state"
)

// ApplyEvent переводит событие Discord RPC в изменение состояния.
//
// Вынесено из App, чтобы этой же логикой пользовалась диагностическая
// утилита cmd/rpcprobe, которой не нужен HyperHDR.
//
// selfID — id текущего пользователя из READY: события SPEAKING_* и
// VOICE_STATE_UPDATE приходят для всех участников канала, чужие надо отбросить.
func ApplyEvent(log *slog.Logger, store *state.Store, selfID string, ev discord.Event) {
	switch ev.Name {
	case discord.EvReady:
		// Начальный снимок состояния клиент присылает сам, отдельными
		// синтетическими событиями VOICE_SETTINGS_UPDATE / VOICE_CHANNEL_SELECT.
		log.Debug("READY обработан")

	case discord.EvVoiceSettingsUpdate:
		// Локальные (self) мут и глушение: то, что пользователь жмёт у себя.
		var vs discord.VoiceSettings
		if err := json.Unmarshal(ev.Data, &vs); err != nil {
			log.Warn("разбор VOICE_SETTINGS_UPDATE", "err", err)
			return
		}
		store.Update(func(s *state.Snapshot) {
			s.SelfMute = vs.Mute
			s.SelfDeaf = vs.Deaf
		})

	case discord.EvVoiceChannelSelect:
		var sel discord.VoiceChannelSelect
		if err := json.Unmarshal(ev.Data, &sel); err != nil {
			log.Warn("разбор VOICE_CHANNEL_SELECT", "err", err)
			return
		}
		store.Update(func(s *state.Snapshot) {
			s.Connected = sel.ChannelID != ""
			s.ChannelID = sel.ChannelID
			s.ChannelName = sel.Name
			s.GuildID = sel.GuildID
			if !s.Connected {
				// Вне канала эти состояния бессмысленны и «залипнут», если их не сбросить.
				s.Speaking = false
				s.ServerMute, s.ServerDeaf = false, false
			}
		})
		log.Info("голосовой канал", "id", sel.ChannelID, "name", sel.Name,
			"connected", sel.ChannelID != "")

	case discord.EvVoiceStateUpdate:
		// Здесь mute/deaf — серверные (замутил модератор), self_* — локальные.
		var vsu discord.VoiceStateUpdate
		if err := json.Unmarshal(ev.Data, &vsu); err != nil {
			log.Warn("разбор VOICE_STATE_UPDATE", "err", err)
			return
		}
		if selfID == "" || vsu.User.ID != selfID {
			return
		}
		store.Update(func(s *state.Snapshot) {
			s.ServerMute = vsu.VoiceState.Mute || vsu.VoiceState.Suppress
			s.ServerDeaf = vsu.VoiceState.Deaf
			s.SelfMute = vsu.VoiceState.SelfMute
			s.SelfDeaf = vsu.VoiceState.SelfDeaf
		})

	case discord.EvSpeakingStart, discord.EvSpeakingStop:
		var sp discord.Speaking
		if err := json.Unmarshal(ev.Data, &sp); err != nil {
			log.Warn("разбор SPEAKING_*", "err", err)
			return
		}
		if selfID == "" || sp.UserID != selfID {
			return
		}
		speaking := ev.Name == discord.EvSpeakingStart
		store.Update(func(s *state.Snapshot) { s.Speaking = speaking })

	case discord.EvVoiceConnectionStatus:
		var st discord.VoiceConnectionStatus
		if err := json.Unmarshal(ev.Data, &st); err != nil {
			return
		}
		log.Debug("статус голосового соединения", "state", st.State, "ping", st.Ping)

	case discord.EvError:
		log.Error("ошибка RPC", "data", string(ev.Data))

	default:
		log.Debug("необработанное событие", "evt", ev.Name)
	}
}
