// Package cs2 — интеграция с Counter-Strike 2 через Game State Integration.
//
// GSI — штатный механизм Valve: в каталог cfg игры кладётся файл, в котором
// указан адрес, и игра сама шлёт туда HTTP POST с полным снимком состояния.
// Процесс игры не трогается вообще — ни памяти, ни дескрипторов, — поэтому
// механизм безопасен с точки зрения античита и на нём же работают стрим-оверлеи.
package cs2

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrUnsupported возвращается на платформах, где мы не умеем искать Steam и
// определять запущенную игру (см. platform_other.go).
var ErrUnsupported = errors.New("интеграция с CS2 поддержана только в Windows")

const (
	// appIDCS2 — идентификатор игры в Steam, по нему ищем нужную библиотеку.
	appIDCS2 = "730"
	// cfgFileName — имя нашего файла. Игра подхватывает любой
	// gamestate_integration_*.cfg, так что чужие интеграции не конфликтуют.
	cfgFileName = "gamestate_integration_hyperhdr.cfg"
	// cfgSectionName — имя корневой секции внутри файла.
	cfgSectionName = "hyperhdr"
)

// gameRelDir — путь от каталога библиотеки Steam до каталога cfg игры.
// В CS2 он глубже, чем был в CS:GO: добавились game/csgo.
var gameRelDir = filepath.Join(
	"steamapps", "common", "Counter-Strike Global Offensive", "game", "csgo", "cfg")

// Status — состояние интеграции для веб-панели.
//
// Ошибки складываются в поле Error, а не возвращаются наружу: панели нужен
// ответ, который можно показать, а не 500 (тот же приём, что в autostart.Get).
type Status struct {
	// Supported — умеет ли текущая ОС искать Steam и определять запущенную игру.
	Supported bool `json:"supported"`
	// Dir — каталог cfg игры, если он найден.
	Dir string `json:"dir,omitempty"`
	// Path — полный путь к нашему cfg.
	Path string `json:"path,omitempty"`
	// Installed — файл на месте.
	Installed bool `json:"installed"`
	// Stale — файл есть, но адрес или токен в нём разошлись с конфигом моста.
	Stale bool `json:"stale,omitempty"`
	// Running — игра запущена прямо сейчас.
	Running bool `json:"running"`
	// NeedsRestart — конфиг не подействует, пока игру не перезапустят.
	NeedsRestart bool `json:"needs_restart"`
	// Addr — где мост слушает данные от игры.
	Addr string `json:"addr,omitempty"`
	// Listening — слушатель поднялся.
	Listening bool `json:"listening"`
	// SeenData — от игры хоть раз приходили данные.
	SeenData bool `json:"seen_data"`
	// Error — почему не удалось определить состояние.
	Error string `json:"error,omitempty"`
}

// NewToken генерирует общий секрет для cfg. Он попадает и в конфиг моста, и в
// файл игры, и проверяется на каждом POST: без него любой локальный процесс
// мог бы прислать «ты умер».
func NewToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("генерация токена: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// CfgDir возвращает каталог cfg игры. Непустой override отменяет поиск Steam:
// это запасной путь для нестандартных установок.
func CfgDir(override string) (string, error) {
	if override != "" {
		if err := dirExists(override); err != nil {
			return "", fmt.Errorf("cs2.cfg_path: %w", err)
		}
		return override, nil
	}
	if !Supported() {
		return "", ErrUnsupported
	}
	steam, err := steamPath()
	if err != nil {
		return "", err
	}
	return cfgDirFromSteam(steam)
}

// cfgDirFromSteam складывает путь до каталога cfg игры, перебирая библиотеки
// Steam. Существование каталога проверяется здесь же: иначе кнопка «Установить»
// молча создаст файл там, где игры нет.
func cfgDirFromSteam(steam string) (string, error) {
	libs := []string{steam}
	raw, err := os.ReadFile(filepath.Join(steam, "steamapps", "libraryfolders.vdf"))
	if err == nil {
		if found, err := librariesWithApp(raw, appIDCS2); err == nil && len(found) > 0 {
			libs = found
		}
	}
	tried := make([]string, 0, len(libs))
	for _, lib := range libs {
		dir := filepath.Join(lib, gameRelDir)
		if err := dirExists(dir); err == nil {
			return dir, nil
		}
		tried = append(tried, dir)
	}
	return "", fmt.Errorf("каталог cfg игры не найден, проверено: %s", strings.Join(tried, "; "))
}

func dirExists(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s не каталог", dir)
	}
	return nil
}

// librariesWithApp возвращает пути библиотек Steam, в которых числится
// приложение appID. Если ни одна библиотека не перечисляет приложения (старый
// формат файла), возвращаются все — какая из них нужная, решит проверка каталога.
func librariesWithApp(raw []byte, appID string) ([]string, error) {
	root, err := parseVDF(raw)
	if err != nil {
		return nil, err
	}
	folders, ok := root["libraryfolders"].(vdfMap)
	if !ok {
		return nil, fmt.Errorf("в libraryfolders.vdf нет секции libraryfolders")
	}
	var all, withApp []string
	for _, v := range folders {
		lib, ok := v.(vdfMap)
		if !ok {
			continue
		}
		path, ok := lib["path"].(string)
		if !ok || path == "" {
			continue
		}
		all = append(all, path)
		if apps, ok := lib["apps"].(vdfMap); ok {
			if _, ok := apps[appID]; ok {
				withApp = append(withApp, path)
			}
		}
	}
	if len(withApp) > 0 {
		return withApp, nil
	}
	return all, nil
}

// renderCfg собирает содержимое gamestate_integration_*.cfg.
//
// buffer 0.0 выбран сознательно: с ненулевым буфером игра склеивает изменения
// за интервал в один кадр, и переход здоровья в ноль может не доехать
// отдельным снимком — смерть будет теряться.
func renderCfg(addr, token string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "// Создано discord-hyperhdr-bridge. Файл читается игрой только при запуске.\n")
	fmt.Fprintf(&b, "%q\n{\n", cfgSectionName)
	fmt.Fprintf(&b, "\t%q\t\t%q\n", "uri", "http://"+addr+"/")
	fmt.Fprintf(&b, "\t%q\t%q\n", "timeout", "5.0")
	fmt.Fprintf(&b, "\t%q\t\t%q\n", "buffer", "0.0")
	fmt.Fprintf(&b, "\t%q\t%q\n", "throttle", "0.1")
	fmt.Fprintf(&b, "\t%q\t%q\n", "heartbeat", "10.0")
	fmt.Fprintf(&b, "\t%q\n\t{\n\t\t%q\t\t%q\n\t}\n", "auth", "token", token)
	b.WriteString("\t\"data\"\n\t{\n")
	for _, key := range []string{
		"provider", "map", "round", "player_id", "player_state", "bomb", "phase_countdowns",
	} {
		fmt.Fprintf(&b, "\t\t%q\t\t%q\n", key, "1")
	}
	b.WriteString("\t}\n}\n")
	return b.String()
}

// install атомарно записывает cfg и возвращает путь к нему.
func install(dir, addr, token string) (string, error) {
	path := filepath.Join(dir, cfgFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(renderCfg(addr, token)), 0o644); err != nil {
		return "", fmt.Errorf("запись %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("замена %s: %w", path, err)
	}
	return path, nil
}

// inspect сообщает, установлен ли наш cfg и совпадает ли он с текущими
// настройками моста. Нечитаемый или чужой файл считается устаревшим: перезапись
// по кнопке — верное действие, а вот молчаливое «установлено» ввело бы в
// заблуждение.
func inspect(dir, addr, token string) Status {
	st := Status{Dir: dir, Path: filepath.Join(dir, cfgFileName)}
	raw, err := os.ReadFile(st.Path)
	if os.IsNotExist(err) {
		return st
	}
	st.Installed = true
	if err != nil {
		st.Stale, st.Error = true, err.Error()
		return st
	}
	root, err := parseVDF(raw)
	if err != nil {
		st.Stale = true
		return st
	}
	sec, ok := root[cfgSectionName].(vdfMap)
	if !ok {
		st.Stale = true
		return st
	}
	auth, _ := sec["auth"].(vdfMap)
	st.Stale = sec["uri"] != "http://"+addr+"/" || auth == nil || auth["token"] != token
	return st
}
