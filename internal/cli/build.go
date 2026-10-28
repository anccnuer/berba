package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"berba/internal/build"
)

func runBuild(args []string) error {
	fsFlags := newFlagSet("build", "构建 APK")
	configPath := fsFlags.String("config", "", "app.json 路径（默认 <目录>/app.json）")
	outDir := fsFlags.String("out", "", "输出目录（默认 <项目>/dist）")
	home := fsFlags.String("home", "", "发行包根目录（默认 exe 所在目录）")
	debug := fsFlags.Bool("debug", false, "构建可调试版本（WebView 调试 + debuggable）")
	keyPath := fsFlags.String("key", "", "签名密钥路径（默认 <项目>/.berba/keys/release.pem）")
	if err := parseArgs(fsFlags, args); err != nil {
		return err
	}

	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = configPathFor(fsFlags.Arg(0))
	} else {
		cfgPath = configPathFor(cfgPath)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	res, err := build.Build(ctx, build.Options{
		ConfigPath: cfgPath,
		OutDir:     *outDir,
		Home:       *home,
		Debug:      *debug,
		KeyPath:    *keyPath,
		Progress: func(s build.Step) {
			fmt.Fprintf(out, "[%d/%d] %s\n", s.Index, s.Total, s.Name)
		},
		Warn: func(msg string) {
			fmt.Fprintf(errOut, "警告: %s\n", msg)
		},
	})
	if err != nil {
		return err
	}

	if res.KeyCreated {
		fmt.Fprintf(out, "  已生成签名密钥: %s\n  指纹 SHA-256: %s\n  警告: 请离线备份该文件，丢失后将无法覆盖升级。\n", res.KeyPath, res.KeyFingerprint)
	}

	fmt.Fprintf(out, "  APK:    %s (%.2f MB)\n", res.APKPath, float64(res.Size)/(1024*1024))
	fmt.Fprintf(out, "  SHA256: %s\n", res.SHA256)
	fmt.Fprintf(out, "  包名:   %s\n", res.Project.Config.Package)
	fmt.Fprintf(out, "  版本:   %s (%d)\n", res.Project.Config.VersionName, res.Project.Config.VersionCode)
	if res.Debug {
		fmt.Fprintln(out, "  模式:   debug")
	}
	fmt.Fprintf(out, "\n安装: berba install %s\n", res.APKPath)
	return nil
}
