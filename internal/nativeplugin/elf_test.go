package nativeplugin

import (
	"debug/elf"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixtureSymbol struct {
	name  string
	bind  elf.SymBind
	vis   elf.SymVis
	undef bool
}

type fixtureELF struct {
	class   elf.Class
	data    elf.Data
	machine elf.Machine
	typ     elf.Type
	align   uint64
	noLoad  bool
	needed  []string
	symbols []fixtureSymbol
}

func baseFixture(class elf.Class, machine elf.Machine, align uint64) fixtureELF {
	syms := make([]fixtureSymbol, 0, len(requiredPluginSymbols))
	for _, name := range requiredPluginSymbols {
		syms = append(syms, fixtureSymbol{name: name, bind: elf.STB_GLOBAL, vis: elf.STV_DEFAULT})
	}
	return fixtureELF{
		class:   class,
		data:    elf.ELFDATA2LSB,
		machine: machine,
		typ:     elf.ET_DYN,
		align:   align,
		needed:  []string{"libc.so", "liblog.so"},
		symbols: syms,
	}
}

func alignUp(n, a int) int {
	return (n + a - 1) / a * a
}

func (f fixtureELF) bytes(t *testing.T) []byte {
	t.Helper()
	if f.data != elf.ELFDATA2MSB {
		f.data = elf.ELFDATA2LSB
	}
	var order binary.ByteOrder = binary.LittleEndian
	if f.data == elf.ELFDATA2MSB {
		order = binary.BigEndian
	}
	is64 := f.class == elf.ELFCLASS64
	symSize, dynSize, ehSize, phSize, shSize := 16, 8, 52, 32, 40
	if is64 {
		symSize, dynSize, ehSize, phSize, shSize = 24, 16, 64, 56, 64
	}

	dynstr := []byte{0}
	addStr := func(s string) uint32 {
		off := uint32(len(dynstr))
		dynstr = append(dynstr, s...)
		dynstr = append(dynstr, 0)
		return off
	}
	neededOff := make([]uint32, len(f.needed))
	for i, lib := range f.needed {
		neededOff[i] = addStr(lib)
	}
	symOff := make([]uint32, len(f.symbols))
	for i, sym := range f.symbols {
		symOff[i] = addStr(sym.name)
	}

	dynsym := make([]byte, symSize*(len(f.symbols)+1))
	for i, sym := range f.symbols {
		e := dynsym[symSize*(i+1):]
		order.PutUint32(e[0:4], symOff[i])
		info := byte(sym.bind)<<4 | byte(elf.STT_FUNC)
		shndx := uint16(elf.SHN_ABS)
		if sym.undef {
			shndx = uint16(elf.SHN_UNDEF)
		}
		if is64 {
			e[4] = info
			e[5] = byte(sym.vis)
			order.PutUint16(e[6:8], shndx)
			order.PutUint64(e[8:16], 0x100)
			order.PutUint64(e[16:24], 4)
		} else {
			order.PutUint32(e[4:8], 0x100)
			order.PutUint32(e[8:12], 4)
			e[12] = info
			e[13] = byte(sym.vis)
			order.PutUint16(e[14:16], shndx)
		}
	}

	dynamic := make([]byte, dynSize*(len(f.needed)+1))
	for i, off := range neededOff {
		e := dynamic[dynSize*i:]
		if is64 {
			order.PutUint64(e[0:8], uint64(elf.DT_NEEDED))
			order.PutUint64(e[8:16], uint64(off))
		} else {
			order.PutUint32(e[0:4], uint32(elf.DT_NEEDED))
			order.PutUint32(e[4:8], off)
		}
	}

	dynstrOff := ehSize + phSize
	dynsymOff := alignUp(dynstrOff+len(dynstr), 8)
	dynOff := alignUp(dynsymOff+len(dynsym), 8)
	shOff := alignUp(dynOff+len(dynamic), 8)
	buf := make([]byte, shOff+shSize*4)

	copy(buf[0:4], "\x7fELF")
	buf[4] = byte(f.class)
	buf[5] = byte(f.data)
	buf[6] = 1
	order.PutUint16(buf[16:18], uint16(f.typ))
	order.PutUint16(buf[18:20], uint16(f.machine))
	order.PutUint32(buf[20:24], 1)
	phnum := 1
	if f.noLoad {
		phnum = 0
	}
	if is64 {
		order.PutUint64(buf[32:40], uint64(ehSize))
		order.PutUint64(buf[40:48], uint64(shOff))
		order.PutUint16(buf[52:54], uint16(ehSize))
		order.PutUint16(buf[54:56], uint16(phSize))
		order.PutUint16(buf[56:58], uint16(phnum))
		order.PutUint16(buf[58:60], uint16(shSize))
		order.PutUint16(buf[60:62], 4)
	} else {
		order.PutUint32(buf[28:32], uint32(ehSize))
		order.PutUint32(buf[32:36], uint32(shOff))
		order.PutUint16(buf[40:42], uint16(ehSize))
		order.PutUint16(buf[42:44], uint16(phSize))
		order.PutUint16(buf[44:46], uint16(phnum))
		order.PutUint16(buf[46:48], uint16(shSize))
		order.PutUint16(buf[48:50], 4)
	}
	if !f.noLoad {
		p := buf[ehSize:]
		if is64 {
			order.PutUint32(p[0:4], uint32(elf.PT_LOAD))
			order.PutUint32(p[4:8], 5)
			order.PutUint64(p[32:40], uint64(len(buf)))
			order.PutUint64(p[40:48], uint64(len(buf)))
			order.PutUint64(p[48:56], f.align)
		} else {
			order.PutUint32(p[0:4], uint32(elf.PT_LOAD))
			order.PutUint32(p[16:20], uint32(len(buf)))
			order.PutUint32(p[20:24], uint32(len(buf)))
			order.PutUint32(p[24:28], 5)
			order.PutUint32(p[28:32], uint32(f.align))
		}
	}

	copy(buf[dynstrOff:], dynstr)
	copy(buf[dynsymOff:], dynsym)
	copy(buf[dynOff:], dynamic)

	putSh := func(idx int, typ elf.SectionType, off, size, link, info, addralign, entsize uint64) {
		b := buf[shOff+idx*shSize:]
		if is64 {
			order.PutUint32(b[4:8], uint32(typ))
			order.PutUint64(b[24:32], off)
			order.PutUint64(b[32:40], size)
			order.PutUint32(b[40:44], uint32(link))
			order.PutUint32(b[44:48], uint32(info))
			order.PutUint64(b[48:56], addralign)
			order.PutUint64(b[56:64], entsize)
		} else {
			order.PutUint32(b[4:8], uint32(typ))
			order.PutUint32(b[16:20], uint32(off))
			order.PutUint32(b[20:24], uint32(size))
			order.PutUint32(b[24:28], uint32(link))
			order.PutUint32(b[28:32], uint32(info))
			order.PutUint32(b[32:36], uint32(addralign))
			order.PutUint32(b[36:40], uint32(entsize))
		}
	}
	putSh(1, elf.SHT_DYNSYM, uint64(dynsymOff), uint64(len(dynsym)), 2, 1, 8, uint64(symSize))
	putSh(2, elf.SHT_STRTAB, uint64(dynstrOff), uint64(len(dynstr)), 0, 0, 1, 0)
	putSh(3, elf.SHT_DYNAMIC, uint64(dynOff), uint64(len(dynamic)), 2, 0, 8, uint64(dynSize))
	return buf
}

func writeFixture(t *testing.T, f fixtureELF) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "libberba_test.so")
	if err := os.WriteFile(path, f.bytes(t), 0o644); err != nil {
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

func TestValidateELFRejects(t *testing.T) {
	exec := baseFixture(elf.ELFCLASS64, elf.EM_AARCH64, 16<<10)
	exec.typ = elf.ET_EXEC
	bigEndian := baseFixture(elf.ELFCLASS64, elf.EM_AARCH64, 16<<10)
	bigEndian.data = elf.ELFDATA2MSB
	wrongMachine := baseFixture(elf.ELFCLASS64, elf.EM_X86_64, 16<<10)
	wrongClass := baseFixture(elf.ELFCLASS32, elf.EM_AARCH64, 16<<10)
	smallAlign := baseFixture(elf.ELFCLASS64, elf.EM_AARCH64, 4<<10)
	noLoad := baseFixture(elf.ELFCLASS64, elf.EM_AARCH64, 16<<10)
	noLoad.noLoad = true
	missing := baseFixture(elf.ELFCLASS64, elf.EM_AARCH64, 16<<10)
	missing.symbols = missing.symbols[:len(missing.symbols)-1]
	local := baseFixture(elf.ELFCLASS64, elf.EM_AARCH64, 16<<10)
	local.symbols[0].bind = elf.STB_LOCAL
	hidden := baseFixture(elf.ELFCLASS64, elf.EM_AARCH64, 16<<10)
	hidden.symbols[0].vis = elf.STV_HIDDEN
	undef := baseFixture(elf.ELFCLASS64, elf.EM_AARCH64, 16<<10)
	undef.symbols[0].undef = true
	badLib := baseFixture(elf.ELFCLASS64, elf.EM_AARCH64, 16<<10)
	badLib.needed = []string{"libc.so", "libc++_shared.so"}

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
