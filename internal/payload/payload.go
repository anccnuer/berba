package payload

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	EnvHome = "BERBA_HOME"
	// NativeShimName 是 JNI shim 在 payload/native/<abi>/ 下的固定文件名。
	NativeShimName = "libberba_jni.so"
)

type Lib struct {
	Name string // lib/<abi>/<file>.so
	Path string
}

type Layout struct {
	Home       string
	ToolsDir   string
	Aapt2      string
	AndroidJar string
	Versions   string
	ShellPath  string
	Shell      *ShellManifest
	Dexes      []string
	Libs       []Lib
	Native     map[string]string // ABI -> payload/native/<abi>/libberba_jni.so
}

// ShellManifest 是 payload/shell.json 的能力协商清单；缺失时按 core-only 处理。
type ShellManifest struct {
	Schema           int      `json:"schema"`
	Capabilities     []string `json:"capabilities"`
	NativeABIVersion int      `json:"nativeAbiVersion"`
}

// Supports 报告当前 payload 是否实现了指定能力。
func (l *Layout) Supports(name string) bool {
	if name == "" || name == "core" {
		return true
	}
	if l == nil || l.Shell == nil {
		return false
	}
	for _, cap := range l.Shell.Capabilities {
		if cap == name {
			return true
		}
	}
	return false
}

func ExeDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

func HomeDir(override string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}
	if env := os.Getenv(EnvHome); env != "" {
		return filepath.Abs(env)
	}
	return ExeDir()
}

func Locate(override string) (*Layout, error) {
	return LocateOn(override, runtime.GOOS)
}

// LocateOn 按目标平台（而非构建机平台）解析发行目录布局，用于交叉打包。
func LocateOn(override, goos string) (*Layout, error) {
	home, err := HomeDir(override)
	if err != nil {
		return nil, err
	}
	l := &Layout{
		Home:       home,
		ToolsDir:   filepath.Join(home, "tools"),
		Aapt2:      filepath.Join(home, "tools", aapt2NameFor(goos)),
		AndroidJar: filepath.Join(home, "tools", "android.jar"),
		Versions:   filepath.Join(home, "tools", "versions.json"),
	}
	for _, p := range []string{l.Aapt2, l.AndroidJar} {
		if err := requireFile(p); err != nil {
			return nil, err
		}
	}

	payloadDir := filepath.Join(home, "payload")
	matches, err := filepath.Glob(filepath.Join(payloadDir, "classes*.dex"))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("缺少 shell dex: %s（发行包不完整，运行 berba doctor 检查）", filepath.Join(payloadDir, "classes.dex"))
	}
	sort.Slice(matches, func(i, j int) bool {
		return dexOrder(filepath.Base(matches[i])) < dexOrder(filepath.Base(matches[j]))
	})
	l.Dexes = matches

	l.ShellPath = filepath.Join(payloadDir, "shell.json")
	if data, err := os.ReadFile(l.ShellPath); err == nil {
		var shell ShellManifest
		if err := json.Unmarshal(data, &shell); err != nil {
			return nil, fmt.Errorf("解析 payload/shell.json 失败: %w", err)
		}
		if shell.Schema != 1 {
			return nil, fmt.Errorf("payload/shell.json 的 schema 不受支持: %d", shell.Schema)
		}
		sort.Strings(shell.Capabilities)
		l.Shell = &shell
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取 payload/shell.json 失败: %w", err)
	}

	libRoot := filepath.Join(payloadDir, "lib")
	abis, _ := os.ReadDir(libRoot)
	for _, abi := range abis {
		if !abi.IsDir() {
			continue
		}
		files, _ := os.ReadDir(filepath.Join(libRoot, abi.Name()))
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".so") {
				continue
			}
			l.Libs = append(l.Libs, Lib{
				Name: path.Join("lib", abi.Name(), f.Name()),
				Path: filepath.Join(libRoot, abi.Name(), f.Name()),
			})
		}
	}

	nativeRoot := filepath.Join(payloadDir, "native")
	if abis, err := os.ReadDir(nativeRoot); err == nil {
		l.Native = map[string]string{}
		for _, abi := range abis {
			if !abi.IsDir() {
				continue
			}
			p := filepath.Join(nativeRoot, abi.Name(), NativeShimName)
			st, err := os.Lstat(p)
			if err != nil || !st.Mode().IsRegular() {
				continue
			}
			l.Native[abi.Name()] = p
		}
	}
	return l, nil
}

// dexOrder 保证 classes.dex 排第一，classes2.dex 次之。
func dexOrder(name string) int {
	if name == "classes.dex" {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(name, "classes%d.dex", &n); err != nil {
		return 1 << 20
	}
	return n
}

func requireFile(p string) error {
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return fmt.Errorf("缺少文件: %s（请使用完整发行包，或设置 %s）", p, EnvHome)
	}
	return nil
}

func aapt2NameFor(goos string) string {
	if goos == "windows" {
		return "aapt2.exe"
	}
	return "aapt2"
}

type VersionsFile struct {
	Version    string        `json:"version"`
	Aapt2      VersionedHash `json:"aapt2"`
	AndroidJar VersionedHash `json:"androidJar"`
	Shell      ShellVersions `json:"shell"`
}

// ShellVersions 记录 payload/classes*.dex 与原生 shim 的哈希。
type ShellVersions struct {
	DexSha256    []string          `json:"dexSha256"`
	NativeSha256 map[string]string `json:"nativeSha256,omitempty"`
}

type VersionedHash struct {
	Version string `json:"version"`
	Sha256  string `json:"sha256"`
}

func ReadVersions(p string) (*VersionsFile, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var v VersionsFile
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func Sha256File(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
