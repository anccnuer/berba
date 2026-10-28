package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

var (
	out    io.Writer = os.Stdout
	errOut io.Writer = os.Stderr
)

const usageText = `guiapp - 把前端页面和 Lua 逻辑打包成 Android APK

用法:
  guiapp <命令> [参数]

命令:
  init [目录]        创建项目脚手架（默认当前目录）
  build [目录]       构建 APK（默认当前目录，需要 app.json）
  keygen             生成签名密钥（缺失时 build 会自动生成）
  doctor             检查工具链与 payload 是否完整
  install [apk]      使用 adb 安装 APK（默认 dist/ 下的 APK）
  version            显示版本

全局参数:
  --home <目录>      指定发行包根目录（默认 exe 所在目录，或环境变量 GUIAPP_HOME）

示例:
  guiapp init myapp --package com.example.myapp
  cd myapp && guiapp build
  guiapp install
`

func Run(args []string, version string) int {
	if len(args) == 0 {
		fmt.Fprint(out, usageText)
		return 1
	}
	var err error
	switch args[0] {
	case "init":
		err = runInit(args[1:])
	case "build":
		err = runBuild(args[1:])
	case "keygen":
		err = runKeygen(args[1:])
	case "doctor":
		err = runDoctor(args[1:])
	case "install":
		err = runInstall(args[1:])
	case "version", "--version", "-v":
		fmt.Fprintf(out, "guiapp %s\n", version)
		return 0
	case "help", "--help", "-h":
		fmt.Fprint(out, usageText)
		return 0
	default:
		fmt.Fprintf(errOut, "未知命令 %q\n\n", args[0])
		fmt.Fprint(errOut, usageText)
		return 1
	}
	if err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		fmt.Fprintf(errOut, "错误: %v\n", err)
		return 1
	}
	return 0
}

func newFlagSet(name, summary string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() {
		fmt.Fprintf(errOut, "%s\n\n用法: guiapp %s [参数]\n\n参数:\n", summary, name)
		fs.PrintDefaults()
	}
	return fs
}

// parseArgs 允许位置参数与选项混用（标准 flag 遇到首个位置参数即停止解析）。
func parseArgs(fs *flag.FlagSet, args []string) error {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			if strings.Contains(name, "=") {
				continue
			}
			if f := fs.Lookup(name); f != nil {
				if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
					continue
				}
				if i+1 < len(args) {
					flags = append(flags, args[i+1])
					i++
				}
			}
			continue
		}
		positional = append(positional, a)
	}
	return fs.Parse(append(flags, positional...))
}
