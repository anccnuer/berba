package capability

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func readRepoFile(t *testing.T, root string, rel ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(append([]string{root}, rel...)...))
	if err != nil {
		t.Fatalf("读取文档失败: %v", err)
	}
	return string(data)
}

// TestHandwrittenDocsCoverSpecAPI 保证人工维护的 API 文档不落后于能力规格。
func TestHandwrittenDocsCoverSpecAPI(t *testing.T) {
	reg, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	root := repoRoot(t)
	for _, rel := range [][]string{
		{"docs", "lua-api.md"},
		{"templates", "init", "AGENTS.md"},
	} {
		name := filepath.Join(root, filepath.FromSlash(filepath.Join(rel...)))
		text := readRepoFile(t, root, rel...)
		var missing []string
		for _, spec := range reg.Specs() {
			for module, m := range spec.Modules {
				for fn := range m.Functions {
					if !strings.Contains(text, module+"."+fn) {
						missing = append(missing, spec.Name+" 模块 "+module+"."+fn)
					}
				}
			}
			for global := range spec.Globals {
				if !strings.Contains(text, global+"(") {
					missing = append(missing, spec.Name+" 全局函数 "+global)
				}
			}
			for event := range spec.Events {
				if !mentionsEvent(text, event) {
					missing = append(missing, spec.Name+" 事件 "+event)
				}
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			t.Errorf("%s 缺少规格中的 API: %s", name, strings.Join(missing, "、"))
		}
	}
}

// TestSpecModulesHaveDocSections 要求 docs/lua-api.md 为每个模块保留独立章节。
func TestSpecModulesHaveDocSections(t *testing.T) {
	reg, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	text := readRepoFile(t, repoRoot(t), "docs", "lua-api.md")
	for _, spec := range reg.Specs() {
		for module := range spec.Modules {
			if !hasSection(text, module) {
				t.Errorf("docs/lua-api.md 缺少模块章节: ## %s（能力 %s）", module, spec.Name)
			}
		}
	}
}

// mentionsEvent 接受文档中的三种写法：`name`、`name(args)`、"name"。
func mentionsEvent(text, event string) bool {
	for _, form := range []string{"`" + event + "`", "`" + event + "(", `"` + event + `"`} {
		if strings.Contains(text, form) {
			return true
		}
	}
	return false
}

func isNameByte(b byte) bool {
	return b == '_' || b == '-' ||
		(b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// hasSection 匹配 "## <module>" 标题，且模块名后不接其他名称字符。
func hasSection(text, module string) bool {
	for _, line := range strings.Split(text, "\n") {
		rest, ok := strings.CutPrefix(line, "## ")
		if !ok || !strings.HasPrefix(rest, module) {
			continue
		}
		if len(rest) == len(module) || !isNameByte(rest[len(module)]) {
			return true
		}
	}
	return false
}

// TestCapabilitiesListedInConfigDoc 要求每个可启用能力在配置文档中被点名。
func TestCapabilitiesListedInConfigDoc(t *testing.T) {
	reg, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	text := readRepoFile(t, repoRoot(t), "docs", "config.md")
	for _, name := range reg.Capabilities() {
		if !strings.Contains(text, "`"+name+"`") {
			t.Errorf("docs/config.md 未说明能力 %q", name)
		}
	}
}
