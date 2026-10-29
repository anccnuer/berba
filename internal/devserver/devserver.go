// Package devserver 在本地提供项目 www 目录的静态服务，并注入桥 mock。
// 启用 Lua 时还会下发 fengari 与浏览器运行时，让 lua/main.lua 直接在页面里执行。
package devserver

import (
	"context"
	"encoding/json"
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

	"berba/internal/devserver/luaruntime"
	"berba/internal/fswalk"
)

const (
	luaFileLimit  = 1 << 20 // 单个 Lua 文件上限
	luaTotalLimit = 8 << 20 // Lua 源码总量上限
)

// AppMeta 是页面内 app.info() 的元数据。
type AppMeta struct {
	Name        string
	Package     string
	VersionName string
	VersionCode int
}

// Options 是开发服务器配置。
type Options struct {
	Root   string
	Entry  string
	SPA    bool
	MockJS string
	Host   string
	Port   int
	// LuaRoot 非空时启用浏览器内 Lua 运行时，LuaEntry 为相对 LuaRoot 的入口文件。
	LuaRoot  string
	LuaEntry string
	App      AppMeta
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

	luaRoot  string
	luaEntry string
	luaOn    bool
	app      AppMeta
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
		app:   opts.App,
	}
	if opts.LuaRoot != "" {
		luaRoot, err := filepath.Abs(opts.LuaRoot)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(luaRoot)
		if err != nil {
			return nil, fmt.Errorf("Lua 目录不可用: %w", err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("Lua 目录不是目录: %s", luaRoot)
		}
		entry := filepath.ToSlash(strings.TrimSpace(opts.LuaEntry))
		entry = strings.TrimPrefix(strings.TrimPrefix(entry, "./"), "/")
		if entry == "" {
			entry = "main.lua"
		}
		if strings.Contains(entry, "..") {
			return nil, fmt.Errorf("Lua 入口路径越界: %q", opts.LuaEntry)
		}
		s.luaRoot = luaRoot
		s.luaEntry = path.Clean(entry)
		s.luaOn = true
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
	if s.luaOn {
		mux.HandleFunc("/__berba/lua.json", s.serveLuaManifest)
		mux.HandleFunc("/__berba/fengari.js", s.serveLuaAsset(luaruntime.Fengari))
		mux.HandleFunc("/__berba/lua-runtime.js", s.serveLuaAsset(luaruntime.Runtime))
		mux.HandleFunc("/__berba/fengari-license.txt", s.serveLuaAsset(luaruntime.License))
	}
	mux.HandleFunc("/", s.serveStatic)
	return mux
}

func methodAllowed(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

func (s *Server) serveShim(w http.ResponseWriter, r *http.Request) {
	if !methodAllowed(w, r) {
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
	} else {
		fmt.Fprint(w, shimScript)
	}
	if s.luaOn {
		fmt.Fprintf(w, "\n/* --- berba lua --- */\n%s\n", luaBootstrap)
	}
}

func (s *Server) serveLuaAsset(data []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !methodAllowed(w, r) {
			return
		}
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if r.Method == http.MethodHead {
			return
		}
		w.Write(data)
	}
}

type luaManifest struct {
	Entry string            `json:"entry"`
	Files map[string]string `json:"files"`
	App   luaManifestApp    `json:"app"`
}

type luaManifestApp struct {
	Name        string `json:"name"`
	Package     string `json:"package"`
	VersionName string `json:"versionName"`
	VersionCode int    `json:"versionCode"`
}

func (s *Server) serveLuaManifest(w http.ResponseWriter, r *http.Request) {
	if !methodAllowed(w, r) {
		return
	}

	files := make(map[string]string)
	var total int64
	err := fswalk.Files(s.luaRoot, func(rel, abs string) error {
		if !strings.HasSuffix(strings.ToLower(rel), ".lua") {
			return nil
		}
		info, err := os.Stat(abs)
		if err != nil {
			return err
		}
		if info.Size() > luaFileLimit {
			return fmt.Errorf("Lua 文件超过 1MB 上限: %s", rel)
		}
		total += info.Size()
		if total > luaTotalLimit {
			return fmt.Errorf("Lua 源码总量超过 8MB 上限")
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return err
		}
		files[rel] = string(data)
		return nil
	})
	if err != nil {
		http.Error(w, "读取 Lua 源码失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if _, ok := files[s.luaEntry]; !ok {
		http.Error(w, "未找到 Lua 入口: "+s.luaEntry, http.StatusInternalServerError)
		return
	}

	payload := luaManifest{
		Entry: s.luaEntry,
		Files: files,
		App: luaManifestApp{
			Name:        s.app.Name,
			Package:     s.app.Package,
			VersionName: s.app.VersionName,
			VersionCode: s.app.VersionCode,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "序列化 Lua 清单失败", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method == http.MethodHead {
		return
	}
	w.Write(data)
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

// shimScript 只依赖标准浏览器 API；桥调用优先使用 mock，其次浏览器内 Lua，
// 都没有时明确 reject。
const shimScript = `(function () {
  var listeners = {};
  function resolveMock(name) {
    var mock = window.__berbaMock;
    return mock && Object.prototype.hasOwnProperty.call(mock, name) ? mock[name] : undefined;
  }
  var api = {
    call: function (name, args) {
      var entry = resolveMock(name);
      if (entry !== undefined) {
        try {
          return Promise.resolve(typeof entry === 'function' ? entry(args) : entry);
        } catch (e) {
          return Promise.reject(e);
        }
      }
      if (window.__berbaLuaReady) {
        return window.__berbaLuaReady.then(function (rt) {
          return rt.call(name, args);
        }).catch(function (e) {
          console.error('[berba dev] 桥调用失败: ' + name, e);
          throw e;
        });
      }
      console.warn('[berba dev] 桥调用未实现: ' + name);
      return Promise.reject(new Error('berba dev: 桥调用 ' + name + ' 未实现（未启用 Lua，可在 dev/mock.js 中 mock）'));
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

// luaBootstrap 在启用 Lua 时追加到 shim 之后：按需加载 fengari 与运行时，
// 再把 /__berba/lua.json 交给 __berbaLua.init。
const luaBootstrap = `(function () {
  function load(src) {
    return new Promise(function (resolve, reject) {
      var script = document.createElement('script');
      script.src = src;
      script.async = false;
      script.onload = function () { resolve(); };
      script.onerror = function () { reject(new Error('无法加载 ' + src)); };
      (document.head || document.documentElement).appendChild(script);
    });
  }
  window.__berbaLuaReady = load('/__berba/fengari.js')
    .then(function () { return load('/__berba/lua-runtime.js'); })
    .then(function () {
      return fetch('/__berba/lua.json').then(function (res) {
        if (!res.ok) throw new Error('读取 Lua 清单失败: HTTP ' + res.status);
        return res.json();
      });
    })
    .then(function (cfg) { return window.__berbaLua.init(cfg); })
    .catch(function (e) {
      console.error('[berba dev] Lua 运行时初始化失败', e);
      throw e;
    });
  window.__berbaLuaReady.catch(function () { /* 由具体调用方处理 */ });
})();
`
