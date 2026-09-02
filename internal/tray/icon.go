package tray

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"image"
	"image/png"
	"math"
	"sync"
)

// iconPNG — логотип приложения (обрезан и уменьшен до 128x128 из icon.jpg).
//
//go:embed assets/icon.png
var iconPNG []byte

// iconSize — сторона иконки трея в пикселях.
const iconSize = 32

var (
	baseOnce sync.Once
	basePix  []byte // BGRA, iconSize x iconSize, строки сверху вниз
)

// Icon возвращает иконку трея в формате ICO: логотип приложения с цветной
// точкой-индикатором в правом нижнем углу.
//
// Точка нужна потому, что в 32x32 неоновый логотип нечитаем — по нему не понять,
// что происходит. Точка показывает тот же цвет, что сейчас горит на ленте.
func Icon(r, g, b uint8) []byte { return buildICO(&dot{r, g, b}) }

// IconPlain — логотип без индикатора (пока состояние неизвестно).
func IconPlain() []byte { return buildICO(nil) }

type dot struct{ r, g, b uint8 }

func buildICO(d *dot) []byte {
	baseOnce.Do(loadBase)

	pix := make([]byte, len(basePix))
	copy(pix, basePix)
	if d != nil {
		drawDot(pix, d)
	}

	// XOR-маска в ICO хранится снизу вверх.
	xor := make([]byte, 0, len(pix))
	stride := iconSize * 4
	for y := iconSize - 1; y >= 0; y-- {
		xor = append(xor, pix[y*stride:(y+1)*stride]...)
	}
	andMask := make([]byte, iconSize*4) // не используется, но обязан присутствовать

	var img bytes.Buffer
	// BITMAPINFOHEADER: высота удвоена — так требует формат ICO (XOR + AND).
	writeLE(&img, uint32(40), uint32(iconSize), uint32(iconSize*2))
	writeLE(&img, uint16(1), uint16(32))
	writeLE(&img, uint32(0), uint32(len(xor)+len(andMask)))
	writeLE(&img, uint32(0), uint32(0), uint32(0), uint32(0))
	img.Write(xor)
	img.Write(andMask)

	var out bytes.Buffer
	writeLE(&out, uint16(0), uint16(1), uint16(1)) // ICONDIR: тип 1 = икона, 1 изображение
	out.Write([]byte{iconSize, iconSize, 0, 0})    // размеры, палитра, резерв
	writeLE(&out, uint16(1), uint16(32))           // плоскости, бит на пиксель
	writeLE(&out, uint32(img.Len()), uint32(22))   // размер и смещение данных
	out.Write(img.Bytes())
	return out.Bytes()
}

// loadBase декодирует встроенный логотип и ужимает его до iconSize усреднением.
// При любой ошибке остаётся серый квадрат — трей без иконки хуже, чем с плохой.
func loadBase() {
	basePix = make([]byte, iconSize*iconSize*4)
	for i := 0; i < len(basePix); i += 4 {
		basePix[i], basePix[i+1], basePix[i+2], basePix[i+3] = 60, 56, 52, 255
	}

	src, err := png.Decode(bytes.NewReader(iconPNG))
	if err != nil {
		return
	}
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw == 0 || sh == 0 {
		return
	}

	for y := 0; y < iconSize; y++ {
		for x := 0; x < iconSize; x++ {
			x0, x1 := b.Min.X+x*sw/iconSize, b.Min.X+(x+1)*sw/iconSize
			y0, y1 := b.Min.Y+y*sh/iconSize, b.Min.Y+(y+1)*sh/iconSize
			cr, cg, cb := average(src, x0, y0, max(x1, x0+1), max(y1, y0+1))
			i := (y*iconSize + x) * 4
			basePix[i], basePix[i+1], basePix[i+2], basePix[i+3] = cb, cg, cr, 255
		}
	}
}

func average(src image.Image, x0, y0, x1, y1 int) (uint8, uint8, uint8) {
	var sr, sg, sb, n uint64
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			r, g, b, _ := src.At(x, y).RGBA()
			sr += uint64(r >> 8)
			sg += uint64(g >> 8)
			sb += uint64(b >> 8)
			n++
		}
	}
	if n == 0 {
		return 0, 0, 0
	}
	return uint8(sr / n), uint8(sg / n), uint8(sb / n)
}

// drawDot рисует индикатор состояния в правом нижнем углу, с тёмной обводкой,
// чтобы светлый цвет не сливался с фоном логотипа.
func drawDot(pix []byte, d *dot) {
	const (
		cx     = float64(iconSize) - 9.5
		cy     = float64(iconSize) - 9.5
		radius = 7.5
		ring   = 1.5
	)
	for y := 0; y < iconSize; y++ {
		for x := 0; x < iconSize; x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist > radius+1 {
				continue
			}
			// Внутри — цвет состояния, по краю — тёмная окантовка.
			fill := clamp01(radius - ring - dist)
			edge := clamp01(radius - dist)
			i := (y*iconSize + x) * 4
			pix[i] = blend(pix[i], d.b, fill, edge)
			pix[i+1] = blend(pix[i+1], d.g, fill, edge)
			pix[i+2] = blend(pix[i+2], d.r, fill, edge)
		}
	}
}

// blend кладёт поверх фона тёмную окантовку (по edge), а затем цвет (по fill).
func blend(bg, c uint8, fill, edge float64) uint8 {
	v := float64(bg)*(1-edge) + 20*edge
	v = v*(1-fill) + float64(c)*fill
	return uint8(clamp01(v/255) * 255)
}

func writeLE(w *bytes.Buffer, vals ...any) {
	for _, v := range vals {
		_ = binary.Write(w, binary.LittleEndian, v)
	}
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
