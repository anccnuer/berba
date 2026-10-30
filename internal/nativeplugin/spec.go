// Package nativeplugin 解析原生插件规格、解析 ABI 库并校验 Android ELF。
package nativeplugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"berba/internal/config"
)

const (
	// ABIVersion 是当前壳与插件共享的 ABI 版本。
	ABIVersion = 1
	// MaxLibSize 限制单个插件库大小，避免误打包大文件。
	MaxLibSize = 32 << 20
	// MaxPlugins 与 app.json 的限制保持一致。
	MaxPlugins = 16
)

// SupportedABIs 按优先级排列；构建取所有插件的交集。
var SupportedABIs = []string{"arm64-v8a", "armeabi-v7a", "x86_64"}

var (
	pluginRE   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	moduleRE   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	functionRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
)

var reservedModules = map[string]bool{
	"app": true, "web": true, "bridge": true, "http": true, "file": true,
	"store": true, "permission": true, "ui": true, "device": true, "json": true,
	"notify": true, "task": true, "native": true, "list": true, "jni": true,
}

// Spec 是插件目录下 spec.json 的内容。
type Spec struct {
	Name       string            `json:"name"`
	ABIVersion int               `json:"abiVersion"`
	Version    string            `json:"version"`
	Modules    map[string]Module `json:"modules"`
}

// Module 对应 native.<module> 表。
type Module struct {
	Functions map[string]Function `json:"functions"`
}

// Function 描述一个插件函数；异步函数需要 Lua 回调。
type Function struct {
	Async bool `json:"async"`
}

// Request 是一次构建的原生插件请求。
type Request struct {
	Plugins        []config.NativePlugin
	BuiltinModules []string
}

// Plugin 是解析后的插件。
type Plugin struct {
	Name    string
	LibName string
	Config  string // 规范化 JSON，保证构建确定性
	Spec    *Spec
	Libs    map[string]string // abi -> 绝对路径
	ABIs    []string
}

// Result 是全部插件的交集与元数据。
type Result struct {
	Plugins  []Plugin
	ABIs     []string
	Warnings []string
}

// LoadSpec 严格解析插件 spec.json。
func LoadSpec(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取插件规格失败: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("插件规格为空: %s", path)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var spec Spec
	if err := dec.Decode(&spec); err != nil {
		return nil, fmt.Errorf("解析插件规格失败: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("插件规格包含多余内容: %s", path)
	}
	if err := spec.validate(); err != nil {
		return nil, err
	}
	return &spec, nil
}

func (s *Spec) validate() error {
	if !pluginRE.MatchString(s.Name) {
		return fmt.Errorf("插件名称不合法: %q", s.Name)
	}
	if s.ABIVersion != ABIVersion {
		return fmt.Errorf("插件 %q 的 abiVersion 必须为 %d（实际 %d）", s.Name, ABIVersion, s.ABIVersion)
	}
	if strings.TrimSpace(s.Version) == "" {
		return fmt.Errorf("插件 %q 缺少 version", s.Name)
	}
	if len(s.Modules) == 0 {
		return fmt.Errorf("插件 %q 未声明模块", s.Name)
	}
	if len(s.Modules) > 64 {
		return fmt.Errorf("插件 %q 模块过多（最多 64 个）", s.Name)
	}
	total := 0
	for name, m := range s.Modules {
		if !moduleRE.MatchString(name) {
			return fmt.Errorf("插件 %q 的模块名不合法: %q", s.Name, name)
		}
		if reservedModules[name] {
			return fmt.Errorf("插件 %q 的模块名 %q 为保留名", s.Name, name)
		}
		if len(m.Functions) == 0 {
			return fmt.Errorf("插件 %q 的模块 %q 未声明函数", s.Name, name)
		}
		total += len(m.Functions)
		for fn := range m.Functions {
			if !functionRE.MatchString(fn) {
				return fmt.Errorf("插件 %q 的模块 %q 函数名不合法: %q", s.Name, name, fn)
			}
		}
	}
	if total > 256 {
		return fmt.Errorf("插件 %q 函数过多（最多 256 个）", s.Name)
	}
	return nil
}

// Resolve 读取每个插件的 spec.json 和 ABI 库，并计算 ABI 交集。
func Resolve(req Request) (*Result, error) {
	if len(req.Plugins) > MaxPlugins {
		return nil, fmt.Errorf("native.plugins 最多 %d 个", MaxPlugins)
	}
	reserved := map[string]bool{}
	for name := range reservedModules {
		reserved[name] = true
	}
	for _, name := range req.BuiltinModules {
		reserved[name] = true
	}

	out := &Result{}
	moduleOwner := map[string]string{}
	abiCount := map[string]int{}
	for _, cfg := range req.Plugins {
		dir, err := filepath.Abs(cfg.Dir)
		if err != nil {
			return nil, err
		}
		spec, err := LoadSpec(filepath.Join(dir, "spec.json"))
		if err != nil {
			return nil, fmt.Errorf("插件 %q: %w", cfg.Name, err)
		}
		if spec.Name != cfg.Name {
			return nil, fmt.Errorf("插件 %q 的 spec.json name=%q 不一致", cfg.Name, spec.Name)
		}
		for name := range spec.Modules {
			if reserved[name] {
				return nil, fmt.Errorf("插件 %q 的模块名 %q 与内置命名空间冲突", cfg.Name, name)
			}
			if owner, ok := moduleOwner[name]; ok {
				return nil, fmt.Errorf("模块名 %q 同时出现在插件 %q 与 %q 中", name, owner, cfg.Name)
			}
			moduleOwner[name] = cfg.Name
		}

		plugin := Plugin{
			Name:    spec.Name,
			LibName: "libberba_" + spec.Name + ".so",
			Spec:    spec,
			Libs:    map[string]string{},
		}
		plugin.Config, err = normalizeConfig(cfg.Config)
		if err != nil {
			return nil, fmt.Errorf("插件 %q: %w", cfg.Name, err)
		}
		for _, abi := range SupportedABIs {
			p := filepath.Join(dir, "lib", abi, plugin.LibName)
			st, err := os.Lstat(p)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("插件 %q 读取 %s 失败: %w", cfg.Name, abi, err)
			}
			if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
				return nil, fmt.Errorf("插件 %q 的 %s 库不是普通文件: %s", cfg.Name, abi, p)
			}
			plugin.Libs[abi] = p
			plugin.ABIs = append(plugin.ABIs, abi)
			abiCount[abi]++
		}
		if len(plugin.ABIs) == 0 {
			return nil, fmt.Errorf("插件 %q 缺少支持的 ABI 库（期望 lib/<abi>/%s）", cfg.Name, plugin.LibName)
		}
		out.Plugins = append(out.Plugins, plugin)
	}

	if len(out.Plugins) == 0 {
		return out, nil
	}
	for _, abi := range SupportedABIs {
		if abiCount[abi] == len(out.Plugins) {
			out.ABIs = append(out.ABIs, abi)
		}
	}
	if len(out.ABIs) == 0 {
		return nil, fmt.Errorf("插件之间没有共同的 ABI（%s）", describeABIs(out.Plugins))
	}
	hasARM64 := false
	for _, abi := range out.ABIs {
		if abi == "arm64-v8a" {
			hasARM64 = true
		}
	}
	if !hasARM64 {
		out.Warnings = append(out.Warnings, "原生插件未包含 arm64-v8a，主流真机可能无法加载")
	}
	sort.Slice(out.Plugins, func(i, j int) bool { return out.Plugins[i].Name < out.Plugins[j].Name })
	return out, nil
}

func describeABIs(plugins []Plugin) string {
	parts := make([]string, 0, len(plugins))
	for _, p := range plugins {
		parts = append(parts, p.Name+"="+strings.Join(p.ABIs, "/"))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func normalizeConfig(raw json.RawMessage) (string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return "{}", nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value interface{}
	if err := dec.Decode(&value); err != nil {
		return "", fmt.Errorf("config 不是合法 JSON: %w", err)
	}
	obj, ok := value.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("config 必须是 JSON 对象")
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return "", fmt.Errorf("规范化 config 失败: %w", err)
	}
	return string(data), nil
}
