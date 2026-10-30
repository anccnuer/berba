package nativeplugin

import (
	"debug/elf"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"berba/internal/nativeplugin/nativeplugintest"
)

// fixtureELF 让用例代码保持简洁，夹具实现集中在 nativeplugintest。
type fixtureELF = nativeplugintest.File

func baseFixture(class elf.Class, machine elf.Machine, align uint64) fixtureELF {
	return nativeplugintest.Base(class, machine, align)
}

func writeFixture(t *testing.T, f fixtureELF) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "libberba_test.so")
	if err := os.WriteFile(path, f.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestValidateELFValid(t *testing.T) {
	cases := []struct {
		abi  string
		file fixtureELF
	}{
		{"arm64-v8a", baseFixture(elf.ELFCLASS64, elf.EM_AARCH64, 16<<10)},
		{"armeabi-v7a", baseFixture(elf.ELFCLASS32, elf.EM_ARM, 4<<10)},
		{"x86_64", baseFixture(elf.ELFCLASS64, elf.EM_X86_64, 16<<10)},
	}
	for _, tc := range cases {
		t.Run(tc.abi, func(t *testing.T) {
			if err := ValidateELF(writeFixture(t, tc.file), tc.abi); err != nil {
				t.Fatalf("ValidateELF = %v", err)
			}
		})
	}
}

func TestValidateShimELFValid(t *testing.T) {
	for _, abi := range SupportedABIs {
		data, err := nativeplugintest.Shim(abi)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "libberba_jni.so")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := ValidateShimELF(path, abi); err != nil {
			t.Fatalf("%s: ValidateShimELF = %v", abi, err)
		}
	}
}

func TestValidateELFRejects(t *testing.T) {
	exec := baseFixture(elf.ELFCLASS64, elf.EM_AARCH64, 16<<10)
	exec.Type = elf.ET_EXEC
	bigEndian := baseFixture(elf.ELFCLASS64, elf.EM_AARCH64, 16<<10)
	bigEndian.Data = elf.ELFDATA2MSB
	wrongMachine := baseFixture(elf.ELFCLASS64, elf.EM_X86_64, 16<<10)
	wrongClass := baseFixture(elf.ELFCLASS32, elf.EM_AARCH64, 16<<10)
	smallAlign := baseFixture(elf.ELFCLASS64, elf.EM_AARCH64, 4<<10)
	noLoad := baseFixture(elf.ELFCLASS64, elf.EM_AARCH64, 16<<10)
	noLoad.NoLoad = true
	missing := baseFixture(elf.ELFCLASS64, elf.EM_AARCH64, 16<<10)
	missing.Symbols = missing.Symbols[:len(missing.Symbols)-1]
	local := baseFixture(elf.ELFCLASS64, elf.EM_AARCH64, 16<<10)
	local.Symbols[0].Bind = elf.STB_LOCAL
	hidden := baseFixture(elf.ELFCLASS64, elf.EM_AARCH64, 16<<10)
	hidden.Symbols[0].Vis = elf.STV_HIDDEN
	undef := baseFixture(elf.ELFCLASS64, elf.EM_AARCH64, 16<<10)
	undef.Symbols[0].Undef = true
	badLib := baseFixture(elf.ELFCLASS64, elf.EM_AARCH64, 16<<10)
	badLib.Needed = []string{"libc.so", "libc++_shared.so"}

	cases := []struct {
		name string
		file fixtureELF
		abi  string
		want string
	}{
		{"not shared object", exec, "arm64-v8a", "不是共享库"},
		{"big endian", bigEndian, "arm64-v8a", "小端"},
		{"wrong machine", wrongMachine, "arm64-v8a", "ELF 架构"},
		{"wrong class", wrongClass, "arm64-v8a", "ELF 架构"},
		{"small alignment", smallAlign, "arm64-v8a", "PT_LOAD 对齐"},
		{"no PT_LOAD", noLoad, "arm64-v8a", "PT_LOAD"},
		{"missing symbol", missing, "arm64-v8a", "berba_plugin_free"},
		{"local symbol", local, "arm64-v8a", "berba_plugin_describe"},
		{"hidden symbol", hidden, "arm64-v8a", "berba_plugin_describe"},
		{"undefined symbol", undef, "arm64-v8a", "berba_plugin_describe"},
		{"unsupported library", badLib, "arm64-v8a", "libc++_shared.so"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateELF(writeFixture(t, tc.file), tc.abi)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateELF error = %v, want contains %q", err, tc.want)
			}
		})
	}
}

func TestValidateELFFileSizeCap(t *testing.T) {
	valid := writeFixture(t, baseFixture(elf.ELFCLASS64, elf.EM_AARCH64, 16<<10))
	data, err := os.ReadFile(valid)
	if err != nil {
		t.Fatal(err)
	}

	// 恰好等于上限：应通过（大小闸门是 > 而不是 >=）
	atCap := filepath.Join(t.TempDir(), "at-cap.so")
	padded := make([]byte, MaxLibSize)
	copy(padded, data)
	if err := os.WriteFile(atCap, padded, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateELF(atCap, "arm64-v8a"); err != nil {
		t.Fatalf("恰好 %d 字节应通过: %v", MaxLibSize, err)
	}

	// 超过上限：应被拒绝（用 Truncate 生成稀疏文件，避免真的写 32 MiB）
	overCap := filepath.Join(t.TempDir(), "over-cap.so")
	f, err := os.Create(overCap)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxLibSize + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ValidateELF(overCap, "arm64-v8a"); err == nil || !strings.Contains(err.Error(), "超过 32 MiB") {
		t.Fatalf("超过上限应被拒绝: %v", err)
	}
}

func TestValidateELFUnknownABIAndBadFile(t *testing.T) {
	path := writeFixture(t, baseFixture(elf.ELFCLASS64, elf.EM_AARCH64, 16<<10))
	if err := ValidateELF(path, "mips"); err == nil || !strings.Contains(err.Error(), "不支持的 ABI") {
		t.Fatalf("unknown ABI error = %v", err)
	}
	if err := ValidateELF(filepath.Join(t.TempDir(), "missing.so"), "arm64-v8a"); err == nil {
		t.Fatal("missing file should fail")
	}
	bad := filepath.Join(t.TempDir(), "bad.so")
	if err := os.WriteFile(bad, []byte("not elf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateELF(bad, "arm64-v8a"); err == nil || !strings.Contains(err.Error(), "解析 ELF 失败") {
		t.Fatalf("bad file error = %v", err)
	}
}
