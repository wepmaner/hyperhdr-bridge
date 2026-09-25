package autostart

import "testing"

func TestCommandExe(t *testing.T) {
	cases := map[string]string{
		`"C:\Program Files\bridge.exe" -config "C:\cfg.yaml" -silent`: `C:\Program Files\bridge.exe`,
		`C:\bin\bridge.exe -silent`:                                   `C:\bin\bridge.exe`,
		`"C:\bin\bridge.exe"`:                                         `C:\bin\bridge.exe`,
		`C:\bin\bridge.exe`:                                           `C:\bin\bridge.exe`,
	}
	for cmd, want := range cases {
		if got := commandExe(cmd); got != want {
			t.Errorf("commandExe(%q) = %q, ожидалось %q", cmd, got, want)
		}
	}
}
