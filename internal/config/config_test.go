package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProject(t *testing.T, appJSON string, files map[string]string) string {
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
	return root
}

const validJSON = `{
  "name": "测试",
  "package": "com.example.test",
  "versionName": "1.0.0",
  "permissions": ["camera", "CAMERA", "android.permission.VIBRATE"]
}`

func TestLoadDefaults(t *testing.T) {
	root := writeProject(t, validJSON, map[string]string{"www/index.html": "<html></html>"})
	p, err := Load(filepath.Join(root, "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Config.VersionCode != 1 || p.Config.Orientation != "auto" || p.Config.Theme != "light" {
		t.Fatalf("unexpected defaults: %+v", p.Config)
	}
	if !p.LocalStart || p.Config.Web.StartURL != DefaultStartURL {
		t.Fatalf("unexpected start url: %q", p.Config.Web.StartURL)
	}
	if len(p.Permissions) != 3 {
		t.Fatalf("permissions = %v", p.Permissions)
	}
	if p.Permissions[0] != "android.permission.CAMERA" || p.Permissions[1] != "android.permission.VIBRATE" || p.Permissions[2] != "android.permission.INTERNET" {
		t.Fatalf("permissions = %v", p.Permissions)
	}
	if p.HasLua {
		t.Fatal("expected HasLua=false without lua dir")
	}
}

func TestLocalStartRequiresFile(t *testing.T) {
	root := writeProject(t, validJSON, map[string]string{"www/other.html": "x"})
	_, err := Load(filepath.Join(root, "app.json"))
	if err == nil {
		t.Fatal("expected missing start url error")
	}
}

func TestInvalidPackage(t *testing.T) {
	for _, pkg := range []string{"com", "1com.example", "android.test", "com.android.x", "java.x.y", "com.example.工具"} {
		if err := ValidatePackage(pkg); err == nil {
			t.Fatalf("expected package %q to be rejected", pkg)
		}
	}
	for _, pkg := range []string{"com.example.demo", "a.b.c_d", "my.app2"} {
		if err := ValidatePackage(pkg); err != nil {
			t.Fatalf("expected package %q to be valid: %v", pkg, err)
		}
	}
}

func TestValidationErrors(t *testing.T) {
	cases := map[string]string{
		"bad version":  `{"name":"x","package":"com.example.x","versionName":"1","orientation":"sideways"}`,
		"bad theme":    `{"name":"x","package":"com.example.x","versionName":"1","theme":"neon"}`,
		"zero code":    `{"name":"x","package":"com.example.x","versionName":"1","versionCode":0}`,
		"empty name":   `{"name":"  ","package":"com.example.x","versionName":"1"}`,
		"bad perm":     `{"name":"x","package":"com.example.x","versionName":"1","permissions":["FLY"]}`,
		"bad http url": `{"name":"x","package":"com.example.x","versionName":"1","web":{"startUrl":"http://example.com/"}}`,
	}
	root := writeProject(t, "{}", map[string]string{"www/index.html": "x"})
	for name, body := range cases {
		path := filepath.Join(root, "app.json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}

func TestRemoteStart(t *testing.T) {
	body := `{"name":"x","package":"com.example.x","versionName":"1","cleartext":true,
	  "web":{"startUrl":"http://example.com/app","allowedOrigins":["https://API.Example.com/"]}}`
	root := writeProject(t, body, map[string]string{"www/index.html": "x"})
	p, err := Load(filepath.Join(root, "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	if p.LocalStart {
		t.Fatal("expected remote start")
	}
	if len(p.AllowedOrigins) != 2 {
		t.Fatalf("origins = %v", p.AllowedOrigins)
	}
	if !p.OriginAllowed("https://api.example.com") || !p.OriginAllowed("http://example.com") {
		t.Fatalf("startUrl origin should be auto-allowed, got %v", p.AllowedOrigins)
	}
	if p.OriginAllowed("https://evil.example.com") {
		t.Fatal("unexpected origin allowed")
	}
}

func TestRejectsUnknownField(t *testing.T) {
	bodies := []string{
		`{"name":"x","package":"com.example.x","versionName":"1","versionNmae":"typo"}`,
		`{"name":"x","package":"com.example.x","versionName":"1","web":{"folders":"www"}}`,
	}
	for _, body := range bodies {
		root := writeProject(t, body, map[string]string{"www/index.html": "x"})
		if _, err := Load(filepath.Join(root, "app.json")); err == nil {
			t.Fatalf("expected unknown field rejection for %s", body)
		}
	}
}

func TestRejectsPathsOutsideRoot(t *testing.T) {
	outside := t.TempDir()
	bodies := []string{
		fmt.Sprintf(`{"name":"x","package":"com.example.x","versionName":"1","web":{"dir":%q}}`, outside),
		`{"name":"x","package":"com.example.x","versionName":"1","icon":"../../evil.png"}`,
		`{"name":"x","package":"com.example.x","versionName":"1","lua":{"dir":"../lua"}}`,
	}
	for _, body := range bodies {
		root := writeProject(t, body, map[string]string{"www/index.html": "x"})
		if _, err := Load(filepath.Join(root, "app.json")); err == nil {
			t.Fatalf("expected out-of-root rejection for %s", body)
		}
	}
}

func TestMissingExplicitLuaDirWarns(t *testing.T) {
	body := `{"name":"x","package":"com.example.x","versionName":"1","lua":{"dir":"missing-lua"}}`
	root := writeProject(t, body, map[string]string{"www/index.html": "x"})
	p, err := Load(filepath.Join(root, "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	if p.HasLua {
		t.Fatal("missing Lua dir must disable Lua")
	}
	if len(p.Warnings) != 1 || !strings.Contains(p.Warnings[0], "Lua 已禁用") {
		t.Fatalf("warnings = %v", p.Warnings)
	}
}

func TestLuaEntryTraversal(t *testing.T) {
	body := `{"name":"x","package":"com.example.x","versionName":"1","lua":{"dir":"lua","entry":"../../evil.lua"}}`
	root := writeProject(t, body, map[string]string{"www/index.html": "x", "lua/main.lua": "x"})
	if _, err := Load(filepath.Join(root, "app.json")); err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func TestAssetConfig(t *testing.T) {
	body := `{"name":"名字","package":"com.example.x","versionName":"2.0","versionCode":7,
	  "web":{"spa":true},"http":{"allowPrivateNetwork":true}}`
	root := writeProject(t, body, map[string]string{"www/index.html": "x", "lua/main.lua": "x"})
	p, err := Load(filepath.Join(root, "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := p.AssetConfig(true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`"luaEntry": "main.lua"`, `"debug": true`, `"spa": true`, `"versionCode": 7`,
		`"allowPrivateNetwork": true`}
	for _, w := range want {
		if !strings.Contains(string(data), w) {
			t.Fatalf("asset config missing %s: %s", w, data)
		}
	}
}

func TestDeepLinks(t *testing.T) {
	body := `{"name":"n","package":"com.example.x","versionName":"1.0.0",
	  "deepLinks":["myapp://open","myapp://open","Notes://View"]}`
	root := writeProject(t, body, map[string]string{"www/index.html": "x", "lua/main.lua": "x"})
	p, err := Load(filepath.Join(root, "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.DeepLinks) != 2 {
		t.Fatalf("deep links dedup/normalize failed: %+v", p.DeepLinks)
	}
	if p.DeepLinks[0].Scheme != "myapp" || p.DeepLinks[0].Host != "open" {
		t.Fatalf("unexpected first link: %+v", p.DeepLinks[0])
	}
	if p.DeepLinks[1].Scheme != "notes" || p.DeepLinks[1].Host != "view" {
		t.Fatalf("unexpected second link: %+v", p.DeepLinks[1])
	}
}

func TestDeepLinksInvalid(t *testing.T) {
	cases := []string{
		`["http://example.com/a"]`,
		`["open"]`,
		`["2bad://host"]`,
		`["myapp://ho st"]`,
	}
	for _, c := range cases {
		body := fmt.Sprintf(`{"name":"n","package":"com.example.x","versionName":"1.0.0","deepLinks":%s}`, c)
		root := writeProject(t, body, map[string]string{"www/index.html": "x", "lua/main.lua": "x"})
		if _, err := Load(filepath.Join(root, "app.json")); err == nil {
			t.Fatalf("expected error for %s", c)
		}
	}
}
