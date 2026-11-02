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
	widgetBgColor       = "#3A3A3C"
	widgetCardColor     = "#1AFFFFFF"
	widgetTitleColor    = "#FFB3B3B3"
	widgetValueColor    = "#FFFFFFFF"
	widgetSubtitleColor = "#FFB3B3B3"
	widgetUpdatedColor  = "#FF8A8A8A"
)

var widgetPreviewDensities = []struct {
	Name string
	W    int
	H    int
}{
	// 4x3 小组件在 5x5 桌面网格里是竖长的（约 515x740），预览图不能再用
	// 1x1 时代的 250x110 宽条；否则选择器里的预览和桌面实际效果差很多。
	{"mdpi", 250, 360},
	{"hdpi", 375, 540},
	{"xhdpi", 500, 720},
	{"xxhdpi", 750, 1080},
	{"xxxhdpi", 1000, 1440},
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
	writeTextView(&b, "widget_title", 16, true, widgetTitleColor, "gone", "match_parent")
	writeTextView(&b, "widget_updated", 11, false, widgetUpdatedColor, "gone", "match_parent")
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
		// 标题占固定宽度，右侧 value/subtitle 上下两行：value 和副标题都完整显示，
		// 不再把两者拼成一行后被省略号截断。
		writeTextView(&b, name+"_title", 12, true, widgetTitleColor, "visible", "54dp")
		b.WriteString(`        <LinearLayout xmlns:android="http://schemas.android.com/apk/res/android"` + "\n")
		b.WriteString(`            android:layout_width="0dp"` + "\n")
		b.WriteString(`            android:layout_height="wrap_content"` + "\n")
		b.WriteString(`            android:layout_weight="1"` + "\n")
		b.WriteString(`            android:orientation="vertical">` + "\n")
		writeTextView(&b, name+"_value", 20, true, widgetValueColor, "visible", "match_parent")
		writeTextView(&b, name+"_subtitle", 11, false, widgetSubtitleColor, "gone", "match_parent")
		b.WriteString("        </LinearLayout>\n")
		b.WriteString("    </LinearLayout>\n")
	}
	b.WriteString("</LinearLayout>\n")
	return writeResFile(resDir, "layout/berba_widget.xml", b.String())
}

// writeTextView 输出一个单行 TextView。
//
// width 传 "match_parent" 或固定宽度（行标题用 54dp）。所有文本都限制一行并在末尾省略，
// 避免中文长标题或很长的 value 把固定 5 行的布局撑开；副标题独立一行后不再和 value 抢宽度。
// 纵向根布局的直接子节点（widget_title / widget_updated）不要加 layout_weight：纵向
// LinearLayout 里带 weight 的子节点会分到剩余高度，两行标题各吸走一半空间，把卡片挤到下面。
func writeTextView(b *strings.Builder, id string, size int, bold bool, textColor, visibility, width string) {
	fmt.Fprintf(b, "    <TextView\n        android:id=\"@+id/%s\"\n", id)
	fmt.Fprintf(b, "        android:layout_width=\"%s\"\n", width)
	b.WriteString(`        android:layout_height="wrap_content"` + "\n")
	fmt.Fprintf(b, `        android:textColor="%s"`+"\n", textColor)
	fmt.Fprintf(b, `        android:textSize="%dsp"`+"\n", size)
	if bold {
		b.WriteString(`        android:textStyle="bold"` + "\n")
	}
	b.WriteString(`        android:ellipsize="end"` + "\n")
	b.WriteString(`        android:maxLines="1"` + "\n")
	fmt.Fprintf(b, `        android:visibility="%s"`+"\n", visibility)
	b.WriteString("        android:text=\"\">\n")
	b.WriteString("    </TextView>\n")
}

// GenerateWidgetInfo 生成 res/xml/berba_widget_info.xml。
func GenerateWidgetInfo(resDir string) error {
	content := `<?xml version="1.0" encoding="utf-8"?>` + "\n" +
		`<appwidget-provider xmlns:android="http://schemas.android.com/apk/res/android"` + "\n" +
		// 固定 5 行 + 标题 + 时间，默认占 4x3 格。minHeight 由部分 launcher
		// （AOSP Launcher3）直接换算成行数，250dp 会被算成 5 行、铺满整屏；
		// 150dp 在这类 launcher 上是 3 行，和 targetCellHeight 保持一致。
		`    android:minWidth="250dp"` + "\n" +
		`    android:minHeight="150dp"` + "\n" +
		// Android 12+ 用单元格数决定首次添加的尺寸；1x1 会让小组件只分到
		// 一个格子，五行内容全部被裁掉，看起来就是「桌面上显示不出来」。
		`    android:targetCellWidth="4"` + "\n" +
		`    android:targetCellHeight="3"` + "\n" +
		`    android:updatePeriodMillis="0"` + "\n" +
		`    android:initialLayout="@layout/berba_widget"` + "\n" +
		`    android:resizeMode="horizontal|vertical"` + "\n" +
		`    android:widgetCategory="home_screen"` + "\n" +
		// 预览必须用静态图：berba_widget 的标题/卡片初始都是 gone，直接拿它当
		// previewLayout 会得到一个空白深色方块。
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
//
// 预览按 4x3 的实际比例画出标题、时间和 5 行卡片槽位，而不是几根抽象横条，
// 这样选择器里看到的形状和桌面上的小组件一致。
func GenerateWidgetPreview(resDir string) error {
	for _, d := range widgetPreviewDensities {
		img := image.NewNRGBA(image.Rect(0, 0, d.W, d.H))
		w := float64(d.W)
		h := float64(d.H)

		fillRoundRect(img, 1, 1, w-2, h-2, w*0.05, parseColor(widgetBgColor))

		pad := w * 0.032
		fillRoundRect(img, pad, h*0.033, pad+w*0.38, h*0.033+h*0.042,
			w*0.012, parseColor(widgetTitleColor))
		fillRoundRect(img, pad, h*0.093, pad+w*0.22, h*0.093+h*0.022,
			w*0.008, parseColor(widgetUpdatedColor))

		rowTop := h * 0.17
		rowH := h * 0.105
		gap := h * 0.017
		cardColor := parseColor(widgetCardColor)
		valueColors := []color.NRGBA{
			parseColor(widgetValueColor),
			{R: 0x3D, G: 0xDC, B: 0x84, A: 0xFF},
			parseColor(widgetValueColor),
			{R: 0xFF, G: 0x8F, B: 0xB1, A: 0xFF},
			{R: 0xFF, G: 0xA7, B: 0x26, A: 0xFF},
		}
		for i := 0; i < widgetRowCount; i++ {
			top := rowTop + float64(i)*(rowH+gap)
			fillRoundRect(img, pad, top, w-pad, top+rowH, w*0.018, cardColor)
			// 左侧标题、右侧 value + subtitle 两行，和实际布局一致。
			fillRoundRect(img, pad+w*0.025, top+rowH*0.38,
				pad+w*0.025+w*0.17, top+rowH*0.62, w*0.012,
				parseColor(widgetTitleColor))
			fillRoundRect(img, w*0.29, top+rowH*0.20,
				w*0.29+w*0.46, top+rowH*0.45, w*0.014,
				valueColors[i])
			fillRoundRect(img, w*0.29, top+rowH*0.57,
				w*0.29+w*0.34, top+rowH*0.70, w*0.010,
				parseColor(widgetSubtitleColor))
		}

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
	var a, r, g, b uint8 = 0xFF, 0, 0, 0
	var err error
	if len(hex) == 8 {
		// Android 资源用 #AARRGGBB；预览图必须保留 alpha，否则半透明卡片会被画成纯白。
		_, err = fmt.Sscanf(hex, "%02x%02x%02x%02x", &a, &r, &g, &b)
	} else {
		_, err = fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	}
	if err != nil {
		return color.NRGBA{R: 0x3A, G: 0x3A, B: 0x3C, A: 0xFF}
	}
	return color.NRGBA{R: r, G: g, B: b, A: a}
}

func writeResFile(resDir, rel, content string) error {
	full := filepath.Join(resDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(content), 0o644)
}
