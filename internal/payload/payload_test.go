package payload

import (
	"os"
	"path/filepath"
	"testing"
)

func setupPayload(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, rel := range []string{"tools/aapt2", "tools/android.jar", "payload/classes.dex"} {
		full := filepath.Join(home, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func TestLocateWithoutShellManifest(t *testing.T) {
	home := setupPayload(t)
	layout, err := LocateOn(home, "linux")
	if err != nil {
		t.Fatal(err)
	}
	if layout.Shell != nil {
		t.Fatalf("shell = %+v", layout.Shell)
	}
	if !layout.Supports("core") {
		t.Fatal("core must always be supported")
	}
	if layout.Supports("notify") {
		t.Fatal("missing shell.json must be core-only")
	}
}

func TestLocateWithShellManifest(t *testing.T) {
	home := setupPayload(t)
	manifest := `{"schema":1,"capabilities":["task","notify"],"nativeAbiVersion":1}`
	if err := os.WriteFile(filepath.Join(home, "payload", "shell.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	layout, err := LocateOn(home, "linux")
	if err != nil {
		t.Fatal(err)
	}
	if layout.Shell == nil {
		t.Fatal("missing shell manifest")
	}
	if layout.Shell.NativeABIVersion != 1 {
		t.Fatalf("native abi = %d", layout.Shell.NativeABIVersion)
	}
	if !layout.Supports("notify") || !layout.Supports("task") || layout.Supports("native") {
		t.Fatalf("supports() mismatch: %+v", layout.Shell.Capabilities)
	}
}

func TestLocateNativeShims(t *testing.T) {
	home := setupPayload(t)
	for _, abi := range []string{"arm64-v8a", "x86_64"} {
		dir := filepath.Join(home, "payload", "native", abi)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, NativeShimName), []byte(abi), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(home, "payload", "native", "armeabi-v7a"), 0o755); err != nil {
		t.Fatal(err)
	}
	layout, err := LocateOn(home, "linux")
	if err != nil {
		t.Fatal(err)
	}
	if len(layout.Native) != 2 {
		t.Fatalf("native = %v", layout.Native)
	}
	if string(mustRead(t, layout.Native["arm64-v8a"])) != "arm64-v8a" {
		t.Fatalf("arm64 shim = %q", layout.Native["arm64-v8a"])
	}
	if _, ok := layout.Native["armeabi-v7a"]; ok {
		t.Fatal("directory without shim must be ignored")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestLocateRejectsInvalidShellManifest(t *testing.T) {
	home := setupPayload(t)
	for _, body := range []string{`{"schema":2}`, `not json`} {
		if err := os.WriteFile(filepath.Join(home, "payload", "shell.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LocateOn(home, "linux"); err == nil {
			t.Fatalf("expected error for %q", body)
		}
	}
}
