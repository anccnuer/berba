package build

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"berba/internal/aapt2"
	"berba/internal/apk"
	"berba/internal/clierr"
	"berba/internal/sign"
)

func TestPublishAtomic(t *testing.T) {
	dir := t.TempDir()
	path, err := Publish(dir, "app.apk", []byte("payload"), func(tmpPath string) error {
		if filepath.Base(tmpPath) == "app.apk" {
			t.Fatal("verification must run on the temp file")
		}
		got, err := os.ReadFile(tmpPath)
		if err != nil {
			return err
		}
		if string(got) != "payload" {
			t.Fatalf("temp content = %q", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "app.apk" {
		t.Fatalf("path = %q", path)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "payload" {
		t.Fatalf("published content = %q, err = %v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("unexpected leftover files: %v", entries)
	}
}

func TestPublishVerifyFailureLeavesNoArtifact(t *testing.T) {
	dir := t.TempDir()
	_, err := Publish(dir, "app.apk", []byte("payload"), func(string) error {
		return errors.New("bad signature")
	})
	if err == nil {
		t.Fatal("expected verification failure")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed publish left artifacts: %v", entries)
	}
}

const testAppJSON = `{
  "name": "测试应用",
  "package": "com.example.test",
  "versionName": "1.0.0",
  "versionCode": 7,
  "web": {
    "dir": "www",
    "startUrl": "https://appassets.androidhost/index.html"
  }
}`

type fakeRunner struct {
	packageName string
	versionCode string
}

func (f *fakeRunner) Compile(_ context.Context, _ string, outZip string) error {
	return os.WriteFile(outZip, nil, 0o644)
}

func (f *fakeRunner) Link(_ context.Context, _, _, outAPK string) error {
	base, err := apk.Build([]apk.Entry{
		{Name: "AndroidManifest.xml", Data: []byte("<manifest/>")},
		{Name: "resources.arsc", Data: []byte("arsc"), Store: true, Align: 4},
	})
	if err != nil {
		return err
	}
	return os.WriteFile(outAPK, base, 0o644)
}

func (f *fakeRunner) DumpBadging(_ context.Context, _ string) (*aapt2.Badging, error) {
	return &aapt2.Badging{Package: f.packageName, VersionCode: f.versionCode, VersionName: "1.0.0"}, nil
}

func setupProject(t *testing.T) (root, home string) {
	t.Helper()
	root = t.TempDir()
	home = t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "app.json"), []byte(testAppJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "www"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "www", "index.html"), []byte("<h1>hi</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"tools/" + aapt2.ExeName(), "tools/android.jar", "payload/classes.dex"} {
		full := filepath.Join(home, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, home
}

func TestBuildWithRunner(t *testing.T) {
	root, home := setupProject(t)
	var steps []Step
	res, err := Build(context.Background(), Options{
		ConfigPath: filepath.Join(root, "app.json"),
		Home:       home,
		OutDir:     filepath.Join(root, "dist"),
		Runner:     &fakeRunner{packageName: "com.example.test", versionCode: "7"},
		Progress:   func(s Step) { steps = append(steps, s) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != stepTotal {
		t.Fatalf("steps = %d, want %d", len(steps), stepTotal)
	}
	for i, s := range steps {
		if s.Index != i+1 || s.Total != stepTotal {
			t.Fatalf("step %d = %+v", i, s)
		}
	}
	data, err := os.ReadFile(res.APKPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sign.Verify(data); err != nil {
		t.Fatalf("verify: %v", err)
	}
	sum, err := os.ReadFile(res.APKPath + ".sha256")
	if err != nil || !bytes.Contains(sum, []byte(res.SHA256)) {
		t.Fatalf("sha256 file = %q, err = %v", sum, err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{"classes.dex", "assets/www/index.html", "assets/app_config.json"} {
		if !names[want] {
			t.Errorf("missing entry %q in %v", want, names)
		}
	}
}

func TestBuildDeterministic(t *testing.T) {
	root, home := setupProject(t)
	buildOnce := func(out string) string {
		res, err := Build(context.Background(), Options{
			ConfigPath: filepath.Join(root, "app.json"),
			Home:       home,
			OutDir:     out,
			Runner:     &fakeRunner{packageName: "com.example.test", versionCode: "7"},
		})
		if err != nil {
			t.Fatal(err)
		}
		return res.SHA256
	}
	first := buildOnce(filepath.Join(root, "dist1"))
	second := buildOnce(filepath.Join(root, "dist2"))
	if first != second {
		t.Fatalf("non-deterministic build: %s != %s", first, second)
	}
}

func TestBuildCanceled(t *testing.T) {
	root, home := setupProject(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Build(ctx, Options{
		ConfigPath: filepath.Join(root, "app.json"),
		Home:       home,
		OutDir:     filepath.Join(root, "dist"),
		Runner:     &fakeRunner{packageName: "com.example.test", versionCode: "7"},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestBuildExitCodes(t *testing.T) {
	t.Run("config=2", func(t *testing.T) {
		root, home := setupProject(t)
		if err := os.WriteFile(filepath.Join(root, "app.json"), []byte(`{"name":"","package":"bad"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Build(context.Background(), Options{
			ConfigPath: filepath.Join(root, "app.json"),
			Home:       home,
			Runner:     &fakeRunner{packageName: "com.example.test", versionCode: "7"},
		})
		if err == nil {
			t.Fatal("expected config error")
		}
		if got := clierr.ExitCode(err); got != 2 {
			t.Fatalf("exit code = %d (%v), want 2", got, err)
		}
	})

	t.Run("build=4", func(t *testing.T) {
		root, home := setupProject(t)
		_, err := Build(context.Background(), Options{
			ConfigPath: filepath.Join(root, "app.json"),
			Home:       home,
			Runner:     &failRunner{},
		})
		if err == nil {
			t.Fatal("expected build error")
		}
		if got := clierr.ExitCode(err); got != 4 {
			t.Fatalf("exit code = %d (%v), want 4", got, err)
		}
	})

	t.Run("verify=5", func(t *testing.T) {
		root, home := setupProject(t)
		_, err := Build(context.Background(), Options{
			ConfigPath: filepath.Join(root, "app.json"),
			Home:       home,
			OutDir:     filepath.Join(root, "dist"),
			Runner:     &fakeRunner{packageName: "com.example.test", versionCode: "99"},
		})
		if err == nil {
			t.Fatal("expected verify error on versionCode mismatch")
		}
		if got := clierr.ExitCode(err); got != 5 {
			t.Fatalf("exit code = %d (%v), want 5", got, err)
		}
	})

	t.Run("cancel=1", func(t *testing.T) {
		root, home := setupProject(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := Build(ctx, Options{
			ConfigPath: filepath.Join(root, "app.json"),
			Home:       home,
			Runner:     &fakeRunner{packageName: "com.example.test", versionCode: "7"},
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if clierr.Is(err) {
			t.Fatal("canceled build should not be tagged as clierr")
		}
	})
}

type failRunner struct{}

func (failRunner) Compile(context.Context, string, string) error {
	return errors.New("compile failed")
}

func (failRunner) Link(context.Context, string, string, string) error {
	return errors.New("link failed")
}

func (failRunner) DumpBadging(context.Context, string) (*aapt2.Badging, error) {
	return nil, errors.New("dump failed")
}
