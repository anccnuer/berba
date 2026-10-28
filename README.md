# guiapp

把前端页面和 Lua 逻辑打包成可安装的 Android APK。终端用户无需安装 JDK、Android SDK 或 Android Studio：解压发行包，运行 `guiapp build` 即可。

- WebView 承载 `www/` 下的页面（本地虚拟域名或远程 URL）
- Lua 沙箱提供应用生命周期、网络、文件、存储、权限等白名单 API
- 页面与 Lua 通过 Promise 桥双向调用
- 纯 Go 实现 ZIP 对齐与 APK Signature Scheme v2 签名
- 支持 Android 7.0+（minSdk 24），面向侧载/企业分发

## 快速开始

1. 解压发行包，确认工具链完整：

   ```bat
   guiapp.exe doctor
   ```

2. 创建项目并构建：

   ```bat
   guiapp.exe init myapp --package com.example.myapp
   cd myapp
   ..\guiapp.exe build
   ```

3. 产物在 `myapp\dist\com.example.myapp-1.0.0.apk`。拷贝到手机安装，或：

   ```bat
   ..\guiapp.exe install
   ```

`build` 首次运行会在 `.guiapp\keys\release.pem` 生成签名密钥。**请离线备份**：它是应用身份，丢失后无法覆盖升级。

## 示例

- `examples/hello`：JS↔Lua 桥、设备信息、本地计数
- `examples/led-banner`：演唱会应援横幅，竖屏设置文字/字体/大小/颜色，横屏全屏无缝滚动，设置自动保存

```bat
cd examples\led-banner
..\..\dist\windows-amd64\guiapp.exe build
```

## 命令

| 命令 | 说明 |
| --- | --- |
| `guiapp init [目录]` | 创建脚手架，支持 `--name`、`--package`、`--force` |
| `guiapp build [目录]` | 构建 APK，支持 `--debug`、`--out`、`--home`、`--config` |
| `guiapp keygen` | 生成签名密钥，支持 `--out`、`--force` |
| `guiapp doctor` | 校验工具链、payload 与文件哈希 |
| `guiapp install [apk]` | 通过 adb 安装，支持 `--device`、`--adb` |
| `guiapp version` | 显示版本 |

发行包根目录默认是 exe 所在目录，也可用 `--home <目录>` 或环境变量 `GUIAPP_HOME` 覆盖。

## 项目结构

```
myapp/
  app.json          应用配置（包名、版本、权限、入口等）
  icon.png          可选，图标（PNG/JPG，建议 512x512）
  www/              前端页面
  lua/              Lua 逻辑，默认入口 main.lua
  .guiapp/          构建中间文件与签名密钥（不要提交）
  dist/             构建产物
```

`app.json` 完整字段见 [docs/config.md](docs/config.md)，Lua API 见 [docs/lua-api.md](docs/lua-api.md)。

## 发行包结构

```
guiapp-<ver>-windows-amd64/
  guiapp.exe
  payload/classes.dex     Android shell（含 LuaJ + org.json）
  tools/aapt2.exe
  tools/android.jar       platform 35
  tools/versions.json     版本与 sha256
  docs/
  NOTICE
  README.md
```

`tools/` 与 `payload/` 必须与 exe 同级；`doctor` 会检查文件是否存在、哈希是否匹配、aapt2 能否执行。

## 工作方式

1. 校验 `app.json`，渲染文本 `AndroidManifest.xml`，生成多密度图标与 `strings.xml`
2. 调用 aapt2 编译资源并 link 出 `base.apk`（每次构建重新分配资源 ID）
3. 自研 ZIP writer 合并 `base.apk` 与 shell dex、`assets/www`、`assets/lua`、`assets/app_config.json`，并做对齐（`resources.arsc` 4 字节、`lib/**/*.so` 16KB）
4. 纯 Go 实现 APK Signature Scheme v2 签名
5. 用 `aapt2 dump badging` 自检包名/版本后输出 APK 与 `.sha256`

详细流程与故障排查见 [docs/build.md](docs/build.md)，安全模型见 [docs/security.md](docs/security.md)。

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
