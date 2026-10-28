package aapt2

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

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

func (t *Tool) Version() (string, error) {
	cmd := exec.Command(t.Aapt2, "version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("aapt2 无法执行: %s", strings.TrimSpace(string(out)))
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		text = "aapt2 可用"
	}
	return text, nil
}

// Compile 把 res 目录编译为 compiled.zip。
func (t *Tool) Compile(resDir, outZip string) error {
	_, err := t.run("compile", "--dir", resDir, "-o", outZip)
	if err != nil {
		return mapError("编译资源", err)
	}
	return nil
}

// Link 生成包含 Manifest 与资源的 base APK。
func (t *Tool) Link(manifest, compiledZip, outAPK string) error {
	_, err := t.run("link",
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

func (t *Tool) DumpBadging(apk string) (*Badging, error) {
	out, err := t.run("dump", "badging", apk)
	if err != nil {
		return nil, mapError("读取 APK 信息", err)
	}
	m := badgingRE.FindStringSubmatch(out)
	if m == nil {
		return nil, fmt.Errorf("无法解析 aapt2 dump badging 输出")
	}
	return &Badging{Package: m[1], VersionCode: m[2], VersionName: m[3]}, nil
}

func (t *Tool) run(args ...string) (string, error) {
	cmd := exec.Command(t.Aapt2, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return string(out), nil
}

func mapError(action string, err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "AndroidManifest.xml:") && strings.Contains(msg, "error"):
		return fmt.Errorf("%s失败，Manifest 有误: %s", action, msg)
	case strings.Contains(msg, "not a valid PNG"), strings.Contains(msg, "failed to decode"):
		return fmt.Errorf("%s失败，图标文件损坏: %s", action, msg)
	case strings.Contains(msg, "Invalid file name"), strings.Contains(msg, "invalid file path"):
		return fmt.Errorf("%s失败，资源文件名不合法: %s", action, msg)
	default:
		return fmt.Errorf("%s失败: %s", action, msg)
	}
}
