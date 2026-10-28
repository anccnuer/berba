package resources

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
)

var densities = []struct {
	Name string
	Size int
}{
	{"mdpi", 48},
	{"hdpi", 72},
	{"xhdpi", 96},
	{"xxhdpi", 144},
	{"xxxhdpi", 192},
}

// 自适应图标：108dp 画布，内容限制在中心 72dp 安全区。
var adaptiveDensities = []struct {
	Name string
	Size int
}{
	{"mdpi", 108},
	{"hdpi", 162},
	{"xhdpi", 216},
	{"xxhdpi", 324},
	{"xxxhdpi", 432},
}

func Generate(resDir, iconPath, appName string) error {
	var src image.Image
	if iconPath == "" {
		src = defaultIcon(appName, 512)
	} else {
		f, err := os.Open(iconPath)
		if err != nil {
			return fmt.Errorf("打开图标失败: %w", err)
		}
		defer f.Close()
		img, _, err := image.Decode(f)
		if err != nil {
			return fmt.Errorf("图标格式不支持（请使用 PNG/JPG）: %w", err)
		}
		src = img
	}

	nrgba := toNRGBA(src)
	square := centerSquare(nrgba)
	for _, d := range densities {
		resized := resizeBilinear(square, d.Size, d.Size)
		dir := filepath.Join(resDir, "mipmap-"+d.Name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		var buf bytes.Buffer
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		if err := enc.Encode(&buf, resized); err != nil {
			return fmt.Errorf("编码图标失败: %w", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "ic_launcher.png"), buf.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return writeAdaptive(resDir, square)
}

func writeAdaptive(resDir string, square *image.NRGBA) error {
	bg := averageBorderColor(square)
	for _, d := range adaptiveDensities {
		inset := d.Size / 6
		content := d.Size - inset*2
		canvas := image.NewNRGBA(image.Rect(0, 0, d.Size, d.Size))
		scaled := resizeBilinear(square, content, content)
		draw.Draw(canvas, image.Rect(inset, inset, inset+content, inset+content), scaled, image.Point{}, draw.Over)

		dir := filepath.Join(resDir, "mipmap-"+d.Name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		var buf bytes.Buffer
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		if err := enc.Encode(&buf, canvas); err != nil {
			return fmt.Errorf("编码自适应图标失败: %w", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "ic_launcher_foreground.png"), buf.Bytes(), 0o644); err != nil {
			return err
		}
	}

	valuesDir := filepath.Join(resDir, "values")
	if err := os.MkdirAll(valuesDir, 0o755); err != nil {
		return err
	}
	colors := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<resources>
    <color name="ic_launcher_background">#%02X%02X%02X</color>
</resources>
`, bg.R, bg.G, bg.B)
	if err := os.WriteFile(filepath.Join(valuesDir, "colors.xml"), []byte(colors), 0o644); err != nil {
		return err
	}

	anydpi := filepath.Join(resDir, "mipmap-anydpi-v26")
	if err := os.MkdirAll(anydpi, 0o755); err != nil {
		return err
	}
	adaptive := `<?xml version="1.0" encoding="utf-8"?>
<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@color/ic_launcher_background"/>
    <foreground android:drawable="@mipmap/ic_launcher_foreground"/>
</adaptive-icon>
`
	return os.WriteFile(filepath.Join(anydpi, "ic_launcher.xml"), []byte(adaptive), 0o644)
}

func averageBorderColor(src *image.NRGBA) color.NRGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	var r, g, b, n uint64
	sample := func(x, y int) {
		c := src.NRGBAAt(x, y)
		if c.A == 0 {
			return
		}
		r += uint64(c.R)
		g += uint64(c.G)
		b += uint64(c.B)
		n++
	}
	step := w / 64
	if step < 1 {
		step = 1
	}
	for x := 0; x < w; x += step {
		sample(x, 0)
		sample(x, h-1)
	}
	for y := 0; y < h; y += step {
		sample(0, y)
		sample(w-1, y)
	}
	if n == 0 {
		return color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	}
	return color.NRGBA{R: uint8(r / n), G: uint8(g / n), B: uint8(b / n), A: 0xFF}
}

func WriteStrings(resDir, appName string) error {
	dir := filepath.Join(resDir, "values")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	content := `<?xml version="1.0" encoding="utf-8"?>` + "\n" +
		`<resources>` + "\n" +
		`    <string name="app_name">` + xmlEscape(EscapeAndroidString(appName)) + `</string>` + "\n" +
		`</resources>` + "\n"
	return os.WriteFile(filepath.Join(dir, "strings.xml"), []byte(content), 0o644)
}

func EscapeAndroidString(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`'`, `\'`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
	)
	out := replacer.Replace(s)
	if strings.HasPrefix(out, "@") || strings.HasPrefix(out, "?") {
		out = `\` + out
	}
	return out
}

func xmlEscape(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	)
	return replacer.Replace(s)
}

func toNRGBA(src image.Image) *image.NRGBA {
	if n, ok := src.(*image.NRGBA); ok {
		return n
	}
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}

func centerSquare(src *image.NRGBA) *image.NRGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	side := w
	if h < side {
		side = h
	}
	ox := (w - side) / 2
	oy := (h - side) / 2
	// 复制到原点为 (0,0) 的新图，避免 SubImage 的 Min 偏移影响后续按索引取样。
	dst := image.NewNRGBA(image.Rect(0, 0, side, side))
	sp := src.Bounds().Min.Add(image.Point{X: ox, Y: oy})
	draw.Draw(dst, dst.Bounds(), src, sp, draw.Src)
	return dst
}

func resizeBilinear(src *image.NRGBA, dw, dh int) *image.NRGBA {
	sw := src.Bounds().Dx()
	sh := src.Bounds().Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	if sw == 0 || sh == 0 {
		return dst
	}
	scaleX := float64(sw) / float64(dw)
	scaleY := float64(sh) / float64(dh)
	for y := 0; y < dh; y++ {
		sy := (float64(y)+0.5)*scaleY - 0.5
		y0 := int(math.Floor(sy))
		fy := sy - float64(y0)
		if y0 < 0 {
			y0 = 0
			fy = 0
		}
		y1 := y0 + 1
		if y1 >= sh {
			y1 = sh - 1
		}
		for x := 0; x < dw; x++ {
			sx := (float64(x)+0.5)*scaleX - 0.5
			x0 := int(math.Floor(sx))
			fx := sx - float64(x0)
			if x0 < 0 {
				x0 = 0
				fx = 0
			}
			x1 := x0 + 1
			if x1 >= sw {
				x1 = sw - 1
			}
			c00 := src.NRGBAAt(x0, y0)
			c10 := src.NRGBAAt(x1, y0)
			c01 := src.NRGBAAt(x0, y1)
			c11 := src.NRGBAAt(x1, y1)
			blend := func(a, b, c, d uint8) uint8 {
				top := float64(a)*(1-fx) + float64(b)*fx
				bottom := float64(c)*(1-fx) + float64(d)*fx
				v := top*(1-fy) + bottom*fy
				if v < 0 {
					v = 0
				}
				if v > 255 {
					v = 255
				}
				return uint8(v + 0.5)
			}
			dst.SetNRGBA(x, y, color.NRGBA{
				R: blend(c00.R, c10.R, c01.R, c11.R),
				G: blend(c00.G, c10.G, c01.G, c11.G),
				B: blend(c00.B, c10.B, c01.B, c11.B),
				A: blend(c00.A, c10.A, c01.A, c11.A),
			})
		}
	}
	return dst
}

func defaultIcon(appName string, size int) *image.NRGBA {
	palette := []color.NRGBA{
		{0x4F, 0x46, 0xE5, 0xFF},
		{0x0F, 0x76, 0x6E, 0xFF},
		{0xEA, 0x58, 0x0C, 0xFF},
		{0x7C, 0x3A, 0xED, 0xFF},
		{0x02, 0x84, 0xC7, 0xFF},
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(appName))
	bg := palette[int(h.Sum32())%len(palette)]

	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetNRGBA(x, y, bg)
		}
	}

	s := float64(size)
	x0, y0 := 0.20*s, 0.24*s
	x1, y1 := 0.80*s, 0.76*s
	r := 0.07 * s
	fillRoundedRect(img, x0, y0, x1, y1, r, color.NRGBA{0xFF, 0xFF, 0xFF, 0xFF})

	dotR := 0.018 * s
	for i, cx := range []float64{0.30 * s, 0.355 * s, 0.41 * s} {
		col := []color.NRGBA{
			{0xEF, 0x53, 0x50, 0xFF},
			{0xFB, 0xBC, 0x05, 0xFF},
			{0x34, 0xA8, 0x53, 0xFF},
		}[i]
		fillCircle(img, cx, 0.34*s, dotR, col)
	}

	line := color.NRGBA{0x4F, 0x46, 0xE5, 0xB0}
	fillRoundedRect(img, 0.29*s, 0.43*s, 0.71*s, 0.47*s, 0.02*s, line)
	fillRoundedRect(img, 0.29*s, 0.53*s, 0.59*s, 0.57*s, 0.02*s, line)
	fillRoundedRect(img, 0.29*s, 0.63*s, 0.66*s, 0.67*s, 0.02*s, line)

	return img
}

func blendPixel(img *image.NRGBA, x, y int, c color.NRGBA) {
	if c.A == 0xFF {
		img.SetNRGBA(x, y, c)
		return
	}
	dst := img.NRGBAAt(x, y)
	sa := float64(c.A) / 255
	da := float64(dst.A) / 255
	oa := sa + da*(1-sa)
	if oa == 0 {
		img.SetNRGBA(x, y, color.NRGBA{})
		return
	}
	blend := func(sc, dc uint8) uint8 {
		v := (float64(sc)*sa + float64(dc)*da*(1-sa)) / oa
		if v > 255 {
			v = 255
		}
		return uint8(v + 0.5)
	}
	img.SetNRGBA(x, y, color.NRGBA{
		R: blend(c.R, dst.R),
		G: blend(c.G, dst.G),
		B: blend(c.B, dst.B),
		A: uint8(oa*255 + 0.5),
	})
}

func fillRoundRect(img *image.NRGBA, x0, y0, x1, y1, r float64, c color.NRGBA) {
	b := img.Bounds()
	for y := int(math.Floor(y0)); y <= int(math.Ceil(y1)); y++ {
		if y < b.Min.Y || y >= b.Max.Y {
			continue
		}
		for x := int(math.Floor(x0)); x <= int(math.Ceil(x1)); x++ {
			if x < b.Min.X || x >= b.Max.X {
				continue
			}
			px := float64(x) + 0.5
			py := float64(y) + 0.5
			cx := math.Max(x0+r, math.Min(px, x1-r))
			cy := math.Max(y0+r, math.Min(py, y1-r))
			dx := px - cx
			dy := py - cy
			if dx*dx+dy*dy <= r*r {
				blendPixel(img, x, y, c)
			}
		}
	}
}

func fillCircle(img *image.NRGBA, cx, cy, r float64, c color.NRGBA) {
	fillRoundRect(img, cx-r, cy-r, cx+r, cy+r, r, c)
}

func fillRoundedRect(img *image.NRGBA, x0, y0, x1, y1, r float64, c color.NRGBA) {
	fillRoundRect(img, x0, y0, x1, y1, r, c)
}
