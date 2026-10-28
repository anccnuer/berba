package devserver

import (
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
