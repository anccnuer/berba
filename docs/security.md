# 安全模型

## 信任边界

- **可信**：应用开发者、项目内的 `app.json`、`www/`、`lua/`、原生插件源码与共享库、构建机。
- **不可信**：远程页面及其加载的第三方脚本、网络响应、用户输入。
- **沙箱目标**：即使页面被注入恶意脚本，也只能使用已授权的桥方法；Lua 代码无法逃逸到任意 Java/文件系统/进程能力。

## Lua 沙箱

只加载以下库：

```
base, table, string, math, coroutine, bit32
```

明确移除：

- `io`、`os`、`package`、`debug`、`utf8`
- `luajava`（无法反射 Java 类）
- `load`、`dofile`、`loadfile`（只能通过受控 `require` 读取 `assets/lua`）

`require` 只接受模块名，映射到 `assets/lua/<name>.lua` 或 `<name>/init.lua`，不解析 `..` 或绝对路径，运行时不再提供任何把字符串编译为函数的入口。

所有 Lua 回调由运行时统一捕获异常并写 Logcat，单个回调出错不会拖垮应用。当前没有指令数/内存配额；不要执行不受信任的 Lua 代码。

## 原生插件

- 只有 `app.json` 同时声明 `capabilities: ["native"]` 与 `native.plugins` 时才会打包并加载原生库；无插件构建不含任何 `.so`。
- 插件是**受信任代码**，与壳同进程运行，绕过 Lua 沙箱，可访问应用的全部 Android 权限、文件与网络；不做崩溃隔离，插件崩溃会带崩应用。
- 构建期校验：`ET_DYN`、ELF 架构与 ABI 匹配、`PT_LOAD` 对齐（64 位 ≥16KB，32 位 ≥4KB）、必需导出符号、`DT_NEEDED` 只允许 `libc.so/libm.so/libdl.so/liblog.so`；C++ 必须静态链接 STL。
- 插件只从 `nativeLibraryDir` 的绝对路径 `dlopen`，JNI shim 只导出 `JNI_OnLoad`；加载失败不会 `dlclose`，避免卸载运行中的完成线程代码。
- 参数/结果/配置上限分别为 1MB/8MB/64KB；异步调用并发上限 256、超时 30 秒。超时、迟到或重复的完成会被丢弃。
- `config` 与插件库一起进入 APK，可被任何人解包读取；**不要写入密钥**。插件 `describe` 与构建元数据不一致只告警，以构建元数据为准。
- 加载失败的插件会降级为抛错桩，不影响其他插件；错误信息写入 Logcat（tag `berba` / `berba-plugin`）。
- NDK 只是构建工具：缺少 shim 时启用原生插件的构建直接失败，不会静默降级。

## 文件与存储

- `file.*` 的根目录固定为应用私有 `getFilesDir()`。
- 路径先做规范化（`getCanonicalFile`），再校验仍位于根目录内；绝对路径、`..` 越界、NUL 被拒绝。
- 单文件读写上限均为 8MB；`write` 先写同目录临时文件再替换，失败会清理临时文件。
- `store.*` 使用应用私有的 `SharedPreferences`，值以 JSON 保存；key 长度 ≤ 256。

应用没有声明 `FileProvider`/`ContentProvider`，不向其他应用暴露文件。

## WebView

- JavaScript 与 DOM Storage 开启；文件访问、Content 访问、跨文件 URL 访问全部关闭。
- Mixed content 固定为 `NEVER_ALLOW`；不支持多窗口与 JS 自动弹窗。
- 导航白名单：仅允许 `http/https` 且 origin 在白名单内；`http://` 还要求 `cleartext: true`。`web.load` 在调用线程同步校验，未授权时直接抛错而不是先加载再停止。其他 scheme（`intent://`、`file://`、`javascript:` 等）与未授权 origin 一律阻止。
- `alert` / `confirm` / `prompt` 使用原生对话框；网页 console 写入 Logcat（tag `berba`，前缀 `[js]`）。
- release 构建不开启 WebView 远程调试；`--debug` 才开启。
- 本地页面通过 `https://appassets.androidhost/*` 虚拟域名提供，`shouldInterceptRequest` 只处理 GET/HEAD，路径拒绝 `..`、反斜杠、NUL，并限制在 `assets/www` 内。

## JS ↔ Lua 桥

- 桥对象名 `__berbaNative`，方法签名 `dispatch(reqId, token, name, argsJson)`。
- **每次启动**用 `SecureRandom` 生成 128 位 token，只保存在页面 shim 的闭包中，不挂到 `window.berba`；token 比较使用 `MessageDigest.isEqual`（常量时间）。
- **origin 由原生侧记录**：`onPageStarted` 时按真实导航 URL 写入，桥调用不再信任页面传入的 `location.origin`。
- **请求 ID 带文档随机前缀**：导航后旧页面迟到的结果不会误 resolve 新页面的 Promise。
- **origin 白名单**：使用本地启动页时本地 origin 自动允许；远程启动页的 origin 自动加入，其他远程 origin 必须写入 `web.allowedOrigins`。未授权调用直接丢弃，不返回错误细节。
- 参数上限 1MB；桥方法名长度上限 128（注册时校验）。
- 结果通过 `evaluateJavascript` 回传，不拼接未经转义的字符串（使用 `JSONObject.quote`）。

风险提示：

- 页面内 XSS 等价于获得该页面可用的桥能力。请在页面设置 CSP、避免 `eval`/`innerHTML` 注入不可信内容。
- 白名单按 **origin** 判定，同源下的任意路径都能调用桥；不要允许不受控的共享域名。
- 远程页面模式会扩大攻击面，能用本地资源时优先用本地资源。
- `device.clipboard(text)`、`device.share`、`device.openUrl` 会突破页面沙箱与导航白名单（仅限 http/https），授权远程页面调用前请评估。`device.vibrate` 需要 `VIBRATE` 权限。

## 网络

- 默认 `android:usesCleartextTraffic="false"`；远程 `http://` 启动页在构建时直接报错，除非显式 `"cleartext": true`。
- 明文会暴露内容与凭据，只应在受控内网使用。
- Lua `http` 仅支持 `http/https`，15 秒连接/读取超时，响应上限 8MB；默认拒绝 loopback、私网、链路本地与 CGNAT 地址，重定向逐跳复检，最多 5 跳。需要访问本机/局域网时显式设置 `http.allowPrivateNetwork`。
- 不提供自定义证书固定（后续可扩展）。

## 权限

- 构建时只写入 `app.json` 声明的权限，并自动补充 `INTERNET`。
- 危险权限仍需运行时申请：`permission.check` / `permission.request`。
- 最小化权限；`SYSTEM_ALERT_WINDOW`、`ACCESS_BACKGROUND_LOCATION` 等敏感权限需要应用商店/系统额外审核或用户手动授权。
- 启用 `notify` 能力会自动加入 `POST_NOTIFICATIONS`，但 API 33+ 仍由用户在运行时授权；未授权时 `notify.show` 失败而不是静默。

## 通知

- 通知只能由应用自身在运行时显示；没有推送服务。后台唤醒入口仅限 `task` 能力注册的闹钟与 Job 组件。
- 点击 Intent 是显式组件（`MainActivity`）且 `PendingIntent` 使用 `FLAG_IMMUTABLE`；data URI 带随机 UUID，避免不同通知互相覆盖。
- 每个安装生成 128 位随机点击 token（`SecureRandom`，存于独立 `SharedPreferences`，不进入 Lua `store`），payload 与 token 都必须匹配且 action 正确，否则丢弃并记日志。
- payload 上限 64KB，解析前先清空 Intent extras，防止同一 Intent 被重复投递。
- 通知标题/正文/`data` 由应用自己写入；锁屏可能显示标题与正文，**不要放入密钥或敏感信息**。`data` 只随显式 Intent 返回给本应用，不是加密存储。

## 定时任务

- `task` 能力会注册 `TaskJobService`（`exported="true"` + `android:permission="android.permission.BIND_JOB_SERVICE"`，仅系统 JobScheduler 可绑定）以及 `TaskAlarmReceiver`、`TaskBootReceiver`（均 `exported="false"`）。
- 任务定义与 `data` 存放在应用私有 `SharedPreferences("berba_tasks")`，不是加密存储；不要放入密钥。触发时只把 `data` 交给本进程的 Lua handler。
- 闹钟 PendingIntent 使用显式组件与 `berba-task://<name>` data URI；任务名经过校验，空值/非法名无法排程。
- Event/Job 只调度任务本身，不执行外部代码；任务 handler 仍受 Lua 沙箱白名单约束。
- force-stop 会由系统取消闹钟；Doze、厂商省电策略与 JobScheduler 配额下触发为 best-effort，不构成安全边界。

## 签名与分发

- v2 签名覆盖 ZIP 条目、Central Directory 与 EOCD；构建结束会本地验签，并用 `aapt2 dump badging` 校验包名/版本。
- 自签证书只证明“同一密钥签发的更新”，不提供公开信任链；侧载/企业分发场景适用。
- 私钥文件权限 `0600`（Windows 上该权限位不生效，请依赖 NTFS ACL），位于 `.berba/keys/`，已被 `.gitignore` 忽略。泄露私钥等于允许他人发布可覆盖升级的更新。
- 发行包中的 `aapt2` 与 `android.jar` 来自 Android SDK，受 SDK 许可约束；不要提交到公开仓库，对外分发前确认许可与 NOTICE。

## 已知限制

- 不做 v1/v3 签名，Android 6 及以下无法安装。
- 没有 Lua 指令数/内存限制，死循环会占满 Lua 工作线程。
- WebView 版本由设备决定，旧设备可能缺少新特性。
- 未实现证书固定、代理配置、后台服务等高风险/重原生能力。文件选择器走系统 SAF，下载与 `device.openUrl` 交由外部应用处理，均不受 WebView 白名单约束。
- 原生插件不受 Lua 沙箱约束；只加载自己构建或审计过的库，不要从网络下载 `.so` 后动态替换。
- 通知只在 Lua 调用 `notify.show` 时显示；`task` 不会自动补发通知。进程被系统回收后，已排程的闹钟仍可在条件允许时唤醒进程执行任务，但通知本身不会自动重建。
