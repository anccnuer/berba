package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"berba/internal/clierr"
	"berba/internal/nativeplugin"
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
	switch {
	case layout.Shell == nil:
		fmt.Fprintf(out, "  ! payload/shell.json 缺失（旧发行包），仅支持 core 能力\n")
	default:
		caps := "无"
		if len(layout.Shell.Capabilities) > 0 {
			caps = strings.Join(layout.Shell.Capabilities, ", ")
		}
		fmt.Fprintf(out, "  能力: %s\n", caps)
		fmt.Fprintf(out, "  原生插件 ABI: %d\n", layout.Shell.NativeABIVersion)
		if layout.Shell.NativeABIVersion > 0 {
			for _, abi := range nativeplugin.SupportedABIs {
				p := layout.Native[abi]
				if p == "" {
					fmt.Fprintf(out, "  ✗ 缺少 %s JNI shim: payload/native/%s/%s\n", abi, abi, payload.NativeShimName)
					ok = false
					continue
				}
				if !check("native shim "+abi, p) {
					continue
				}
				if err := nativeplugin.ValidateShimELF(p, abi); err != nil {
					fmt.Fprintf(out, "  ✗ %v\n", err)
					ok = false
				}
			}
		}
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
			if layout.Shell != nil && layout.Shell.NativeABIVersion > 0 {
				switch {
				case len(vf.Shell.NativeSha256) == 0:
					fmt.Fprintf(out, "  ✗ versions.json 未记录 shim 哈希\n")
					ok = false
				default:
					for _, abi := range nativeplugin.SupportedABIs {
						p := layout.Native[abi]
						want := vf.Shell.NativeSha256[abi]
						if p == "" || want == "" {
							fmt.Fprintf(out, "  ✗ versions.json 缺少 %s shim 哈希\n", abi)
							ok = false
							continue
						}
						if !checkHash("native shim "+abi, p, want) {
							ok = false
						}
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
