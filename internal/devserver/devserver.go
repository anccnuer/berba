// Package devserver 在本地提供项目 www 目录的静态服务，并注入桥 mock。
package devserver

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// Options 是开发服务器配置。
type Options struct {
	Root   string
	Entry  string
	SPA    bool
	MockJS string
	Host   string
	Port   int
}

// Server 提供带 shim 注入的静态文件服务。
type Server struct {
	root    string
	entry   string
	spa     bool
	mock    string
	host    string
	port    int
	handler http.Handler
}

// New 校验目录并构建处理器。
func New(opts Options) (*Server, error) {
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("web 目录不可用: %w", err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("web 目录不是目录: %s", root)
	}
	if opts.Entry == "" {
		opts.Entry = "/index.html"
	}
	if !strings.HasPrefix(opts.Entry, "/") {
		opts.Entry = "/" + opts.Entry
	}
	if opts.Host == "" {
		opts.Host = "127.0.0.1"
	}
	s := &Server{
		root:  root,
		entry: opts.Entry,
		spa:   opts.SPA,
		mock:  opts.MockJS,
		host:  opts.Host,
		port:  opts.Port,
	}
	s.handler = s.routes()
	return s, nil
}

// Handler 暴露处理器，便于测试或嵌入其他服务器。
func (s *Server) Handler() http.Handler {
	return s.handler
}

// Listen 绑定端口并返回访问地址（含入口路径）。
func (s *Server) Listen() (net.Listener, string, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(s.host, strconv.Itoa(s.port)))
	if err != nil {
		return nil, "", err
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		ln.Close()
		return nil, "", errors.New("无法获取监听地址")
	}
	host := s.host
	if host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return ln, fmt.Sprintf("http://%s:%d%s", host, addr.Port, s.entry), nil
}

// Serve 提供服务直到 ctx 取消。
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{Handler: s.handler}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			srv.Close()
		case <-done:
		}
	}()
	err := srv.Serve(ln)
	close(done)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/__berba.js", s.serveShim)
	mux.HandleFunc("/", s.serveStatic)
	return mux
}

func (s *Server) serveShim(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		return
	}
	if s.mock != "" {
		fmt.Fprintf(w, "%s\n/* --- dev/mock.js --- */\n%s\n", shimScript, s.mock)
		return
	}
	fmt.Fprint(w, shimScript)
}

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	urlPath := r.URL.Path
	if strings.ContainsRune(urlPath, 0) {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	if strings.ContainsRune(urlPath, '\\') {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	clean := path.Clean("/" + urlPath)
	rel := strings.TrimPrefix(clean, "/")

	filePath := filepath.Join(s.root, filepath.FromSlash(rel))
	st, err := os.Stat(filePath)
	if err == nil && st.IsDir() {
		filePath = filepath.Join(filePath, "index.html")
		st, err = os.Stat(filePath)
	}
	if err != nil && os.IsNotExist(err) && s.spa && path.Ext(clean) == "" {
		filePath = filepath.Join(s.root, "index.html")
		st, err = os.Stat(filePath)
	}
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, "读取文件失败", http.StatusInternalServerError)
		return
	}
	contentType := contentTypeFor(filePath)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if strings.HasPrefix(contentType, "text/html") {
		data = injectShim(data)
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method == http.MethodHead {
		return
	}
	w.Write(data)
}

func injectShim(html []byte) []byte {
	tag := []byte(`<script src="/__berba.js"></script>`)
	lower := bytesToLower(html)
	if i := strings.Index(string(lower), "<head"); i >= 0 {
		if j := strings.IndexByte(string(html[i:]), '>'); j >= 0 {
			pos := i + j + 1
			return join(html[:pos], tag, html[pos:])
		}
	}
	if i := strings.Index(string(lower), "<body"); i >= 0 {
		if j := strings.IndexByte(string(html[i:]), '>'); j >= 0 {
			pos := i + j + 1
			return join(html[:pos], tag, html[pos:])
		}
	}
	return join(tag, html, nil)
}

func join(parts ...[]byte) []byte {
	var b []byte
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}

func bytesToLower(b []byte) []byte {
	out := make([]byte, len(b))
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return out
}

func contentTypeFor(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json", ".map":
		return "application/json; charset=utf-8"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".xml":
		return "application/xml; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".wasm":
		return "application/wasm"
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}

// shimScript 只依赖标准浏览器 API；桥调用由 mock 处理，未 mock 时明确 reject。
const shimScript = `(function () {
  var listeners = {};
  function resolveMock(name) {
    var mock = window.__berbaMock;
    return mock && Object.prototype.hasOwnProperty.call(mock, name) ? mock[name] : undefined;
  }
  var api = {
    call: function (name, args) {
      try {
        var entry = resolveMock(name);
        if (entry === undefined) {
          console.warn('[berba dev] 未 mock 的桥调用: ' + name, args);
          return Promise.reject(new Error('berba dev: 未 mock 的桥调用 ' + name));
        }
        return Promise.resolve(typeof entry === 'function' ? entry(args) : entry);
      } catch (e) {
        return Promise.reject(e);
      }
    },
    on: function (name, fn) {
      (listeners[name] || (listeners[name] = [])).push(fn);
      return fn;
    },
    off: function (name, fn) {
      var list = listeners[name];
      if (!list) return;
      var i = list.indexOf(fn);
      if (i >= 0) list.splice(i, 1);
    }
  };
  window.berba = api;
  window.__berbaEmit = function (name, data) {
    var list = listeners[name];
    if (!list) return;
    list.slice().forEach(function (fn) {
      try { fn(data); } catch (e) { console.error(e); }
    });
  };
})();
`
