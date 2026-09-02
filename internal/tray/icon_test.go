package tray

import (
	"encoding/binary"
	"os"
	"testing"
)

// TestIconStructure проверяет, что иконка собирается в валидный ICO:
// systray на Windows молча игнорирует битые данные, и значка просто нет.
func TestIconStructure(t *testing.T) {
	ico := Icon(255, 140, 0)

	wantLen := 22 + 40 + iconSize*iconSize*4 + iconSize*4
	if len(ico) != wantLen {
		t.Fatalf("размер ICO = %d, ожидался %d", len(ico), wantLen)
	}
	if got := binary.LittleEndian.Uint16(ico[2:4]); got != 1 {
		t.Errorf("тип ICONDIR = %d, ожидался 1", got)
	}
	if ico[6] != iconSize || ico[7] != iconSize {
		t.Errorf("размеры = %dx%d, ожидались %dx%d", ico[6], ico[7], iconSize, iconSize)
	}
	if got := binary.LittleEndian.Uint32(ico[18:22]); got != 22 {
		t.Errorf("смещение данных = %d, ожидалось 22", got)
	}

	// Логотип должен реально декодироваться, иначе останется серая заглушка.
	baseOnce.Do(loadBase)
	if allSame(basePix) {
		t.Error("встроенный логотип не декодировался: иконка одноцветная")
	}

	// ICON_DUMP=path — сохранить иконку для визуальной проверки.
	if path := os.Getenv("ICON_DUMP"); path != "" {
		if err := os.WriteFile(path, ico, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("иконка сохранена в %s", path)
	}
}

func allSame(pix []byte) bool {
	for i := 4; i < len(pix); i += 4 {
		if pix[i] != pix[0] || pix[i+1] != pix[1] || pix[i+2] != pix[2] {
			return false
		}
	}
	return true
}
