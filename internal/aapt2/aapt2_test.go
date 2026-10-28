package aapt2

import (
	"strings"
	"testing"
)

func TestParseBadging(t *testing.T) {
	out := "package: name='com.example.demo' versionCode='42' versionName='1.2.3' platformBuildVersionName='15'\n" +
		"minSdkVersion:'24'\ntargetSdkVersion:'35'\n"
	m := badgingRE.FindStringSubmatch(out)
	if m == nil {
		t.Fatal("badging regex did not match")
	}
	got := Badging{Package: m[1], VersionCode: m[2], VersionName: m[3]}
	want := Badging{Package: "com.example.demo", VersionCode: "42", VersionName: "1.2.3"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if badgingRE.MatchString("W/zip: something") {
		t.Fatal("unexpected match")
	}
}

func TestMapError(t *testing.T) {
	cases := []struct {
		msg  string
		want string
	}{
		{"AndroidManifest.xml:3: error: attribute package not found", "Manifest 有误"},
		{"error: not a valid PNG file", "图标文件损坏"},
		{"Invalid file name: res/x y.png", "资源文件名不合法"},
		{"something else", "链接资源失败: something else"},
	}
	for _, c := range cases {
		got := mapError("链接资源", errString(c.msg)).Error()
		if !strings.Contains(got, c.want) {
			t.Errorf("mapError(%q) = %q, want contains %q", c.msg, got, c.want)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestLocateMissing(t *testing.T) {
	if _, err := Locate(t.TempDir()); err == nil {
		t.Fatal("expected error for empty tools dir")
	}
}
