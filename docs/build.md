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
| `--debug` | 可调试构建（WebView 远程调试 + `android:debuggable="true"`） |
| `--home <dir>` | 发行包根目录（默认 exe 所在目录或 `BERBA_HOME`） |

`berba install` 会优先使用 `--adb`、`BERBA_ADB`、PATH 中的 adb；找不到 adb 时只打印提示，不影响构建。

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

shell 版本只打包当前平台（aapt2 必须与目标平台一致），产物命名 `berba-<版本>-<os>-<arch>.zip`；生成 zip 需要 `zip` 或 Python。

脚本执行：

1. `go build -trimpath -ldflags "-s -w -X main.version=<版本>"` → `berba.exe`
2. 编译 shell（`build-shell.ps1`：javac → jar → d8，min-api 24，合并 LuaJ）→ `payload/classes.dex`
3. 复制 `aapt2.exe`、`android.jar`，生成 `tools/versions.json`（版本 + SHA-256）
4. 附带 `NOTICE`、`README.md`、`docs/`
5. 输出 `dist/windows-amd64/` 与 `dist/berba-<版本>-windows-amd64.zip`

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

建议至少覆盖 API 24 / 30 / 35：

- 安装、启动、返回键、横竖屏切换
- 本地页面加载、`fetch/XHR`、SPA 路由
- `berba.call` → Lua → Promise 回传；`pageFinished` 事件
- 权限申请与拒绝后再次申请
- `http`、`file`、`store`、`ui` 冒烟
- 远程 URL + origin 白名单；非白名单 origin 调用桥应被拒绝

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
