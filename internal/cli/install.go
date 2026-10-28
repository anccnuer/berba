package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"berba/internal/aapt2"
	"berba/internal/payload"
)

func locateAapt2(layout *payload.Layout) (*aapt2.Tool, error) {
	return aapt2.Locate(layout.ToolsDir)
}

func runInstall(args []string) error {
	fsFlags := newFlagSet("install", "使用 adb 安装 APK")
	device := fsFlags.String("device", "", "指定设备序列号")
	adbPath := fsFlags.String("adb", "", "adb 路径（默认 PATH 或 BERBA_ADB）")
	apkPath := fsFlags.String("apk", "", "APK 路径（默认 dist/ 下最新的 APK）")
	if err := parseArgs(fsFlags, args); err != nil {
		return err
	}

	apk := *apkPath
	if apk == "" && fsFlags.Arg(0) != "" {
		apk = fsFlags.Arg(0)
	}
	if apk == "" {
		matches, _ := filepath.Glob(filepath.Join("dist", "*.apk"))
		if len(matches) == 0 {
			return fmt.Errorf("未找到 APK，请先运行 berba build 或指定路径")
		}
		sort.Slice(matches, func(i, j int) bool {
			si, errI := os.Stat(matches[i])
			sj, errJ := os.Stat(matches[j])
			if errI != nil || errJ != nil {
				return matches[i] > matches[j]
			}
			return si.ModTime().After(sj.ModTime())
		})
		apk = matches[0]
	}
	if st, err := os.Stat(apk); err != nil || st.IsDir() {
		return fmt.Errorf("APK 不存在: %s", apk)
	}

	adb := *adbPath
	if adb == "" {
		adb = os.Getenv("BERBA_ADB")
	}
	if adb == "" {
		adb, _ = exec.LookPath("adb")
	}
	if adb == "" {
		fmt.Fprintln(out, "未找到 adb，已跳过安装。")
		fmt.Fprintln(out, "请将 adb 加入 PATH，或设置 BERBA_ADB，或使用 --adb 指定。")
		fmt.Fprintf(out, "APK 已就绪: %s\n", apk)
		return nil
	}

	cmdArgs := []string{}
	if *device != "" {
		cmdArgs = append(cmdArgs, "-s", *device)
	}
	cmdArgs = append(cmdArgs, "install", "-r", apk)
	cmd := exec.Command(adb, cmdArgs...)
	cmd.Stdout = out
	cmd.Stderr = errOut
	cmd.Stdin = os.Stdin
	fmt.Fprintf(out, "运行: %s %s\n", adb, joinArgs(cmdArgs))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("adb install 失败: %w", err)
	}
	return nil
}

func joinArgs(args []string) string {
	s := ""
	for i, a := range args {
		if i > 0 {
			s += " "
		}
		s += a
	}
	return s
}
