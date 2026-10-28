package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	LocalHost       = "appassets.androidhost"
	LocalOrigin     = "https://" + LocalHost
	DefaultStartURL = LocalOrigin + "/index.html"
)

var packageRE = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z][a-zA-Z0-9_]*)+$`)
var deepLinkSchemeRE = regexp.MustCompile(`^[a-z][a-z0-9+.\-]{1,31}$`)

// DeepLink 是一条自定义 scheme 深链（Manifest intent-filter 用 scheme+host 匹配）。
type DeepLink struct {
	Scheme string
	Host   string
}

// parseDeepLinks 校验并去重深链，按 scheme+host 排序保证 Manifest 确定性。
func parseDeepLinks(raw []string) ([]DeepLink, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > 16 {
		return nil, fmt.Errorf("deepLinks 最多 16 条")
	}
	seen := map[string]bool{}
	var links []DeepLink
	for _, item := range raw {
		s := strings.TrimSpace(item)
		if s == "" {
			return nil, fmt.Errorf("deepLinks 不能包含空字符串")
		}
		u, err := url.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("deepLinks 不合法: %q", item)
		}
		scheme := strings.ToLower(u.Scheme)
		if scheme == "" {
			return nil, fmt.Errorf("deepLinks 必须带 scheme: %q", item)
		}
		switch scheme {
		case "http", "https", "file", "content", "data":
			return nil, fmt.Errorf("deepLinks 不支持 %s: 协议", scheme)
		}
		if !deepLinkSchemeRE.MatchString(scheme) {
			return nil, fmt.Errorf("deepLinks scheme 不合法: %q", item)
		}
		host := strings.ToLower(u.Host)
		if strings.ContainsAny(host, " \t\n\"'<>") {
			return nil, fmt.Errorf("deepLinks host 不合法: %q", item)
		}
		key := scheme + "|" + host
		if seen[key] {
			continue
		}
		seen[key] = true
		links = append(links, DeepLink{Scheme: scheme, Host: host})
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].Scheme != links[j].Scheme {
			return links[i].Scheme < links[j].Scheme
		}
		return links[i].Host < links[j].Host
	})
	return links, nil
}

type WebConfig struct {
	Dir            string   `json:"dir"`
	StartURL       string   `json:"startUrl"`
	SPA            bool     `json:"spa"`
	AllowedOrigins []string `json:"allowedOrigins"`
}

type LuaConfig struct {
	Dir   string `json:"dir"`
	Entry string `json:"entry"`
}

type HTTPConfig struct {
	AllowPrivateNetwork bool `json:"allowPrivateNetwork"`
}

type Config struct {
	Name        string            `json:"name"`
	Package     string            `json:"package"`
	VersionName string            `json:"versionName"`
	VersionCode int               `json:"versionCode"`
	Icon        string            `json:"icon"`
	Orientation string            `json:"orientation"`
	Theme       string            `json:"theme"`
	Cleartext   bool              `json:"cleartext"`
	Permissions []string          `json:"permissions"`
	DeepLinks   []string          `json:"deepLinks"`
	MetaData    map[string]string `json:"metaData"`
	Web         WebConfig         `json:"web"`
	Lua         LuaConfig         `json:"lua"`
	HTTP        HTTPConfig        `json:"http"`
	Debug       bool              `json:"debug"`
}

type Project struct {
	Root           string
	ConfigPath     string
	Config         Config
	Permissions    []string
	DeepLinks      []DeepLink
	IconPath       string
	WebDir         string
	LuaDir         string
	HasLua         bool
	LocalStart     bool
	StartURL       *url.URL
	AllowedOrigins []string
	Warnings       []string
}

func Load(path string) (*Project, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}

	cfg := Config{
		VersionCode: 1,
		Orientation: "auto",
		Theme:       "light",
		Web: WebConfig{
			Dir:      "www",
			StartURL: DefaultStartURL,
		},
		Lua: LuaConfig{
			Dir:   "lua",
			Entry: "main.lua",
		},
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("解析 app.json 失败: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("app.json 只能包含一个 JSON 对象")
		}
		return nil, fmt.Errorf("解析 app.json 失败: %w", err)
	}

	p := &Project{
		Root:       filepath.Dir(abs),
		ConfigPath: abs,
		Config:     cfg,
	}
	if err := p.finish(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Project) finish() error {
	cfg := &p.Config
	cfg.Name = strings.TrimSpace(cfg.Name)
	cfg.Package = strings.TrimSpace(cfg.Package)
	cfg.VersionName = strings.TrimSpace(cfg.VersionName)

	if cfg.Name == "" {
		return fmt.Errorf("name 不能为空")
	}
	if err := ValidatePackage(cfg.Package); err != nil {
		return err
	}
	if cfg.VersionName == "" {
		return fmt.Errorf("versionName 不能为空")
	}
	if cfg.VersionCode < 1 {
		return fmt.Errorf("versionCode 必须大于 0")
	}
	switch cfg.Orientation {
	case "auto", "portrait", "landscape":
	default:
		return fmt.Errorf("orientation 只能是 auto / portrait / landscape")
	}
	switch cfg.Theme {
	case "light", "dark", "device":
	default:
		return fmt.Errorf("theme 只能是 light / dark / device")
	}

	if strings.TrimSpace(cfg.Web.Dir) == "" {
		cfg.Web.Dir = "www"
	}
	if cfg.Web.StartURL == "" {
		cfg.Web.StartURL = DefaultStartURL
	}
	webDir, err := resolveWithin(p.Root, cfg.Web.Dir, "web.dir")
	if err != nil {
		return err
	}
	p.WebDir = webDir
	info, err := os.Stat(p.WebDir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("web.dir 不存在或不是目录: %s", p.WebDir)
	}

	start, err := url.Parse(cfg.Web.StartURL)
	if err != nil || start.Host == "" {
		return fmt.Errorf("web.startUrl 不合法: %q", cfg.Web.StartURL)
	}
	switch start.Scheme {
	case "https", "http":
	default:
		return fmt.Errorf("web.startUrl 只支持 http/https: %q", cfg.Web.StartURL)
	}
	if start.Host == LocalHost {
		if start.Scheme != "https" {
			return fmt.Errorf("本地页面必须使用 https://%s/", LocalHost)
		}
		p.LocalStart = true
		rel := strings.TrimPrefix(start.Path, "/")
		if rel == "" {
			rel = "index.html"
		}
		rel = filepath.FromSlash(filepath.Clean(rel))
		if rel == "." || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("web.startUrl 路径不合法: %q", start.Path)
		}
		full := filepath.Join(p.WebDir, rel)
		if !within(p.WebDir, full) {
			return fmt.Errorf("web.startUrl 路径越界: %q", start.Path)
		}
		if st, err := os.Stat(full); err != nil || st.IsDir() {
			return fmt.Errorf("本地入口文件不存在: %s", full)
		}
	} else {
		if start.Scheme == "http" && !cfg.Cleartext {
			return fmt.Errorf("远程 http 页面需要设置 cleartext: true（不推荐）")
		}
	}
	p.StartURL = start

	origins := make([]string, 0, len(cfg.Web.AllowedOrigins)+1)
	seen := map[string]bool{}
	for _, raw := range cfg.Web.AllowedOrigins {
		o, err := normalizeOrigin(raw)
		if err != nil {
			return err
		}
		if !seen[o] {
			seen[o] = true
			origins = append(origins, o)
		}
	}
	if p.LocalStart {
		if !seen[LocalOrigin] {
			seen[LocalOrigin] = true
			origins = append(origins, LocalOrigin)
		}
	} else if origin := OriginOf(start); origin != "" && !seen[origin] {
		seen[origin] = true
		origins = append(origins, origin)
	}
	p.AllowedOrigins = origins

	links, err := parseDeepLinks(cfg.DeepLinks)
	if err != nil {
		return err
	}
	p.DeepLinks = links

	luaDirSet := strings.TrimSpace(cfg.Lua.Dir) != ""
	if !luaDirSet {
		cfg.Lua.Dir = "lua"
	}
	p.LuaDir, err = resolveWithin(p.Root, cfg.Lua.Dir, "lua.dir")
	if err != nil {
		return err
	}
	if info, err := os.Stat(p.LuaDir); err == nil && info.IsDir() {
		entry := cfg.Lua.Entry
		if entry == "" {
			entry = "main.lua"
		}
		entryPath := filepath.Join(p.LuaDir, filepath.FromSlash(entry))
		if !within(p.LuaDir, entryPath) {
			return fmt.Errorf("lua.entry 路径越界: %q", entry)
		}
		if st, err := os.Stat(entryPath); err != nil || st.IsDir() {
			return fmt.Errorf("Lua 入口文件不存在: %s", entryPath)
		}
		p.HasLua = true
	} else if luaDirSet {
		p.Warnings = append(p.Warnings, fmt.Sprintf("lua.dir 不存在或不是目录，Lua 已禁用: %s", p.LuaDir))
	}

	if cfg.Icon != "" {
		p.IconPath, err = resolveWithin(p.Root, cfg.Icon, "icon")
		if err != nil {
			return err
		}
		st, err := os.Stat(p.IconPath)
		if err != nil || st.IsDir() {
			return fmt.Errorf("icon 不存在或不是文件: %s", p.IconPath)
		}
	}

	perms := make([]string, 0, len(cfg.Permissions)+1)
	seenPerm := map[string]bool{}
	addPerm := func(full string) {
		if !seenPerm[full] {
			seenPerm[full] = true
			perms = append(perms, full)
		}
	}
	for _, raw := range cfg.Permissions {
		full, err := NormalizePermission(raw)
		if err != nil {
			return err
		}
		addPerm(full)
	}
	addPerm("android.permission.INTERNET")
	p.Permissions = perms

	for name := range cfg.MetaData {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("metaData 的 key 不能为空")
		}
	}
	return nil
}

func ValidatePackage(pkg string) error {
	if !packageRE.MatchString(pkg) {
		return fmt.Errorf("package 不合法: %q（示例: com.example.myapp）", pkg)
	}
	for _, prefix := range []string{"android.", "com.android.", "java.", "javax."} {
		if strings.HasPrefix(pkg, prefix) {
			return fmt.Errorf("package 不能使用保留前缀 %q", prefix)
		}
	}
	return nil
}

func resolveWithin(root, path, field string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("%s 不能为空", field)
	}
	var p string
	if filepath.IsAbs(path) {
		p = filepath.Clean(path)
	} else {
		p = filepath.Clean(filepath.Join(root, filepath.FromSlash(path)))
	}
	if !within(root, p) {
		return "", fmt.Errorf("%s 必须位于项目目录内: %q", field, path)
	}
	return p, nil
}

func within(root, child string) bool {
	rel, err := filepath.Rel(root, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func normalizeOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("allowedOrigins 不合法: %q", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("allowedOrigins 只支持 http/https: %q", raw)
	}
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("allowedOrigins 只能是 origin，不能带路径: %q", raw)
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), nil
}

func OriginOf(u *url.URL) string {
	if u == nil || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
}

func (p *Project) OriginAllowed(origin string) bool {
	origin = strings.ToLower(strings.TrimSuffix(origin, "/"))
	for _, o := range p.AllowedOrigins {
		if strings.ToLower(o) == origin {
			return true
		}
	}
	return false
}

var permissionAliases = map[string]string{
	"INTERNET":                   "android.permission.INTERNET",
	"ACCESS_NETWORK_STATE":       "android.permission.ACCESS_NETWORK_STATE",
	"CAMERA":                     "android.permission.CAMERA",
	"RECORD_AUDIO":               "android.permission.RECORD_AUDIO",
	"ACCESS_FINE_LOCATION":       "android.permission.ACCESS_FINE_LOCATION",
	"ACCESS_COARSE_LOCATION":     "android.permission.ACCESS_COARSE_LOCATION",
	"ACCESS_BACKGROUND_LOCATION": "android.permission.ACCESS_BACKGROUND_LOCATION",
	"READ_EXTERNAL_STORAGE":      "android.permission.READ_EXTERNAL_STORAGE",
	"WRITE_EXTERNAL_STORAGE":     "android.permission.WRITE_EXTERNAL_STORAGE",
	"READ_MEDIA_IMAGES":          "android.permission.READ_MEDIA_IMAGES",
	"READ_MEDIA_VIDEO":           "android.permission.READ_MEDIA_VIDEO",
	"READ_MEDIA_AUDIO":           "android.permission.READ_MEDIA_AUDIO",
	"POST_NOTIFICATIONS":         "android.permission.POST_NOTIFICATIONS",
	"VIBRATE":                    "android.permission.VIBRATE",
	"WAKE_LOCK":                  "android.permission.WAKE_LOCK",
	"BLUETOOTH":                  "android.permission.BLUETOOTH",
	"BLUETOOTH_CONNECT":          "android.permission.BLUETOOTH_CONNECT",
	"NFC":                        "android.permission.NFC",
	"CALL_PHONE":                 "android.permission.CALL_PHONE",
	"READ_CONTACTS":              "android.permission.READ_CONTACTS",
	"READ_CALENDAR":              "android.permission.READ_CALENDAR",
	"BODY_SENSORS":               "android.permission.BODY_SENSORS",
	"ACTIVITY_RECOGNITION":       "android.permission.ACTIVITY_RECOGNITION",
	"SYSTEM_ALERT_WINDOW":        "android.permission.SYSTEM_ALERT_WINDOW",
}

var permissionNameRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

func NormalizePermission(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("permissions 中存在空项")
	}
	if strings.HasPrefix(s, "android.permission.") {
		name := strings.TrimPrefix(s, "android.permission.")
		if !permissionNameRE.MatchString(name) {
			return "", fmt.Errorf("权限名不合法: %q", raw)
		}
		return s, nil
	}
	key := strings.ToUpper(strings.ReplaceAll(s, "-", "_"))
	if full, ok := permissionAliases[key]; ok {
		return full, nil
	}
	return "", fmt.Errorf("未知权限 %q；可写完整 android.permission.X 或使用内置别名", raw)
}
