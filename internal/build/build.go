// Package build 提供与 CLI 解耦的 APK 构建流水线。
package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"berba/internal/aapt2"
	"berba/internal/apk"
	"berba/internal/clierr"
	"berba/internal/config"
	"berba/internal/manifest"
	"berba/internal/payload"
	"berba/internal/resources"
	"berba/internal/sign"
)

const stepTotal = 7

// Step 描述构建流水线的一个阶段。
type Step struct {
	Index int
	Total int
	Name  string
}

// Runner 抽象 aapt2 调用，便于测试注入。
type Runner interface {
	Compile(ctx context.Context, resDir, outZip string) error
	Link(ctx context.Context, manifest, compiledZip, outAPK string) error
	DumpBadging(ctx context.Context, apk string) (*aapt2.Badging, error)
}

// Options 是构建的全部输入。
type Options struct {
	ConfigPath string
	OutDir     string
	Home       string
	Debug      bool
	KeyPath    string

	Runner   Runner
	Progress func(Step)
	Warn     func(string)
}

// Result 汇总一次成功构建的产物与元信息。
type Result struct {
	Project        *config.Project
	APKPath        string
	SHA256         string
	Size           int64
	Debug          bool
	KeyPath        string
	KeyCreated     bool
	KeyFingerprint string
}

// Build 执行完整构建。ctx 取消会中止 aapt2 与后续阶段。
// 返回的错误按阶段用 clierr 标注退出码：配置=2、工具链=3、构建=4、校验=5。
func Build(ctx context.Context, opts Options) (result *Result, retErr error) {
	phase := clierr.CodeConfig
	defer func() {
		if retErr != nil && !clierr.Is(retErr) && !errors.Is(retErr, context.Canceled) {
			retErr = clierr.New(phase, "", retErr)
		}
	}()
	step := func(index int, name string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		phase = phaseForStep(index)
		if opts.Progress != nil {
			opts.Progress(Step{Index: index, Total: stepTotal, Name: name})
		}
		return nil
	}
	warn := func(msg string) {
		if opts.Warn != nil {
			opts.Warn(msg)
		}
	}

	if err := step(1, "校验配置 "+opts.ConfigPath); err != nil {
		return nil, err
	}
	proj, err := config.Load(opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	for _, w := range proj.Warnings {
		warn(w)
	}
	isDebug := opts.Debug || proj.Config.Debug

	if err := step(2, "定位工具链"); err != nil {
		return nil, err
	}
	layout, err := payload.Locate(opts.Home)
	if err != nil {
		return nil, err
	}
	runner := opts.Runner
	if runner == nil {
		tool, err := aapt2.Locate(layout.ToolsDir)
		if err != nil {
			return nil, err
		}
		runner = tool
	}

	buildDir := filepath.Join(proj.Root, ".berba", "build")
	resDir := filepath.Join(buildDir, "res")
	if err := os.RemoveAll(buildDir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(resDir, 0o755); err != nil {
		return nil, err
	}

	if err := step(3, "渲染 Manifest 与资源"); err != nil {
		return nil, err
	}
	manifestData, err := manifest.Render(proj, isDebug)
	if err != nil {
		return nil, err
	}
	if err := manifest.Validate(manifestData); err != nil {
		return nil, err
	}
	manifestPath := filepath.Join(buildDir, "AndroidManifest.xml")
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		return nil, err
	}
	if err := resources.Generate(resDir, proj.IconPath, proj.Config.Name); err != nil {
		return nil, fmt.Errorf("生成图标失败: %w", err)
	}
	if err := resources.WriteStrings(resDir, proj.Config.Name); err != nil {
		return nil, err
	}

	if err := step(4, "运行 aapt2"); err != nil {
		return nil, err
	}
	compiled := filepath.Join(buildDir, "compiled.zip")
	baseAPK := filepath.Join(buildDir, "base.apk")
	if err := runner.Compile(ctx, resDir, compiled); err != nil {
		return nil, err
	}
	if err := runner.Link(ctx, manifestPath, compiled, baseAPK); err != nil {
		return nil, err
	}

	if err := step(5, "组装 APK"); err != nil {
		return nil, err
	}
	extra, err := collectPayload(proj, layout, isDebug)
	if err != nil {
		return nil, err
	}
	unsigned, err := apk.Assemble(baseAPK, extra)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(buildDir, "app-unsigned.apk"), unsigned, 0o644); err != nil {
		return nil, err
	}

	keyPath := opts.KeyPath
	explicitKey := keyPath != ""
	if !explicitKey {
		keyPath = filepath.Join(proj.Root, ".berba", "keys", "release.pem")
	}
	var key *sign.Key
	var created bool
	if explicitKey {
		key, err = sign.Load(keyPath)
		if err != nil {
			return nil, fmt.Errorf("读取签名密钥失败: %w", err)
		}
	} else {
		key, created, err = sign.LoadOrCreate(keyPath)
		if err != nil {
			return nil, err
		}
	}
	signed, err := sign.Sign(unsigned, key)
	if err != nil {
		return nil, err
	}
	phase = clierr.CodeVerify
	if _, err := sign.Verify(signed); err != nil {
		return nil, fmt.Errorf("签名自检失败: %w", err)
	}

	distDir := opts.OutDir
	if distDir == "" {
		distDir = filepath.Join(proj.Root, "dist")
	}
	baseName := sanitizeName(proj.Config.Package + "-" + proj.Config.VersionName)
	fileName := baseName + ".apk"
	if isDebug {
		fileName = baseName + "-debug.apk"
	}

	if err := step(6, "校验签名与包信息"); err != nil {
		return nil, err
	}
	apkPath, err := Publish(distDir, fileName, signed, func(tmpPath string) error {
		badging, err := runner.DumpBadging(ctx, tmpPath)
		if err != nil {
			return fmt.Errorf("构建后自检失败: %w", err)
		}
		if badging.Package != proj.Config.Package {
			return fmt.Errorf("包名不一致: manifest=%s apk=%s", proj.Config.Package, badging.Package)
		}
		if badging.VersionCode != strconv.Itoa(proj.Config.VersionCode) {
			return fmt.Errorf("versionCode 不一致: 期望 %d，实际 %s", proj.Config.VersionCode, badging.VersionCode)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sum := sha256.Sum256(signed)
	sumHex := hex.EncodeToString(sum[:])
	if err := os.WriteFile(apkPath+".sha256", []byte(sumHex+"  "+fileName+"\n"), 0o644); err != nil {
		return nil, err
	}

	if err := step(7, "完成"); err != nil {
		return nil, err
	}
	return &Result{
		Project:        proj,
		APKPath:        apkPath,
		SHA256:         sumHex,
		Size:           int64(len(signed)),
		Debug:          isDebug,
		KeyPath:        keyPath,
		KeyCreated:     created,
		KeyFingerprint: key.Fingerprint(),
	}, nil
}

// Publish 先写入临时文件并完成校验，成功后才替换目标目录中的正式产物。
func Publish(distDir, fileName string, data []byte, verify func(tmpPath string) error) (string, error) {
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(distDir, "."+fileName+".*.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := verify(tmpPath); err != nil {
		return "", err
	}
	finalPath := filepath.Join(distDir, fileName)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		// Windows 不允许 rename 覆盖已存在文件。
		if removeErr := os.Remove(finalPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return "", err
		}
		if err := os.Rename(tmpPath, finalPath); err != nil {
			return "", err
		}
	}
	return finalPath, nil
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

// phaseForStep 把流水线阶段映射为退出码。
func phaseForStep(index int) clierr.Code {
	switch index {
	case 1:
		return clierr.CodeConfig
	case 2:
		return clierr.CodeToolchain
	case 6:
		return clierr.CodeVerify
	default:
		return clierr.CodeBuild
	}
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
