package nativeplugin

import (
	"debug/elf"
	"fmt"
	"os"
	"sort"
	"strings"
)

// abiSpec 记录 Android ABI 对应的 ELF 头与最低页对齐。
type abiSpec struct {
	class   elf.Class
	machine elf.Machine
	align   uint64
}

var abiSpecs = map[string]abiSpec{
	"arm64-v8a":   {class: elf.ELFCLASS64, machine: elf.EM_AARCH64, align: 16 << 10},
	"armeabi-v7a": {class: elf.ELFCLASS32, machine: elf.EM_ARM, align: 4 << 10},
	"x86_64":      {class: elf.ELFCLASS64, machine: elf.EM_X86_64, align: 16 << 10},
}

// requiredPluginSymbols 是壳在 dlsym 时依赖的 C ABI。
var requiredPluginSymbols = []string{
	"berba_plugin_describe",
	"berba_plugin_init",
	"berba_plugin_call",
	"berba_plugin_free",
}

// allowedLibraries 是插件允许依赖的系统库；C++ 插件必须静态链接 STL。
var allowedLibraries = map[string]bool{
	"libc.so":   true,
	"libm.so":   true,
	"libdl.so":  true,
	"liblog.so": true,
}

// ValidateELF 校验插件库是否满足当前 ABI 的加载约束。
func ValidateELF(path, abi string) error {
	return validateFile(path, abi, requiredPluginSymbols, "插件库")
}

// ValidateShimELF 校验 payload 中的 JNI shim 是否与 ABI 匹配。
func ValidateShimELF(path, abi string) error {
	return validateFile(path, abi, []string{"JNI_OnLoad"}, "JNI shim")
}

func validateFile(path, abi string, required []string, kind string) error {
	spec, ok := abiSpecs[abi]
	if !ok {
		return fmt.Errorf("不支持的 ABI: %q", abi)
	}
	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("读取%s失败: %w", kind, err)
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("%s不是普通文件: %s", kind, path)
	}
	if st.Size() > MaxLibSize {
		return fmt.Errorf("%s超过 %d MiB: %s", kind, MaxLibSize>>20, path)
	}
	f, err := elf.Open(path)
	if err != nil {
		return fmt.Errorf("解析 ELF 失败: %w", err)
	}
	defer f.Close()
	if err := validateELF(f, spec, required); err != nil {
		return fmt.Errorf("%s（%s）: %w", path, abi, err)
	}
	return nil
}

func validateELF(f *elf.File, spec abiSpec, required []string) error {
	if f.Type != elf.ET_DYN {
		return fmt.Errorf("不是共享库（ELF 类型 %v，期望 ET_DYN）", f.Type)
	}
	if f.Data != elf.ELFDATA2LSB {
		return fmt.Errorf("必须是小端 ELF（实际 %v）", f.Data)
	}
	if f.Class != spec.class || f.Machine != spec.machine {
		return fmt.Errorf("ELF 架构为 %v/%v，期望 %v/%v", f.Class, f.Machine, spec.class, spec.machine)
	}
	if err := checkLoadAlignment(f, spec.align); err != nil {
		return err
	}
	syms, err := f.DynamicSymbols()
	if err != nil {
		return fmt.Errorf("读取动态符号失败: %w", err)
	}
	if missing := missingExports(syms, required); len(missing) > 0 {
		return fmt.Errorf("缺少导出符号: %s", strings.Join(missing, ", "))
	}
	libs, err := f.ImportedLibraries()
	if err != nil {
		return fmt.Errorf("读取依赖库失败: %w", err)
	}
	if bad := unsupportedLibraries(libs); len(bad) > 0 {
		return fmt.Errorf("依赖了不支持的库: %s（C++ 插件请静态链接 STL）", strings.Join(bad, ", "))
	}
	return nil
}

func checkLoadAlignment(f *elf.File, min uint64) error {
	found := false
	for _, p := range f.Progs {
		if p.Type != elf.PT_LOAD {
			continue
		}
		found = true
		if p.Align < min {
			return fmt.Errorf("PT_LOAD 对齐为 %#x，小于要求的 %#x", p.Align, min)
		}
	}
	if !found {
		return fmt.Errorf("ELF 缺少 PT_LOAD 段")
	}
	return nil
}

func missingExports(syms []elf.Symbol, required []string) []string {
	exported := make(map[string]bool, len(syms))
	for _, sym := range syms {
		if sym.Section == elf.SHN_UNDEF || elf.ST_BIND(sym.Info) == elf.STB_LOCAL {
			continue
		}
		if elf.ST_VISIBILITY(sym.Other) != elf.STV_DEFAULT {
			continue
		}
		exported[sym.Name] = true
	}
	var missing []string
	for _, name := range required {
		if !exported[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

func unsupportedLibraries(libs []string) []string {
	seen := make(map[string]bool, len(libs))
	var bad []string
	for _, lib := range libs {
		if lib == "" || allowedLibraries[lib] || seen[lib] {
			continue
		}
		seen[lib] = true
		bad = append(bad, lib)
	}
	sort.Strings(bad)
	return bad
}
