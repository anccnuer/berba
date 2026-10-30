// Package capability 定义声明式能力规格，并生成 Android 壳注册表、能力清单与文档。
package capability

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

//go:generate go run ./cmd/berbacap

const (
	// Schema 是能力规格与生成物使用的版本号。
	Schema = 1

	KindCore       = "core"
	KindCapability = "capability"

	// NativeABIVersion 为壳支持的原生插件 ABI 版本。
	NativeABIVersion = 1
)

// Spec 描述一个内置能力（或核心模块集合）。
type Spec struct {
	Schema      int                 `json:"schema"`
	Name        string              `json:"name"`
	Kind        string              `json:"kind"`
	Summary     string              `json:"summary"`
	RequiresLua bool                `json:"requiresLua"`
	Permissions []string            `json:"permissions"`
	Components  []string            `json:"components"`
	Resources   []string            `json:"resources"`
	Installer   string              `json:"installer"`
	Modules     map[string]Module   `json:"modules"`
	Globals     map[string]Function `json:"globals"`
	Events      map[string]Event    `json:"events"`
}

// Module 对应 Lua 中的一个全局表。
type Module struct {
	Browser   string              `json:"browser"`
	Functions map[string]Function `json:"functions"`
}

// Function 描述一个可调用函数；Async 表示回调异步执行，Foreground 表示需要前台界面。
type Function struct {
	Async      bool   `json:"async"`
	Foreground bool   `json:"foreground"`
	Doc        string `json:"doc"`
}

// Event 描述事件归属方与前台依赖。
type Event struct {
	Owner      string `json:"owner"`
	Foreground bool   `json:"foreground"`
	Doc        string `json:"doc"`
}

// Registry 保存全部内置规格。
type Registry struct {
	specs map[string]*Spec
}

func newRegistry(specs []*Spec) *Registry {
	m := make(map[string]*Spec, len(specs))
	for _, s := range specs {
		m[s.Name] = s
	}
	return &Registry{specs: m}
}

// Spec 返回指定名称的规格。
func (r *Registry) Spec(name string) (*Spec, bool) {
	s, ok := r.specs[name]
	return s, ok
}

// Names 返回排序后的规格名。
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.specs))
	for name := range r.specs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Specs 返回按名称排序的规格。
func (r *Registry) Specs() []*Spec {
	names := r.Names()
	specs := make([]*Spec, 0, len(names))
	for _, name := range names {
		specs = append(specs, r.specs[name])
	}
	return specs
}

// Capabilities 返回所有可启用能力名（不含 core），已排序。
func (r *Registry) Capabilities() []string {
	var names []string
	for _, s := range r.Specs() {
		if s.Kind == KindCapability {
			names = append(names, s.Name)
		}
	}
	return names
}

// ResolvedModule 是合并后的模块。
type ResolvedModule struct {
	Name      string
	Browser   string
	Functions []ResolvedFunction
}

// ResolvedFunction 是合并后的函数。
type ResolvedFunction struct {
	Name       string
	Async      bool
	Foreground bool
	Doc        string
}

// ResolvedEvent 是合并后的事件。
type ResolvedEvent struct {
	Owner      string
	Name       string
	Foreground bool
	Doc        string
}

// ResolvedNativePlugin 是构建期解析出的原生插件元数据。
type ResolvedNativePlugin struct {
	Name    string
	LibName string
	Config  string
	Modules []ResolvedNativeModule
}

// ResolvedNativeModule 是插件暴露的 native.<module> 表。
type ResolvedNativeModule struct {
	Name      string
	Functions []ResolvedNativeFunction
}

// ResolvedNativeFunction 是插件函数；Async 表示需要 Lua 回调。
type ResolvedNativeFunction struct {
	Name  string
	Async bool
}

// Resolved 是一次构建最终启用的能力集合。
type Resolved struct {
	Capabilities      []string
	Modules           []ResolvedModule
	Globals           []ResolvedFunction
	Events            []ResolvedEvent
	Permissions       []string
	Components        []string
	Resources         []string
	Installers        []string
	RequiresLua       bool
	ExtractNativeLibs bool
	NativePlugins     []ResolvedNativePlugin

	enabled map[string]bool
}

// Has 报告某个能力（或 core）是否启用。
func (r *Resolved) Has(name string) bool {
	if r == nil {
		return false
	}
	if r.enabled != nil {
		if enabled, ok := r.enabled[name]; ok {
			return enabled
		}
	}
	for _, cap := range r.Capabilities {
		if cap == name {
			return true
		}
	}
	return false
}

// SetNativePlugins 写入构建期解析的原生插件，并按名称排序以保证确定性。
func (r *Resolved) SetNativePlugins(plugins []ResolvedNativePlugin) {
	r.NativePlugins = append([]ResolvedNativePlugin(nil), plugins...)
	sort.Slice(r.NativePlugins, func(i, j int) bool {
		return r.NativePlugins[i].Name < r.NativePlugins[j].Name
	})
	for i := range r.NativePlugins {
		sort.Slice(r.NativePlugins[i].Modules, func(a, b int) bool {
			return r.NativePlugins[i].Modules[a].Name < r.NativePlugins[i].Modules[b].Name
		})
		for j := range r.NativePlugins[i].Modules {
			fns := r.NativePlugins[i].Modules[j].Functions
			sort.Slice(fns, func(a, b int) bool { return fns[a].Name < fns[b].Name })
		}
	}
	r.ExtractNativeLibs = len(r.NativePlugins) > 0
}

// Request 是构建期请求启用的能力。
type Request struct {
	Capabilities []string
}

var (
	nameRE       = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	functionRE   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	permissionRE = regexp.MustCompile(`^android\.permission\.[A-Z][A-Z0-9_]*$`)
	classRE      = regexp.MustCompile(`^[A-Z][A-Za-z0-9_]*$`)
)

var allowedComponents = map[string]bool{
	"task_job_service":    true,
	"task_alarm_receiver": true,
	"task_boot_receiver":  true,
	"widget_provider":     true,
}

var allowedResources = map[string]bool{
	"notification_icon": true,
	"widget_drawable":   true,
	"widget_info":       true,
	"widget_layout":     true,
}

var allowedBrowser = map[string]bool{
	"":        true,
	"full":    true,
	"partial": true,
	"stub":    true,
}

var allowedOwners = map[string]bool{
	"app": true,
	"web": true,
}

func validateSpec(s *Spec) error {
	if s.Schema != Schema {
		return fmt.Errorf("能力规格 %q 的 schema 必须为 %d", s.Name, Schema)
	}
	if !nameRE.MatchString(s.Name) {
		return fmt.Errorf("能力名称不合法: %q", s.Name)
	}
	switch s.Kind {
	case KindCore, KindCapability:
	default:
		return fmt.Errorf("能力 %q 的 kind 只能是 core 或 capability", s.Name)
	}
	if strings.TrimSpace(s.Summary) == "" {
		return fmt.Errorf("能力 %q 缺少 summary", s.Name)
	}
	if !classRE.MatchString(s.Installer) {
		return fmt.Errorf("能力 %q 的 installer 不合法: %q", s.Name, s.Installer)
	}
	for _, perm := range s.Permissions {
		if !permissionRE.MatchString(perm) {
			return fmt.Errorf("能力 %q 的权限不合法: %q", s.Name, perm)
		}
	}
	for _, c := range s.Components {
		if !allowedComponents[c] {
			return fmt.Errorf("能力 %q 引用了未知组件模板: %q", s.Name, c)
		}
	}
	for _, res := range s.Resources {
		if !allowedResources[res] {
			return fmt.Errorf("能力 %q 引用了未知资源生成器: %q", s.Name, res)
		}
	}
	if len(s.Modules) == 0 && len(s.Globals) == 0 && len(s.Events) == 0 {
		return fmt.Errorf("能力 %q 未声明任何模块、全局函数或事件", s.Name)
	}
	for name, m := range s.Modules {
		if !nameRE.MatchString(name) {
			return fmt.Errorf("能力 %q 的模块名不合法: %q", s.Name, name)
		}
		if !allowedBrowser[m.Browser] {
			return fmt.Errorf("能力 %q 的模块 %q 的 browser 只能是 full/partial/stub", s.Name, name)
		}
		if len(m.Functions) == 0 {
			return fmt.Errorf("能力 %q 的模块 %q 未声明函数", s.Name, name)
		}
		for fn, f := range m.Functions {
			if !functionRE.MatchString(fn) {
				return fmt.Errorf("能力 %q 的模块 %q 函数名不合法: %q", s.Name, name, fn)
			}
			if strings.TrimSpace(f.Doc) == "" {
				return fmt.Errorf("能力 %q 的模块 %q 函数 %q 缺少 doc", s.Name, name, fn)
			}
		}
	}
	for name, f := range s.Globals {
		if !functionRE.MatchString(name) {
			return fmt.Errorf("能力 %q 的全局函数名不合法: %q", s.Name, name)
		}
		if strings.TrimSpace(f.Doc) == "" {
			return fmt.Errorf("能力 %q 的全局函数 %q 缺少 doc", s.Name, name)
		}
	}
	for name, e := range s.Events {
		if !functionRE.MatchString(name) {
			return fmt.Errorf("能力 %q 的事件名不合法: %q", s.Name, name)
		}
		if !allowedOwners[e.Owner] {
			return fmt.Errorf("能力 %q 的事件 %q owner 只能是 app 或 web", s.Name, name)
		}
		if strings.TrimSpace(e.Doc) == "" {
			return fmt.Errorf("能力 %q 的事件 %q 缺少 doc", s.Name, name)
		}
	}
	return nil
}

func sortUnique(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}
