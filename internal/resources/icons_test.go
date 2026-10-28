package resources

import (
	"encoding/xml"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rgbaAt(img image.Image, x, y int) color.NRGBA {
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

var densitySizes = map[string]int{
	"mipmap-mdpi":    48,
	"mipmap-hdpi":    72,
	"mipmap-xhdpi":   96,
	"mipmap-xxhdpi":  144,
	"mipmap-xxxhdpi": 192,
}

func TestGenerateDefaultIcon(t *testing.T) {
	resDir := t.TempDir()
	if err := Generate(resDir, "", "示例应用"); err != nil {
		t.Fatal(err)
	}
	for dir, size := range densitySizes {
		p := filepath.Join(resDir, dir, "ic_launcher.png")
		f, err := os.Open(p)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
			t.Fatalf("%s: got %dx%d, want %dx%d", dir, b.Dx(), b.Dy(), size, size)
		}
	}
}

func TestGenerateFromNonSquarePNG(t *testing.T) {
	dir := t.TempDir()
	src := image.NewNRGBA(image.Rect(0, 0, 300, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 300; x++ {
			src.Set(x, y, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	iconPath := filepath.Join(dir, "icon.png")
	f, err := os.Create(iconPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, src); err != nil {
		t.Fatal(err)
	}
	f.Close()

	resDir := filepath.Join(dir, "res")
	if err := Generate(resDir, iconPath, "test"); err != nil {
		t.Fatal(err)
	}
	f, err = os.Open(filepath.Join(resDir, "mipmap-xxxhdpi", "ic_launcher.png"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 192 || b.Dy() != 192 {
		t.Fatalf("got %dx%d", b.Dx(), b.Dy())
	}
	// 非方形图居中裁剪后必须保留内容，而不是整张透明。
	if c := rgbaAt(img, 96, 96); c.R != 255 || c.G != 0 || c.B != 0 || c.A != 255 {
		t.Fatalf("cropped pixel = %+v, want opaque red", c)
	}
}

func TestGenerateAdaptiveIcon(t *testing.T) {
	resDir := t.TempDir()
	if err := Generate(resDir, "", "示例应用"); err != nil {
		t.Fatal(err)
	}

	want := map[string]int{
		"mipmap-mdpi":    108,
		"mipmap-hdpi":    162,
		"mipmap-xhdpi":   216,
		"mipmap-xxhdpi":  324,
		"mipmap-xxxhdpi": 432,
	}
	for dir, size := range want {
		f, err := os.Open(filepath.Join(resDir, dir, "ic_launcher_foreground.png"))
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
			t.Fatalf("%s: got %dx%d, want %dx%d", dir, b.Dx(), b.Dy(), size, size)
		}
		if c := rgbaAt(img, 0, 0); c.A != 0 {
			t.Fatalf("%s: corner should be transparent, got %+v", dir, c)
		}
	}

	var colors struct {
		Colors []struct {
			Name  string `xml:"name,attr"`
			Value string `xml:",chardata"`
		} `xml:"color"`
	}
	data, err := os.ReadFile(filepath.Join(resDir, "values", "colors.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := xml.Unmarshal(data, &colors); err != nil {
		t.Fatalf("colors.xml is not valid XML: %v", err)
	}
	if len(colors.Colors) != 1 || colors.Colors[0].Name != "ic_launcher_background" ||
		!strings.HasPrefix(colors.Colors[0].Value, "#") {
		t.Fatalf("unexpected colors.xml: %s", data)
	}

	adaptive, err := os.ReadFile(filepath.Join(resDir, "mipmap-anydpi-v26", "ic_launcher.xml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"@color/ic_launcher_background", "@mipmap/ic_launcher_foreground", "adaptive-icon"} {
		if !strings.Contains(string(adaptive), want) {
			t.Fatalf("adaptive icon missing %q: %s", want, adaptive)
		}
	}
}

func TestGenerateRejectsBrokenIcon(t *testing.T) {
	dir := t.TempDir()
	iconPath := filepath.Join(dir, "icon.png")
	if err := os.WriteFile(iconPath, []byte("not a png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Generate(filepath.Join(dir, "res"), iconPath, "test"); err == nil {
		t.Fatal("expected error for broken icon")
	}
}

func TestWriteStringsEscapes(t *testing.T) {
	resDir := t.TempDir()
	name := `A&B <C> "quote" 'apos' \back`
	if err := WriteStrings(resDir, name); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(resDir, "values", "strings.xml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Strings []struct {
			Name  string `xml:"name,attr"`
			Value string `xml:",chardata"`
		} `xml:"string"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("strings.xml is not valid XML: %v\n%s", err, data)
	}
	if len(doc.Strings) != 1 || doc.Strings[0].Name != "app_name" {
		t.Fatalf("unexpected strings.xml: %s", data)
	}
	got := doc.Strings[0].Value
	got = strings.ReplaceAll(got, `\"`, `"`)
	got = strings.ReplaceAll(got, `\'`, `'`)
	got = strings.ReplaceAll(got, `\\`, `\`)
	if got != name {
		t.Fatalf("roundtrip mismatch: %q != %q", got, name)
	}
}
