package capability

import (
	"fmt"
	"sort"
	"strings"
)

// GenerateJavaRegistry 生成 shell/app/src/main/java/com/berba/shell/CapabilityRegistry.java。
func GenerateJavaRegistry(r *Registry) ([]byte, error) {
	var b strings.Builder
	b.WriteString("package com.berba.shell;\n\n")
	b.WriteString("import org.luaj.vm2.Globals;\n\n")
	b.WriteString("/**\n")
	b.WriteString(" * 由 go generate ./internal/capability 生成，请勿手动修改。\n")
	b.WriteString(" * 规格声明了 installer 但实现类缺失时，编译会直接失败。\n")
	b.WriteString(" */\n")
	b.WriteString("final class CapabilityRegistry {\n")
	fmt.Fprintf(&b, "    static final int SCHEMA = %d;\n\n", Schema)
	b.WriteString("    private CapabilityRegistry() {\n    }\n\n")

	specs := r.Specs()
	var core, capabilities []*Spec
	for _, spec := range specs {
		if spec.Kind == KindCore {
			core = append(core, spec)
		} else {
			capabilities = append(capabilities, spec)
		}
	}
	sort.Slice(capabilities, func(i, j int) bool { return capabilities[i].Name < capabilities[j].Name })
	names := make([]string, 0, len(capabilities))
	for _, spec := range capabilities {
		names = append(names, spec.Name)
	}

	b.WriteString("    static boolean isImplemented(String capability) {\n")
	if len(names) == 0 {
		b.WriteString("        return false;\n")
	} else {
		b.WriteString("        if (capability == null) {\n            return false;\n        }\n")
		b.WriteString("        switch (capability) {\n")
		for _, name := range names {
			fmt.Fprintf(&b, "            case %q:\n", name)
		}
		b.WriteString("                return true;\n")
		b.WriteString("            default:\n                return false;\n        }\n")
	}
	b.WriteString("    }\n\n")

	b.WriteString("    static void install(LuaRuntime rt, Globals g, CapabilityConfig cfg) {\n")
	for _, spec := range core {
		fmt.Fprintf(&b, "        %s.install(rt, g); // %s\n", spec.Installer, spec.Name)
	}
	if len(capabilities) > 0 {
		b.WriteString("\n")
		for _, spec := range capabilities {
			fmt.Fprintf(&b, "        if (cfg.hasCapability(%q)) {\n", spec.Name)
			fmt.Fprintf(&b, "            %s.install(rt, g); // 能力 %s\n", spec.Installer, spec.Name)
			b.WriteString("        }\n")
		}
	}
	b.WriteString("    }\n}\n")
	return []byte(b.String()), nil
}
