package apk

import (
	"archive/zip"
	"bytes"
	crand "crypto/rand"
	"encoding/binary"
	"io"
	"testing"
)

type entryInfo struct {
	method     uint16
	dataOffset int
	crc        uint32
}

func TestBuildAlignmentAndReadback(t *testing.T) {
	entries := []Entry{
		{Name: "AndroidManifest.xml", Data: []byte("<manifest/>")},
		{Name: "resources.arsc", Data: bytes.Repeat([]byte{0x01, 0x02, 0x03}, 1000), Store: true, Align: 4},
		{Name: "lib/arm64-v8a/libx.so", Data: bytes.Repeat([]byte{0x7f}, 5000), Store: true, Align: 16384},
		{Name: "classes.dex", Data: bytes.Repeat([]byte("dex\n"), 2000)},
		{Name: "assets/www/index.html", Data: []byte("<html>hi</html>")},
	}
	data, err := Build(entries)
	if err != nil {
		t.Fatal(err)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		got[f.Name] = b
	}
	for _, e := range entries {
		if !bytes.Equal(got[e.Name], e.Data) {
			t.Fatalf("entry %s data mismatch", e.Name)
		}
	}

	infos := inspectZip(t, data)
	if infos["resources.arsc"].method != 0 {
		t.Fatal("resources.arsc must be stored")
	}
	if infos["resources.arsc"].dataOffset%4 != 0 {
		t.Fatalf("resources.arsc not 4-byte aligned: %d", infos["resources.arsc"].dataOffset)
	}
	if infos["lib/arm64-v8a/libx.so"].method != 0 {
		t.Fatal("native lib must be stored")
	}
	if infos["lib/arm64-v8a/libx.so"].dataOffset%16384 != 0 {
		t.Fatalf("native lib not 16KB aligned: %d", infos["lib/arm64-v8a/libx.so"].dataOffset)
	}
	if infos["classes.dex"].method != 8 {
		t.Fatal("classes.dex should be deflated")
	}
}

func TestStoredFallbackIsAligned(t *testing.T) {
	// 伪随机数据无法压缩，writer 会回退为 STORED，此时仍须 4 字节对齐。
	data := make([]byte, 1001)
	if _, err := crand.Read(data); err != nil {
		t.Fatal(err)
	}
	out, err := Build([]Entry{
		{Name: "a.txt", Data: []byte("x")},
		{Name: "res/mipmap-mdpi-v4/ic_launcher.png", Data: data},
	})
	if err != nil {
		t.Fatal(err)
	}
	info := inspectZip(t, out)["res/mipmap-mdpi-v4/ic_launcher.png"]
	if info.method != 0 {
		t.Fatalf("expected stored fallback, got method %d", info.method)
	}
	if info.dataOffset%4 != 0 {
		t.Fatalf("stored entry not 4-byte aligned: %d", info.dataOffset)
	}
}

func TestBuildRejectsTraversal(t *testing.T) {
	if _, err := Build([]Entry{{Name: "../evil", Data: []byte("x")}}); err == nil {
		t.Fatal("expected traversal rejection")
	}
	if _, err := Build([]Entry{{Name: "/abs", Data: []byte("x")}}); err == nil {
		t.Fatal("expected absolute path rejection")
	}
}

func inspectZip(t *testing.T, data []byte) map[string]entryInfo {
	t.Helper()
	if len(data) < 22 {
		t.Fatal("short zip")
	}
	var eocd int
	for i := len(data) - 22; i >= 0; i-- {
		if binary.LittleEndian.Uint32(data[i:]) == 0x06054B50 {
			eocd = i
			break
		}
	}
	if eocd == 0 {
		t.Fatal("no eocd")
	}
	cdOffset := int(binary.LittleEndian.Uint32(data[eocd+16:]))
	cdSize := int(binary.LittleEndian.Uint32(data[eocd+12:]))
	out := map[string]entryInfo{}
	pos := cdOffset
	for pos < cdOffset+cdSize {
		if binary.LittleEndian.Uint32(data[pos:]) != 0x02014B50 {
			t.Fatalf("bad cd sig at %d", pos)
		}
		method := binary.LittleEndian.Uint16(data[pos+10:])
		crc := binary.LittleEndian.Uint32(data[pos+16:])
		nameLen := int(binary.LittleEndian.Uint16(data[pos+28:]))
		extraLen := int(binary.LittleEndian.Uint16(data[pos+30:]))
		commentLen := int(binary.LittleEndian.Uint16(data[pos+32:]))
		localOff := int(binary.LittleEndian.Uint32(data[pos+42:]))
		name := string(data[pos+46 : pos+46+nameLen])
		localNameLen := int(binary.LittleEndian.Uint16(data[localOff+26:]))
		localExtraLen := int(binary.LittleEndian.Uint16(data[localOff+28:]))
		dataOffset := localOff + 30 + localNameLen + localExtraLen
		out[name] = entryInfo{method: method, dataOffset: dataOffset, crc: crc}
		pos += 46 + nameLen + extraLen + commentLen
	}
	return out
}
