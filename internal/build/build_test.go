package build

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	for _, want := range []string{"classes.dex", "assets/www/index.html", "assets/app_config.json", "assets/berba_capabilities.json"} {
		if !names[want] {
			t.Errorf("missing entry %q in %v", want, names)
		}
	}
	for name := range names {
		if strings.HasPrefix(name, "lib/") {
			t.Fatalf("core-only APK must not contain native libs: %q", name)
		}
	}
	var capAsset struct {
		Schema       int      `json:"schema"`
		Capabilities []string `json:"capabilities"`
	}
	for _, f := range zr.File {
		if f.Name != "assets/berba_capabilities.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewDecoder(rc).Decode(&capAsset); err != nil {
			rc.Close()
			t.Fatal(err)
		}
		rc.Close()
	}
	if capAsset.Schema != 1 || len(capAsset.Capabilities) != 0 {
		t.Fatalf("capability asset = %+v", capAsset)
	}
}

func TestBuildNotifyCapability(t *testing.T) {
	root, home := setupProject(t)
	if err := os.MkdirAll(filepath.Join(root, "lua"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lua", "main.lua"), []byte("log('hi')"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "payload", "shell.json"),
		[]byte(`{"schema":1,"capabilities":["notify"],"nativeAbiVersion":0}`), 0o644); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"通知示例","package":"com.example.test","versionName":"1.0.0","versionCode":7,
	  "capabilities":["notify"],
	  "web":{"dir":"www","startUrl":"https://appassets.androidhost/index.html"}}`
	if err := os.WriteFile(filepath.Join(root, "app.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Build(context.Background(), Options{
		ConfigPath: filepath.Join(root, "app.json"),
		Home:       home,
		OutDir:     filepath.Join(root, "dist"),
		Runner:     &fakeRunner{packageName: "com.example.test", versionCode: "7"},
	})
	if err != nil {
		t.Fatal(err)
	}

	manifest, err := os.ReadFile(filepath.Join(root, ".berba", "build", "AndroidManifest.xml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`android:name="android.permission.POST_NOTIFICATIONS"`,
		`android:launchMode="singleTask"`,
	} {
		if !bytes.Contains(manifest, []byte(want)) {
			t.Fatalf("manifest missing %q:\n%s", want, manifest)
		}
	}
	for _, dir := range []string{"drawable-mdpi", "drawable-xxxhdpi"} {
		if _, err := os.Stat(filepath.Join(root, ".berba", "build", "res", dir, "ic_stat_berba.png")); err != nil {
			t.Fatalf("%s icon: %v", dir, err)
		}
	}

	data, err := os.ReadFile(res.APKPath)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var asset struct {
		Capabilities []string `json:"capabilities"`
		Modules      map[string]struct {
			Functions map[string]json.RawMessage `json:"functions"`
		} `json:"modules"`
		Events map[string]map[string]json.RawMessage `json:"events"`
	}
	for _, f := range zr.File {
		if f.Name != "assets/berba_capabilities.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		err = json.NewDecoder(rc).Decode(&asset)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(asset.Capabilities) != 1 || asset.Capabilities[0] != "notify" {
		t.Fatalf("capabilities = %v", asset.Capabilities)
	}
	if _, ok := asset.Modules["notify"].Functions["show"]; !ok {
		t.Fatalf("notify functions = %v", asset.Modules["notify"].Functions)
	}
	if _, ok := asset.Events["app"]["notification"]; !ok {
		t.Fatalf("app events = %v", asset.Events["app"])
	}
}

func TestBuildTaskCapability(t *testing.T) {
	root, home := setupProject(t)
	if err := os.MkdirAll(filepath.Join(root, "lua"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lua", "main.lua"), []byte("log('hi')"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "payload", "shell.json"),
		[]byte(`{"schema":1,"capabilities":["task"],"nativeAbiVersion":0}`), 0o644); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"任务示例","package":"com.example.test","versionName":"1.0.0","versionCode":7,
	  "capabilities":["task"],
	  "web":{"dir":"www","startUrl":"https://appassets.androidhost/index.html"}}`
	if err := os.WriteFile(filepath.Join(root, "app.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Build(context.Background(), Options{
		ConfigPath: filepath.Join(root, "app.json"),
		Home:       home,
		OutDir:     filepath.Join(root, "dist"),
		Runner:     &fakeRunner{packageName: "com.example.test", versionCode: "7"},
	})
	if err != nil {
		t.Fatal(err)
	}

	manifest, err := os.ReadFile(filepath.Join(root, ".berba", "build", "AndroidManifest.xml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`android:name="android.permission.RECEIVE_BOOT_COMPLETED"`,
		`android:name="android.permission.SCHEDULE_EXACT_ALARM"`,
		`android:name="com.berba.shell.TaskJobService"`,
		`android:permission="android.permission.BIND_JOB_SERVICE"`,
		`android:name="com.berba.shell.TaskAlarmReceiver"`,
		`android:name="com.berba.shell.TaskBootReceiver"`,
		`android:name="android.intent.action.BOOT_COMPLETED"`,
	} {
		if !bytes.Contains(manifest, []byte(want)) {
			t.Fatalf("manifest missing %q:\n%s", want, manifest)
		}
	}
	if bytes.Contains(manifest, []byte("singleTask")) {
		t.Fatalf("task only manifest should not be singleTask:\n%s", manifest)
	}
	if _, err := os.Stat(filepath.Join(root, ".berba", "build", "res", "drawable-mdpi", "ic_stat_berba.png")); err == nil {
		t.Fatal("task only build should not include notification icon")
	}

	data, err := os.ReadFile(res.APKPath)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var asset struct {
		Capabilities []string `json:"capabilities"`
		Modules      map[string]struct {
			Functions map[string]json.RawMessage `json:"functions"`
		} `json:"modules"`
		Events map[string]map[string]json.RawMessage `json:"events"`
	}
	for _, f := range zr.File {
		if f.Name != "assets/berba_capabilities.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		err = json.NewDecoder(rc).Decode(&asset)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(asset.Capabilities) != 1 || asset.Capabilities[0] != "task" {
		t.Fatalf("capabilities = %v", asset.Capabilities)
	}
	for _, fn := range []string{"schedule", "cancel", "list", "exact"} {
		if _, ok := asset.Modules["task"].Functions[fn]; !ok {
			t.Fatalf("task functions = %v", asset.Modules["task"].Functions)
		}
	}
	if _, ok := asset.Events["app"]["task"]; !ok {
		t.Fatalf("app events = %v", asset.Events["app"])
	}
}

func TestBuildNativeMissingShim(t *testing.T) {
	root, home := setupProject(t)
	if err := os.MkdirAll(filepath.Join(root, "lua"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lua", "main.lua"), []byte("log('hi')"), 0o644); err != nil {
		t.Fatal(err)
	}
	pluginDir := filepath.Join(root, "native", "echo")
	libDir := filepath.Join(pluginDir, "lib", "arm64-v8a")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := `{"name":"echo","abiVersion":1,"version":"1.0.0",
	  "modules":{"echo":{"functions":{"call":{}}}}}`
	if err := os.WriteFile(filepath.Join(pluginDir, "spec.json"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "libberba_echo.so"), []byte("elf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "payload", "shell.json"),
		[]byte(`{"schema":1,"capabilities":["native"],"nativeAbiVersion":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"原生示例","package":"com.example.test","versionName":"1.0.0","versionCode":7,
	  "capabilities":["native"],
	  "native":{"plugins":[{"name":"echo"}]},
	  "web":{"dir":"www","startUrl":"https://appassets.androidhost/index.html"}}`
	if err := os.WriteFile(filepath.Join(root, "app.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Build(context.Background(), Options{
		ConfigPath: filepath.Join(root, "app.json"),
		Home:       home,
		OutDir:     filepath.Join(root, "dist"),
		Runner:     &fakeRunner{packageName: "com.example.test", versionCode: "7"},
	})
	if err == nil || !strings.Contains(err.Error(), "JNI shim") {
		t.Fatalf("Build error = %v, want missing shim", err)
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

	t.Run("unknown capability=2", func(t *testing.T) {
		root, home := setupProject(t)
		body := `{"name":"测试应用","package":"com.example.test","versionName":"1.0.0","versionCode":7,
		  "capabilities":["future"],
		  "web":{"dir":"www","startUrl":"https://appassets.androidhost/index.html"}}`
		if err := os.WriteFile(filepath.Join(root, "app.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Build(context.Background(), Options{
			ConfigPath: filepath.Join(root, "app.json"),
			Home:       home,
			Runner:     &fakeRunner{packageName: "com.example.test", versionCode: "7"},
		})
		if err == nil {
			t.Fatal("expected unknown capability error")
		}
		if got := clierr.ExitCode(err); got != 2 {
			t.Fatalf("exit code = %d (%v), want 2", got, err)
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

func TestBuildWidgetCapability(t *testing.T) {
	root, home := setupProject(t)
	if err := os.MkdirAll(filepath.Join(root, "lua"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lua", "main.lua"), []byte("log('hi')"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "payload", "shell.json"),
		[]byte(`{"schema":1,"capabilities":["widget","task"],"nativeAbiVersion":0}`), 0o644); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"小组件示例","package":"com.example.test","versionName":"1.0.0","versionCode":7,
	  "capabilities":["widget","task"],
	  "web":{"dir":"www","startUrl":"https://appassets.androidhost/index.html"}}`
	if err := os.WriteFile(filepath.Join(root, "app.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Build(context.Background(), Options{
		ConfigPath: filepath.Join(root, "app.json"),
		Home:       home,
		OutDir:     filepath.Join(root, "dist"),
		Runner:     &fakeRunner{packageName: "com.example.test", versionCode: "7"},
	})
	if err != nil {
		t.Fatal(err)
	}

	manifest, err := os.ReadFile(filepath.Join(root, ".berba", "build", "AndroidManifest.xml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`android:name="com.berba.shell.WidgetProvider"`,
		`android:name="android.appwidget.action.APPWIDGET_UPDATE"`,
		`android:name="android.appwidget.provider"`,
		`android:resource="@xml/berba_widget_info"`,
		`android:launchMode="singleTask"`,
	} {
		if !bytes.Contains(manifest, []byte(want)) {
			t.Fatalf("manifest missing %q:\n%s", want, manifest)
		}
	}

	resDir := filepath.Join(root, ".berba", "build", "res")
	for _, rel := range []string{
		"layout/berba_widget.xml",
		"xml/berba_widget_info.xml",
		"drawable/berba_widget_bg.xml",
		"drawable/berba_widget_card.xml",
		"drawable-xxhdpi/berba_widget_preview.png",
	} {
		if _, err := os.Stat(filepath.Join(resDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
	}

	data, err := os.ReadFile(res.APKPath)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var asset struct {
		Capabilities []string `json:"capabilities"`
		Modules      map[string]struct {
			Functions map[string]json.RawMessage `json:"functions"`
		} `json:"modules"`
		Events map[string]map[string]json.RawMessage `json:"events"`
	}
	for _, f := range zr.File {
		if f.Name != "assets/berba_capabilities.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		err = json.NewDecoder(rc).Decode(&asset)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(asset.Capabilities) != 2 || asset.Capabilities[0] != "task" || asset.Capabilities[1] != "widget" {
		t.Fatalf("capabilities = %v", asset.Capabilities)
	}
	for _, fn := range []string{"update", "clear", "list"} {
		if _, ok := asset.Modules["widget"].Functions[fn]; !ok {
			t.Fatalf("widget functions = %v", asset.Modules["widget"].Functions)
		}
	}
	if _, ok := asset.Events["app"]["widget"]; !ok {
		t.Fatalf("app events = %v", asset.Events["app"])
	}
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
