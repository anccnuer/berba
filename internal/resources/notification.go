package resources

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

var notificationDensities = []struct {
	Name string
	Size int
}{
	{"mdpi", 24},
	{"hdpi", 36},
	{"xhdpi", 48},
	{"xxhdpi", 72},
	{"xxxhdpi", 96},
}

// GenerateNotificationIcon 生成通知栏小图标：白色图形 + 透明背景。
func GenerateNotificationIcon(resDir string) error {
	for _, d := range notificationDensities {
		img := image.NewNRGBA(image.Rect(0, 0, d.Size, d.Size))
		drawBell(img)
		dir := filepath.Join(resDir, "drawable-"+d.Name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		var buf bytes.Buffer
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		if err := enc.Encode(&buf, img); err != nil {
			return fmt.Errorf("编码通知图标失败: %w", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "ic_stat_berba.png"), buf.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func drawBell(img *image.NRGBA) {
	s := float64(img.Bounds().Dx())
	white := color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	fillCircle(img, 0.50*s, 0.17*s, 0.045*s, white)
	fillCircle(img, 0.50*s, 0.37*s, 0.225*s, white)
	fillRoundedRect(img, 0.275*s, 0.37*s, 0.725*s, 0.66*s, 0.05*s, white)
	fillRoundedRect(img, 0.18*s, 0.63*s, 0.82*s, 0.71*s, 0.035*s, white)
	fillCircle(img, 0.50*s, 0.80*s, 0.07*s, white)
}
