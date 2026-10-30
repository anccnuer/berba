package capability

import (
	"fmt"
	"sort"
	"strings"
)

// RenderDocs 生成 docs/capabilities.md。
func RenderDocs(r *Registry) ([]byte, error) {
	var b strings.Builder
	b.WriteString("# 能力规格\n\n")
	b.WriteString("> 本文件由 `go generate ./internal/capability` 生成，请勿手动修改。\n\n")
	b.WriteString("每个模块声明函数是否异步（`异步`）、是否需要前台界面（`前台`），以及绑定到哪个 Lua 全局表。\n")
	b.WriteString("`core` 始终安装；其余能力需要在 `app.json` 的 `capabilities` 中显式启用。\n\n")

	for _, spec := range r.Specs() {
		fmt.Fprintf(&b, "## %s（%s）\n\n%s\n\n", spec.Name, spec.Kind, spec.Summary)
		if spec.RequiresLua {
			b.WriteString("需要 Lua 运行时：是\n\n")
		}
		if len(spec.Permissions) > 0 {
			b.WriteString("权限：\n\n")
			for _, perm := range sortUnique(spec.Permissions) {
				fmt.Fprintf(&b, "- `%s`\n", perm)
			}
			b.WriteString("\n")
		}
		if len(spec.Components) > 0 {
			b.WriteString("Manifest 组件：\n\n")
			for _, c := range sortUnique(spec.Components) {
				fmt.Fprintf(&b, "- `%s`\n", c)
			}
			b.WriteString("\n")
		}
		if len(spec.Resources) > 0 {
			b.WriteString("生成资源：\n\n")
			for _, res := range sortUnique(spec.Resources) {
				fmt.Fprintf(&b, "- `%s`\n", res)
			}
			b.WriteString("\n")
		}

		modules := make([]string, 0, len(spec.Modules))
		for name := range spec.Modules {
			modules = append(modules, name)
		}
		sort.Strings(modules)
		for _, name := range modules {
			m := spec.Modules[name]
			fmt.Fprintf(&b, "### `%s`\n\n", name)
			if m.Browser != "" {
				fmt.Fprintf(&b, "浏览器实现：`%s`\n\n", m.Browser)
			}
			b.WriteString("| 函数 | 异步 | 前台 | 说明 |\n| --- | --- | --- | --- |\n")
			fns := make([]string, 0, len(m.Functions))
			for fn := range m.Functions {
				fns = append(fns, fn)
			}
			sort.Strings(fns)
			for _, fn := range fns {
				f := m.Functions[fn]
				fmt.Fprintf(&b, "| `%s.%s` | %s | %s | %s |\n", name, fn, yesNo(f.Async), yesNo(f.Foreground), escapeCell(f.Doc))
			}
			b.WriteString("\n")
		}

		if len(spec.Globals) > 0 {
			b.WriteString("### 全局函数\n\n")
			b.WriteString("| 函数 | 异步 | 前台 | 说明 |\n| --- | --- | --- | --- |\n")
			names := make([]string, 0, len(spec.Globals))
			for name := range spec.Globals {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				f := spec.Globals[name]
				fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", name, yesNo(f.Async), yesNo(f.Foreground), escapeCell(f.Doc))
			}
			b.WriteString("\n")
		}

		if len(spec.Events) > 0 {
			b.WriteString("### 事件\n\n")
			b.WriteString("| 事件 | 归属 | 前台 | 说明 |\n| --- | --- | --- | --- |\n")
			names := make([]string, 0, len(spec.Events))
			for name := range spec.Events {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				e := spec.Events[name]
				fmt.Fprintf(&b, "| `%s` | `%s` | %s | %s |\n", name, e.Owner, yesNo(e.Foreground), escapeCell(e.Doc))
			}
			b.WriteString("\n")
		}
	}
	return []byte(b.String()), nil
}

func yesNo(v bool) string {
	if v {
		return "是"
	}
	return "否"
}

func escapeCell(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "|", "\\|")
}
