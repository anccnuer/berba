# 测试与验收

berba 的 CI 无法连接真机，因此采用**自动化产物校验 + 人工 smoke 清单**两层策略。自动化部分由 `scripts/` 与 GitHub Actions 覆盖（见文末），本文聚焦发布前必须在真机/模拟器上手动走查的清单。

## 何时需要跑人工清单

- 重打 `payload/classes.dex`（壳有任何改动）后，发布前必须整份走查。
- 仅改 Go（`internal/*`）时，至少走查 1（安装启动）、2（桥往返）、6（构建可复现）。
- 修改 `JsBridge`/`MainActivity`/`LuaApis`/`AssetServer` 任一鉴权或网络路径时，3、8、9 为阻断项。
- 修改 `NotifyController`/`NotifyModule`/`NotificationToken`/Manifest 生成时，10 为阻断项。
- 修改 `TaskModule`/`TaskScheduler`/`TaskStore`/`TaskSchedule`/`TaskJobService`/`TaskAlarmReceiver`/`TaskBootReceiver` 或任务组件模板时，11 为阻断项。
- 修改 `NativePluginBridge`/`NativePluginLoader`/`NativeModule`、`shell/jni/*`、`internal/nativeplugin/*` 或 Manifest 的 `extractNativeLibs` 时，12 为阻断项。

## 人工 smoke 清单

在**最低支持版本（API 24）**与**当前目标版本（API 35）**各至少一台设备/模拟器上执行。逐条打勾，任一失败视为发布阻断。

### 1. 安装与启动
- [ ] 全新安装（无历史密钥）：`berba build` 自动生成 `.berba/keys/release.pem` 并打印指纹。
- [ ] `berba install` 覆盖升级成功（同密钥、`versionCode` 递增）。
- [ ] 冷启动无白屏：本地启动页在 2s 内渲染；远程启动页显示加载态而非直接报错。
- [ ] 启动图之后进入页面，状态栏颜色/图标明暗符合 `theme` 与 `ui.statusBar` 设置。

### 2. 桥（JS ↔ 原生）往返
- [ ] 页面调用 `berba.call('device.info')` 等已授权方法，Promise 正常 resolve。
- [ ] 未知方法名返回结构化错误（非静默超时）。
- [ ] `berba.on(...)` 事件在原生触发时送达；`off` 后不再送达。
- [ ] 连续快速发起多次调用，响应与请求一一对应（reqId 不串）。

### 3. 返回键与生命周期
- [ ] 单击返回：触发 `app.on('back')`；页面 `preventDefault` 时不退出。
- [ ] 连续快速返回不排队卡死（去抖生效），最终正常退出。
- [ ] Home 再回来：`pause`→`resume` 顺序正确，WebView 状态不丢。
- [ ] 旋转 5 次以上：入口只执行一次（日志可证），无重复 `bridge.register`/事件处理器，无 Activity 泄漏；旧界面的回调不再执行。
- [ ] 触发 `destroy` 后 Lua 运行时仍存活；在途 `http` 不因 Activity 销毁被取消，回调仍能执行（`app.info().foreground` 为 false）。
- [ ] `app.exit` 只结束当前界面；后台任务继续，重新深链拉起时入口不重复执行。
- [ ] 「最近任务」划掉或 `am kill` 后重新启动：入口重新执行一次，`boot`/`create` 各一次。

### 4. 权限
- [ ] 在 `app.json` 声明 `camera`，首次使用弹出系统授权。
- [ ] 拒绝授权后：桥方法返回“权限被拒”，不崩溃，可再次申请。
- [ ] 未声明的权限不会被运行时申请。

### 5. 文件选择器 / 下载
- [ ] `<input type=file>` 触发 SAF 选择器，选中文件后 `files` 回调有值。
- [ ] 页面触发下载（`DownloadListener`）写入外部下载目录并提示路径。

### 6. 构建可复现（可在 CI 自动执行）
- [ ] 同一输入连续 `berba build` 两次，APK 的 SHA-256 完全一致。
- [ ] `berba doctor` 全部通过，含 aapt2/android.jar/dex 哈希一致。

### 7. 深链
- [ ] 配置 `deepLinks: ["myapp://open"]`，`adb shell am start -W -a android.intent.action.VIEW -d "myapp://open"` 拉起应用。
- [ ] 已在前台时再次深链进入 `app.on('newIntent')`，URL 正确透传。
- [ ] 恶意/超长深链内容不会绕过页面自身校验（壳只负责送达，不解释语义）。

### 8. 远程页面 origin 白名单
- [ ] 远程 `startUrl` 的 origin 自动加入白名单，桥可用。
- [ ] 页面跳转到**非白名单** origin：加载被拒并给出明确错误，桥不可用（不能靠页面自报 origin 绕过）。
- [ ] `web.allowedOrigins` 中额外 origin 可正常调用桥。

### 9. Lua HTTP 网络策略
- [ ] 默认配置下访问 `http://127.0.0.1` / 局域网地址被拒（`NetworkPolicy`）。
- [ ] 显式 `http.allowPrivateNetwork: true` 后放行。
- [ ] 重定向逐跳复检：302 跳到私网/非白名单协议时中止。
- [ ] `http.cancel` 能中断在途请求并返回“已取消”；请求尚未注册连接时取消也不丢失。
- [ ] Activity 销毁（旋转/`app.exit`）不会取消在途请求；仅进程结束或测试用 `shutdownForTests` 才统一清理。

### 10. 本地通知（`notify` 能力）
- [ ] `app.json` 写 `"capabilities": ["notify"]` 后构建成功；`aapt2 dump badging` 含 `POST_NOTIFICATIONS`，Manifest 为 `launchMode="singleTask"`，APK 内含 `drawable-*/ic_stat_berba.png`。
- [ ] API 33+ 首次 `permission.request("POST_NOTIFICATIONS")` 弹出系统授权；拒绝后 `notify.enabled()` 为 `false`，`notify.show` 返回 `nil, "通知权限未授予"`。
- [ ] `notify.channel` 创建通道；重复调用只更新名称/描述，重要性保持不变（系统设置中可见）。
- [ ] 字符串 id 重复 `notify.show` 是替换；数字 id 与自增 id 生成独立通知；`notify.cancel`/`notify.cancelAll` 立即移除。
- [ ] 热启动点击通知：`app.on("notification")` 收到 `{ id, data, coldStart=false }`，且只触发一次。
- [ ] 冷启动（先 `am force-stop`）点击通知：`create` 先执行，随后收到 `coldStart=true` 的 `notification`。
- [ ] `autoCancel=false` 的通知可重复点击并重复触发；`ongoing=true` 不能被用户划掉。
- [ ] 伪造/篡改的点击 Intent（错误 token、action、缺少 payload、超过 64KB）不会触发 Lua 事件，Logcat 有丢弃记录。
- [ ] 通知标题/正文超长、channel id 非法、`data` 序列化后超 64KB 时返回明确错误，不崩溃。

### 11. 后台定时任务（`task` 能力）
- [ ] `app.json` 写 `"capabilities": ["task"]` 后构建成功；`aapt2 dump xmltree` 中 `TaskJobService` 为 `exported="true"` 且带 `BIND_JOB_SERVICE`，两个 Receiver 为 `exported="false"`；权限含 `RECEIVE_BOOT_COMPLETED`/`SCHEDULE_EXACT_ALARM`；APK 内含声明了 `task` 的能力资产。
- [ ] `delay=60` 的任务到点触发一次 `app.on("task")`，`ctx.name/data/runId` 正确；`every=900` 的任务触发后自动重排下一次（`task.list()` 的 `nextRun` 前进）。
- [ ] `daily="08:30"` 的任务在临近时刻触发，且设备时区/夏令时切换后仍按本地 08:30 计算。
- [ ] `ctx.done()` 前发起异步 `http.request`，回调内调用 `ctx.done()`；Job 保持存活直到回调完成（日志中无提前结束）。
- [ ] 不调用 `ctx.done()`：达到 `timeout`（默认 60 秒）后任务被结束并记录超时，不永久占用 Job。
- [ ] 重复调用 `ctx.done()` 安全（只结束一次，无异常）。
- [ ] 运行中 `task.cancel(name)`：Job 被停止、`onStopJob` 返回 `false`，之后不再触发。
- [ ] 同时存在多个任务（至少 3 个不同 `name`，含 `replace=false` 重名报错）时各自独立触发，互不串 `data`/`runId`。
- [ ] 重启设备或模拟器冷启动后任务自动重排；过期的 `every/daily` 任务补触发一次且 `ctx.late=true`，不重放积压。
- [ ] 在系统设置中修改日期/时间/时区后 `daily` 任务按新本地时间重排；已过期的 `daily` 任务补触发一次并标记 `late`。
- [ ] 撤销/授予精确闹钟权限（API 31+，`adb shell appops set <pkg> SCHEDULE_EXACT_ALARM deny/allow`）后任务仍可重排；`exact=true` 在无权限时降级为不精确并记录警告，不抛异常。
- [ ] Doze：`adb shell dumpsys deviceidle force-idle` 后任务在允许窗口内仍能触发或明确延后，不崩溃；文档所述 best-effort 语义与实测一致。
- [ ] `am force-stop` 后闹钟被系统清除，应用不再被唤醒（符合文档预期）；重新打开应用后 `task.list()` 仍显示任务定义并可再次调度。
- [ ] API 24/25 设备上可正常调度与触发（壳未使用 `java.time` 等 API 26+ 类型）。

### 12. 原生插件（`native` 能力）
- [ ] 无插件项目构建的 APK 不含 `lib/**`；启用 `native` + `native.plugins` 后 APK 含 `lib/<abi>/libberba_jni.so` 与 `libberba_<name>.so`，Manifest 的 `extractNativeLibs="true"`，能力资产 `native.plugins` 与 `spec.json` 一致。
- [ ] `zipalign -c -P 16 -v 4` 通过；`readelf -lW` 显示 64 位 `PT_LOAD` 对齐 ≥ `0x4000`、32 位 ≥ `0x1000`；`readelf -d` 的 `NEEDED` 仅含白名单库。
- [ ] arm64-v8a、armeabi-v7a、x86_64 设备/模拟器上分别加载成功；`native.list()` 的 `ok=true`。
- [ ] 同步函数返回 JSON 正确映射为 Lua table/字符串/数字；`nil` 与非法参数返回 `nil, err`，不崩溃。
- [ ] 异步函数回调在 Lua 线程执行；跨线程完成后结果与错误都能送达；重复/迟到完成不会触发两次回调。
- [ ] 异步函数 30 秒超时：回调收到超时错误，pending 清理，随后迟到完成被丢弃。
- [ ] 插件 `init` 返回非 0、库缺失、ABI 不匹配或缺少导出符号时，仅该插件函数抛错；其他插件与核心 API 正常。
- [ ] 同时发起超过 256 个异步调用时，第 257 个收到明确错误而不是崩溃或静默丢失。
- [ ] 1MB 参数与 8MB 结果边界：超限报错；64KB `config` 超限构建失败。
- [ ] 未在 `capabilities` 声明 `native` 时，浏览器 dev 里 `native` 表不存在且 `app.on("task")`/`app.on("notification")` 报未知事件；声明后 `native.list()` 返回 `app.json` 的插件与函数且 `ok=false`，插件函数抛“原生插件仅在设备上可用”。

## 自动化覆盖（无需真机）

| 检查 | 命令 / 位置 |
| --- | --- |
| Go 单测 + 竞态 | `go test -race ./...` |
| 纯 Java 策略类（OriginPolicy/NetworkPolicy/LuaSandbox/TaskSchedule 等） | `scripts/build-shell.sh` / `.ps1` 内 JVM 冒烟，输出 `LuaSandbox OK` / `NetworkPolicy OK` / `TaskSchedule OK` |
| 签名与对齐 | `apksigner verify --min-sdk-version 24`、`zipalign -c 4` |
| Manifest 断言 | `aapt2 dump badging` / `aapt2 dump xmltree` 校验包名、版本、权限、cleartext、深链 scheme、launchMode、notify 的 `singleTask`、task 的三个组件及其 `exported` |
| 规格与文档一致 | `go test ./internal/capability`：生成物逐字节一致；`docs/lua-api.md` 与 `templates/init/AGENTS.md` 覆盖规格里的每个模块函数、事件与全局函数，`docs/config.md` 点名每个能力 |
| 通知能力产物 | `go test ./internal/build ./internal/resources`：Manifest 权限、`ic_stat_berba.png` 各密度尺寸、能力资产包含 `notify` |
| 任务能力产物 | `go test ./internal/build`：Manifest 权限/组件、`shell.json` 支持 `task`、能力资产含 `task` 模块与 `app.task` 事件、未启用时不出现任务组件 |
| dev 能力元数据 | `go test ./internal/cli ./internal/devserver`：`/__berba/lua.json` 的 `capabilities`/`modules`/`events`/`native` 与 `app.json` 一致；未知能力与缺失 `spec.json` 直接报错 |
| 原生插件产物 | `go test ./internal/nativeplugin ./internal/build ./internal/release`：规格/路径/ABI 交集/ELF fixtures（架构、对齐、导出符号、依赖白名单）、无插件不含 `lib/`、缺 shim 构建失败、发行包含 `include/berba_plugin.h` 与 shim 哈希 |
| 原生 shim 构建 | CI 固定 NDK `26.1.10909125`，`scripts/build-shell.sh` 产出三 ABI `libberba_jni.so`，用 `llvm-readelf` 断言架构、仅导出 `JNI_OnLoad`、`PT_LOAD` 对齐与依赖白名单；`examples/native-echo/build.sh` 编译示例插件并打包成 APK（加载验证见手动设备清单） |
| 产物内容 | 解包断言 `assets/www`、`assets/lua`、`app_config.json` 与 `.sha256` 一致 |
| 可复现 | CI 连续构建两次比对 SHA-256 |
| 发行包完整性 | `berba doctor` 校验 aapt2/android.jar/dex 哈希 |

## 可选：设备农场

如需自动化真机验证，可将清单 1–5、7–11 接入 Firebase Test Lab 或等价服务，作为**非阻断**补充信号；发布阻断仍以本文的人工清单 + 上表自动化为准。
