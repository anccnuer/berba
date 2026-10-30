// Package nativeplugintest 构造用于测试的 ELF 共享库。
//
// 生产代码不得 import 本包；它只给 nativeplugin 与 release 的测试提供最小 ELF
// 夹具，避免在两个测试里各写一份 ELF 构造器。
package nativeplugintest

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
)

// PluginSymbols 是 C ABI 要求插件导出的符号，与 nativeplugin 的校验保持一致。
var PluginSymbols = []string{
	"berba_plugin_describe",
	"berba_plugin_init",
	"berba_plugin_call",
	"berba_plugin_free",
}

// Symbol 描述夹具中的一个动态符号。
type Symbol struct {
	Name  string
	Bind  elf.SymBind
	Vis   elf.SymVis
	Undef bool
}

// File 描述要生成的 ELF 文件；零值字段由 Base 补齐。
type File struct {
	Class   elf.Class
	Data    elf.Data
	Machine elf.Machine
	Type    elf.Type
	Align   uint64
	NoLoad  bool
	Needed  []string
	Symbols []Symbol
}

// ABISpec 记录 Android ABI 的 ELF 头与最低页对齐。
type ABISpec struct {
	Class   elf.Class
	Machine elf.Machine
	Align   uint64
}

// ABIs 覆盖 nativeplugin 支持的全部 ABI。
var ABIs = map[string]ABISpec{
	"arm64-v8a":   {Class: elf.ELFCLASS64, Machine: elf.EM_AARCH64, Align: 16 << 10},
	"armeabi-v7a": {Class: elf.ELFCLASS32, Machine: elf.EM_ARM, Align: 4 << 10},
	"x86_64":      {Class: elf.ELFCLASS64, Machine: elf.EM_X86_64, Align: 16 << 10},
}

// Base 返回一个导出全部 PluginSymbols 的合法共享库夹具。
func Base(class elf.Class, machine elf.Machine, align uint64) File {
	syms := make([]Symbol, 0, len(PluginSymbols))
	for _, name := range PluginSymbols {
		syms = append(syms, Symbol{Name: name, Bind: elf.STB_GLOBAL, Vis: elf.STV_DEFAULT})
	}
	return File{
		Class:   class,
		Data:    elf.ELFDATA2LSB,
		Machine: machine,
		Type:    elf.ET_DYN,
		Align:   align,
		Needed:  []string{"libc.so", "liblog.so"},
		Symbols: syms,
	}
}

// Shim 返回指定 ABI 下只导出 JNI_OnLoad 的合法 JNI shim。
func Shim(abi string) ([]byte, error) {
	spec, ok := ABIs[abi]
	if !ok {
		return nil, fmt.Errorf("不支持的 ABI: %q", abi)
	}
	f := Base(spec.Class, spec.Machine, spec.Align)
	f.Symbols = []Symbol{{Name: "JNI_OnLoad", Bind: elf.STB_GLOBAL, Vis: elf.STV_DEFAULT}}
	return f.Bytes(), nil
}

// AlignUp 向上取整到 a 的倍数。
func AlignUp(n, a int) int {
	return (n + a - 1) / a * a
}

// Bytes 序列化夹具；数据一律按小端生成。
func (f File) Bytes() []byte {
	if f.Data != elf.ELFDATA2MSB {
		f.Data = elf.ELFDATA2LSB
	}
	var order binary.ByteOrder = binary.LittleEndian
	if f.Data == elf.ELFDATA2MSB {
		order = binary.BigEndian
	}
	is64 := f.Class == elf.ELFCLASS64
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
	neededOff := make([]uint32, len(f.Needed))
	for i, lib := range f.Needed {
		neededOff[i] = addStr(lib)
	}
	symOff := make([]uint32, len(f.Symbols))
	for i, sym := range f.Symbols {
		symOff[i] = addStr(sym.Name)
	}

	dynsym := make([]byte, symSize*(len(f.Symbols)+1))
	for i, sym := range f.Symbols {
		e := dynsym[symSize*(i+1):]
		order.PutUint32(e[0:4], symOff[i])
		info := byte(sym.Bind)<<4 | byte(elf.STT_FUNC)
		shndx := uint16(elf.SHN_ABS)
		if sym.Undef {
			shndx = uint16(elf.SHN_UNDEF)
		}
		if is64 {
			e[4] = info
			e[5] = byte(sym.Vis)
			order.PutUint16(e[6:8], shndx)
			order.PutUint64(e[8:16], 0x100)
			order.PutUint64(e[16:24], 4)
		} else {
			order.PutUint32(e[4:8], 0x100)
			order.PutUint32(e[8:12], 4)
			e[12] = info
			e[13] = byte(sym.Vis)
			order.PutUint16(e[14:16], shndx)
		}
	}

	dynamic := make([]byte, dynSize*(len(f.Needed)+1))
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
	dynsymOff := AlignUp(dynstrOff+len(dynstr), 8)
	dynOff := AlignUp(dynsymOff+len(dynsym), 8)
	shOff := AlignUp(dynOff+len(dynamic), 8)
	buf := make([]byte, shOff+shSize*4)

	copy(buf[0:4], "\x7fELF")
	buf[4] = byte(f.Class)
	buf[5] = byte(f.Data)
	buf[6] = 1
	order.PutUint16(buf[16:18], uint16(f.Type))
	order.PutUint16(buf[18:20], uint16(f.Machine))
	order.PutUint32(buf[20:24], 1)
	phnum := 1
	if f.NoLoad {
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
	if !f.NoLoad {
		p := buf[ehSize:]
		if is64 {
			order.PutUint32(p[0:4], uint32(elf.PT_LOAD))
			order.PutUint32(p[4:8], 5)
			order.PutUint64(p[32:40], uint64(len(buf)))
			order.PutUint64(p[40:48], uint64(len(buf)))
			order.PutUint64(p[48:56], f.Align)
		} else {
			order.PutUint32(p[0:4], uint32(elf.PT_LOAD))
			order.PutUint32(p[16:20], uint32(len(buf)))
			order.PutUint32(p[20:24], uint32(len(buf)))
			order.PutUint32(p[24:28], 5)
			order.PutUint32(p[28:32], uint32(f.Align))
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
