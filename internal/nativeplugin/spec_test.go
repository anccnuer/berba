package nativeplugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"berba/internal/config"
)

func writePlugin(t *testing.T, dir, name, spec string, abis ...string) config.NativePlugin {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spec.json"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	libName := "libberba_" + name + ".so"
	for _, abi := range abis {
		libDir := filepath.Join(dir, "lib", abi)
		if err := os.MkdirAll(libDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(libDir, libName), []byte("elf"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return config.NativePlugin{Name: name, Dir: dir}
}

func validSpec(name, module string) string {
	return `{
  "name": "` + name + `",
  "abiVersion": 1,
  "version": "1.0.0",
  "modules": {"` + module + `": {"functions": {"call": {"async": false}, "run": {"async": true}}}}
}`
}

func TestLoadSpecRejectsInvalid(t *testing.T) {
	cases := []struct {
		name string
		spec string
		want string
	}{
		{"empty", "  \n", "为空"},
		{"unknown field", `{"name":"echo","abiVersion":1,"version":"1","modules":{"echo":{"functions":{"f":{}}}},"extra":1}`, "解析插件规格失败"},
		{"trailing", validSpec("echo", "echo") + `{}`, "多余内容"},
		{"bad name", `{"name":"Echo","abiVersion":1,"version":"1","modules":{"echo":{"functions":{"f":{}}}}}`, "插件名称不合法"},
		{"bad abi", `{"name":"echo","abiVersion":2,"version":"1","modules":{"echo":{"functions":{"f":{}}}}}`, "abiVersion"},
		{"no version", `{"name":"echo","abiVersion":1,"modules":{"echo":{"functions":{"f":{}}}}}`, "缺少 version"},
		{"no module", `{"name":"echo","abiVersion":1,"version":"1","modules":{}}`, "未声明模块"},
		{"reserved module", validSpec("echo", "task"), "保留名"},
		{"bad module", validSpec("echo", "Echo"), "模块名不合法"},
		{"bad function", `{"name":"echo","abiVersion":1,"version":"1","modules":{"echo":{"functions":{"a-b":{}}}}}`, "函数名不合法"},
		{"empty functions", `{"name":"echo","abiVersion":1,"version":"1","modules":{"echo":{"functions":{}}}}`, "未声明函数"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "spec.json")
			if err := os.WriteFile(path, []byte(tc.spec), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadSpec(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadSpec error = %v, want contains %q", err, tc.want)
			}
		})
	}
}

func TestLoadSpecValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.json")
	if err := os.WriteFile(path, []byte(validSpec("echo", "echo")), 0o644); err != nil {
		t.Fatal(err)
	}
	spec, err := LoadSpec(path)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Name != "echo" || spec.ABIVersion != 1 || !spec.Modules["echo"].Functions["run"].Async {
		t.Fatalf("unexpected spec: %+v", spec)
	}
}

func TestResolveIntersection(t *testing.T) {
	root := t.TempDir()
	a := writePlugin(t, filepath.Join(root, "a"), "alpha", validSpec("alpha", "echo"), "arm64-v8a", "armeabi-v7a")
	b := writePlugin(t, filepath.Join(root, "b"), "beta", validSpec("beta", "text"), "arm64-v8a", "x86_64")
	b.Config = json.RawMessage(`{"b":2,"a":1}`)

	res, err := Resolve(Request{Plugins: []config.NativePlugin{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Plugins) != 2 {
		t.Fatalf("plugins = %d", len(res.Plugins))
	}
	if len(res.ABIs) != 1 || res.ABIs[0] != "arm64-v8a" {
		t.Fatalf("ABIs = %v", res.ABIs)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("warnings = %v", res.Warnings)
	}
	if res.Plugins[0].Name != "alpha" || res.Plugins[1].Name != "beta" {
		t.Fatalf("plugins not sorted: %v", res.Plugins)
	}
	if res.Plugins[0].LibName != "libberba_alpha.so" {
		t.Fatalf("lib name = %q", res.Plugins[0].LibName)
	}
	if got := res.Plugins[0].Config; got != "{}" {
		t.Fatalf("default config = %q", got)
	}
	if got := res.Plugins[1].Config; got != `{"a":1,"b":2}` {
		t.Fatalf("normalized config = %q", got)
	}
	if len(res.Plugins[0].Libs) != 2 {
		t.Fatalf("libs = %v", res.Plugins[0].Libs)
	}
}

func TestResolveNoARM64Warns(t *testing.T) {
	root := t.TempDir()
	p := writePlugin(t, filepath.Join(root, "p"), "alpha", validSpec("alpha", "echo"), "armeabi-v7a", "x86_64")
	res, err := Resolve(Request{Plugins: []config.NativePlugin{p}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ABIs) != 2 {
		t.Fatalf("ABIs = %v", res.ABIs)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "arm64-v8a") {
		t.Fatalf("warnings = %v", res.Warnings)
	}
}

func TestResolveErrors(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name    string
		plugins func(t *testing.T) []config.NativePlugin
		builtin []string
		want    string
	}{
		{
			name: "no common ABI",
			plugins: func(t *testing.T) []config.NativePlugin {
				return []config.NativePlugin{
					writePlugin(t, filepath.Join(root, "a"), "alpha", validSpec("alpha", "echo"), "arm64-v8a"),
					writePlugin(t, filepath.Join(root, "b"), "beta", validSpec("beta", "text"), "x86_64"),
				}
			},
			want: "没有共同的 ABI",
		},
		{
			name: "duplicate module",
			plugins: func(t *testing.T) []config.NativePlugin {
				return []config.NativePlugin{
					writePlugin(t, filepath.Join(root, "c"), "alpha", validSpec("alpha", "echo"), "arm64-v8a"),
					writePlugin(t, filepath.Join(root, "d"), "beta", validSpec("beta", "echo"), "arm64-v8a"),
				}
			},
			want: "同时出现",
		},
		{
			name: "builtin module",
			plugins: func(t *testing.T) []config.NativePlugin {
				return []config.NativePlugin{
					writePlugin(t, filepath.Join(root, "e"), "alpha", validSpec("alpha", "audio"), "arm64-v8a"),
				}
			},
			builtin: []string{"audio"},
			want:    "内置命名空间冲突",
		},
		{
			name: "missing lib",
			plugins: func(t *testing.T) []config.NativePlugin {
				return []config.NativePlugin{
					writePlugin(t, filepath.Join(root, "f"), "alpha", validSpec("alpha", "echo")),
				}
			},
			want: "缺少支持的 ABI 库",
		},
		{
			name: "name mismatch",
			plugins: func(t *testing.T) []config.NativePlugin {
				p := writePlugin(t, filepath.Join(root, "g"), "alpha", validSpec("beta", "echo"), "arm64-v8a")
				return []config.NativePlugin{p}
			},
			want: "不一致",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Resolve(Request{Plugins: tc.plugins(t), BuiltinModules: tc.builtin})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Resolve error = %v, want contains %q", err, tc.want)
			}
		})
	}
}

func TestResolveRejectsBadConfig(t *testing.T) {
	root := t.TempDir()
	for _, raw := range []string{`[]`, `"x"`, `1`} {
		p := writePlugin(t, filepath.Join(root, "h"), "alpha", validSpec("alpha", "echo"), "arm64-v8a")
		p.Config = json.RawMessage(raw)
		_, err := Resolve(Request{Plugins: []config.NativePlugin{p}})
		if err == nil || !strings.Contains(err.Error(), "config 必须是 JSON 对象") {
			t.Fatalf("config %s: error = %v", raw, err)
		}
	}
}

func TestResolveNoPlugins(t *testing.T) {
	res, err := Resolve(Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Plugins) != 0 || len(res.ABIs) != 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
}
