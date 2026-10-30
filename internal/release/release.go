// Package release 把已准备好的发行目录打包为可分发 zip。
package release

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"berba/internal/aapt2"
	"berba/internal/fswalk"
	"berba/internal/nativeplugin"
	"berba/internal/payload"
	"berba/internal/publish"
)

// Options 是发行包的全部输入。Home 应包含 berba 可执行文件、tools/ 与 payload/。
type Options struct {
	Home         string
	OutDir       string
	DocsDir      string
	Version      string
	Platform     string
	TargetOS     string // 空 = 构建机平台；用于交叉打包时决定 aapt2 文件名与 zip 命名
	TargetArch   string // 空 = 构建机架构；写入 zip 文件名
	Aapt2Version string // 仅测试或交叉打包注入；为空时执行本机 aapt2 获取
	Progress     func(string)
}

// Result 描述生成的发行包。
type Result struct {
	ZipPath  string
	SHA256   string
	Size     int64
	Versions payload.VersionsFile
}

// Package 校验发行目录、写入 versions.json、复制文档并生成确定性 zip。
func Package(ctx context.Context, opts Options) (*Result, error) {
	if opts.Home == "" {
		return nil, fmt.Errorf("未指定发行目录")
	}
	home, err := filepath.Abs(opts.Home)
	if err != nil {
		return nil, err
	}
	if opts.Version == "" {
		return nil, fmt.Errorf("未指定版本号")
	}
	platform := opts.Platform
	if platform == "" {
		platform = "android-35"
	}
	targetOS := opts.TargetOS
	if targetOS == "" {
		targetOS = runtime.GOOS
	}
	targetArch := opts.TargetArch
	if targetArch == "" {
		targetArch = runtime.GOARCH
	}
	switch targetOS {
	case "linux", "windows", "darwin":
	default:
		return nil, fmt.Errorf("不支持的目标平台: %s（可选 linux / windows / darwin）", targetOS)
	}
	progress := opts.Progress
	if progress == nil {
		progress = func(string) {}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	progress("校验发行目录")
	layout, err := payload.LocateOn(home, targetOS)
	if err != nil {
		return nil, err
	}

	aapt2Version := opts.Aapt2Version
	if aapt2Version == "" {
		if targetOS != runtime.GOOS {
			return nil, fmt.Errorf("交叉打包目标平台 %s 无法在本机执行 aapt2，需注入 --aapt2-version", targetOS)
		}
		progress("读取 aapt2 版本")
		tool, err := aapt2.Locate(layout.ToolsDir)
		if err != nil {
			return nil, err
		}
		aapt2Version, err = tool.Version(ctx)
		if err != nil {
			return nil, err
		}
	}

	progress("校验原生 shim")
	shell := payload.ShellVersions{}
	for _, dex := range layout.Dexes {
		shell.DexSha256 = append(shell.DexSha256, mustHash(dex))
	}
	// 发行包必须携带 shell.json：它既是能力协商清单，也是原生 shim 的 ABI 版本来源。
	// LocateOn 对缺失的 shell.json 是宽容的（便于旧 payload 继续工作），这里收紧为错误。
	if layout.Shell == nil {
		return nil, fmt.Errorf("发行包缺少 payload/shell.json（用 scripts/build-shell 重新生成）")
	}
	if layout.Shell.NativeABIVersion != nativeplugin.ABIVersion {
		return nil, fmt.Errorf("shell 的 nativeAbiVersion=%d，期望 %d（请重新构建 shell）", layout.Shell.NativeABIVersion, nativeplugin.ABIVersion)
	}
	shell.NativeSha256 = make(map[string]string, len(nativeplugin.SupportedABIs))
	for _, abi := range nativeplugin.SupportedABIs {
		shim, ok := layout.Native[abi]
		if !ok {
			return nil, fmt.Errorf("发行包缺少 payload/native/%s/%s", abi, payload.NativeShimName)
		}
		if err := nativeplugin.ValidateShimELF(shim, abi); err != nil {
			return nil, err
		}
		shell.NativeSha256[abi] = mustHash(shim)
	}

	progress("写入 versions.json")
	versions := payload.VersionsFile{
		Version: opts.Version,
		Aapt2: payload.VersionedHash{
			Version: aapt2Version,
			Sha256:  mustHash(layout.Aapt2),
		},
		AndroidJar: payload.VersionedHash{
			Version: platform,
			Sha256:  mustHash(layout.AndroidJar),
		},
		Shell: shell,
	}
	if err := writeVersions(layout.Versions, versions); err != nil {
		return nil, err
	}

	if opts.DocsDir != "" {
		progress("复制文档")
		if err := copyDocs(opts.DocsDir, home); err != nil {
			return nil, err
		}
	}

	progress("生成发行 zip")
	outDir := opts.OutDir
	if outDir == "" {
		outDir = filepath.Dir(home)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	name := fmt.Sprintf("berba-%s-%s-%s.zip", opts.Version, targetOS, targetArch)
	zipPath, sum, size, err := writeZip(ctx, home, outDir, name)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(zipPath+".sha256", []byte(sum+"  "+name+"\n"), 0o644); err != nil {
		return nil, err
	}
	return &Result{ZipPath: zipPath, SHA256: sum, Size: size, Versions: versions}, nil
}

func mustHash(path string) string {
	sum, err := payload.Sha256File(path)
	if err != nil {
		return ""
	}
	return sum
}

func writeVersions(path string, v payload.VersionsFile) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func copyDocs(srcRoot, dstRoot string) error {
	var files []string
	for _, name := range []string{"README.md", "NOTICE"} {
		if st, err := os.Stat(filepath.Join(srcRoot, name)); err == nil && !st.IsDir() {
			files = append(files, name)
		}
	}
	for _, dir := range []string{"docs", "include"} {
		src := filepath.Join(srcRoot, dir)
		st, err := os.Stat(src)
		if err != nil || !st.IsDir() {
			continue
		}
		if err := fswalk.Files(src, func(relSlash, absPath string) error {
			files = append(files, filepath.Join(dir, filepath.FromSlash(relSlash)))
			return nil
		}); err != nil {
			return err
		}
	}
	for _, rel := range files {
		if err := copyFile(filepath.Join(srcRoot, rel), filepath.Join(dstRoot, rel)); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

var zipEpoch = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

func writeZip(ctx context.Context, home, outDir, name string) (path, sum string, size int64, err error) {
	var entries []string
	if err := fswalk.Files(home, func(relSlash, absPath string) error {
		entries = append(entries, relSlash)
		return nil
	}); err != nil {
		return "", "", 0, err
	}
	sort.Strings(entries)

	tmp, err := os.CreateTemp(outDir, "."+name+".*.tmp")
	if err != nil {
		return "", "", 0, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	h := sha256.New()
	mw := io.MultiWriter(tmp, h)
	zw := zip.NewWriter(mw)
	for _, rel := range entries {
		if err := ctx.Err(); err != nil {
			tmp.Close()
			return "", "", 0, err
		}
		info, err := os.Stat(filepath.Join(home, filepath.FromSlash(rel)))
		if err != nil {
			tmp.Close()
			return "", "", 0, err
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			tmp.Close()
			return "", "", 0, err
		}
		hdr.Name = rel
		hdr.Method = zip.Deflate
		hdr.Modified = zipEpoch
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			tmp.Close()
			return "", "", 0, err
		}
		f, err := os.Open(filepath.Join(home, filepath.FromSlash(rel)))
		if err != nil {
			tmp.Close()
			return "", "", 0, err
		}
		_, copyErr := io.Copy(w, f)
		f.Close()
		if copyErr != nil {
			tmp.Close()
			return "", "", 0, copyErr
		}
	}
	if err := zw.Close(); err != nil {
		tmp.Close()
		return "", "", 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", "", 0, err
	}
	final := filepath.Join(outDir, name)
	if err := publish.RenameOver(tmpPath, final); err != nil {
		return "", "", 0, err
	}
	st, err := os.Stat(final)
	if err != nil {
		return "", "", 0, err
	}
	return final, hex.EncodeToString(h.Sum(nil)), st.Size(), nil
}

// SanitizeVersion 拒绝会破坏文件名或路径的版本号。
func SanitizeVersion(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" || strings.ContainsAny(v, `/\:*?"<>|`) || strings.Contains(v, "..") {
		return "", fmt.Errorf("版本号不合法: %q", v)
	}
	return v, nil
}
