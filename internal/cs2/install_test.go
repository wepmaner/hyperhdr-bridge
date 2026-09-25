package cs2

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Реальный libraryfolders.vdf: несколько библиотек, игра лежит не в первой.
// Обратные слэши в путях экранированы — парсер обязан их развернуть.
const libraryFoldersVDF = `"libraryfolders"
{
	"0"
	{
		"path"		"C:\\Program Files (x86)\\Steam"
		"label"		""
		"apps"
		{
			"228980"		"115383040"
		}
	}
	"1"
	{
		"path"		"D:\\SteamLibrary"
		"label"		""
		"apps"
		{
			"730"		"32212254720"
			"440"		"22212254720"
		}
	}
}
`

func TestParseVDFNested(t *testing.T) {
	root, err := parseVDF([]byte(libraryFoldersVDF))
	if err != nil {
		t.Fatalf("разбор VDF: %v", err)
	}
	lf, ok := root["libraryfolders"].(vdfMap)
	if !ok {
		t.Fatalf("нет секции libraryfolders: %#v", root)
	}
	one, ok := lf["1"].(vdfMap)
	if !ok {
		t.Fatalf("нет библиотеки 1: %#v", lf)
	}
	if got := one["path"]; got != `D:\SteamLibrary` {
		t.Errorf("path = %#v, ожидался развёрнутый путь", got)
	}
}

// Из всех библиотек нужна та, в которой числится приложение 730.
func TestLibrariesWithCS2(t *testing.T) {
	libs, err := librariesWithApp([]byte(libraryFoldersVDF), appIDCS2)
	if err != nil {
		t.Fatalf("разбор библиотек: %v", err)
	}
	if len(libs) != 1 || libs[0] != `D:\SteamLibrary` {
		t.Fatalf("библиотеки = %#v, ожидалась только D:\\SteamLibrary", libs)
	}
}

// Если ни одна библиотека не перечисляет приложения (старый формат файла),
// нельзя молча вернуть пусто — перебираем все, каталог проверит вызывающий.
func TestLibrariesFallbackWhenNoAppLists(t *testing.T) {
	raw := `"libraryfolders"
{
	"0" { "path" "C:\\Steam" }
	"1" { "path" "E:\\Games" }
}`
	libs, err := librariesWithApp([]byte(raw), appIDCS2)
	if err != nil {
		t.Fatalf("разбор библиотек: %v", err)
	}
	if len(libs) != 2 {
		t.Fatalf("библиотеки = %#v, ожидались обе", libs)
	}
}

func TestParseVDFSkipsComments(t *testing.T) {
	raw := `// заголовок
"root"
{
	// комментарий внутри
	"k"	"v"
}`
	root, err := parseVDF([]byte(raw))
	if err != nil {
		t.Fatalf("разбор VDF: %v", err)
	}
	m, ok := root["root"].(vdfMap)
	if !ok || m["k"] != "v" {
		t.Errorf("получено %#v", root)
	}
}

func TestParseVDFRejectsUnbalancedBraces(t *testing.T) {
	if _, err := parseVDF([]byte(`"root" { "k" "v"`)); err == nil {
		t.Error("ожидалась ошибка на незакрытой скобке")
	}
}

// cfgDirFromSteam складывает путь до каталога cfg игры и обязан убедиться,
// что он существует: иначе кнопка «Установить» создаст файл в пустоте.
func TestCfgDirFromSteam(t *testing.T) {
	steam := t.TempDir()
	lib := filepath.Join(steam, "lib")
	want := filepath.Join(lib, gameRelDir)
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatalf("подготовка каталога: %v", err)
	}
	vdf := `"libraryfolders" { "0" { "path" "` + strings.ReplaceAll(lib, `\`, `\\`) + `" ` +
		`"apps" { "730" "1" } } }`
	if err := os.MkdirAll(filepath.Join(steam, "steamapps"), 0o755); err != nil {
		t.Fatalf("подготовка steamapps: %v", err)
	}
	if err := os.WriteFile(filepath.Join(steam, "steamapps", "libraryfolders.vdf"),
		[]byte(vdf), 0o600); err != nil {
		t.Fatalf("запись vdf: %v", err)
	}

	got, err := cfgDirFromSteam(steam)
	if err != nil {
		t.Fatalf("поиск каталога cfg: %v", err)
	}
	if got != want {
		t.Errorf("каталог cfg = %q, ожидался %q", got, want)
	}
}

// Steam есть, а игры нет — это не паника, а внятная ошибка для панели.
func TestCfgDirFromSteamGameMissing(t *testing.T) {
	steam := t.TempDir()
	if err := os.MkdirAll(filepath.Join(steam, "steamapps"), 0o755); err != nil {
		t.Fatalf("подготовка steamapps: %v", err)
	}
	if err := os.WriteFile(filepath.Join(steam, "steamapps", "libraryfolders.vdf"),
		[]byte(`"libraryfolders" { "0" { "path" "Z:\\nope" "apps" { "730" "1" } } }`), 0o600); err != nil {
		t.Fatalf("запись vdf: %v", err)
	}
	if _, err := cfgDirFromSteam(steam); err == nil {
		t.Error("ожидалась ошибка: каталог игры не существует")
	}
}

// Сгенерированный cfg должен читаться нашим же парсером и содержать адрес,
// токен и запрошенные блоки данных.
func TestRenderCfgIsValidVDF(t *testing.T) {
	raw := renderCfg("127.0.0.1:7799", "abc123")
	root, err := parseVDF([]byte(raw))
	if err != nil {
		t.Fatalf("сгенерированный cfg не разбирается: %v\n%s", err, raw)
	}
	sec, ok := root[cfgSectionName].(vdfMap)
	if !ok {
		t.Fatalf("нет секции %q: %#v", cfgSectionName, root)
	}
	if sec["uri"] != "http://127.0.0.1:7799/" {
		t.Errorf("uri = %#v", sec["uri"])
	}
	// buffer 0 — игра шлёт каждое изменение сразу. При склейке кадров переход
	// здоровья в ноль может не доехать отдельным снимком, и смерть потеряется.
	if sec["buffer"] != "0.0" {
		t.Errorf("buffer = %#v, ожидался 0.0", sec["buffer"])
	}
	auth, ok := sec["auth"].(vdfMap)
	if !ok || auth["token"] != "abc123" {
		t.Errorf("auth = %#v", sec["auth"])
	}
	data, ok := sec["data"].(vdfMap)
	if !ok {
		t.Fatalf("нет блока data: %#v", sec)
	}
	for _, want := range []string{"provider", "map", "round", "player_id", "player_state", "bomb"} {
		if data[want] != "1" {
			t.Errorf("data.%s = %#v, ожидалась 1", want, data[want])
		}
	}
}

func TestInspectReportsInstalledAndStale(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, cfgFileName)

	st := inspect(dir, "127.0.0.1:7799", "tok")
	if st.Installed {
		t.Error("файла нет, а Installed = true")
	}

	if err := os.WriteFile(path, []byte(renderCfg("127.0.0.1:7799", "tok")), 0o600); err != nil {
		t.Fatalf("запись cfg: %v", err)
	}
	st = inspect(dir, "127.0.0.1:7799", "tok")
	if !st.Installed || st.Stale {
		t.Errorf("свежий cfg: installed=%v stale=%v err=%q", st.Installed, st.Stale, st.Error)
	}

	// Порт в конфиге моста поменяли — установленный cfg больше не годится.
	st = inspect(dir, "127.0.0.1:7800", "tok")
	if !st.Installed || !st.Stale {
		t.Errorf("другой порт: installed=%v stale=%v", st.Installed, st.Stale)
	}
	// Токен перегенерировали — то же самое.
	st = inspect(dir, "127.0.0.1:7799", "other")
	if !st.Installed || !st.Stale {
		t.Errorf("другой токен: installed=%v stale=%v", st.Installed, st.Stale)
	}
}

// Чужой файл с тем же именем не должен приводить к панике или к «установлено».
func TestInspectHandlesGarbageFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, cfgFileName), []byte("не VDF вовсе {{{"), 0o600); err != nil {
		t.Fatalf("запись мусора: %v", err)
	}
	st := inspect(dir, "127.0.0.1:7799", "tok")
	if !st.Installed || !st.Stale {
		t.Errorf("мусорный cfg должен считаться устаревшим: %+v", st)
	}
}

func TestInstallWritesReadableCfg(t *testing.T) {
	dir := t.TempDir()
	path, err := install(dir, "127.0.0.1:7799", "tok")
	if err != nil {
		t.Fatalf("установка cfg: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Errorf("cfg записан не туда: %q", path)
	}
	if st := inspect(dir, "127.0.0.1:7799", "tok"); !st.Installed || st.Stale {
		t.Errorf("после установки: %+v", st)
	}
}
