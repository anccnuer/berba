package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"berba/internal/clierr"
	"berba/internal/payload"
)

func runDoctor(args []string) error {
	fsFlags := newFlagSet("doctor", "检查发行包完整性")
	home := fsFlags.String("home", "", "发行包根目录（默认 exe 所在目录）")
	if err := parseArgs(fsFlags, args); err != nil {
		return err
	}

	layout, err := payload.Locate(*home)
	if err != nil {
		fmt.Fprintf(out, "✗ %v\n", err)
		return clierr.New(clierr.CodeToolchain, "", err)
	}
	fmt.Fprintf(out, "发行包目录: %s\n", layout.Home)
	fmt.Fprintf(out, "  平台: %s/%s\n", runtime.GOOS, runtime.GOARCH)

	ok := true
	check := func(label, p string) bool {
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			fmt.Fprintf(out, "  ✗ %s 缺失: %s\n", label, p)
			ok = false
			return false
		}
		fmt.Fprintf(out, "  ✓ %s (%d bytes)\n", label, st.Size())
		return true
	}

	check("aapt2", layout.Aapt2)
	check("android.jar", layout.AndroidJar)
	for _, dex := range layout.Dexes {
		check("shell dex "+filepath.Base(dex), dex)
	}

	if layout.Versions != "" {
		if vf, err := payload.ReadVersions(layout.Versions); err == nil {
			fmt.Fprintf(out, "  版本清单: berba %s\n", vf.Version)
			for _, item := range []struct {
				label string
				path  string
				hash  string
			}{
				{"aapt2", layout.Aapt2, vf.Aapt2.Sha256},
				{"android.jar", layout.AndroidJar, vf.AndroidJar.Sha256},
			} {
				if item.hash == "" {
					continue
				}
				if !checkHash(item.label, item.path, item.hash) {
					ok = false
				}
			}
			switch {
			case len(vf.Shell.DexSha256) == 0:
				fmt.Fprintf(out, "  ! versions.json 未记录 dex 哈希（旧发行包），跳过校验\n")
			case len(vf.Shell.DexSha256) != len(layout.Dexes):
				fmt.Fprintf(out, "  ✗ dex 哈希数量与现场文件不一致（清单 %d，实际 %d）\n", len(vf.Shell.DexSha256), len(layout.Dexes))
				ok = false
			default:
				for i, dex := range layout.Dexes {
					if !checkHash("shell dex "+filepath.Base(dex), dex, vf.Shell.DexSha256[i]) {
						ok = false
					}
				}
			}
		} else {
			fmt.Fprintf(out, "  ! 无法读取 versions.json: %v\n", err)
		}
	}

	if runtime.GOOS != "windows" {
		if st, err := os.Stat(layout.Aapt2); err == nil && st.Mode()&0o111 == 0 {
			fmt.Fprintf(out, "  ✗ aapt2 没有执行权限，请运行 chmod +x\n")
			ok = false
		}
	}

	tool, err := locateAapt2(layout)
	if err != nil {
		fmt.Fprintf(out, "  ✗ %v\n", err)
		ok = false
	} else if v, err := tool.Version(context.Background()); err != nil {
		fmt.Fprintf(out, "  ✗ aapt2 无法运行: %v\n", err)
		ok = false
	} else {
		fmt.Fprintf(out, "  ✓ aapt2 可执行: %s\n", v)
	}

	if !ok {
		return clierr.New(clierr.CodeVerify, "", fmt.Errorf("检查未通过"))
	}
	fmt.Fprintln(out, "全部检查通过，可以开始构建。")
	return nil
}

func checkHash(label, path, want string) bool {
	actual, err := payload.Sha256File(path)
	if err != nil {
		fmt.Fprintf(out, "  ✗ 无法计算 %s 哈希: %v\n", label, err)
		return false
	}
	if actual != want {
		fmt.Fprintf(out, "  ✗ %s 哈希不匹配（文件被修改或版本漂移）\n", label)
		return false
	}
	fmt.Fprintf(out, "  ✓ %s 哈希一致\n", label)
	return true
}
