package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"berba/internal/build"
	"berba/internal/watch"
)

func runWatch(args []string) error {
	fsFlags := newFlagSet("watch", "监听项目变更并自动重建（可选安装）")
	outDir := fsFlags.String("out", "", "输出目录（默认 <项目>/dist）")
	home := fsFlags.String("home", "", "发行包根目录（默认 exe 所在目录）")
	debug := fsFlags.Bool("debug", false, "构建 debug 版本")
	keyPath := fsFlags.String("key", "", "签名密钥路径")
	install := fsFlags.Bool("install", false, "构建成功后安装到设备")
	device := fsFlags.String("device", "", "adb 设备序列号（配合 --install）")
	adbPath := fsFlags.String("adb", "", "adb 路径（配合 --install）")
	interval := fsFlags.Duration("interval", 500*time.Millisecond, "轮询间隔")
	if err := parseArgs(fsFlags, args); err != nil {
		return err
	}

	cfgPath := configPathFor(fsFlags.Arg(0))
	root, err := filepath.Abs(filepath.Dir(cfgPath))
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	buildOnce := func() {
		fmt.Fprintln(out, "开始构建...")
		res, err := build.Build(ctx, build.Options{
			ConfigPath: cfgPath,
			OutDir:     *outDir,
			Home:       *home,
			Debug:      *debug,
			KeyPath:    *keyPath,
			Warn: func(msg string) {
				fmt.Fprintf(errOut, "警告: %s\n", msg)
			},
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			fmt.Fprintf(errOut, "构建失败: %v\n", err)
			return
		}
		fmt.Fprintf(out, "构建成功: %s (%.2f MB)\n", res.APKPath, float64(res.Size)/(1024*1024))
		if *install {
			if err := installAPK(res.APKPath, *device, *adbPath); err != nil {
				fmt.Fprintf(errOut, "安装失败: %v\n", err)
			}
		}
	}

	buildOnce()

	skip := append([]string{}, watch.DefaultSkip...)
	if *outDir != "" {
		if abs, err := filepath.Abs(*outDir); err == nil {
			if rel, err := filepath.Rel(root, abs); err == nil && rel != "." && !filepath.IsAbs(rel) && rel != ".." && !hasParentPrefix(rel) {
				skip = append(skip, filepath.Base(abs))
			}
		}
	}

	fmt.Fprintf(out, "监听 %s（间隔 %s），Ctrl+C 退出\n", root, interval.String())
	return watch.Watch(ctx, root, *interval, skip, buildOnce)
}

func hasParentPrefix(rel string) bool {
	return len(rel) >= 3 && rel[:2] == ".." && (rel[2] == '/' || rel[2] == '\\')
}
