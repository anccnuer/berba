package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"guiapp/internal/aapt2"
	"guiapp/internal/apk"
	"guiapp/internal/config"
	"guiapp/internal/manifest"
	"guiapp/internal/payload"
	"guiapp/internal/resources"
	"guiapp/internal/sign"
)

func runBuild(args []string) error {
	fsFlags := newFlagSet("build", "构建 APK")
	configPath := fsFlags.String("config", "", "app.json 路径（默认 <目录>/app.json）")
	outDir := fsFlags.String("out", "", "输出目录（默认 <项目>/dist）")
	home := fsFlags.String("home", "", "发行包根目录（默认 exe 所在目录）")
	debug := fsFlags.Bool("debug", false, "构建可调试版本（WebView 调试 + debuggable）")
	if err := parseArgs(fsFlags, args); err != nil {
		return err
	}

	cfgPath := *configPath
	if cfgPath == "" {
		target := fsFlags.Arg(0)
		if target == "" {
			target = "app.json"
		}
		if st, err := os.Stat(target); err == nil && st.IsDir() {
			cfgPath = filepath.Join(target, "app.json")
		} else {
			cfgPath = target
		}
	} else if st, err := os.Stat(cfgPath); err == nil && st.IsDir() {
		cfgPath = filepath.Join(cfgPath, "app.json")
	}

	fmt.Fprintf(out, "[1/7] 校验配置 %s\n", cfgPath)
	proj, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	isDebug := *debug || proj.Config.Debug

	fmt.Fprintln(out, "[2/7] 定位工具链")
	layout, err := payload.Locate(*home)
	if err != nil {
		return err
	}
	tool, err := aapt2.Locate(layout.ToolsDir)
	if err != nil {
		return err
	}

	buildDir := filepath.Join(proj.Root, ".guiapp", "build")
	resDir := filepath.Join(buildDir, "res")
	if err := os.RemoveAll(buildDir); err != nil {
		return err
	}
	if err := os.MkdirAll(resDir, 0o755); err != nil {
		return err
	}

	fmt.Fprintln(out, "[3/7] 渲染 Manifest 与资源")
	manifestData, err := manifest.Render(proj, isDebug)
	if err != nil {
		return err
	}
	if err := manifest.Validate(manifestData); err != nil {
		return err
	}
	manifestPath := filepath.Join(buildDir, "AndroidManifest.xml")
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		return err
	}
	if err := resources.Generate(resDir, proj.IconPath, proj.Config.Name); err != nil {
		return fmt.Errorf("生成图标失败: %w", err)
	}
	if err := resources.WriteStrings(resDir, proj.Config.Name); err != nil {
		return err
	}

	fmt.Fprintln(out, "[4/7] 运行 aapt2")
	compiled := filepath.Join(buildDir, "compiled.zip")
	baseAPK := filepath.Join(buildDir, "base.apk")
	if err := tool.Compile(resDir, compiled); err != nil {
		return err
	}
	if err := tool.Link(manifestPath, compiled, baseAPK); err != nil {
		return err
	}

	fmt.Fprintln(out, "[5/7] 组装 APK")
	extra, err := collectPayload(proj, layout, isDebug)
	if err != nil {
		return err
	}
	unsigned, err := apk.Assemble(baseAPK, extra)
	if err != nil {
		return err
	}
	unsignedPath := filepath.Join(buildDir, "app-unsigned.apk")
	if err := os.WriteFile(unsignedPath, unsigned, 0o644); err != nil {
		return err
	}

	keyPath := filepath.Join(proj.Root, ".guiapp", "keys", "release.pem")
	key, created, err := sign.LoadOrCreate(keyPath)
	if err != nil {
		return err
	}
	if created {
		fmt.Fprintf(out, "  已生成签名密钥: %s\n  指纹 SHA-256: %s\n  警告: 请离线备份该文件，丢失后将无法覆盖升级。\n", keyPath, key.Fingerprint())
	}
	signed, err := sign.Sign(unsigned, key)
	if err != nil {
		return err
	}

	distDir := *outDir
	if distDir == "" {
		distDir = filepath.Join(proj.Root, "dist")
	}
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		return err
	}
	fileName := sanitizeName(proj.Config.Package+"-"+proj.Config.VersionName) + ".apk"
	apkPath := filepath.Join(distDir, fileName)
	if err := os.WriteFile(apkPath, signed, 0o644); err != nil {
		return err
	}

	fmt.Fprintln(out, "[6/7] 校验签名与包信息")
	if _, err := sign.Verify(signed); err != nil {
		return fmt.Errorf("签名自检失败: %w", err)
	}
	badging, err := tool.DumpBadging(apkPath)
	if err != nil {
		return fmt.Errorf("构建后自检失败: %w", err)
	}
	if badging.Package != proj.Config.Package {
		return fmt.Errorf("包名不一致: manifest=%s apk=%s", proj.Config.Package, badging.Package)
	}
	if badging.VersionCode != strconv.Itoa(proj.Config.VersionCode) {
		return fmt.Errorf("versionCode 不一致: 期望 %d，实际 %s", proj.Config.VersionCode, badging.VersionCode)
	}

	sum := sha256.Sum256(signed)
	sumHex := hex.EncodeToString(sum[:])
	sumPath := apkPath + ".sha256"
	if err := os.WriteFile(sumPath, []byte(sumHex+"  "+fileName+"\n"), 0o644); err != nil {
		return err
	}

	fmt.Fprintf(out, "[7/7] 完成\n\n")
	fmt.Fprintf(out, "  APK:    %s (%.2f MB)\n", apkPath, float64(len(signed))/(1024*1024))
	fmt.Fprintf(out, "  SHA256: %s\n", sumHex)
	fmt.Fprintf(out, "  包名:   %s\n", proj.Config.Package)
	fmt.Fprintf(out, "  版本:   %s (%d)\n", proj.Config.VersionName, proj.Config.VersionCode)
	if isDebug {
		fmt.Fprintln(out, "  模式:   debug")
	}
	fmt.Fprintf(out, "\n安装: guiapp install %s\n", apkPath)
	return nil
}

func collectPayload(proj *config.Project, layout *payload.Layout, debug bool) ([]apk.Entry, error) {
	var entries []apk.Entry
	for _, dex := range layout.Dexes {
		data, err := os.ReadFile(dex)
		if err != nil {
			return nil, err
		}
		entries = append(entries, apk.Entry{Name: filepath.Base(dex), Data: data})
	}
	for _, lib := range layout.Libs {
		data, err := os.ReadFile(lib.Path)
		if err != nil {
			return nil, err
		}
		entries = append(entries, apk.Entry{Name: lib.Name, Data: data})
	}

	www, err := apk.DirEntries(proj.WebDir, "assets/www")
	if err != nil {
		return nil, fmt.Errorf("收集 www 目录失败: %w", err)
	}
	entries = append(entries, www...)

	if proj.HasLua {
		lua, err := apk.DirEntries(proj.LuaDir, "assets/lua")
		if err != nil {
			return nil, fmt.Errorf("收集 lua 目录失败: %w", err)
		}
		entries = append(entries, lua...)
	}

	ac, err := proj.AssetConfig(debug)
	if err != nil {
		return nil, err
	}
	entries = append(entries, apk.Entry{Name: "assets/app_config.json", Data: ac})
	return entries, nil
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
