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
	if attrs["minWidth"] != "250dp" || attrs["minHeight"] != "110dp" {
		t.Fatalf("min size = %q x %q", attrs["minWidth"], attrs["minHeight"])
	}
	if attrs["previewImage"] != "@drawable/berba_widget_preview" {
		t.Fatalf("previewImage = %q", attrs["previewImage"])
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
	// 固定 5 个行槽位：根布局 + 5 行 LinearLayout。
	if got := strings.Count(out, "<LinearLayout"); got != widgetRowCount+1 {
		t.Fatalf("LinearLayout count = %d, want %d\n%s", got, widgetRowCount+1, out)
	}
	for i := 0; i < widgetRowCount; i++ {
		row := fmt.Sprintf("widget_row%d", i)
		for _, suffix := range []string{"", "_title", "_value"} {
			id := fmt.Sprintf(`android:id="@+id/%s%s"`, row, suffix)
			if !strings.Contains(out, id) {
				t.Fatalf("layout missing %s:\n%s", id, out)
			}
		}
	}
	if got := strings.Count(out, `@+id/widget_row`); got != widgetRowCount*3 {
		t.Fatalf("row slot ids = %d, want %d\n%s", got, widgetRowCount*3, out)
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
