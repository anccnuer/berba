# berba

把前端页面和 Lua 逻辑打包成可安装的 Android APK。终端用户无需安装 JDK、Android SDK 或 Android Studio：解压发行包，运行 `berba build` 即可。

- WebView 承载 `www/` 下的页面（本地虚拟域名或远程 URL）
- Lua 沙箱提供应用生命周期、网络、文件、存储、权限等白名单 API
- 页面与 Lua 通过 Promise 桥双向调用
- 系统能力白名单：权限申请、剪贴板读写、震动、分享、文件选择器、深链（`deepLinks` + `app.on("newIntent")`）
- 纯 Go 实现 ZIP 对齐与 APK Signature Scheme v2 签名
- 支持 Android 7.0+（minSdk 24），面向侧载/企业分发

## 快速开始

1. 解压发行包，确认工具链完整：

   ```bat
   berba.exe doctor
   ```

2. 创建项目并开发：

   ```bat
   berba.exe init myapp --package com.example.myapp
   cd myapp
   ..\berba.exe dev --open
   ```

   `dev` 用本地 HTTP 服务提供 `www/`，并在浏览器里用 `dev/mock.js` 模拟 Lua 桥；改完页面直接刷新即可。构建安装包：`..\berba.exe build`。

3. 产物在 `myapp\dist\com.example.myapp-1.0.0.apk`。拷贝到手机安装，或：

   ```bat
   ..\berba.exe install
   ```

`build` 首次运行会在 `.berba\keys\release.pem` 生成签名密钥。**请离线备份**：它是应用身份，丢失后无法覆盖升级。

## 示例

- `examples/hello`：JS↔Lua 桥、设备信息、本地计数
- `examples/led-banner`：演唱会应援横幅，竖屏设置文字/字体/大小/颜色，横屏全屏无缝滚动，设置自动保存

```bat
cd examples\led-banner
..\..\dist\windows-amd64\berba.exe build
```

## 命令

| 命令 | 说明 |
| --- | --- |
| `berba init [目录]` | 创建脚手架，支持 `--name`、`--package`、`--force` |
| `berba build [目录]` | 构建 APK，支持 `--debug`、`--out`、`--home`、`--config`、`--key` |
| `berba dev [目录]` | 本地静态服务器 + 桥 mock，支持 `--port`、`--host`、`--open`、`--mock` |
| `berba watch [目录]` | 监听变更自动重建，支持 `--install`、`--interval` 及构建参数 |
| `berba keygen` | 生成签名密钥，支持 `--out`、`--force`（旧密钥会先备份） |
| `berba doctor` | 校验工具链、payload 完整性，以及 aapt2/android.jar/dex 哈希 |
| `berba install [apk]` | 通过 adb 安装，支持 `--device`、`--adb`、`--apk` |
| `berba release` | 打包发行目录，写入 `versions.json` 并生成 zip 与 SHA-256 |
| `berba version` | 显示版本 |

发行包根目录默认是 exe 所在目录，也可用 `--home <目录>` 或环境变量 `BERBA_HOME` 覆盖。

`--debug` 产物命名为 `<package>-<versionName>-debug.apk`，不会覆盖 release 产物。`--key` 指定的密钥不存在时直接报错，只有默认路径（`.berba/keys/release.pem`）才会自动生成。

## 项目结构

```
myapp/
  app.json          应用配置（包名、版本、权限、入口等）
  icon.png          可选，图标（PNG/JPG，建议 512x512）
  www/              前端页面
  lua/              Lua 逻辑，默认入口 main.lua
  dev/mock.js       可选，berba dev 的桥 mock（不进入 APK）
  AGENTS.md         AI Agent 开发指南（能力边界与 API）
  .berba/          构建中间文件与签名密钥（不要提交）
  dist/             构建产物
```

`app.json` 完整字段见 [docs/config.md](docs/config.md)，Lua API 见 [docs/lua-api.md](docs/lua-api.md)。配置使用严格解析，未知字段（包括拼写错误）会导致构建失败。

## 发行包结构

解压后目录内容：

```
berba.exe
payload/classes.dex     Android shell（含 LuaJ）
tools/aapt2.exe
tools/android.jar       platform 35
tools/versions.json     版本与 aapt2/android.jar/dex 的 sha256
docs/
NOTICE
README.md
```

`tools/` 与 `payload/` 必须与 exe 同级；`doctor` 会检查文件是否存在、aapt2/android.jar/dex 哈希是否匹配、payload 是否齐全、aapt2 能否执行。

## 工作方式

1. 校验 `app.json`，渲染文本 `AndroidManifest.xml`，生成多密度图标与 `strings.xml`
2. 调用 aapt2 编译资源并 link 出 `base.apk`（每次构建重新分配资源 ID）
3. 自研 ZIP writer 合并 `base.apk` 与 shell dex、`assets/www`、`assets/lua`、`assets/app_config.json`，并做对齐（`resources.arsc` 4 字节、`lib/**/*.so` 16KB）
4. 纯 Go 实现 APK Signature Scheme v2 签名
5. 在临时文件上完成签名、包名与版本校验后再原子替换 `dist/` 产物，最后输出 `.sha256`

详细流程与故障排查见 [docs/build.md](docs/build.md)，安全模型见 [docs/security.md](docs/security.md)，发布前人工验收清单见 [docs/testing.md](docs/testing.md)。

## 从源码构建

需要 Go 1.24+。若要生成发行包，还需要 JDK 17+ 与 Android SDK（platform 35 + build-tools）：

```powershell
go test ./...
scripts\package-dist.ps1 -Version 0.1.0 -Sdk C:\Android\Sdk
```

Linux/macOS 使用 shell 版本（只能打包当前平台）：

```bash
VERSION=0.1.0 ANDROID_SDK_ROOT=/path/to/sdk scripts/package-dist.sh
```

`scripts\build-shell.ps1` 单独编译 Android shell：

```powershell
scripts\build-shell.ps1 -Sdk C:\Android\Sdk -Out payload
```

它使用 `javac` + `jar` + `d8`，不依赖 Gradle/AGP。LuaJ 固定为 3.0.1，放在 `.tools\luaj\luaj-jse-3.0.1.jar`（或设置 `LUAJ_JAR`）。

## 限制

- 仅支持 ARM 原生库占位（当前 shell 无 `.so`），不支持原生插件热插拔
- Lua 为 LuaJ 3.0.1（Lua 5.2 语义、无 JIT），定位为胶水逻辑
- 不提供 Google Play/AAB、v1/v3 签名、iOS
- 构建机需能运行 `tools/aapt2.exe`；发行包不包含 adb
