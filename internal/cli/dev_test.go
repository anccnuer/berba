package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"berba/internal/config"
)

func writeDevProject(t *testing.T, appJSON string, files map[string]string) *config.Project {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "app.json"), []byte(appJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	proj, err := config.Load(filepath.Join(root, "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	return proj
}

func TestDevLuaMetaMirrorsCapabilities(t *testing.T) {
	proj := writeDevProject(t, `{
	  "name": "能力", "package": "com.example.cap", "versionName": "1.0.0", "versionCode": 1,
	  "capabilities": ["notify", "task", "native"],
	  "native": {"plugins": [{"name": "echo"}]},
	  "lua": {"dir": "lua", "entry": "main.lua"}
	}`, map[string]string{
		"www/index.html": "<html></html>",
		"lua/main.lua":   "log('hi')",
		"native/echo/spec.json": `{
		  "name": "echo", "abiVersion": 1, "version": "1.0.0",
		  "modules": {"echo": {"functions": {"call": {}, "later": {"async": true}}}}
		}`,
	})

	caps, modules, events, native, err := devLuaMeta(proj)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(caps, ",") != "native,notify,task" {
		t.Fatalf("capabilities = %v", caps)
	}
	for _, name := range []string{"app", "notify", "task", "native"} {
		if _, ok := modules[name]; !ok {
			t.Fatalf("modules 缺少 %q: %v", name, modules)
		}
	}
	if strings.Join(modules["notify"], ",") != "cancel,cancelAll,channel,enabled,show" {
		t.Fatalf("notify functions = %v", modules["notify"])
	}
	if !strings.Contains(strings.Join(events["app"], ","), "notification") ||
		!strings.Contains(strings.Join(events["app"], ","), "task") {
		t.Fatalf("app events = %v", events["app"])
	}
	if !strings.Contains(strings.Join(events["app"], ","), "create") {
		t.Fatalf("core 事件应始终存在: %v", events["app"])
	}
	if native == nil || native.ABIVersion != 1 || len(native.Plugins) != 1 {
		t.Fatalf("native = %+v", native)
	}
	plugin := native.Plugins[0]
	if plugin.Name != "echo" || plugin.LibName != "libberba_echo.so" {
		t.Fatalf("plugin = %+v", plugin)
	}
	fns := plugin.Modules["echo"].Functions
	if !fns["later"].Async || fns["call"].Async {
		t.Fatalf("functions = %+v", fns)
	}
}

func TestDevLuaMetaCoreOnly(t *testing.T) {
	proj := writeDevProject(t, `{
	  "name": "核心", "package": "com.example.core", "versionName": "1.0.0", "versionCode": 1,
	  "lua": {"dir": "lua", "entry": "main.lua"}
	}`, map[string]string{
		"www/index.html": "<html></html>",
		"lua/main.lua":   "log('hi')",
	})

	caps, modules, events, native, err := devLuaMeta(proj)
	if err != nil {
		t.Fatal(err)
	}
	if caps == nil || len(caps) != 0 {
		t.Fatalf("capabilities = %#v, 期望空数组", caps)
	}
	if _, ok := modules["notify"]; ok {
		t.Fatal("未启用的能力不应出现在模块元数据中")
	}
	if _, ok := modules["app"]; !ok {
		t.Fatalf("core 模块缺失: %v", modules)
	}
	if strings.Contains(strings.Join(events["app"], ","), "task") {
		t.Fatalf("未启用 task 却有 task 事件: %v", events["app"])
	}
	if native == nil || len(native.Plugins) != 0 {
		t.Fatalf("native = %+v", native)
	}
}

func TestDevLuaMetaRejectsUnknownCapability(t *testing.T) {
	proj := writeDevProject(t, `{
	  "name": "未知", "package": "com.example.unknown", "versionName": "1.0.0", "versionCode": 1,
	  "capabilities": ["bogus"],
	  "lua": {"dir": "lua", "entry": "main.lua"}
	}`, map[string]string{
		"www/index.html": "<html></html>",
		"lua/main.lua":   "log('hi')",
	})
	if _, _, _, _, err := devLuaMeta(proj); err == nil ||
		!strings.Contains(err.Error(), "未知能力") {
		t.Fatalf("devLuaMeta error = %v, want 未知能力", err)
	}
}

func TestDevLuaMetaRejectsMissingPluginSpec(t *testing.T) {
	proj := writeDevProject(t, `{
	  "name": "缺规格", "package": "com.example.nospec", "versionName": "1.0.0", "versionCode": 1,
	  "capabilities": ["native"],
	  "native": {"plugins": [{"name": "echo"}]},
	  "lua": {"dir": "lua", "entry": "main.lua"}
	}`, map[string]string{
		"www/index.html":       "<html></html>",
		"lua/main.lua":         "log('hi')",
		"native/echo/keep.txt": "x",
	})
	if _, _, _, _, err := devLuaMeta(proj); err == nil ||
		!strings.Contains(err.Error(), "spec.json") {
		t.Fatalf("devLuaMeta error = %v, want 缺少 spec.json", err)
	}
}
