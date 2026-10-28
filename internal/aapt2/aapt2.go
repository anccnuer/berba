package aapt2

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const commandTimeout = 120 * time.Second

type Tool struct {
	Aapt2      string
	AndroidJar string
}

func ExeName() string {
	if runtime.GOOS == "windows" {
		return "aapt2.exe"
	}
	return "aapt2"
}

func Locate(toolsDir string) (*Tool, error) {
	if toolsDir == "" {
		return nil, fmt.Errorf("未指定工具目录")
	}
	t := &Tool{
		Aapt2:      filepath.Join(toolsDir, ExeName()),
		AndroidJar: filepath.Join(toolsDir, "android.jar"),
	}
	for _, p := range []string{t.Aapt2, t.AndroidJar} {
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			return nil, fmt.Errorf("缺少工具文件: %s（运行 berba doctor 检查发行包）", p)
		}
	}
	return t, nil
}

func (t *Tool) Version(ctx context.Context) (string, error) {
	out, err := t.run(ctx, "version")
	if err != nil {
		return "", fmt.Errorf("aapt2 无法执行: %v", err)
	}
	text := strings.TrimSpace(out)
	if text == "" {
		text = "aapt2 可用"
	}
	return text, nil
}

// Compile 把 res 目录编译为 compiled.zip。
func (t *Tool) Compile(ctx context.Context, resDir, outZip string) error {
	_, err := t.run(ctx, "compile", "--dir", resDir, "-o", outZip)
	if err != nil {
		return mapError("编译资源", err)
	}
	return nil
}

// Link 生成包含 Manifest 与资源的 base APK。
func (t *Tool) Link(ctx context.Context, manifest, compiledZip, outAPK string) error {
	_, err := t.run(ctx, "link",
		"-o", outAPK,
		"-I", t.AndroidJar,
		"--manifest", manifest,
		compiledZip,
	)
	if err != nil {
		return mapError("链接资源", err)
	}
	return nil
}

var badgingRE = regexp.MustCompile(`package: name='([^']*)' versionCode='([^']*)' versionName='([^']*)'`)

type Badging struct {
	Package     string
	VersionCode string
	VersionName string
}

func (t *Tool) DumpBadging(ctx context.Context, apk string) (*Badging, error) {
	out, err := t.run(ctx, "dump", "badging", apk)
	if err != nil {
		return nil, mapError("读取 APK 信息", err)
	}
	m := badgingRE.FindStringSubmatch(out)
	if m == nil {
		return nil, fmt.Errorf("无法解析 aapt2 dump badging 输出")
	}
	return &Badging{Package: m[1], VersionCode: m[2], VersionName: m[3]}, nil
}

type runError struct {
	code   int
	stderr string
	err    error
}

func (e *runError) Error() string {
	if e.stderr != "" {
		return e.stderr
	}
	return e.err.Error()
}

func (e *runError) Unwrap() error {
	return e.err
}

func (t *Tool) run(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, t.Aapt2, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("aapt2 执行超时（%s）", commandTimeout)
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return "", ctx.Err()
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		code := -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
		return "", &runError{code: code, stderr: msg, err: err}
	}
	return string(out), nil
}

func mapError(action string, err error) error {
	var re *runError
	if errors.As(err, &re) {
		first := re.stderr
		if i := strings.IndexByte(first, '\n'); i >= 0 {
			first = first[:i]
		}
		switch {
		case strings.Contains(first, "AndroidManifest.xml"), strings.Contains(first, "Android resource linking failed"):
			return fmt.Errorf("%s失败，Manifest 或资源有误: %s", action, re.stderr)
		case strings.Contains(first, "not a valid PNG"), strings.Contains(first, "failed to decode"):
			return fmt.Errorf("%s失败，图标文件损坏: %s", action, re.stderr)
		case strings.Contains(first, "Invalid file name"), strings.Contains(first, "invalid file path"):
			return fmt.Errorf("%s失败，资源文件名不合法: %s", action, re.stderr)
		}
	}
	return fmt.Errorf("%s失败: %v", action, err)
}
