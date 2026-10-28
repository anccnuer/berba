# 构建与发行

## 终端用户

发行包解压即用，不需要 JDK、Android SDK、Gradle 或 adb。

```bat
berba.exe doctor
berba.exe init myapp --package com.example.myapp
cd myapp
..\berba.exe build
```

产物：

```
dist/com.example.myapp-1.0.0.apk
dist/com.example.myapp-1.0.0.apk.sha256
```

`.sha256` 内容为 `<hash>  <文件名>`，可用 `sha256sum -c` / `certutil -hashfile` 校验。

常用参数：

| 参数 | 说明 |
| --- | --- |
| `--config <path>` | 指定 app.json |
| `--out <dir>` | 输出目录（默认 `<项目>/dist`） |
| `--debug` | 可调试构建（WebView 远程调试 + `android:debuggable="true"`，产物名追加 `-debug`） |
| `--home <dir>` | 发行包根目录（默认 exe 所在目录或 `BERBA_HOME`） |
| `--key <path>` | 指定签名私钥；路径不存在时报错，仅默认路径会自动生成 |

`--home` 只影响 `tools/` 与 `payload/` 的查找，不改变项目目录或输出目录。构建先在临时文件中验签、badging 自检，通过后才原子替换 `dist/` 下的正式产物；失败不会留下半成品。

`berba install` 接受位置参数或 `--apk <path>` 指定 APK，默认使用 `dist/` 下最新的 APK；设备可用 `--device <serial>` 指定。adb 查找顺序为 `--adb`、`BERBA_ADB`、PATH；找不到 adb 时只打印提示，不影响构建。

## 本地开发（dev / watch）

`berba dev` 用本地 HTTP 服务提供 `web.dir` 下的文件，无需构建 APK 即可在浏览器里调试页面：

```bat
berba dev --open
```

- 入口取自 `web.startUrl` 的路径，默认 `/index.html`；`web.spa` 为 true 时无扩展名路径回退到 `index.html`。
- 所有 HTML 响应会在 `<head>` 后注入 `/__berba.js`，提供与壳一致的 `window.berba.call/on/off`。桥调用由 `dev/mock.js` 中的 `window.__berbaMock` 处理：

  ```js
  // dev/mock.js
  window.__berbaMock = {
    add: (req) => ({ sum: (req.a || 0) + (req.b || 0) }),
    "device.model": () => "dev-browser",
  };
  window.__berbaEmit && window.__berbaEmit("tick", { at: Date.now() });
  ```

  未 mock 的方法会打印警告并 reject。mock 脚本不会进入 APK。
- 参数：`--port`（默认 5173，0 表示随机）、`--host`、`--open`、`--mock <path>`（默认 `<项目>/dev/mock.js`）。

`berba watch` 轮询项目文件（忽略 `.git`、`.berba`、`dist`、`node_modules` 等），稳定后自动重建：

```bat
berba watch --install
```

- 首次启动立即构建一次；构建失败只打印错误，继续监听，修复后自动重试。
- 参数：`--out`、`--debug`、`--key`、`--home`、`--interval`（默认 500ms）、`--install`、`--device`、`--adb`。
- `--install` 复用 `berba install` 的 adb 查找与 `install -r` 逻辑。

## 退出码

| 退出码 | 含义 |
| --- | --- |
| `0` | 成功（`help`、`version` 也是 0） |
| `1` | 参数/用法错误、未分类的 IO 错误、用户中断（Ctrl-C） |
| `2` | 配置错误：`app.json` 校验失败（包名、权限、入口、origin 等） |
| `3` | 工具链问题：aapt2/android.jar/dex 缺失或不可执行、adb 安装失败 |
| `4` | 构建过程失败：资源渲染、aapt2、打包、签名 |
| `5` | 校验失败：签名自检、包名/versionCode 自检不一致、`doctor` 检查未通过 |

失败原因写在 stderr，前缀为 `错误:`。CI 中可直接依据退出码区分“配置问题”与“工具链/构建问题”。`build`、`dev`、`watch`、`doctor`、`install` 均遵循该约定。

## 构建流水线

1. **校验配置**：读取 `app.json`，检查包名、版本、主题、方向、权限、入口文件、图标与 origin。
2. **定位工具链**：`tools/aapt2.exe`、`tools/android.jar`、`payload/classes*.dex`。
3. **渲染资源**：生成 `.berba/build/AndroidManifest.xml`、多密度 `mipmap-*/ic_launcher.png`、`values/strings.xml`。
4. **aapt2**：
   ```
   aapt2 compile --dir .berba/build/res -o .berba/build/compiled.zip
   aapt2 link -o .berba/build/base.apk -I tools/android.jar \
       --manifest .berba/build/AndroidManifest.xml .berba/build/compiled.zip
   ```
5. **组装**：读取 `base.apk` 全部条目，追加 `classes*.dex`、`lib/<abi>/*.so`、`assets/www/*`、`assets/lua/*`、`assets/app_config.json`，用自研 writer 重写 ZIP。
6. **签名**：加载或生成 `.berba/keys/release.pem`，写入 APK Signature Scheme v2 签名块。
7. **自检**：本地验签 + `aapt2 dump badging` 校验包名与 versionCode，输出 APK 与哈希。

对齐规则：`resources.arsc` 与所有 STORED 条目 4 字节对齐；`lib/**/*.so` 16KB 对齐。仅支持 32 位 ZIP（APK < 4GB）。

## 签名密钥

- 位置：`<项目>/.berba/keys/release.pem`，权限 `0600`，`.gitignore` 已忽略。
- 内容：RSA-2048 私钥 + 自签证书（CN/组织 `berba`，有效期 30 年）。
- 首次 `build` 自动生成并打印 SHA-256 指纹；也可提前运行 `berba keygen`。
- **密钥即应用身份**：丢失后无法覆盖安装升级，只能卸载重装。请离线备份。
- 更换密钥需要同步提高 `versionCode` 并卸载旧版，否则设备会拒绝安装。

## 组装发行包（开发者）

需要 JDK 17+、Android SDK（`platforms;android-35`、`build-tools`）和 Go 1.24+。

```powershell
# LuaJ 放到默认位置（或不放，用 -Luaj 指定）
# .tools\luaj\luaj-jse-3.0.1.jar

scripts\package-dist.ps1 -Version 0.1.0 -Sdk C:\Android\Sdk
```

Linux/macOS：

```bash
VERSION=0.1.0 ANDROID_SDK_ROOT=/path/to/sdk scripts/package-dist.sh
```

shell 版本只打包当前平台（aapt2 必须与目标平台一致），产物命名 `berba-<版本>-<os>-<arch>.zip`。

脚本执行：

1. `go build -trimpath -ldflags "-s -w -X main.version=<版本>"` → `berba.exe`
2. 编译 shell（`build-shell.ps1`：javac → jar → d8，min-api 24，合并 LuaJ）→ `payload/classes.dex`
3. 复制 `aapt2.exe`、`android.jar`
4. 调用 `berba release` 完成剩余工作

`berba release` 是唯一的打包实现：

```bash
berba release --home dist/windows-amd64 --out dist --version 0.1.0 --docs .
```

它校验发行目录，读取 aapt2 版本，写入 `tools/versions.json`（版本 + aapt2/android.jar/dex 的 SHA-256），复制 `README.md`、`NOTICE`、`docs/`，然后按固定顺序与固定时间戳生成 zip，最后写出 `<zip>.sha256`。相同输入重复打包得到相同哈希，便于发布核对。`berba doctor` 会重新计算这些哈希并逐一比对，因此 shell dex 被篡改或版本漂移会立即被发现。

单独编译 shell：

```powershell
scripts\build-shell.ps1 -Sdk C:\Android\Sdk -Out payload
```

```bash
ANDROID_SDK_ROOT=/path/to/sdk scripts/build-shell.sh payload
```

`build-shell.sh` 会自动选择最新的 build-tools；可用 `PLATFORM`、`BUILD_TOOLS`、`LUAJ_JAR`、`OUT`、`WORK` 覆盖。源码在 `shell/app/src/main/java/`，仅依赖 Android 框架、`org.json`（系统内置）与 LuaJ。

Android SDK 二进制受 SDK 许可约束，**不要提交到 git**；发行 zip 在本地或 CI 组装，并随包附带 `NOTICE`。

## 验证

构建后建议执行：

```bash
# 包名、版本、权限、minSdk
aapt2 dump badging dist/app.apk

# 条目与对齐
unzip -l dist/app.apk
zipalign -c -v 4 dist/app.apk

# v2 签名与证书
apksigner verify --print-certs dist/app.apk

# 安装
adb install -r dist/app.apk
```

`berba build` 已内置 v2 验签与 badging 自检，以上命令用于交叉验证。

### 真机矩阵

发布前的完整人工 smoke 清单（安装/启动、桥往返、返回键与生命周期、权限、文件选择与下载、深链、origin 白名单、Lua HTTP 网络策略、可复现构建）见 [testing.md](testing.md)。建议至少覆盖 API 24 / 30 / 35 各一台设备。

## 调试

- `berba build --debug` 或 `"debug": true`：开启 `android:debuggable` 与 WebView 调试。用 Chrome 打开 `chrome://inspect` 调试页面。
- Lua 输出：`log(...)` 与未捕获错误均写入 Logcat，tag 为 `berba`：
  ```
  adb logcat -s berba
  ```
- 构建中间文件在 `.berba/build/`：`AndroidManifest.xml`、`base.apk`、`app-unsigned.apk`。签名失败时先检查未签名 APK。

## 故障排查

| 现象 | 处理 |
| --- | --- |
| `缺少 shell dex` / `缺少文件: tools/...` | 使用完整发行包，或用 `--home` 指向正确目录；运行 `berba doctor` |
| `doctor` 报哈希不匹配 | `tools/` 被修改或混用了版本，重新解压发行包 |
| `aapt2` 无法运行 | Windows 检查杀软隔离；Linux/mac 检查可执行权限 |
| `resource ... not found` | Manifest 引用了不存在的资源；本工具只生成 `app_name` 与 `ic_launcher`，不要手改 Manifest |
| 安装报 `NO_CERTIFICATES` | 仅支持 Android 7.0+（v2 签名），确认设备 API ≥ 24 |
| 安装报 `INSTALL_FAILED_UPDATE_INCOMPATIBLE` | 签名或包名与已安装版本不同，卸载旧版或恢复原密钥 |
| 页面白屏 | 检查 `web.startUrl` 指向的文件是否存在；远程 origin 是否在白名单；`adb logcat -s berba` |
| JS 调用桥无响应 | 当前页面 origin 不在白名单（未授权调用会被静默拒绝） |
| Windows SmartScreen 提示 | 未做代码签名，属正常提示；后续可对 exe 做 Authenticode 签名 |
