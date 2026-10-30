package build

import (
	"archive/zip"
	"bytes"
	"context"
	"debug/elf"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"berba/internal/sign"
)

// 生成仅包含 ELF 头、一个 PT_LOAD、dynsym/dynstr/dynamic 的最小 x86_64 共享库。
func testELF(t *testing.T, symbols []string) []byte {
	t.Helper()
	const (
		ehSize  = 64
		phSize  = 56
		shSize  = 64
		symSize = 24
		dynSize = 16
	)
	alignUp := func(n, a int) int { return (n + a - 1) / a * a }
	order := binary.LittleEndian

	dynstr := []byte{0}
	addStr := func(s string) uint32 {
		off := uint32(len(dynstr))
		dynstr = append(dynstr, s...)
		dynstr = append(dynstr, 0)
		return off
	}
	needed := addStr("libc.so")
	symOff := make([]uint32, len(symbols))
	for i, name := range symbols {
		symOff[i] = addStr(name)
	}

	dynsym := make([]byte, symSize*(len(symbols)+1))
	for i := range symbols {
		e := dynsym[symSize*(i+1):]
		order.PutUint32(e[0:4], symOff[i])
		e[4] = byte(elf.STB_GLOBAL)<<4 | byte(elf.STT_FUNC)
		e[5] = byte(elf.STV_DEFAULT)
		order.PutUint16(e[6:8], uint16(elf.SHN_ABS))
		order.PutUint64(e[8:16], 0x100)
		order.PutUint64(e[16:24], 4)
	}

	dynamic := make([]byte, dynSize)
	order.PutUint64(dynamic[0:8], uint64(elf.DT_NEEDED))
	order.PutUint64(dynamic[8:16], uint64(needed))

	dynstrOff := ehSize + phSize
	dynsymOff := alignUp(dynstrOff+len(dynstr), 8)
	dynOff := alignUp(dynsymOff+len(dynsym), 8)
	shOff := alignUp(dynOff+len(dynamic), 8)
	buf := make([]byte, shOff+shSize*4)

	copy(buf[0:4], "\x7fELF")
	buf[4] = byte(elf.ELFCLASS64)
	buf[5] = byte(elf.ELFDATA2LSB)
	buf[6] = 1
	order.PutUint16(buf[16:18], uint16(elf.ET_DYN))
	order.PutUint16(buf[18:20], uint16(elf.EM_X86_64))
	order.PutUint32(buf[20:24], 1)
	order.PutUint64(buf[32:40], ehSize)
	order.PutUint64(buf[40:48], uint64(shOff))
	order.PutUint16(buf[52:54], ehSize)
	order.PutUint16(buf[54:56], phSize)
	order.PutUint16(buf[56:58], 1)
	order.PutUint16(buf[58:60], shSize)
	order.PutUint16(buf[60:62], 4)

	ph := buf[ehSize:]
	order.PutUint32(ph[0:4], uint32(elf.PT_LOAD))
	order.PutUint32(ph[4:8], 5)
	order.PutUint64(ph[32:40], uint64(len(buf)))
	order.PutUint64(ph[40:48], uint64(len(buf)))
	order.PutUint64(ph[48:56], 16<<10)

	copy(buf[dynstrOff:], dynstr)
	copy(buf[dynsymOff:], dynsym)
	copy(buf[dynOff:], dynamic)

	putSh := func(idx int, typ elf.SectionType, off, size, link, info, entsize uint64) {
		b := buf[shOff+idx*shSize:]
		order.PutUint32(b[4:8], uint32(typ))
		order.PutUint64(b[24:32], off)
		order.PutUint64(b[32:40], size)
		order.PutUint32(b[40:44], uint32(link))
		order.PutUint32(b[44:48], uint32(info))
		order.PutUint64(b[48:56], 8)
		order.PutUint64(b[56:64], entsize)
	}
	putSh(1, elf.SHT_DYNSYM, uint64(dynsymOff), uint64(len(dynsym)), 2, 1, symSize)
	putSh(2, elf.SHT_STRTAB, uint64(dynstrOff), uint64(len(dynstr)), 0, 0, 0)
	putSh(3, elf.SHT_DYNAMIC, uint64(dynOff), uint64(len(dynamic)), 2, 0, dynSize)
	return buf
}

func writeNativeFixture(t *testing.T, root, home string) {
	t.Helper()
	pluginDir := filepath.Join(root, "native", "echo")
	libDir := filepath.Join(pluginDir, "lib", "x86_64")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := `{"name":"echo","abiVersion":1,"version":"1.0.0",
	  "modules":{"echo":{"functions":{"call":{}}}}}`
	if err := os.WriteFile(filepath.Join(pluginDir, "spec.json"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	pluginELF := testELF(t, []string{
		"berba_plugin_describe", "berba_plugin_init",
		"berba_plugin_call", "berba_plugin_free",
	})
	if err := os.WriteFile(filepath.Join(libDir, "libberba_echo.so"), pluginELF, 0o644); err != nil {
		t.Fatal(err)
	}
	shimDir := filepath.Join(home, "payload", "native", "x86_64")
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shimDir, "libberba_jni.so"),
		testELF(t, []string{"JNI_OnLoad"}), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "payload", "shell.json"),
		[]byte(`{"schema":1,"capabilities":["native"],"nativeAbiVersion":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildNativeCapability(t *testing.T) {
	root, home := setupProject(t)
	if err := os.MkdirAll(filepath.Join(root, "lua"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lua", "main.lua"), []byte("log('hi')"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeNativeFixture(t, root, home)
	body := `{"name":"原生示例","package":"com.example.test","versionName":"1.0.0","versionCode":7,
	  "capabilities":["native"],
	  "native":{"plugins":[{"name":"echo","config":{"greeting":"hi"}}]},
	  "web":{"dir":"www","startUrl":"https://appassets.androidhost/index.html"}}`
	if err := os.WriteFile(filepath.Join(root, "app.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Build(context.Background(), Options{
		ConfigPath: filepath.Join(root, "app.json"),
		Home:       home,
		OutDir:     filepath.Join(root, "dist"),
		Runner:     &fakeRunner{packageName: "com.example.test", versionCode: "7"},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(res.APKPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sign.Verify(data); err != nil {
		t.Fatalf("verify: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	libs := map[string]*zip.File{}
	var capAsset []byte
	for _, f := range zr.File {
		switch {
		case strings.HasPrefix(f.Name, "lib/"):
			libs[f.Name] = f
		case f.Name == "assets/berba_capabilities.json":
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			capAsset, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, name := range []string{
		"lib/x86_64/libberba_jni.so", "lib/x86_64/libberba_echo.so",
	} {
		f, ok := libs[name]
		if !ok {
			t.Fatalf("missing %s in %v", name, libs)
		}
		if f.Method != zip.Store {
			t.Errorf("%s method = %d, want store", name, f.Method)
		}
		offset, err := f.DataOffset()
		if err != nil {
			t.Fatal(err)
		}
		if offset%16384 != 0 {
			t.Errorf("%s offset = %d, want 16KB aligned", name, offset)
		}
	}

	var asset struct {
		Capabilities []string `json:"capabilities"`
		Native       struct {
			ABIVersion int `json:"abiVersion"`
			Plugins    []struct {
				Name    string                     `json:"name"`
				LibName string                     `json:"libName"`
				Config  map[string]json.RawMessage `json:"config"`
				Modules map[string]struct {
					Functions map[string]struct {
						Async bool `json:"async"`
					} `json:"functions"`
				} `json:"modules"`
			} `json:"plugins"`
		} `json:"native"`
	}
	if err := json.Unmarshal(capAsset, &asset); err != nil {
		t.Fatal(err)
	}
	if len(asset.Capabilities) != 1 || asset.Capabilities[0] != "native" {
		t.Fatalf("capabilities = %v", asset.Capabilities)
	}
	if asset.Native.ABIVersion != 1 || len(asset.Native.Plugins) != 1 {
		t.Fatalf("native asset = %+v", asset.Native)
	}
	plugin := asset.Native.Plugins[0]
	if plugin.Name != "echo" || plugin.LibName != "libberba_echo.so" {
		t.Fatalf("plugin = %+v", plugin)
	}
	if string(plugin.Config["greeting"]) != `"hi"` {
		t.Fatalf("config = %s", plugin.Config["greeting"])
	}
	if _, ok := plugin.Modules["echo"].Functions["call"]; !ok {
		t.Fatalf("modules = %+v", plugin.Modules)
	}

	again, err := Build(context.Background(), Options{
		ConfigPath: filepath.Join(root, "app.json"),
		Home:       home,
		OutDir:     filepath.Join(root, "dist2"),
		Runner:     &fakeRunner{packageName: "com.example.test", versionCode: "7"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.SHA256 != res.SHA256 {
		t.Fatalf("native build not deterministic: %s != %s", res.SHA256, again.SHA256)
	}
}
