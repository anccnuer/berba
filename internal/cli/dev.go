package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"

	"berba/internal/clierr"
	"berba/internal/config"
	"berba/internal/devserver"
)

func runDev(args []string) error {
	fsFlags := newFlagSet("dev", "启动本地静态服务器（注入桥 mock）")
	port := fsFlags.Int("port", 5173, "监听端口（0 表示随机）")
	host := fsFlags.String("host", "127.0.0.1", "监听地址")
	open := fsFlags.Bool("open", false, "启动后打开浏览器")
	mockPath := fsFlags.String("mock", "", "桥 mock 脚本（默认 <项目>/dev/mock.js，存在时加载）")
	if err := parseArgs(fsFlags, args); err != nil {
		return err
	}

	cfgPath := configPathFor(fsFlags.Arg(0))
	proj, err := config.Load(cfgPath)
	if err != nil {
		return clierr.New(clierr.CodeConfig, "", err)
	}

	entry := "/index.html"
	if proj.LocalStart {
		if p := proj.StartURL.Path; p != "" {
			entry = p
		}
	} else {
		fmt.Fprintf(errOut, "警告: startUrl 不是本地页面，dev 将改为提供 %s 下的文件\n", proj.WebDir)
	}

	mock := ""
	mockFile := *mockPath
	explicitMock := mockFile != ""
	if mockFile == "" {
		mockFile = filepath.Join(proj.Root, "dev", "mock.js")
	}
	if data, readErr := os.ReadFile(mockFile); readErr == nil {
		mock = string(data)
	} else if explicitMock {
		return fmt.Errorf("读取 mock 脚本失败: %w", readErr)
	}

	luaRoot := ""
	luaEntry := ""
	if proj.HasLua {
		luaRoot = proj.LuaDir
		luaEntry = strings.TrimSpace(proj.Config.Lua.Entry)
		if luaEntry == "" {
			luaEntry = "main.lua"
		}
	}

	srv, err := devserver.New(devserver.Options{
		Root:     proj.WebDir,
		Entry:    entry,
		SPA:      proj.Config.Web.SPA,
		MockJS:   mock,
		Host:     *host,
		Port:     *port,
		LuaRoot:  luaRoot,
		LuaEntry: luaEntry,
		App: devserver.AppMeta{
			Name:        proj.Config.Name,
			Package:     proj.Config.Package,
			VersionName: proj.Config.VersionName,
			VersionCode: proj.Config.VersionCode,
		},
	})
	if err != nil {
		return err
	}
	ln, url, err := srv.Listen()
	if err != nil {
		return fmt.Errorf("监听端口失败: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fmt.Fprintf(out, "开发服务器: %s\n", url)
	if luaRoot != "" {
		fmt.Fprintf(out, "Lua 运行时: 浏览器内执行 %s（fengari）\n", luaEntry)
	}
	switch {
	case mock != "" && luaRoot != "":
		fmt.Fprintf(out, "桥 mock:    %s（优先于 Lua）\n", mockFile)
	case mock != "":
		fmt.Fprintf(out, "桥 mock:    %s\n", mockFile)
	case luaRoot != "":
		fmt.Fprintln(out, "桥 mock:    未找到 dev/mock.js，桥调用交给 Lua 运行时")
	default:
		fmt.Fprintln(out, "桥 mock:    未找到 dev/mock.js，调用 window.berba.call 将 reject")
	}
	fmt.Fprintln(out, "按 Ctrl+C 退出")
	if *open {
		openBrowser(url)
	}
	return srv.Serve(ctx, ln)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(errOut, "警告: 无法打开浏览器: %v\n", err)
	}
}
