package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"berba/internal/payload"
	"berba/internal/release"
)

func runRelease(args []string, version string) error {
	fsFlags := newFlagSet("release", "打包已准备好的发行目录")
	home := fsFlags.String("home", "", "发行目录（默认 exe 所在目录）")
	outDir := fsFlags.String("out", "", "输出目录（默认发行目录的上级）")
	ver := fsFlags.String("version", version, "发行版本号")
	docs := fsFlags.String("docs", "", "文档根目录（含 README.md、NOTICE、docs/）")
	platform := fsFlags.String("platform", "android-35", "android.jar 对应的 platform")
	if err := parseArgs(fsFlags, args); err != nil {
		return err
	}

	verValue, err := release.SanitizeVersion(*ver)
	if err != nil {
		return err
	}
	homeDir, err := payload.HomeDir(*home)
	if err != nil {
		return err
	}
	docsDir := *docs
	if docsDir != "" {
		if docsDir, err = filepath.Abs(docsDir); err != nil {
			return err
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	res, err := release.Package(ctx, release.Options{
		Home:     homeDir,
		OutDir:   *outDir,
		DocsDir:  docsDir,
		Version:  verValue,
		Platform: *platform,
		Progress: func(step string) {
			fmt.Fprintf(out, "==> %s\n", step)
		},
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "\n完成: %s (%.1f MB)\n", res.ZipPath, float64(res.Size)/(1024*1024))
	fmt.Fprintf(out, "SHA256: %s\n", res.SHA256)
	fmt.Fprintf(out, "版本清单: berba %s / aapt2 %s / %s\n", res.Versions.Version, res.Versions.Aapt2.Version, res.Versions.AndroidJar.Version)
	return nil
}
