# berba 能力扩展：Stage 7 桌面小组件（widget）

## 当前基线

v0.20 已发布（tag `v0.20`，CI 全绿）。已完成的能力与契约：

- **Stage 1 — 能力规格与构建契约**：`internal/capability/specs/*.json` 驱动权限、Manifest 组件、资源、`CapabilityRegistry.java`、`shell/shell.json`、`docs/capabilities.md` 与运行时能力资产。
- **Stage 2 — 进程级运行时**：`RuntimeManager` 单例持有唯一 Lua 实例，`UiSession` 管理界面生命周期，UI 回调按 epoch 丢弃过期结果。
- **Stage 3 — notify**：本地通知、通道、权限、冷/热点击事件与构建期小图标生成。
- **Stage 4 — task**：`AlarmManager` + `JobScheduler` 定时任务，无界面分发、开机/改时间重排、显式 `ctx.done()` 与超时。
- **Stage 5 — 原生插件**：C ABI v1（int32 状态码 + 长度缓冲 + `berba_host{abi_version, context, log}` + `berba_plugin_complete`），JNI shim、构建期 ELF 校验。
- **Stage 6 — 文档、示例、CI**：dev 运行时能力桩、`docs/`、`templates/init/AGENTS.md`、CI 的 NDK/shim 与 APK 断言、双构建复现。

> 唯一未完成的仍是**设备手动矩阵**（见文末）。实现细节已固化在 `docs/capabilities.md`、`docs/lua-api.md`、`docs/native-plugins.md`、`docs/build.md`、`docs/security.md`，本文不再重复。

---

## 进度（2026-10-30 收工）

> 工作区有未提交改动；下次从这个清单继续。代码已能编译并通过现有测试，但示例、专项测试与 CI 断言还没做。

| 项 | 状态 |
| --- | --- |
| 7.1 规格 / Manifest / 资源 | ✅ 已实现 |
| 7.2 壳 Provider + 渲染 | ✅ 已实现，`build-shell` 通过（javac + JVM 测试 + d8） |
| 7.3 缓存渲染与点击事件 | ✅ 已实现 |
| 7.4 dev 桩与文档 | 🟡 dev 桩、`docs/lua-api.md`、`docs/config.md`、`templates/init/AGENTS.md` 已改；`examples/widget`、`README.md` 未改 |
| 7.5 测试与 CI | 🔴 未做：`internal/resources/widget_test.go`、`WidgetStateTest.java`、CI 断言 |
| 设备矩阵 | 🔴 未上机（含新增 widget 项） |

已完成的具体改动：

- 新增 `internal/capability/specs/widget.json`；`spec.go` 白名单加入 `widget_provider` 与 3 个资源名。
- `internal/manifest/render.go` 增加 `widget_provider` receiver 模板（含 `android.appwidget.provider` meta-data）。
- 新增 `internal/resources/widget.go`：`berba_widget.xml`（固定 5 行槽位）、`berba_widget_info.xml`、两个 shape drawable。
- `internal/build/build.go` 接入 `widget_layout` / `widget_info` / `widget_drawable` 资源分支。
- 重新生成了 `CapabilityRegistry.java`、`shell/shell.json`、`docs/capabilities.md`（未提交）。
- 新增 `WidgetState.java`（纯 Java、自带极简 JSON、截断与颜色校验）、`WidgetController.java`（缓存 + RemoteViews + PendingIntent）、`WidgetProvider.java`（只读缓存渲染）、`WidgetModule.java`（`widget.update/clear/list`）。
- `LuaRuntime` / `RuntimeManager` / `MainActivity` 接入 widget 控制器与点击事件（`berba-widget://open/<id>`）。
- `runtime.js` 增加 `widget` 桩、`widget` 事件白名单与 dev 能力识别。

已验证：

```text
gofmt -l .           # 无输出
go build ./...       # 通过
go test ./...        # 全绿
scripts/build-shell.sh   # javac --release 17 + JVM 测试 + d8 通过（无 NDK，跳过 shim）
```

未验证：真机、`berba build examples/widget`（示例尚未创建）、CI。

下次继续的顺序：

1. `examples/widget/`（`app.json` + `lua/main.lua` + `www/`），用 `berba build` 验证 aapt2 编译与 receiver 落地。
2. `internal/resources/widget_test.go`、`internal/manifest` 组件断言、`WidgetStateTest.java`。
3. `ci.yml` 增加 widget 示例构建与资源断言；`README.md` 能力列表。
4. 设备矩阵 widget 项（添加/删除/重启/force-stop/点击/Doze/深色模式/字体放大）。

## 本轮目标与非目标

### 目标

- 新增 `widget` 能力：用户在桌面放置**卡片式小组件**，显示 Lua 计算的任意数据。
- Lua 可随时刷新；进程被回收、launcher 重启后仍显示最后一次缓存。
- 复用现有能力体系（规格 → 生成物 → 构建校验 → dev 桩 → 文档 → CI），不引入新工具链。

### 非目标（widget v1）

- 不做 `android:configure` 配置 Activity（需要第二个 Activity，与「多 Activity」非目标冲突）。
- 不做可滚动集合（`RemoteViewsService` + `BIND_REMOTEVIEWS` Service）。
- 不做 Android 12 响应式尺寸档位（`RemoteViews(Map<SizeF, ...>)`）。
- 不做图片/图表渲染（文本 + 颜色；图片走 Binder 易触发 `TransactionTooLarge`）。
- 不改变已实现的原生插件 ABI、不引入 AndroidX/Kotlin/前台服务。

---

## 跨阶段约束

- **minSdk 24**：新增 Java 代码禁用 API 26+ 类型（`java.time`、`String.join`、`List.of` 等），CI 已有 grep 守卫；无 core library desugaring。
- **RemoteViews 限制**：只能使用框架控件（`LinearLayout`/`FrameLayout`/`RelativeLayout`/`TextView`/`ImageView`/`Button`/`ProgressBar`/`Chronometer` 等），不能放 WebView，不能动态 `addView`，只能用固定槽位 + `setViewVisibility`。
- **进程可能被回收**：`onUpdate` 只读缓存渲染，不启动 Lua；任何需要 Lua 的刷新都由 `task`/前台界面驱动。
- **单 Activity**：本轮不新增 Activity；widget 的配置在 App 内完成。
- **确定性构建 / 零第三方 Go 依赖**：资源 XML 一律用 `strings.Builder` 生成，双次构建结果必须一致。
- **生成物同源**：`CapabilityRegistry.java`、`shell/shell.json`、`docs/capabilities.md` 由 `go generate ./internal/capability` 更新并提交，CI 校验无 diff。

---

## 数据契约

### `app.json`

```json
{
  "capabilities": ["widget", "task"]
}
```

- `widget` 是可选能力；启用后自动补齐 Manifest 组件与资源，并在能力资产中声明 `widget` 模块与 `widget` 事件。
- 仍要求 `<project>/lua/` 存在（`requiresLua`）。

### 小组件状态（壳内持久化）

写在 `SharedPreferences("berba_widget")` 的 `state` 键，供 `WidgetProvider` 在没有 Lua 时渲染：

```json
{
  "updatedAt": 1761800000000,
  "title": "今日概览",
  "cards": [
    { "title": "天气", "value": "26°", "subtitle": "多云", "color": "#FF8FB1" }
  ]
}
```

- 上限：序列化后 ≤ 32KB、最多 20 张卡；**渲染固定 5 行**，多余的截断并记日志。
- 字符串截断：`title` ≤ 24、`value` ≤ 16、`subtitle` ≤ 32（超出补 `…`）。
- `color` 为 `#RRGGBB`/`#AARRGGBB`，非法时忽略并回退主题色。

### Lua API

```lua
local res, err = widget.update({
  title = "今日概览",
  cards = {
    { title = "天气", value = "26°", subtitle = "多云" },
    { title = "步数", value = "8,421", subtitle = "目标 10,000", color = "#3DDC84" },
  },
})
-- res = { updatedAt = 1761800000000, ids = { 12, 15 } }

local list = widget.list()   -- { { id = 12, updatedAt = ... }, ... }
widget.clear()

app.on("widget", function(ev)
  -- ev.id / ev.coldStart
end)
```

- `widget.update` 无实例时只写缓存并返回 `ids = {}`，不报错。
- 参数非法返回 `nil, "中文错误"`，与 `notify`/`task` 风格一致。

---

## Stage 7 — widget

> 每个小节独立可验证；7.1 → 7.2 → 7.3 有依赖，7.4/7.5 可与 7.2 并行。

### 7.1 规格、Manifest 与资源 ✅

**新增**

- `internal/capability/specs/widget.json`

  ```json
  {
    "schema": 1,
    "name": "widget",
    "kind": "capability",
    "summary": "桌面小组件：卡片式数据展示。",
    "requiresLua": true,
    "components": ["widget_provider"],
    "resources": ["widget_layout", "widget_info", "widget_drawable"],
    "installer": "WidgetModule",
    "modules": {
      "widget": {
        "browser": "stub",
        "functions": {
          "update": {"async": false, "foreground": false, "doc": "写入缓存并渲染卡片；无实例时只缓存。"},
          "clear":  {"async": false, "foreground": false, "doc": "清空缓存并把小组件显示为占位。"},
          "list":   {"async": false, "foreground": false, "doc": "返回已放置的实例 id 与缓存时间。"}
        }
      }
    },
    "events": {
      "widget": {"owner": "app", "foreground": false, "doc": "点击小组件；冷启动在 create 之后触发。"}
    }
  }
  ```

- `internal/resources/widget.go`
  - `GenerateWidgetLayout(resDir)` → `res/layout/berba_widget.xml`
  - `GenerateWidgetInfo(resDir)` → `res/xml/berba_widget_info.xml`
  - `GenerateWidgetDrawable(resDir)` → `res/drawable/berba_widget_bg.xml`、`berba_widget_card.xml`（shape 圆角）
  - 可选 `GenerateWidgetPreview(resDir)` → `drawable-*/berba_widget_preview.png`（复用 `internal/resources/icons.go` 的绘图方式）

**修改**

- `internal/manifest/render.go`：`componentTemplates` 增加 `widget_provider`：

  ```xml
  <receiver android:name="com.berba.shell.WidgetProvider" android:exported="false">
    <intent-filter><action android:name="android.appwidget.action.APPWIDGET_UPDATE"/></intent-filter>
    <meta-data android:name="android.appwidget.provider" android:resource="@xml/berba_widget_info"/>
  </receiver>
  ```

- `internal/build/build.go`：资源 switch 增加 `widget_layout` / `widget_info` / `widget_drawable`（可选 `widget_preview`）；未知资源仍报错。
- 重新生成并提交：`shell/shell.json`、`CapabilityRegistry.java`、`docs/capabilities.md`。

**布局与元数据**

- `res/layout/berba_widget.xml`：根 `LinearLayout(vertical)`，固定 **5 个行槽位** `widget_row0..4`，每行 `title` + `value` 两个 `TextView`；数据不足时 `setViewVisibility(GONE)`。
- 顶部 `widget_title` + `widget_updated`（更新时间，小字）。
- `res/xml/berba_widget_info.xml`：`minWidth=250dp`、`minHeight=110dp`、`updatePeriodMillis=0`（由 Lua 主动推）、`initialLayout=@layout/berba_widget`、`resizeMode="horizontal|vertical"`、`widgetCategory="home_screen"`、`previewImage=@drawable/berba_widget_preview`（可选）。

### 7.2 壳：Provider 与渲染 ✅

**新增**

- `shell/.../WidgetProvider.java`（`public`，`extends AppWidgetProvider`）
  - `onUpdate` / `onAppWidgetOptionsChanged` / `onDeleted` → 交给 `WidgetController`
  - 只读缓存渲染，**不启动 Lua**
- `shell/.../WidgetController.java`
  - `render(Context)`：遍历 `AppWidgetManager.getAppWidgetIds`，逐个 `updateAppWidget`
  - `buildViews(state)`：`setTextViewText` / `setTextColor` / `setViewVisibility`
  - `write(String json)`：校验（≤32KB、≤20 张卡）→ 持久化 → 主线程渲染
  - 点击：根布局 `setOnClickPendingIntent`，`Intent` 指向 `MainActivity`，`data=berba-widget://open/<appWidgetId>`，`FLAG_IMMUTABLE | FLAG_UPDATE_CURRENT`
- `shell/.../WidgetModule.java`：安装 `widget.update/clear/list`，负责参数校验与转发
- `shell/.../WidgetState.java`：**纯 Java**（无 Android 依赖）的卡片结构、JSON 解析/序列化、截断逻辑，便于 JVM 测试

**修改**

- `RuntimeManager.java`：`capabilities.hasCapability("widget")` 时创建 `WidgetController`；按 `NotifyController` 的模式增加 `captureWidget(Intent)` 队列与 `deliverWidgets(LuaRuntime)`（`whenReady` 后派发 `widget` 事件 `{id, coldStart}`）。
- `MainActivity.java`：`onCreate` / `onNewIntent` 识别 `berba-widget://` scheme，交给 `RuntimeManager.captureWidget`；消费后移除 extras，避免旋转重复触发。
- `CapabilityRegistry.java`（生成）自动加入 `WidgetModule.install`。

### 7.3 数据流与刷新 ✅

```text
task 定时触发（无界面）
  → Lua 用 http/file/store 取数
  → widget.update(cards)     写缓存 + 推送 RemoteViews
  → 桌面立即可见

launcher 重新绑定 / 重启 / 进程被杀后 onUpdate
  → 只读缓存渲染，不启动 Lua
点击卡片
  → PendingIntent 打开 MainActivity → widget 事件（冷/热各一次）
```

- `updatePeriodMillis=0`：系统不自动唤醒应用；首次添加由 `onUpdate` 渲染缓存。
- 需要定时刷新时用 `task`（`every ≥ 900s`），在 `app.on("task")` 里调用 `widget.update`。
- 渲染线程：`AppWidgetManager.updateAppWidget` 可在任意线程调用，但统一 post 到主线程，避免与 launcher 回调竞争。

### 7.4 dev、文档与示例 🟡

- `internal/devserver/luaruntime/assets/runtime.js`：新增 `widget` 桩（`update/clear` 打印并返回本地时间戳，`list` 返回 `{}`），事件白名单加入 `widget`，保持在 `applyModuleMeta` 的自检范围内。
- `docs/lua-api.md`、`docs/config.md`、`docs/testing.md`、`templates/init/AGENTS.md`、`README.md`：增加 widget 章节（RemoteViews 限制、刷新频率、无 configure Activity）。
- `examples/widget/`
  - `app.json`：`"capabilities": ["widget", "task"]`
  - `lua/main.lua`：`create` 时推一次，`app.on("task")` 定时刷新
  - `www/index.html`：卡片预览 + 「立即刷新」按钮（走 `bridge`）

### 7.5 测试与 CI 🔴

**Go**

- `internal/resources/widget_test.go`：生成文件齐全；XML 可被 `encoding/xml` 解析；关键属性存在（`minWidth`、`initialLayout`、`widgetCategory`）；槽位数量固定；两次生成逐字节一致。
- `internal/manifest/render_test.go`：启用 widget 时 receiver + meta-data 出现，未启用不出现。
- `internal/capability/resolve_test.go`：组件/资源去重与排序。
- `internal/build/build_test.go`：res 目录含 `layout/`、`xml/`、`drawable/` 下的 widget 文件。

**JVM**

- `WidgetStateTest`：空数据、超长字符串截断、超过 5 张卡截断、非法 JSON 回退、颜色格式。
- 保持 `LuaSandboxTest`、`NetworkPolicyTest`、`TaskScheduleTest` 通过（两个 `build-shell` 脚本仍编全部 `*Test.java`）。

**CI（`ci.yml`）**

- `berba build examples/widget`，断言：
  - `aapt2 dump xmltree` 含 `com.berba.shell.WidgetProvider` 与 `android.appwidget.provider`
  - `aapt2 dump resources` 含 `berba_widget` / `berba_widget_info`
  - `apksigner verify`、双构建 SHA-256 一致
- 保持 `gofmt` / `go vet` / `staticcheck` / `go test -race` / `go generate` diff / `java.time`+androidx 守卫。

**退出标准**

- `go test -race ./...` 全绿；`gofmt -l .` 无输出；`staticcheck` 干净。
- `go generate ./internal/capability && git diff --exit-code` 无差异。
- `examples/widget` APK 含 receiver 与资源；点击冷/热启动各触发一次；`task` 刷新可见。
- dev 下 `widget.*` 为明确 stub（不静默成功）。
- 设备矩阵 widget 项通过。

---

## 设备手动矩阵（API 24 / 26 / 31 / 33 / 34 / 35）

- **运行时**：10 次旋转、后台 30 分钟、`adb shell am kill`、开启「不保留活动」、深链冷/热、旋转中的 bridge Promise、`app.exit`、入口/`boot`/`create` 计数。
- **通知**：授权/拒绝/撤销、通道创建与重要性不可变、冷/热点击、重复 ID 替换、`cancel`/`cancelAll`、伪造 token Intent、深浅色状态栏图标。
- **任务**：once/every/daily、重启、应用更新、改时间/时区、Android 14 精确闹钟关闭、Doze（`dumpsys deviceidle force-idle`）、force-stop、异步 HTTP 后 `done`、未 `done` 超时、双 `done`、运行中取消、多任务并存。
- **原生**：arm64 真机、armv7 模拟器、x86_64 模拟器、16KB page（API 35）、缺库/错 ABI、init 失败、同步/异步、原生线程完成、插件崩溃（预期进程退出）。
- **widget**：添加/删除/同应用多实例、重启与 force-stop 后仍显示缓存、点击冷/热启动各一次、`task` 定时刷新、Doze 下刷新、launcher 重启、深色模式、系统字体放大。
- **构建**：各 ABI 安装启动；`aapt2` / `zipalign` / `apksigner` 输出正确。

---

## 验证策略（回归基线）

```text
gofmt -l . ; go vet ./... ; staticcheck ./... ; go test -race ./...
go generate ./internal/capability && git diff --exit-code
! grep -R "java\.time" shell/app/src/main/java   # minSdk 24 禁止 API 26+ 类型
scripts/build-shell.sh              # 含 NDK shim
berba build examples/hello examples/notify examples/task examples/native-echo examples/widget
aapt2 dump xmltree / dump resources # 权限/组件/资源断言
unzip -l                            # 能力资产/插件库/shim 出现时机
zipalign -c -P 16 -v 4
apksigner verify
双次构建 SHA-256 比对
```

---

## 主要风险与缓解

| 风险 | 缓解 |
| --- | --- |
| RemoteViews 控件/布局受限，卡片「做不出设计效果」 | v1 固定槽位 + 圆角 shape drawable；设计规范写进 `docs/lua-api.md`；不做图片与图表 |
| 进程被回收后小组件空白 | `onUpdate` 只读 `SharedPreferences` 缓存渲染，不依赖 Lua 存活 |
| 刷新频率被系统节流 | `updatePeriodMillis=0` + `task`（`every ≥ 900s`）主动推；文档不承诺秒级 |
| 大图/长文本触发 `TransactionTooLarge` 或布局溢出 | 文本截断 + 卡片数上限 + 32KB 状态上限；v1 不支持图片 |
| 国产 ROM/launcher 行为差异 | 设备矩阵覆盖 API 24–35；明确 best-effort 语义 |
| 需要第二个 Activity 做配置页 | v1 在 App 内配置，避免触碰「多 Activity」非目标 |
| `berba-widget://` 与深链/通知 Intent 混淆 | 独立 scheme + 显式组件 + 消费后移除 extras；解析失败只记日志 |
| 资源名冲突 | 所有生成资源统一 `berba_widget*` 前缀；构建期未知资源仍报错 |
| 规格与实现漂移 | 沿用 `CapabilityRegistry.java` / `shell.json` / `docs/capabilities.md` 生成与 diff 校验 |

---

## 关键文件

**新增**

- `internal/capability/specs/widget.json`
- `internal/resources/widget.go`（+ `widget_test.go`）
- `shell/.../WidgetProvider.java`、`WidgetController.java`、`WidgetModule.java`、`WidgetState.java`
- `shell/app/src/test/java/com/berba/shell/WidgetStateTest.java`
- `examples/widget/`

**修改**

- `internal/manifest/render.go`（`widget_provider` 模板）
- `internal/build/build.go`（资源分支）
- `internal/capability/{resolve_test,spec_test}.go`（组件/资源断言）
- `internal/devserver/luaruntime/assets/runtime.js`（widget 桩）
- `shell/.../RuntimeManager.java`、`MainActivity.java`
- `shell/.../CapabilityRegistry.java`、`shell/shell.json`、`docs/capabilities.md`（生成）
- `docs/{lua-api,config,testing}.md`、`templates/init/AGENTS.md`、`README.md`
- `ci.yml`（widget 示例构建与资源断言）

**复用**

- `internal/resources/icons.go` 的 PNG 绘图模式
- `NotifyController` 的点击队列 / `RuntimeManager.captureNotification` 模式
- `internal/apk` 的 store + 16KB 对齐（不涉及 `.so` 时无改动）
- `LuaConvert`、`CapabilityConfig`、`applyModuleMeta` 自检
