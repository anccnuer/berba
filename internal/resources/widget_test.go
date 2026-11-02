package resources

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func generateWidgetResources(t *testing.T, resDir string) {
	t.Helper()
	if err := GenerateWidgetLayout(resDir); err != nil {
		t.Fatal(err)
	}
	if err := GenerateWidgetInfo(resDir); err != nil {
		t.Fatal(err)
	}
	if err := GenerateWidgetDrawable(resDir); err != nil {
		t.Fatal(err)
	}
	if err := GenerateWidgetPreview(resDir); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateWidgetResources(t *testing.T) {
	resDir := t.TempDir()
	generateWidgetResources(t, resDir)

	want := []string{
		"layout/berba_widget.xml",
		"xml/berba_widget_info.xml",
		"drawable/berba_widget_bg.xml",
		"drawable/berba_widget_card.xml",
		"drawable-mdpi/berba_widget_preview.png",
		"drawable-xxxhdpi/berba_widget_preview.png",
	}
	for _, rel := range want {
		info, err := os.Stat(filepath.Join(resDir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		if info.Size() == 0 {
			t.Fatalf("%s is empty", rel)
		}
	}
}

func TestWidgetInfoXML(t *testing.T) {
	resDir := t.TempDir()
	if err := GenerateWidgetInfo(resDir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(resDir, "xml", "berba_widget_info.xml"))
	if err != nil {
		t.Fatal(err)
	}
	attrs := map[string]string{}
	root := ""
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("widget_info.xml is not valid XML: %v\n%s", err, data)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if root == "" {
			root = start.Name.Local
		}
		for _, a := range start.Attr {
			// encoding/xml 把前缀解析为命名空间 URL；xmlns 声明本身跳过。
			if a.Name.Space == "xmlns" {
				continue
			}
			attrs[a.Name.Local] = a.Value
		}
	}
	if root != "appwidget-provider" {
		t.Fatalf("root element = %q", root)
	}
	for _, key := range []string{"minWidth", "minHeight", "updatePeriodMillis", "initialLayout",
		"resizeMode", "widgetCategory", "previewImage"} {
		if _, ok := attrs[key]; !ok {
			t.Fatalf("widget_info.xml missing android:%s: %s", key, data)
		}
	}
	if attrs["updatePeriodMillis"] != "0" {
		t.Fatalf("updatePeriodMillis = %q, want 0（由 Lua 主动推送）", attrs["updatePeriodMillis"])
	}
	if attrs["initialLayout"] != "@layout/berba_widget" {
		t.Fatalf("initialLayout = %q", attrs["initialLayout"])
	}
	if attrs["widgetCategory"] != "home_screen" {
		t.Fatalf("widgetCategory = %q", attrs["widgetCategory"])
	}
	if attrs["minWidth"] != "250dp" || attrs["minHeight"] != "150dp" {
		t.Fatalf("min size = %q x %q", attrs["minWidth"], attrs["minHeight"])
	}
	// 五行卡片需要 4x3 的默认尺寸；targetCell 1x1 会让新添加的小组件只剩一格。
	if attrs["targetCellWidth"] != "4" || attrs["targetCellHeight"] != "3" {
		t.Fatalf("target cells = %q x %q", attrs["targetCellWidth"], attrs["targetCellHeight"])
	}
	if attrs["previewImage"] != "@drawable/berba_widget_preview" {
		t.Fatalf("previewImage = %q", attrs["previewImage"])
	}
	// 初始布局的文本都是 gone，直接当 previewLayout 会显示空白。
	if _, ok := attrs["previewLayout"]; ok {
		t.Fatalf("不应设置 previewLayout（会覆盖 previewImage 并显示空白）: %s", data)
	}
}

func TestWidgetLayoutHasFiveRows(t *testing.T) {
	resDir := t.TempDir()
	if err := GenerateWidgetLayout(resDir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(resDir, "layout", "berba_widget.xml"))
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, want := range []string{
		`android:id="@+id/widget_root"`,
		`android:background="@drawable/berba_widget_bg"`,
		`android:id="@+id/widget_title"`,
		`android:id="@+id/widget_updated"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("layout missing %s:\n%s", want, out)
		}
	}
	// 固定 5 个行槽位：根布局 + 5 行 LinearLayout + 每行右侧的 value/subtitle 容器。
	if got := strings.Count(out, "<LinearLayout"); got != widgetRowCount*2+1 {
		t.Fatalf("LinearLayout count = %d, want %d\n%s", got, widgetRowCount*2+1, out)
	}
	for i := 0; i < widgetRowCount; i++ {
		row := fmt.Sprintf("widget_row%d", i)
		for _, suffix := range []string{"", "_title", "_value", "_subtitle"} {
			id := fmt.Sprintf(`android:id="@+id/%s%s"`, row, suffix)
			if !strings.Contains(out, id) {
				t.Fatalf("layout missing %s:\n%s", id, out)
			}
		}
	}
	if got := strings.Count(out, `@+id/widget_row`); got != widgetRowCount*4 {
		t.Fatalf("row slot ids = %d, want %d\n%s", got, widgetRowCount*4, out)
	}
	// visibility 是枚举属性：写错值会在 aapt2 链接期直接失败。
	for _, line := range strings.Split(out, "\n") {
		const marker = `android:visibility="`
		index := strings.Index(line, marker)
		if index < 0 {
			continue
		}
		rest := line[index+len(marker):]
		if visibilityAllowed(rest) {
			continue
		}
		t.Fatalf("visibility 取值非法: %q\n%s", rest, out)
	}
	// 回归：widget_title / widget_updated 是纵向根布局的直接子节点。
	// 给它们加 layout_weight 会分到纵向剩余高度，两行各吸走一半空间变成空盒子，
	// 卡片被挤到下面——真机上看到的就是「标题位置一大块空白」。
	// 只有横向行里的 title/value 才需要 0dp + weight=1 平分宽度。
	for _, id := range []string{"widget_title", "widget_updated"} {
		block := widgetTextViewBlock(t, out, id)
		if strings.Contains(block, "layout_weight") {
			t.Fatalf("%s 是纵向根布局子节点，不应带 layout_weight:\n%s", id, block)
		}
		if !strings.Contains(block, `android:layout_width="match_parent"`) {
			t.Fatalf("%s 应使用 match_parent 宽度:\n%s", id, block)
		}
	}
	// 行标题固定 54dp；value/subtitle 共用右侧剩余宽度，但各自占一行。
	if got := strings.Count(out, `android:layout_weight="1"`); got != widgetRowCount {
		t.Fatalf("row body weight count = %d, want %d\n%s", got, widgetRowCount, out)
	}
	for i := 0; i < widgetRowCount; i++ {
		titleBlock := widgetTextViewBlock(t, out, fmt.Sprintf("widget_row%d_title", i))
		if !strings.Contains(titleBlock, `android:layout_width="54dp"`) ||
			strings.Contains(titleBlock, "layout_weight") {
			t.Fatalf("widget_row%d_title 应固定 54dp 且不带 weight:\n%s", i, titleBlock)
		}
		for _, suffix := range []string{"_value", "_subtitle"} {
			block := widgetTextViewBlock(t, out, fmt.Sprintf("widget_row%d%s", i, suffix))
			if !strings.Contains(block, `android:layout_width="match_parent"`) ||
				strings.Contains(block, "layout_weight") {
				t.Fatalf("widget_row%d%s 应使用 match_parent 且不带 weight:\n%s", i, suffix, block)
			}
		}
	}
}

// widgetTextViewBlock 取出指定 id 的 <TextView>…</TextView> 片段，便于逐属性断言。
func widgetTextViewBlock(t *testing.T, layout, id string) string {
	t.Helper()
	marker := fmt.Sprintf(`android:id="@+id/%s"`, id)
	start := strings.Index(layout, marker)
	if start < 0 {
		t.Fatalf("layout 里没有 %s\n%s", id, layout)
	}
	end := strings.Index(layout[start:], "</TextView>")
	if end < 0 {
		t.Fatalf("%s 的 TextView 没有闭合\n%s", id, layout)
	}
	return layout[start : start+end]
}

// visibilityAllowed 校验 visibility 取值是枚举成员（`rest` 形如 `gone"`）。
func visibilityAllowed(rest string) bool {
	for _, allowed := range []string{"visible\"", "gone\"", "invisible\""} {
		if strings.HasPrefix(rest, allowed) {
			return true
		}
	}
	return false
}

func TestWidgetDrawables(t *testing.T) {
	resDir := t.TempDir()
	if err := GenerateWidgetDrawable(resDir); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]string{
		"berba_widget_bg.xml":   {"<shape", "<corners", widgetBgColor},
		"berba_widget_card.xml": {"<shape", "<corners", widgetCardColor},
	} {
		data, err := os.ReadFile(filepath.Join(resDir, "drawable", name))
		if err != nil {
			t.Fatal(err)
		}
		out := string(data)
		for _, s := range want {
			if !strings.Contains(out, s) {
				t.Fatalf("%s missing %q:\n%s", name, s, out)
			}
		}
	}
}

func TestWidgetPreviewSizes(t *testing.T) {
	resDir := t.TempDir()
	if err := GenerateWidgetPreview(resDir); err != nil {
		t.Fatal(err)
	}
	for _, d := range widgetPreviewDensities {
		f, err := os.Open(filepath.Join(resDir, "drawable-"+d.Name, "berba_widget_preview.png"))
		if err != nil {
			t.Fatalf("%s: %v", d.Name, err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatalf("%s: %v", d.Name, err)
		}
		if b := img.Bounds(); b.Dx() != d.W || b.Dy() != d.H {
			t.Fatalf("%s: got %dx%d, want %dx%d", d.Name, b.Dx(), b.Dy(), d.W, d.H)
		}
	}
}

func TestParseColorKeepsAlpha(t *testing.T) {
	card := parseColor("#1AFFFFFF")
	if card.A != 0x1A || card.R != 0xFF || card.G != 0xFF || card.B != 0xFF {
		t.Fatalf("parseColor(#1AFFFFFF) = %+v, want alpha 0x1A and white", card)
	}
	opaque := parseColor("#3A3A3C")
	if opaque.A != 0xFF {
		t.Fatalf("parseColor(#3A3A3C).A = %#x, want 0xFF", opaque.A)
	}
}

func TestWidgetResourcesDeterministic(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	generateWidgetResources(t, first)
	generateWidgetResources(t, second)

	rels := []string{
		"layout/berba_widget.xml",
		"xml/berba_widget_info.xml",
		"drawable/berba_widget_bg.xml",
		"drawable/berba_widget_card.xml",
	}
	for _, rel := range rels {
		a, err := os.ReadFile(filepath.Join(first, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(second, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != string(b) {
			t.Fatalf("%s differs between builds", rel)
		}
	}
	// 预览图是确定性的位图编码：两次构建必须逐字节一致。
	a, err := os.ReadFile(filepath.Join(first, "drawable-xxhdpi", "berba_widget_preview.png"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(second, "drawable-xxhdpi", "berba_widget_preview.png"))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("preview png differs between builds")
	}
}
