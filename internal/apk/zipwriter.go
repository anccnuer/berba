package apk

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"sort"
	"strings"
)

const (
	methodStore   = 0
	methodDeflate = 8
	zipUTF8Flag   = 0x0800
	zipVersion    = 20
	zipDOSTime    = 0
	zipDOSDate    = 0x0021
)

type Entry struct {
	Name  string
	Data  []byte
	Store bool
	Align int
}

type prepared struct {
	name          []byte
	method        uint16
	crc           uint32
	comp          []byte
	uncompressed  uint32
	align         int
	localOffset   uint32
	extraLen      uint16
	compressedLen uint32
}

func Build(entries []Entry) ([]byte, error) {
	if len(entries) > 0xFFFF {
		return nil, fmt.Errorf("条目过多")
	}
	// 固定条目顺序，保证相同输入产出字节一致。
	ordered := append([]Entry(nil), entries...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return strings.ReplaceAll(ordered[i].Name, "\\", "/") < strings.ReplaceAll(ordered[j].Name, "\\", "/")
	})
	seen := make(map[string]bool, len(ordered))
	preparedEntries := make([]prepared, 0, len(ordered))
	for _, e := range ordered {
		name := strings.ReplaceAll(e.Name, "\\", "/")
		if !safeEntryName(name) {
			return nil, fmt.Errorf("非法 zip 条目名: %q", e.Name)
		}
		if len(name) > 0xFFFF {
			return nil, fmt.Errorf("zip 条目名过长: %q", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("zip 条目重复: %q", name)
		}
		seen[name] = true
		if len(e.Data) > 0xFFFFFFFF {
			return nil, fmt.Errorf("zip 条目过大: %q", name)
		}

		method := uint16(methodDeflate)
		comp := e.Data
		if !e.Store && e.Align == 0 {
			var buf bytes.Buffer
			fw, err := flate.NewWriter(&buf, flate.BestCompression)
			if err != nil {
				return nil, err
			}
			if _, err := fw.Write(e.Data); err != nil {
				return nil, err
			}
			if err := fw.Close(); err != nil {
				return nil, err
			}
			if buf.Len() < len(e.Data) {
				comp = buf.Bytes()
			} else {
				method = methodStore
			}
		} else {
			method = methodStore
		}
		if len(comp) > 0xFFFFFFFF {
			return nil, fmt.Errorf("zip 条目压缩后过大: %q", name)
		}
		align := e.Align
		if method == methodStore && align < 4 {
			align = 4
		}

		preparedEntries = append(preparedEntries, prepared{
			name:          []byte(name),
			method:        method,
			crc:           crc32.ChecksumIEEE(e.Data),
			comp:          comp,
			uncompressed:  uint32(len(e.Data)),
			align:         align,
			compressedLen: uint32(len(comp)),
		})
	}

	var out bytes.Buffer
	var offset uint64
	for i := range preparedEntries {
		pe := &preparedEntries[i]
		if offset > 0xFFFFFFFF {
			return nil, fmt.Errorf("APK 超过 4GB，暂不支持")
		}
		pe.localOffset = uint32(offset)
		base := offset + 30 + uint64(len(pe.name))
		pad := paddingFor(base, pe.align)
		if pad > 0xFFFF {
			return nil, fmt.Errorf("对齐填充过大: %q", pe.name)
		}
		pe.extraLen = uint16(pad)
		extra := makeExtra(pad)

		var hdr [30]byte
		binary.LittleEndian.PutUint32(hdr[0:], 0x04034b50)
		binary.LittleEndian.PutUint16(hdr[4:], zipVersion)
		binary.LittleEndian.PutUint16(hdr[6:], zipUTF8Flag)
		binary.LittleEndian.PutUint16(hdr[8:], pe.method)
		binary.LittleEndian.PutUint16(hdr[10:], zipDOSTime)
		binary.LittleEndian.PutUint16(hdr[12:], zipDOSDate)
		binary.LittleEndian.PutUint32(hdr[14:], pe.crc)
		binary.LittleEndian.PutUint32(hdr[18:], pe.compressedLen)
		binary.LittleEndian.PutUint32(hdr[22:], pe.uncompressed)
		binary.LittleEndian.PutUint16(hdr[26:], uint16(len(pe.name)))
		binary.LittleEndian.PutUint16(hdr[28:], pe.extraLen)

		out.Write(hdr[:])
		out.Write(pe.name)
		out.Write(extra)
		out.Write(pe.comp)
		offset += uint64(len(hdr)) + uint64(len(pe.name)) + uint64(len(extra)) + uint64(len(pe.comp))
		if pe.align > 1 {
			dataStart := uint64(pe.localOffset) + 30 + uint64(len(pe.name)) + uint64(len(extra))
			if dataStart%uint64(pe.align) != 0 {
				return nil, fmt.Errorf("内部错误: 条目 %q 未对齐", pe.name)
			}
		}
		if offset > 0xFFFFFFFF {
			return nil, fmt.Errorf("APK 超过 4GB，暂不支持")
		}
	}

	cdOffset := offset
	for _, pe := range preparedEntries {
		var hdr [46]byte
		binary.LittleEndian.PutUint32(hdr[0:], 0x02014b50)
		binary.LittleEndian.PutUint16(hdr[4:], zipVersion)
		binary.LittleEndian.PutUint16(hdr[6:], zipVersion)
		binary.LittleEndian.PutUint16(hdr[8:], zipUTF8Flag)
		binary.LittleEndian.PutUint16(hdr[10:], pe.method)
		binary.LittleEndian.PutUint16(hdr[12:], zipDOSTime)
		binary.LittleEndian.PutUint16(hdr[14:], zipDOSDate)
		binary.LittleEndian.PutUint32(hdr[16:], pe.crc)
		binary.LittleEndian.PutUint32(hdr[20:], pe.compressedLen)
		binary.LittleEndian.PutUint32(hdr[24:], pe.uncompressed)
		binary.LittleEndian.PutUint16(hdr[28:], uint16(len(pe.name)))
		binary.LittleEndian.PutUint16(hdr[30:], 0)
		binary.LittleEndian.PutUint16(hdr[32:], 0)
		binary.LittleEndian.PutUint16(hdr[34:], 0)
		binary.LittleEndian.PutUint16(hdr[36:], 0)
		binary.LittleEndian.PutUint32(hdr[38:], 0)
		binary.LittleEndian.PutUint32(hdr[42:], pe.localOffset)

		out.Write(hdr[:])
		out.Write(pe.name)
		offset += uint64(len(hdr)) + uint64(len(pe.name))
		if offset > 0xFFFFFFFF {
			return nil, fmt.Errorf("APK 超过 4GB，暂不支持")
		}
	}
	cdSize := offset - cdOffset
	if cdSize > 0xFFFFFFFF {
		return nil, fmt.Errorf("APK central directory 过大")
	}

	var eocd [22]byte
	binary.LittleEndian.PutUint32(eocd[0:], 0x06054b50)
	binary.LittleEndian.PutUint16(eocd[4:], 0)
	binary.LittleEndian.PutUint16(eocd[6:], 0)
	binary.LittleEndian.PutUint16(eocd[8:], uint16(len(preparedEntries)))
	binary.LittleEndian.PutUint16(eocd[10:], uint16(len(preparedEntries)))
	binary.LittleEndian.PutUint32(eocd[12:], uint32(cdSize))
	binary.LittleEndian.PutUint32(eocd[16:], uint32(cdOffset))
	binary.LittleEndian.PutUint16(eocd[20:], 0)
	out.Write(eocd[:])
	return out.Bytes(), nil
}

func paddingFor(base uint64, align int) int {
	if align <= 1 {
		return 0
	}
	mod := int(base % uint64(align))
	if mod == 0 {
		return 0
	}
	pad := align - mod
	if pad < 4 {
		pad += align
	}
	return pad
}

func makeExtra(pad int) []byte {
	if pad == 0 {
		return nil
	}
	extra := make([]byte, pad)
	binary.LittleEndian.PutUint16(extra[0:], 0xD935)
	binary.LittleEndian.PutUint16(extra[2:], uint16(pad-4))
	return extra
}
