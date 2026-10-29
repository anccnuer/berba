package devserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func get(t *testing.T, handler http.Handler, target string) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	res := rec.Result()
	data, _ := io.ReadAll(res.Body)
	res.Body.Close()
	return res, string(data)
}

func TestServesHTMLWithShim(t *testing.T) {
	root := writeTree(t, map[string]string{
		"index.html": "<!doctype html><html><head><title>x</title></head><body>hi</body></html>",
	})
	s, err := New(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	res, body := get(t, s.Handler(), "/")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if got := res.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("content-type = %q", got)
	}
	if !strings.Contains(body, `src="/__berba.js"`) {
		t.Fatalf("shim not injected: %s", body)
	}
	if strings.Index(body, "__berba.js") > strings.Index(body, "<title>") {
		t.Fatalf("shim must be injected before page content: %s", body)
	}
}

func TestServesAssetsAndSPA(t *testing.T) {
	root := writeTree(t, map[string]string{
		"index.html":  "<html><head></head><body>spa</body></html>",
		"app.js":      "console.log(1)",
		"css/app.css": "body{}",
	})
	s, err := New(Options{Root: root, SPA: true})
	if err != nil {
		t.Fatal(err)
	}
	res, body := get(t, s.Handler(), "/app.js")
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/javascript") {
		t.Fatalf("js status=%d type=%q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if body != "console.log(1)" {
		t.Fatalf("js body = %q", body)
	}
	res, body = get(t, s.Handler(), "/missing/route")
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "spa") {
		t.Fatalf("spa fallback status=%d body=%s", res.StatusCode, body)
	}
	res, _ = get(t, s.Handler(), "/missing.png")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("asset 404 expected, got %d", res.StatusCode)
	}
}

func TestRejectsTraversal(t *testing.T) {
	root := writeTree(t, map[string]string{"index.html": "ok"})
	s, err := New(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(root), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outside)

	res, body := get(t, s.Handler(), "/../outside.txt")
	if strings.Contains(body, "secret") {
		t.Fatalf("traversal escaped root: %s", body)
	}
	if res.StatusCode == http.StatusOK {
		t.Fatalf("traversal should not resolve to a file")
	}
	res, _ = get(t, s.Handler(), "/..%5Coutside.txt")
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("backslash path should be rejected, got %d", res.StatusCode)
	}
}

func TestShimIncludesMock(t *testing.T) {
	root := writeTree(t, map[string]string{"index.html": "ok"})
	s, err := New(Options{Root: root, MockJS: `window.__berbaMock = {};`})
	if err != nil {
		t.Fatal(err)
	}
	res, body := get(t, s.Handler(), "/__berba.js")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if !strings.Contains(body, "window.__berbaMock = {};") || !strings.Contains(body, "window.berba") {
		t.Fatalf("shim missing mock or api: %s", body)
	}
	if strings.Contains(body, "token") {
		t.Fatalf("dev shim must not expose a token")
	}
}

func TestListenPicksPort(t *testing.T) {
	root := writeTree(t, map[string]string{"index.html": "ok"})
	s, err := New(Options{Root: root, Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	ln, url, err := s.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if !strings.HasPrefix(url, "http://127.0.0.1:") || !strings.HasSuffix(url, "/index.html") {
		t.Fatalf("url = %s", url)
	}
}

func newLuaServer(t *testing.T, luaFiles map[string]string) *Server {
	t.Helper()
	web := writeTree(t, map[string]string{"index.html": "<html><head></head><body>x</body></html>"})
	luaRoot := writeTree(t, luaFiles)
	s, err := New(Options{
		Root:     web,
		LuaRoot:  luaRoot,
		LuaEntry: "main.lua",
		App: AppMeta{
			Name:        "演示",
			Package:     "com.example.demo",
			VersionName: "1.2.3",
			VersionCode: 7,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLuaManifestListsSources(t *testing.T) {
	s := newLuaServer(t, map[string]string{
		"main.lua":     `bridge.register("ping", function() return "pong" end)`,
		"lib/util.lua": "return {}",
		"README.txt":   "not lua",
	})
	res, body := get(t, s.Handler(), "/__berba/lua.json")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", res.StatusCode, body)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q", ct)
	}
	var payload struct {
		Entry string            `json:"entry"`
		Files map[string]string `json:"files"`
		App   struct {
			Name        string `json:"name"`
			Package     string `json:"package"`
			VersionName string `json:"versionName"`
			VersionCode int    `json:"versionCode"`
		} `json:"app"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if payload.Entry != "main.lua" {
		t.Fatalf("entry = %q", payload.Entry)
	}
	if !strings.Contains(payload.Files["main.lua"], "bridge.register") {
		t.Fatalf("main.lua missing: %v", payload.Files)
	}
	if _, ok := payload.Files["lib/util.lua"]; !ok {
		t.Fatalf("nested lua missing: %v", payload.Files)
	}
	if _, ok := payload.Files["README.txt"]; ok {
		t.Fatalf("non-lua file should be skipped: %v", payload.Files)
	}
	if payload.App.Name != "演示" || payload.App.Package != "com.example.demo" ||
		payload.App.VersionName != "1.2.3" || payload.App.VersionCode != 7 {
		t.Fatalf("app meta = %+v", payload.App)
	}
}

func TestLuaAssetsServed(t *testing.T) {
	s := newLuaServer(t, map[string]string{"main.lua": "return true"})
	cases := map[string]string{
		"/__berba/fengari.js":     "fengari",
		"/__berba/lua-runtime.js": "__berbaLua",
	}
	for target, needle := range cases {
		res, body := get(t, s.Handler(), target)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d", target, res.StatusCode)
		}
		if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
			t.Fatalf("%s content-type = %q", target, ct)
		}
		if !strings.Contains(body, needle) {
			t.Fatalf("%s 响应缺少 %q", target, needle)
		}
	}
}

func TestLuaManifestHeadAndMethod(t *testing.T) {
	s := newLuaServer(t, map[string]string{"main.lua": "return true"})

	req := httptest.NewRequest(http.MethodHead, "/__berba/lua.json", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	res := rec.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("HEAD status=%d bodyLen=%d", res.StatusCode, rec.Body.Len())
	}

	req = httptest.NewRequest(http.MethodPost, "/__berba/lua.json", nil)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d", rec.Code)
	}
}

func TestShimBootstrapsLua(t *testing.T) {
	s := newLuaServer(t, map[string]string{"main.lua": "return true"})
	res, body := get(t, s.Handler(), "/__berba.js")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	for _, needle := range []string{"__berbaLuaReady", "/__berba/lua.json", "/__berba/fengari.js", "/__berba/lua-runtime.js"} {
		if !strings.Contains(body, needle) {
			t.Fatalf("shim 缺少 %q", needle)
		}
	}
}

func TestLuaEntryMissing(t *testing.T) {
	s := newLuaServer(t, map[string]string{"other.lua": "return true"})
	res, body := get(t, s.Handler(), "/__berba/lua.json")
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", res.StatusCode, body)
	}
	if !strings.Contains(body, "未找到 Lua 入口") {
		t.Fatalf("body = %s", body)
	}
}

func TestLuaFileTooLarge(t *testing.T) {
	big := strings.Repeat("--", luaFileLimit/2+1)
	s := newLuaServer(t, map[string]string{"main.lua": big})
	res, body := get(t, s.Handler(), "/__berba/lua.json")
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%.120s", res.StatusCode, body)
	}
	if !strings.Contains(body, "1MB") {
		t.Fatalf("body = %.200s", body)
	}
}

func TestLuaRejectsSymlink(t *testing.T) {
	web := writeTree(t, map[string]string{"index.html": "ok"})
	luaRoot := writeTree(t, map[string]string{"main.lua": "return true"})
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.lua"), []byte("return 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.lua"), filepath.Join(luaRoot, "link.lua")); err != nil {
		t.Skipf("创建符号链接失败（需要权限）: %v", err)
	}
	s, err := New(Options{Root: web, LuaRoot: luaRoot, LuaEntry: "main.lua"})
	if err != nil {
		t.Fatal(err)
	}
	res, body := get(t, s.Handler(), "/__berba/lua.json")
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%.200s", res.StatusCode, body)
	}
	if strings.Contains(body, "return 1") {
		t.Fatalf("symlink contents leaked: %s", body)
	}
}

func TestLuaDisabledRoutesMissing(t *testing.T) {
	root := writeTree(t, map[string]string{"index.html": "ok"})
	s, err := New(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"/__berba/lua.json", "/__berba/fengari.js", "/__berba/lua-runtime.js"} {
		res, _ := get(t, s.Handler(), target)
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s status = %d", target, res.StatusCode)
		}
	}
	_, body := get(t, s.Handler(), "/__berba.js")
	if strings.Contains(body, "/__berba/fengari.js") || strings.Contains(body, "/__berba/lua.json") {
		t.Fatalf("disabled shim should not bootstrap lua runtime")
	}
}
