package release

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"berba/internal/aapt2"
	"berba/internal/payload"
)

func stage(t *testing.T) (home, docs string) {
	t.Helper()
	home = t.TempDir()
	docs = t.TempDir()
	files := map[string]string{
		filepath.Join("tools", aapt2.ExeName()): "aapt2",
		filepath.Join("tools", "android.jar"):   "jar",
		filepath.Join("payload", "classes.dex"): "dex",
		"berba" + exeSuffix():                   "exe",
	}
	for name, content := range files {
		p := filepath.Join(home, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(docs, "README.md"), []byte("# docs"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(docs, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "docs", "a.md"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(docs, "include"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "include", "berba_plugin.h"), []byte("/* abi */"), 0o644); err != nil {
		t.Fatal(err)
	}
	return home, docs
}

func exeSuffix() string {
	if os.PathSeparator == '\\' {
		return ".exe"
	}
	return ""
}

func TestPackage(t *testing.T) {
	home, docs := stage(t)
	out := t.TempDir()
	res, err := Package(context.Background(), Options{
		Home:         home,
		OutDir:       out,
		DocsDir:      docs,
		Version:      "1.2.3",
		Aapt2Version: "2.19",
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(res.ZipPath) != "berba-1.2.3-"+runtime.GOOS+"-"+runtime.GOARCH+".zip" {
		t.Fatalf("zip name = %s", filepath.Base(res.ZipPath))
	}
	raw, err := os.ReadFile(res.ZipPath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != res.SHA256 {
		t.Fatal("sha256 mismatch")
	}
	sumFile, err := os.ReadFile(res.ZipPath + ".sha256")
	if err != nil || !bytes.Contains(sumFile, []byte(res.SHA256)) {
		t.Fatalf("sha256 file = %q, err = %v", sumFile, err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{"README.md", "docs/a.md", "include/berba_plugin.h", "tools/versions.json", "payload/classes.dex"} {
		if !names[want] {
			t.Errorf("missing %q in %v", want, names)
		}
	}
	vf, err := payload.ReadVersions(filepath.Join(home, "tools", "versions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if vf.Version != "1.2.3" || vf.Aapt2.Version != "2.19" || vf.Aapt2.Sha256 == "" {
		t.Fatalf("unexpected versions: %+v", vf)
	}
	dexSum := sha256.Sum256([]byte("dex"))
	if len(vf.Shell.DexSha256) != 1 || vf.Shell.DexSha256[0] != hex.EncodeToString(dexSum[:]) {
		t.Fatalf("shell dex hashes = %v", vf.Shell.DexSha256)
	}
}

func TestPackageDeterministic(t *testing.T) {
	home, docs := stage(t)
	makeZip := func(out string) string {
		res, err := Package(context.Background(), Options{
			Home: home, OutDir: out, DocsDir: docs, Version: "0.1.0", Aapt2Version: "2.19",
		})
		if err != nil {
			t.Fatal(err)
		}
		return res.SHA256
	}
	first := makeZip(t.TempDir())
	second := makeZip(t.TempDir())
	if first != second {
		t.Fatalf("non-deterministic release zip: %s != %s", first, second)
	}
}

func TestSanitizeVersion(t *testing.T) {
	for _, ok := range []string{"1.0.0", "v0.1.0", "2026.10"} {
		if _, err := SanitizeVersion(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "../x", "a/b", `a\b`, "a:b"} {
		if _, err := SanitizeVersion(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func stageCross(t *testing.T, targetOS string) string {
	t.Helper()
	home := t.TempDir()
	aapt2File, exe := "aapt2", ""
	if targetOS == "windows" {
		aapt2File, exe = "aapt2.exe", ".exe"
	}
	files := map[string]string{
		filepath.Join("tools", aapt2File):       "aapt2",
		filepath.Join("tools", "android.jar"):   "jar",
		filepath.Join("payload", "classes.dex"): "dex",
		"berba" + exe:                           "exe",
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		p := filepath.Join(home, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(files[name]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func TestPackageCrossTarget(t *testing.T) {
	other := "windows"
	if runtime.GOOS == "windows" {
		other = "linux"
	}
	home := stageCross(t, other)

	if _, err := Package(context.Background(), Options{
		Home: home, Version: "0.13", TargetOS: other,
	}); err == nil {
		t.Fatal("cross packaging without injected aapt2 version must fail")
	}

	out := t.TempDir()
	res, err := Package(context.Background(), Options{
		Home: home, OutDir: out, Version: "0.13",
		TargetOS: other, TargetArch: "amd64", Aapt2Version: "2.19-cross",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("berba-0.13-%s-amd64.zip", other)
	if filepath.Base(res.ZipPath) != want {
		t.Fatalf("zip name = %s, want %s", filepath.Base(res.ZipPath), want)
	}
	vf, err := payload.ReadVersions(filepath.Join(home, "tools", "versions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if vf.Aapt2.Version != "2.19-cross" || vf.Aapt2.Sha256 == "" || len(vf.Shell.DexSha256) != 1 {
		t.Fatalf("unexpected versions: %+v", vf)
	}
}

func TestPackageRequiresNativeShims(t *testing.T) {
	home, docs := stage(t)
	writeShell := func(version int) {
		body := fmt.Sprintf(`{"schema":1,"capabilities":["native"],"nativeAbiVersion":%d}`, version)
		if err := os.WriteFile(filepath.Join(home, "payload", "shell.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeShell(0)
	if _, err := Package(context.Background(), Options{
		Home: home, OutDir: t.TempDir(), DocsDir: docs, Version: "0.13", Aapt2Version: "2.19",
	}); err == nil || !strings.Contains(err.Error(), "nativeAbiVersion") {
		t.Fatalf("old shell accepted: %v", err)
	}
	writeShell(1)
	if _, err := Package(context.Background(), Options{
		Home: home, OutDir: t.TempDir(), DocsDir: docs, Version: "0.13", Aapt2Version: "2.19",
	}); err == nil || !strings.Contains(err.Error(), "payload/native") {
		t.Fatalf("missing shim accepted: %v", err)
	}
}

func TestPackageRejectsBadTarget(t *testing.T) {
	home, _ := stage(t)
	if _, err := Package(context.Background(), Options{
		Home: home, Version: "0.13", TargetOS: "plan9",
	}); err == nil {
		t.Fatal("plan9 target accepted")
	}
}
