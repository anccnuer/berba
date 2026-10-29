# berba 能力扩展：单一规格驱动 + 通知 / 定时任务 / 原生插件

## Context

berba 目前的形态是「固定壳 + 白名单 Lua API + 纯 Go APK 装配」。核心链路质量很高（确定性 ZIP、v2 签名、严格配置、路径约束），但能力面被锁死在壳里：

- **无通知、无后台执行**：壳里没有 `NotificationManager` / `AlarmManager` / `Service` / `Receiver`；`LuaRuntime` 直接持有 `MainActivity`（`LuaRuntime.java:48-61`），Activity 销毁即 Lua 结束。提醒、同步、定时任务类应用无法实现。
- **WebView 高级能力未接**：`BridgeChromeClient` 只处理 console/alert/file chooser（`BridgeChromeClient.java:22-59`）；下载只是转交外部浏览器（`MainActivity.java:912-918`）。
- **无原生扩展通道**：`payload/lib` 只是占位；`luajava` 被沙箱移除，能力上限完全由壳决定。
- **能力面三处手写、无单一契约**：`LuaApis.java`（11 个模块）、浏览器端 `runtime.js`（1451 行镜像实现）、`docs/lua-api.md` + `templates/init/AGENTS.md` 各自维护；新增能力要改 5+ 处，dev/device 漂移已经存在。
- **Manifest 是硬编码模板**（`internal/manifest/render.go:42-85`），没有组件注册机制；`app.json` 也无法声明能力，只能靠 permissions 间接表达。

用户已确认的方向：

1. 首批补齐**通知 + 后台定时任务**，后台采用 **AlarmManager + JobScheduler**，**不做前台服务**。
2. 首批补齐**原生插件（JNI/.so）**，接受为壳引入 **NDK 编译的极小程序 JNI shim**。
3. 架构采用**单一声明式能力规格，驱动 Android 壳、浏览器 dev 运行时、Manifest/权限、文档与校验**。
4. 运行时改为**进程级单例**；`destroy` 表示界面解绑，`app.exit` 只结束界面，后台通知/任务继续。
5. 交付「架构 + 首批模块」，而不是只出分析报告。

核心优雅点：**能力是一等公民**。内置模块与原生插件走同一条注册路径；一份规格同时生成构建校验、Manifest、运行时元数据、Java 注册表和文档；UI 只是运行时可附着/解绑的会话，不再拥有 Lua 生命周期。

---

## 目标与非目标

### 目标

- 新增 `capabilities` / `native.plugins` 配置，构建期严格校验、自动补齐权限与 Manifest 组件。
- 落地 `notify`、`task` 两个内置能力，以及 `native` 插件能力。
- 全进程一个 Lua 实例；UI 可反复 attach/detach；后台任务与通知在无界面时可用。
- 生成并校验 `CapabilityRegistry.java`、`docs/capabilities.md`、`shell/shell.json`、运行时能力资产，防止三端漂移。
- 保持零第三方 Go 依赖、确定性构建、v2 签名、旧项目可构建。

### 非目标（首批不做）

- 前台服务、WorkManager、AndroidX、Kotlin、Room、推送/FCM、后台定位、应用保活。
- 通知 action 按钮、分组、自定义布局、全屏 Intent、通道创建后改重要性。
- 插件 dex/Java 层、插件自带 Activity/Service/Receiver/资源、远程下载插件、热卸载、传递依赖 `.so`、32 位 x86/RISC-V。
- `getUserMedia` / geolocation / 下载管理系统（属于另一条能力线，后续单独做）。
- 原生崩溃隔离；插件是受信任的项目自有代码。
- iOS、AAB、v1/v3 签名、多 Activity。

---

## 总体设计

### 1. 单一能力规格

新增 `internal/capability/`，内嵌 `specs/*.json`（`//go:embed`）。每个模块声明：

```json
{
  "schema": 1,
  "name": "notify",
  "kind": "capability",
  "summary": "本地通知",
  "requiresLua": true,
  "permissions": ["android.permission.POST_NOTIFICATIONS"],
  "components": [],
  "resources": ["notification_icon"],
  "installer": "NotifyModule",
  "modules": {
    "notify": {
      "browser": "partial",
      "functions": {
        "show":   {"async": false, "foreground": false, "doc": "显示或替换通知。"},
        "cancel": {"async": false, "foreground": false, "doc": "取消通知。"},
        "enabled":{"async": false, "foreground": false, "doc": "通知是否可用。"}
      }
    }
  },
  "events": {
    "notification": {"owner": "app", "foreground": true, "doc": "用户点击通知。"}
  }
}
```

约束：

- **不写任意 Manifest XML**。`components` 只允许 Go 内注册的命名模板（`task_job_service`、`task_alarm_receiver`、`task_boot_receiver`），未知名称构建失败。
- **不写任意资源路径**。`resources` 只允许命名生成器（`notification_icon`），由 Go 生成 PNG。
- 不做类型 IDL；`async` / `foreground` / `browser` 足够驱动 Lua 绑定、文档和 dev 桩。
- `kind: core` 的模块始终安装；`kind: capability` 需要在 `app.json.capabilities` 中显式启用。
- 规格加载严格解析（未知字段报错）、模块/函数名去重、名称模式校验。

### 2. 构建期产物

`internal/capability` 提供：

- `LoadBuiltin() (*Registry, error)`：加载内嵌规格。
- `Resolve(Request) (*Resolved, error)`：合并请求的内置能力 + 原生插件规格，产出模块、事件、权限、组件、资源、启用集合。
- `RuntimeDoc()`：生成精简的运行时元数据（不含 doc）。
- `RenderDocs()`：生成 `docs/capabilities.md`。
- `GenerateJavaRegistry()`：生成 `shell/app/src/main/java/com/berba/shell/CapabilityRegistry.java`。
- `GenerateShellManifest()`：生成 `shell/shell.json`。

生成文件全部**提交到仓库**，由 `//go:generate go run ./cmd/berbacap ...` 更新；Go 测试重新生成并逐字节比对，CI 用 `git diff --exit-code` 保证不过期。这样 `javac` 在没有 Go 的 IDE 里也能直接编译，且缺少 `NotifyModule` 等实现会直接编译失败。

### 3. 进程级运行时与 UiSession

`RuntimeManager`（进程单例，持有 application `Context`）拥有唯一的 `LuaRuntime`：

- `start` 幂等；入口脚本每进程只执行一次。
- 生命周期事件：
  - `boot`：入口执行后、首次任务/界面之前，**每进程一次**。
  - `create` / `pause` / `resume` / `destroy`：每次 UI attach/detach，保持现有 Activity 语义。
  - `back` / `newIntent`：仅 UI 存在时。
  - `notification`：通知点击，冷/热启动都补发。
  - `task`：JobService 无界面分发。
- `UiSession` 用 `WeakReference<MainActivity>`，绝不把 Activity 存进模块；`LuaRuntime.ui()` 可空，`requireUi()` 对前台专属 API 抛出中文错误。
- 生产环境**不主动 shutdown**；进程死亡是唯一结束。仅保留 `shutdownForTests()`。
- UI detach 时**不取消 HTTP**；`destroy` 只表示界面解绑。
- Bridge 回调捕获 `UiSession.epoch()`；页面销毁后的迟到回调直接丢弃，避免回调到已销毁的 WebView。

API 可用性矩阵：

| 无界面可用 | 需要前台 UI |
| --- | --- |
| `app.info/on`、`bridge.register`、`http`、`file`、`store`、`json`、`log` | `app.exit`、`web.*`、`ui.*` |
| `permission.check`、`device.model/sdk/platform/locale/screen/vibrate` | `permission.request`、`device.clipboard/share/openUrl/orientation` |
| `notify.*`、`task.*`、`native.*` | — |

`bridge.emit` 无界面时记日志并丢弃；`app.info()` 增加 `foreground` 字段。

### 4. 内置能力与原生插件同构

- 内置能力：`CapabilityRegistry.install(rt, g, cfg)` 调用手写安装器（`NotifyModule.install` / `TaskModule.install` / `NativeModule.install`）。
- 原生插件：构建期把插件 `spec.json` 的模块/函数合并进运行时能力资产；运行时由 `NativeModule` 通用地创建 `native.<module>` 表，无需为每个插件写 Java。
- 未启用的能力安装**抛错桩**（每个函数都抛「能力 X 未在 app.json 中启用」），而不是让 Lua 得到 `nil` 后产生晦涩错误。
- 设备端安装完成后做一次**函数存在性自检**：规格声明的每个函数必须存在于对应 Lua 表；缺失记 error（debug 构建可抛错）。dev 运行时做同样自检。

---

## 数据契约

### app.json 新增字段

```json
{
  "capabilities": ["notify", "task", "native"],
  "native": {
    "plugins": [
      { "name": "echo", "dir": "native/echo", "config": { "greeting": "hi" } }
    ]
  }
}
```

- `capabilities`：可选；默认空；去重排序；未知能力构建失败；声明后自动补齐该能力的权限与 Manifest 组件；自动权限在构建输出中打印。
- 要求 Lua 的能力在 `lua.dir` 不存在时报错（`requiresLua`）。
- `native.plugins`：
  - `name` 匹配 `^[a-z][a-z0-9_]{0,31}$`；保留名 `jni`（shim 文件名冲突）。
  - `dir` 默认 `native/<name>`，必须位于项目内且拒绝符号链接逃逸。
  - 约定目录：`<dir>/spec.json`、`<dir>/lib/<abi>/libberba_<name>.so`。
  - `config` 为内联 JSON，构建期规范化（紧凑、键排序），上限 64KB；文档明确它可从 APK 解包读取，禁止放密钥。
  - 有 `native.plugins` 但 `capabilities` 不含 `native` → 构建失败；含 `native` 但无插件 → 警告。
- 旧 `app.json` 完全兼容（新增字段可选）。

### 运行时能力资产 `assets/berba_capabilities.json`

每次构建必写（即使为空），旧壳忽略，新壳读取；缺失时按 core-only 处理。

```json
{
  "schema": 1,
  "capabilities": ["notify", "task"],
  "modules": {
    "notify": {"functions": {"show": {"async": false, "foreground": false}}},
    "task":   {"functions": {"schedule": {"async": false, "foreground": false}}}
  },
  "events": {
    "app": {"boot": {}, "create": {}, "notification": {"foreground": true}, "task": {}},
    "web": {"pageStarted": {}, "pageFinished": {}, "error": {}}
  },
  "native": {
    "abiVersion": 1,
    "plugins": [
      {"name": "echo", "libName": "libberba_echo.so",
       "modules": {"echo": {"functions": {"greet": {"async": false}}}}}
    ]
  }
}
```

不含 doc 字符串；列表/键排序保证确定性。

### `payload/shell.json`（能力协商）

```json
{
  "schema": 1,
  "capabilities": ["notify", "task", "native"],
  "nativeAbiVersion": 1
}
```

- 由规格生成、提交仓库、构建壳时复制到 `payload/shell.json`。
- `internal/payload.Locate` 可选读取；缺失 = core-only。
- 构建时若请求的能力不在 `shell.json` 中，**在工具链阶段失败**（退出码 3），避免产出缺类的 APK。
- `berba doctor` 打印支持的能力和原生 ABI；`release` 把 `shell.json` 与 shim 哈希写入 `versions.json`。

### 原生插件规格与 ABI

插件 `spec.json`：

```json
{
  "name": "echo",
  "abiVersion": 1,
  "version": "1.0.0",
  "modules": {
    "echo": {
      "functions": {
        "greet":       {"async": false},
        "greet_later": {"async": true}
      }
    }
  }
}
```

公共头 `shell/jni/berba_plugin.h`：

```c
#define BERBA_PLUGIN_ABI_VERSION 1

typedef struct berba_host {
    uint32_t abi_version;
    uint32_t struct_size;
    void *userdata;
    void (*complete)(void *userdata, const char *request_id, const char *result_json);
    void (*log)(void *userdata, int level, const char *message);
} berba_host;

int  berba_plugin_describe(char **out_json);
int  berba_plugin_init(const char *config_json, const berba_host *host);
const char *berba_plugin_call(const char *module, const char *method,
                              const char *args_json, const char *request_id);
void berba_plugin_free(const char *s);
void berba_plugin_shutdown(void);   /* 可选实现 */
```

约定：

- `describe` / `call` 返回的字符串由插件分配，宿主用 `berba_plugin_free` 释放；异步 `complete` 的字符串是借用的，shim 先复制。
- `call` 返回 `NULL` 表示异步，插件必须从任意线程调用 `host->complete` 恰好一次；非 `NULL` 表示同步完成。
- 结果统一信封：`{"ok":true,"value":...}` / `{"ok":false,"error":"..."}`。
- JSON 在 JNI 边界用 **UTF-8 `byte[]`**，不用 `GetStringUTFChars` / `NewStringUTF`，避免 modified UTF-8 与代理对问题。
- 同步调用在 Lua 线程执行（插件须快速返回）；异步调用走 IO 线程，完成回调 post 回 Lua 线程。
- Java 侧 30 秒超时、最多 256 个 pending、参数 1MB、结果 8MB；迟到/重复的完成直接丢弃。
- pending 记录必须在调用 `berba_plugin_call` **之前**插入，插件可能从另一线程立即完成。
- **不 `dlclose`**：调用 `berba_plugin_shutdown` 后故意泄漏句柄到进程结束，避免完成线程访问已卸载代码。
- 插件模块名不能与内置命名空间冲突（`app/web/bridge/http/file/store/permission/ui/device/json/notify/task/native/list`）；跨插件模块名唯一。
- 插件是受信任代码，拥有进程全部权限；不做崩溃隔离。

---

## 分阶段实施

> 每个阶段独立可构建、可测试；Stage 1-2 是后续所有能力的前置。

### Stage 1 — 能力规格与构建契约

**目标**：引入规格、配置字段、payload 协商、Manifest 组件模板、生成/校验机制；不新增运行时能力。

新增：

- `internal/capability/spec.go` — `Spec` / `Module` / `Function` / `Event` / `Registry` / `Resolved`。
- `internal/capability/load.go` — `//go:embed specs/*.json`，严格解析、去重、名称校验。
- `internal/capability/resolve.go` — 合并内置能力与插件规格；输出权限/组件/资源/事件。
- `internal/capability/docs.go` / `javagen.go` / `shellgen.go`。
- `internal/capability/cmd/berbacap/main.go` — `go generate` 入口。
- `internal/capability/specs/core.json` — 现有 11 个模块的函数、`log` 全局、核心事件（`boot/create/pause/resume/destroy/back/newIntent`、`web.on` 三事件）。
- 测试：`spec_test.go`、`resolve_test.go`、`docs_test.go`、`javagen_test.go`、`shellgen_test.go`。
- `shell/app/src/main/java/com/berba/shell/CapabilityRegistry.java`（生成并提交）。
- `docs/capabilities.md`（生成并提交）。
- `shell/shell.json`（生成并提交）。

修改：

- `internal/config/config.go`：增加 `Capabilities []string`、`Native NativeConfig`；校验名称/重复/目录/`native` 与 plugins 的对应关系；权限合并顺序为「用户权限 → 能力权限（排序）→ INTERNET」。
- `internal/manifest/render.go`：`Data` 增加 `Components []string`、`ExtractNativeLibs bool`；用 Go 命名模板渲染 service/receiver；`Validate` 改为 `encoding/xml` 解析；权限/组件确定性排序。
- `internal/manifest/render_test.go`：组件出现/缺失、XML 合法、确定性。
- `internal/build/build.go`：`config.Load` 后 `capability.Resolve`；`payload.Locate` 后按 `shell.json` 校验能力；`manifest.Render` 传入 resolved；`collectPayload` 写 `assets/berba_capabilities.json`。
- `internal/config/assetconfig.go`：不膨胀 `app_config.json`，能力资产独立。
- `internal/payload/payload.go`：增加 `Shell *ShellManifest`、`Supports(name)`。
- `internal/cli/doctor.go`：打印 payload 能力。
- `scripts/build-shell.sh/.ps1`、`scripts/package-dist.sh/.ps1`：复制 `payload/shell.json`。
- `go generate` 入口与 CI 校验。

**退出标准**：`go test ./...` 通过；`examples/hello` 产出的 APK 含空能力资产、无新增 Manifest 组件；旧配置不变；生成文件无 diff。

---

### Stage 2 — 进程级运行时重构

**目标**：运行时无界面可用、全进程单例；现有 UI 行为不回归。

新增：

- `shell/.../RuntimeManager.java`
- `shell/.../UiSession.java`
- `shell/.../CapabilityConfig.java` — 解析 `assets/berba_capabilities.json`；缺失/损坏时 core-only。

修改：

- `LuaRuntime.java`：
  - 去掉 `MainActivity` 字段；增加 `Context`、`CapabilityConfig`、`volatile UiSession`。
  - `start()` 幂等、入口仅一次；`boot`；`whenReady` 队列。
  - `attach(UiSession, Runnable afterCreate)` / `detach(UiSession)`；`ui()` 可空、`requireUi()` 抛中文错误。
  - `context()`；`postUi()` 无界面时丢弃并记日志。
  - `APP_EVENTS` 改为由 `CapabilityConfig` 驱动（core + 已启用能力事件）。
  - 移除生产 `shutdown()`；仅 `shutdownForTests()`；UI detach 不取消 HTTP。
  - `emitToJs` / bridge 回调按 `UiSession.epoch()` 丢弃过期结果。
- `LuaApis.java`：
  - 按矩阵替换 `rt.activity()`：`app.exit/web/ui/permission.request/device.clipboard/share/openUrl/orientation` 用 `requireUi()`；`file/store/http/permission.check/device 其余` 用 `context()`。
  - `app.info()` 增加 `foreground`。
  - `app.exit` 只结束当前界面。
  - `http.cancel` 先标记取消再断开，避免注册前取消丢失。
  - 安装入口改为 `CapabilityRegistry.install(rt, g, cfg)`。
- `MainActivity.java`：
  - `onCreate` 通过 `RuntimeManager.attach(this)` 获取 `UiSession`；`create` 完成后再加载 `startUrl`（保持现有「无 `web.load` 才自动加载」逻辑）。
  - `onDestroy` 只 detach + 销毁 WebView，不 shutdown Lua。
  - `onNewIntent`/深链/通知意图走当前 session。
  - `onLuaReady` 移除；初始化失败仅记日志，下次 attach 时 toast。
- `JsBridge.java` / `AssetServer.java` / `BridgeChromeClient.java`：改为持有 `UiSession`；页面 origin、桥回调、页面事件均按 session 校验。
- `LuaRuntime` 自检：安装后核对能力规格函数是否存在。

**退出标准**：现有 `hello` / `led-banner` 在旋转、退后台、`am kill`、深链、`app.exit` 下行为不回归；入口只执行一次；Activity 销毁后 HTTP 不被取消；旋转循环无 Activity 泄漏。

---

### Stage 3 — notify

**目标**：本地通知、通道、权限、点击事件、构建期图标生成。

新增：

- `internal/capability/specs/notify.json`
- `internal/resources/notification.go` — 生成 `drawable-{mdpi..xxxhdpi}/ic_stat_berba.png`（24/36/48/72/96，白色透明底简单图形）。
- `shell/.../NotifyModule.java` — Lua 表 `notify.show/cancel/cancelAll/enabled/channel`。
- `shell/.../NotifyController.java` — 通道、ID 映射、PendingIntent、权限检查。
- `shell/.../NotificationToken.java` — 首次使用生成 128-bit 随机 token，常量时间比较。
- `examples/notify/`

关键细节：

- `string id` → `tag=id, id=0`；`number id` → `tag=null, id=n`；缺省 id 用自增计数；避免字符串 hash 冲突。
- `notify.enabled()` 同时检查 `NotificationManager.areNotificationsEnabled()` 和 API 33+ 的 `POST_NOTIFICATIONS`。
- 通道 API 26+；默认通道用应用名初始化；创建后重要性不可改，更新只改名称/描述。
- 小图标运行时用 `getIdentifier("ic_stat_berba", "drawable", packageName)`，回退系统图标；**shell 代码不引用 `R`**。
- 点击 Intent 显式指向 `MainActivity`，携带 `payload` + `token` + 唯一 data URI；冷启动读取后**移除 extras**，避免旋转重复触发。
- 错误的 token / 非本应用 Intent 直接丢弃并记日志。
- `RuntimeManager` 增加通知 payload 队列；`create` 完成后投递 `notification` 事件（payload：`{id, data, coldStart}`）。
- dev 运行时：部分实现 Web Notifications；`notify.channel` no-op；权限拒绝时给出明确警告；点击可触发本地事件。

修改：`internal/build/build.go`（notify 启用时生成图标）、`CapabilityRegistry.java`（重新生成）、`MainActivity.java`、`RuntimeManager.java`、`docs/lua-api.md`、`docs/config.md`、`docs/security.md`、`templates/init/AGENTS.md`。

**退出标准**：启用/不启用 notify 的 APK 权限正确；冷/热点击各触发一次；错误 token 不触发；图标在 API 24-35 可解析。

---

### Stage 4 — task

**目标**：定时任务、开机重排、精确闹钟降级、无界面分发、显式完成 + 超时。

新增：

- `internal/capability/specs/task.json`（权限 `RECEIVE_BOOT_COMPLETED`、`SCHEDULE_EXACT_ALARM`；组件三个命名模板）。
- `shell/.../TaskModule.java`、`TaskRecord.java`、`TaskStore.java`、`TaskSchedule.java`、`TaskScheduler.java`、`TaskInvocation.java`。
- `shell/.../TaskAlarmReceiver.java`、`TaskBootReceiver.java`、`TaskJobService.java`。
- `shell/app/src/test/java/com/berba/shell/TaskScheduleTest.java`。
- `examples/task/`。

Lua API：

```lua
local info, err = task.schedule("sync", {
  every = 900,          -- 或 daily = "08:30" / delay = 60
  data = { since = 123 },
  exact = false,
  timeout = 60,         -- 10-600 秒，默认 60
  replace = true,
})
task.cancel("sync")
local list = task.list()
local exactAllowed = task.exact()

app.on("task", function(ctx)
  -- ctx.name / data / scheduledAt / firedAt / late / runId
  http.request({ url = "...", cb = function() ctx.done() end })
end)
```

关键细节：

- **只使用一次性闹钟**，每次触发后重排；不用 `setRepeating`。
- Receiver 中**先推进 runId/nextRun 并重排下一次闹钟，再入队 Job**；即使 Job 未执行，下一次仍在。
- `TaskJobService.onStartJob` 返回 `true`；只有 `ctx.done()` 或超时才 `jobFinished`；**禁止 handler 返回即完成**（异步 HTTP/native 回调会被杀掉）。
- `onStopJob` 返回 `false`，标记取消；下一次闹钟已存在。
- `every` 最小 900 秒（`setAndAllowWhileIdle` 速率限制）；`timeout` 10-600 秒（JobScheduler 上限 10 分钟）。
- 默认 `setAndAllowWhileIdle`；`exact=true` 时若 `canScheduleExactAlarms()` 为真用精确闹钟，否则降级并记警告；捕获 `SecurityException`。
- 持久化在独立 `SharedPreferences("berba_tasks")`，JSON 版本化，`commit()` 写入；上限 64 个任务、单任务 data 64KB。
- 每个任务持久化 `runId` 与 `jobId`；过期/被替换的 Job 检查 `runId` 后直接跳过。
- Boot Receiver 处理 `BOOT_COMPLETED`、`MY_PACKAGE_REPLACED`、`TIME_SET`、`TIMEZONE_CHANGED`、精确闹钟权限变化；过期任务立即补一次并标记 `late`，不重放积压。
- `TaskJobService` 必须 `exported="true"` + `android:permission="android.permission.BIND_JOB_SERVICE"`；两个 Receiver `exported="false"`。
- 无 `task` handler 时记日志并正常结束；handler 抛错立即 complete 并记日志；重复 `done` 安全。
- 文档明确：force-stop 后闹钟取消、Doze/OEM 下为 best-effort、无前台服务。

修改：`internal/manifest/render.go`（组件模板）、`CapabilityRegistry.java`、`LuaRuntime.java`/`RuntimeManager.java`（`dispatchTask`）、`internal/devserver/.../runtime.js`（`task` 桩：允许注册 handler，schedule 抛「浏览器不支持后台任务」）、`scripts/build-shell.*`（编译并运行**全部** `*Test.java`；PowerShell 当前只跑了部分）、`docs/*`。

**退出标准**：`TaskScheduleTest` 覆盖 interval/daily/DST/错过/溢出；`ctx.done`、超时、双 done、取消中断均正确；重启/改时间重排；精确闹钟关闭时降级；APK 组件属性正确。

---

### Stage 5 — 原生插件

**目标**：纯 C/C++/Rust 插件加载、稳定 ABI、构建期校验、示例插件。

新增：

- `internal/nativeplugin/spec.go` — 插件 `spec.json` 解析/校验、名称/模块/函数冲突检查。
- `internal/nativeplugin/elf.go` — `debug/elf` 校验：`ET_DYN`、ELF class/machine 与 ABI 匹配、`PT_LOAD` 对齐（64 位 ≥16KB，32 位 ≥4KB）、必需导出符号、`DT_NEEDED` 白名单（`libc.so/libm.so/libdl.so/liblog.so`）、大小上限。
- `internal/nativeplugin/resolve.go` — 插件目录、ABI 交集、运行时元数据。
- `shell/jni/berba_plugin.h`、`shell/jni/berba_jni.c` — JNI shim（`JNI_OnLoad` + `RegisterNatives`，`dlopen/dlsym`，异步线程 attach/detach，UTF-8 byte[]）。
- `shell/.../NativePluginBridge.java`、`NativePluginLoader.java`、`NativeModule.java`。
- `examples/native-echo/`（`spec.json`、`plugin.c`、`build.sh`）。
- `docs/native-plugins.md`。
- 测试：`internal/nativeplugin/*_test.go`（名字/路径/规格/ELF fixtures）。

关键细节：

- shim 打包在 `payload/native/<abi>/libberba_jni.so`，**不放进 `payload/lib`**，避免所有 APK 都被动带上 ABI。
- 构建仅当启用 native 插件时，把 shim 与插件库加入 APK：`lib/<abi>/libberba_jni.so`、`lib/<abi>/libberba_<name>.so`；ABI 取所有插件**交集**；交集为空报错；缺 arm64-v8a 警告。
- 有插件时 Manifest 设置 `android:extractNativeLibs="true"`；插件库在 `nativeLibraryDir` 下由 shim `dlopen` 绝对路径。
- shim 只导出 `JNI_OnLoad`；插件 ABI 函数用 `__attribute__((visibility("default")))`。
- 编译参数：`-shared -fPIC -O2 -fvisibility=hidden -Wl,--no-undefined -Wl,-z,max-page-size=16384 -Wl,--build-id=none -ffile-prefix-map=<src>=.`。
- 加载流程：`System.loadLibrary("berba_jni")` 一次 → 逐插件 `dlopen(RTLD_NOW|RTLD_LOCAL)` → `dlsym` 必需符号 → `describe` 交叉校验（不一致只记警告，以构建元数据为准）→ `init(config, host)` → 安装 `native.<module>` 表。
- 失败降级：某插件加载/init/describe 失败时，为其模块安装抛错桩并记日志，不崩溃应用。
- `native.list()` 返回插件/模块/函数信息；模块名保留 `list`。
- dev：`native` 表存在但函数抛「原生插件仅在设备上可用」；可选支持 `window.__berbaMock.native`。
- `berba release` 携带 `payload/native`、`payload/shell.json`、`include/berba_plugin.h`；`versions.json` 记录 shim 哈希。
- `berba doctor` 检查 shim 存在性与哈希、`nativeAbiVersion`。
- `scripts/build-shell.*` 增加 NDK 检测与 arm64-v8a / armeabi-v7a / x86_64 shim 构建；CI 安装固定版本 NDK。
- 示例插件在 CI 用 NDK 编译后再 `berba build`，不提交二进制。

修改：`internal/config/config.go`（插件字段校验）、`internal/build/build.go`（ABI 交集、插件库、`extractNativeLibs`、能力资产）、`internal/payload/payload.go`（扫描 `payload/native`）、`CapabilityRegistry.java`、`LuaRuntime.java`（启动时加载插件）、`scripts/package-dist.*`、`docs/security.md`。

**退出标准**：无插件构建不含 shim；有插件构建含 shim + 插件库；`zipalign -c -P 16 -v 4`、ELF 对齐、`apksigner verify` 通过；arm64/armv7/x86_64 加载正常；同步/异步/跨线程完成/超时/init 失败路径正确；双构建可复现。

---

### Stage 6 — 文档、示例、CI 与手动验收

- `runtime.js` 完整接入能力元数据：启用/禁用桩、`notify` 部分实现、`task`/`native` 桩、事件白名单、函数存在性自检。
- `devserver` 的 `lua.json` 增加 `capabilities/modules/events/native`；`berba dev` 不要求完整 payload。
- 更新 `README`、`docs/config.md`、`docs/lua-api.md`、`docs/build.md`、`docs/security.md`、`docs/testing.md`、`templates/init/AGENTS.md`、`templates/init/app.json`（可选示例字段）。
- 三个示例加入 CI：`examples/notify`、`examples/task`、`examples/native-echo`。
- CI 增加：NDK 构建、能力协商失败用例、APK 组件/权限断言、原生 ELF 检查、生成文件无 diff、双构建可复现、Windows smoke（使用 Linux 产物）。
- 完成设备矩阵手动验收（见下）。

---

## 验证策略

### Go 测试

- 规格：严格解析、未知字段、重复能力/模块/函数、非法名称、未知组件/资源、core/capability 规则、`requiresLua`。
- 解析合并：权限去重与顺序、事件、组件排序、未知能力、插件冲突、ABI 交集。
- 生成物：`docs/capabilities.md`、`CapabilityRegistry.java`、`shell/shell.json` 重新生成后逐字节一致。
- 配置：旧配置不变、新字段、插件路径逃逸/符号链接、`native` 与 plugins 对应、config 超限。
- Manifest：XML 合法、组件出现/缺失、`extractNativeLibs`、确定性、权限顺序。
- 构建：空能力资产、notify/task/native 条目、旧 payload 请求能力时报错、无插件构建不含 shim。
- Payload：`shell.json` 缺失/存在、`Supports`、native ABI 列表。
- 原生：名称/路径/规格/ELF（CI 用 NDK 构建 fixture，检查 machine/class/对齐/导出符号/依赖白名单）。
- devserver：`lua.json` 能力元数据、禁用桩、事件白名单。

### JVM 测试

- 保留 `LuaSandboxTest`、`NetworkPolicyTest`。
- 新增 `TaskScheduleTest`：interval、daily、DST 前后、错过、溢出、禁用。
- 新增纯 Java 校验器测试（任务名/插件名、LuaConvert 转义），避免依赖 `org.json` 的 Android 实现。
- 修复 `build-shell.ps1` 只编译部分测试的问题，两个脚本都编译运行全部 `*Test.java`。

### CI 断言

```text
gofmt -l . ; go vet ./... ; staticcheck ./... ; go test -race ./...
go generate ./internal/capability && git diff --exit-code
scripts/build-shell.sh              # 含 NDK shim
berba build examples/hello examples/notify examples/task examples/native-echo
aapt2 dump xmltree                  # 权限/组件/extractNativeLibs 断言
unzip -l                            # 能力资产/插件库/shim 出现时机
zipalign -c -P 16 -v 4
apksigner verify
llvm-readelf -lW                    # LOAD 对齐 0x4000（64 位）
双次构建 SHA-256 比对
```

### 设备手动矩阵（API 24 / 26 / 31 / 33 / 34 / 35）

- 运行时：10 次旋转、后台 30 分钟、`adb shell am kill`、开启「不保留活动」、深链冷/热、旋转中的 bridge Promise、`app.exit`、入口/`boot`/`create` 计数。
- 通知：授权/拒绝/撤销、通道创建与重要性不可变、冷/热点击、重复 ID 替换、cancel/cancelAll、伪造 token Intent、深浅色状态栏图标。
- 任务：once/every/daily、重启、应用更新、改时间/时区、Android 14 精确闹钟关闭、Doze（`dumpsys deviceidle force-idle`）、force-stop、异步 HTTP 后 `done`、未 `done` 超时、双 `done`、运行中取消、多任务并存。
- 原生：arm64 真机、armv7 模拟器、x86_64 模拟器、16KB page（API 35）、缺库/错 ABI、init 失败、同步/异步、原生线程完成、插件崩溃（预期进程退出）。
- 构建：各 ABI 安装启动；`aapt2` / `zipalign` / `apksigner` 输出正确。

---

## 主要风险与缓解

| 风险 | 缓解 |
| --- | --- |
| `MainActivity`/`LuaApis` 重构回归 | Stage 2 先于任何新能力、行为保持；生命周期回归矩阵；Activity 引用审计（只允许 `UiSession` 弱引用）。 |
| Job 在异步 Lua 工作完成前被结束 | 强制 `ctx.done()` + 超时；禁止 handler 返回即完成；测试覆盖异步 HTTP。 |
| 后台在 OEM/Doze 下不可靠 | 明确定位 best-effort；`every` ≥900s；boot/时间变化重排；文档写清 force-stop 行为。 |
| Android 14 精确闹钟默认拒绝 | `canScheduleExactAlarms()` 检查 + 非精确降级 + 权限变化重排 + `task.exact()`。 |
| 原生插件崩溃导致应用退出 | 受信任模型 + ELF/ABI 校验 + 示例 + 文档；崩溃隔离明确不做。 |
| CLI/payload 版本漂移 | `payload/shell.json` 协商，工具链阶段失败；`doctor` 打印能力；release 绑定 dex/shell.json。 |
| 规格与实现漂移 | 生成 Java 注册表/文档/`shell.json` 并测试；运行时函数存在性自检；CI diff 检查。 |
| NDK 破坏可复现构建 | 固定 NDK 版本、固定 flags、`--build-id=none`、`-ffile-prefix-map`、双构建比对。 |
| 通知 Intent 伪造 | 每安装随机 token + 常量时间比较 + 显式组件 + 冷启动消费 extras。 |
| dev 通过但设备失败 | 能力支持矩阵；`task`/`native` 在 dev 明确抛错；notify 仅部分模拟；保留上机清单。 |
| 进程级运行时内存/状态常驻 | 文档明确；无生产 shutdown；OS 回收；`destroy` 语义变更公告。 |
| JNI 完成竞态 | pending 先插入再调用；完成 post 回 Lua 线程；超时/取消清理；双 done 幂等；不 `dlclose`。 |

---

## 关键文件

**新增**

- `internal/capability/{spec,load,resolve,docs,javagen,shellgen}.go`、`internal/capability/specs/*.json`、`internal/capability/cmd/berbacap/main.go`
- `internal/nativeplugin/{spec,elf,resolve}.go`
- `shell/.../RuntimeManager.java`、`UiSession.java`、`CapabilityConfig.java`、`CapabilityRegistry.java`（生成）
- `shell/.../NotifyModule.java`、`NotifyController.java`、`NotificationToken.java`
- `shell/.../TaskModule.java`、`TaskRecord.java`、`TaskStore.java`、`TaskSchedule.java`、`TaskScheduler.java`、`TaskInvocation.java`、`TaskAlarmReceiver.java`、`TaskBootReceiver.java`、`TaskJobService.java`
- `shell/.../NativePluginBridge.java`、`NativePluginLoader.java`、`NativeModule.java`
- `shell/jni/berba_plugin.h`、`shell/jni/berba_jni.c`
- `shell/shell.json`（生成）、`docs/capabilities.md`（生成）、`docs/native-plugins.md`
- `examples/{notify,task,native-echo}/`
- `internal/resources/notification.go`

**修改**

- `internal/config/config.go`、`internal/config/assetconfig.go`
- `internal/manifest/render.go`、`internal/build/build.go`、`internal/payload/payload.go`
- `internal/cli/{build,dev,doctor,release}.go`
- `internal/devserver/devserver.go`、`internal/devserver/luaruntime/assets/runtime.js`
- `shell/.../LuaRuntime.java`、`LuaApis.java`、`MainActivity.java`、`JsBridge.java`、`AssetServer.java`、`BridgeChromeClient.java`
- `scripts/build-shell.sh/.ps1`、`scripts/package-dist.sh/.ps1`
- `docs/{config,lua-api,build,security,testing}.md`、`README.md`、`templates/init/AGENTS.md`

**复用**

- `internal/config/resolveWithin` / `NormalizePermission`
- `internal/apk.classify`（`.so` 已 store + 16KB 对齐）
- `internal/fswalk`（符号链接拒绝）
- `internal/publish.RenameOver`
- `internal/resources/icons.go` 的 PNG 生成模式
- `internal/devserver` 的 shim 注入与 `lua.json`
- `LuaConvert`、`LuaSandbox`、`JsBridge` 的 token/origin 校验
