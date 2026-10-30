package resources

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
)

// 小组件固定渲染 5 个行槽位；多余的卡片截断并记日志（见 WidgetController）。
const widgetRowCount = 5

// 卡片主题色：与 RemoteViews 文本默认色保持对比，深浅色 launcher 下都可读。
const (
	widgetBgColor      = "#3A3A3C"
	widgetCardColor    = "#1AFFFFFF"
	widgetTitleColor   = "#FFB3B3B3"
	widgetValueColor   = "#FFFFFFFF"
	widgetUpdatedColor = "#FF8A8A8A"
)

var widgetPreviewDensities = []struct {
	Name string
	W    int
	H    int
}{
	{"mdpi", 250, 110},
	{"hdpi", 375, 165},
	{"xhdpi", 500, 220},
	{"xxhdpi", 750, 330},
	{"xxxhdpi", 1000, 440},
}

// GenerateWidgetLayout 生成 res/layout/berba_widget.xml：固定 5 行槽位。
func GenerateWidgetLayout(resDir string) error {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n")
	b.WriteString(`<LinearLayout xmlns:android="http://schemas.android.com/apk/res/android"` + "\n")
	b.WriteString(`    android:id="@+id/widget_root"` + "\n")
	b.WriteString(`    android:layout_width="match_parent"` + "\n")
	b.WriteString(`    android:layout_height="match_parent"` + "\n")
	b.WriteString(`    android:orientation="vertical"` + "\n")
	b.WriteString(`    android:padding="8dp"` + "\n")
	b.WriteString(`    android:background="@drawable/berba_widget_bg">` + "\n")
	writeTextView(&b, "widget_title", 16, true, widgetTitleColor, "gone")
	writeTextView(&b, "widget_updated", 11, false, widgetUpdatedColor, "gone")
	for i := 0; i < widgetRowCount; i++ {
		name := fmt.Sprintf("widget_row%d", i)
		b.WriteString(`    <LinearLayout xmlns:android="http://schemas.android.com/apk/res/android"` + "\n")
		fmt.Fprintf(&b, `        android:id="@+id/%s"`+"\n", name)
		b.WriteString(`        android:layout_width="match_parent"` + "\n")
		b.WriteString(`        android:layout_height="wrap_content"` + "\n")
		b.WriteString(`        android:layout_marginTop="6dp"` + "\n")
		b.WriteString(`        android:orientation="horizontal"` + "\n")
		b.WriteString(`        android:gravity="center_vertical"` + "\n")
		b.WriteString(`        android:padding="4dp"` + "\n")
		b.WriteString(`        android:background="@drawable/berba_widget_card"` + "\n")
		b.WriteString(`        android:visibility="gone">` + "\n")
		writeTextView(&b, name+"_title", 12, true, widgetTitleColor, "visible")
		writeTextView(&b, name+"_value", 22, true, widgetValueColor, "visible")
		b.WriteString("    </LinearLayout>\n")
	}
	b.WriteString("</LinearLayout>\n")
	return writeResFile(resDir, "layout/berba_widget.xml", b.String())
}

func writeTextView(b *strings.Builder, id string, size int, bold bool, textColor, visibility string) {
	isTitle := id == "widget_title" || id == "widget_updated" || strings.HasSuffix(id, "_title")
	isValue := strings.HasSuffix(id, "_value")
	fmt.Fprintf(b, "    <TextView\n        android:id=\"@+id/%s\"\n", id)
	b.WriteString(`        android:layout_width="0dp"` + "\n")
	b.WriteString(`        android:layout_height="wrap_content"` + "\n")
	b.WriteString(`        android:layout_weight="1"` + "\n")
	fmt.Fprintf(b, `        android:textColor="%s"`+"\n", textColor)
	fmt.Fprintf(b, `        android:textSize="%dsp"`+"\n", size)
	if bold {
		b.WriteString(`        android:textStyle="bold"` + "\n")
	}
	if isTitle || isValue {
		b.WriteString(`        android:ellipsize="end"` + "\n")
		b.WriteString(`        android:maxLines="1"` + "\n")
	}
	fmt.Fprintf(b, `        android:visibility="%s"`+"\n", visibility)
	b.WriteString("        android:text=\"\">\n")
	b.WriteString("    </TextView>\n")
}

// GenerateWidgetInfo 生成 res/xml/berba_widget_info.xml。
func GenerateWidgetInfo(resDir string) error {
	content := `<?xml version="1.0" encoding="utf-8"?>` + "\n" +
		`<appwidget-provider xmlns:android="http://schemas.android.com/apk/res/android"` + "\n" +
		`    android:minWidth="250dp"` + "\n" +
		`    android:minHeight="110dp"` + "\n" +
		`    android:targetCellWidth="1"` + "\n" +
		`    android:targetCellHeight="1"` + "\n" +
		`    android:updatePeriodMillis="0"` + "\n" +
		`    android:initialLayout="@layout/berba_widget"` + "\n" +
		`    android:resizeMode="horizontal|vertical"` + "\n" +
		`    android:widgetCategory="home_screen"` + "\n" +
		`    android:previewLayout="@layout/berba_widget"` + "\n" +
		`    android:previewImage="@drawable/berba_widget_preview">` + "\n" +
		"</appwidget-provider>\n"
	return writeResFile(resDir, "xml/berba_widget_info.xml", content)
}

// GenerateWidgetDrawable 生成圆角背景与卡片 drawable。
func GenerateWidgetDrawable(resDir string) error {
	bg := `<?xml version="1.0" encoding="utf-8"?>` + "\n" +
		`<shape xmlns:android="http://schemas.android.com/apk/res/android" android:shape="rectangle">` + "\n" +
		fmt.Sprintf("    <solid android:color=\"%s\"/>\n", widgetBgColor) +
		`    <corners android:radius="12dp"/>` + "\n" +
		"</shape>\n"
	card := `<?xml version="1.0" encoding="utf-8"?>` + "\n" +
		`<shape xmlns:android="http://schemas.android.com/apk/res/android" android:shape="rectangle">` + "\n" +
		fmt.Sprintf("    <solid android:color=\"%s\"/>\n", widgetCardColor) +
		`    <corners android:radius="8dp"/>` + "\n" +
		"</shape>\n"
	if err := writeResFile(resDir, "drawable/berba_widget_bg.xml", bg); err != nil {
		return err
	}
	return writeResFile(resDir, "drawable/berba_widget_card.xml", card)
}

// GenerateWidgetPreview 生成各密度的预览图，供 launcher 选择器展示。
func GenerateWidgetPreview(resDir string) error {
	for _, d := range widgetPreviewDensities {
		img := image.NewNRGBA(image.Rect(0, 0, d.W, d.H))
		fillRoundRect(img, 1, 1, float64(d.W-2), float64(d.H-2), float64(d.W)*0.06,
			parseColor(widgetBgColor))
		bar := float64(d.W) * 0.42
		fillRoundRect(img, float64(d.W)*0.10, float64(d.H)*0.16,
			float64(d.W)*0.10+bar, float64(d.H)*0.16+float64(d.H)*0.08,
			float64(d.W)*0.02, parseColor(widgetTitleColor))
		fillRoundRect(img, float64(d.W)*0.10, float64(d.H)*0.40,
			float64(d.W)*0.10+bar*0.72, float64(d.H)*0.40+float64(d.H)*0.13,
			float64(d.W)*0.02, parseColor(widgetValueColor))
		fillRoundRect(img, float64(d.W)*0.10, float64(d.H)*0.62,
			float64(d.W)*0.10+bar*0.55, float64(d.H)*0.62+float64(d.H)*0.13,
			float64(d.W)*0.02, parseColor(widgetCardColor))
		dir := filepath.Join(resDir, "drawable-"+d.Name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		var buf bytes.Buffer
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		if err := enc.Encode(&buf, img); err != nil {
			return fmt.Errorf("编码小组件预览失败: %w", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "berba_widget_preview.png"), buf.Bytes(), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func parseColor(hexColor string) color.NRGBA {
	hex := strings.TrimPrefix(hexColor, "#")
	if len(hex) == 8 {
		hex = hex[2:]
	}
	var r, g, b uint8
	if _, err := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b); err != nil {
		return color.NRGBA{R: 0x3A, G: 0x3A, B: 0x3C, A: 0xFF}
	}
	return color.NRGBA{R: r, G: g, B: b, A: 0xFF}
}

func writeResFile(resDir, rel, content string) error {
	full := filepath.Join(resDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0o644)
}
