# Lua API 与 JS 桥

运行时为 LuaJ 3.0.1（Lua 5.2 语义），只加载白名单库：`base`、`table`、`string`、`math`、`coroutine`、`bit32`。**没有** `io`、`os`、`package`、`debug`、`utf8`、`luajava`，`load`、`dofile`、`loadfile` 被移除。Lua 代码无法直接访问 Java 或文件系统以外的系统能力。

所有 Lua 回调在单一 Lua 工作线程执行；`ui`、`web`、`app.exit` 等 UI 操作会自动切到主线程。耗时操作用 `http`（内部线程池）或 `file`（同步，限制 8MB）。

运行时是**进程级单例**：入口只执行一次，Activity 重建（旋转、语言切换等）不会重新执行入口。没有前台界面时，`ui`、`web`、`app.exit`、`device.clipboard`、`device.share`、`device.openUrl`、`device.orientation`、`permission.request` 会报“当前没有前台界面”；`file`、`store`、`http`、`permission.check`、`notify.*`、`task.*`、`native.*` 等后台 API 仍可用。Activity 销毁不会取消在途 HTTP。按能力开关安装模块：未启用的 `notify`/`task`/`native` 对应模块不存在。旧项目的适配样例见 [迁移说明](migration.md)。

## 入口与 require

入口文件默认 `lua/main.lua`，进程启动时执行一次。`require("a.b")` 会依次查找：

```
assets/lua/a/b.lua
assets/lua/a/b/init.lua
```

模块只执行一次并缓存返回值；返回值会被 `require` 返回（无返回值为 `true`）。循环 `require` 返回 `true` 以避免死锁。只能加载 `assets/lua/` 内的文件。

## app

### `app.on(event, fn)`

支持的事件：

| 事件 | 参数 | 说明 |
| --- | --- | --- |
| `boot` | 无 | 进程启动后、入口执行完触发一次；此时通常还没有前台界面 |
| `create` | 无 | 每次 Activity 绑定后触发。所有 `create` 回调执行完，若没有调用 `web.load`，会自动加载 `startUrl` |
| `pause` | 无 | Activity `onPause` |
| `resume` | 无 | Activity `onResume` |
| `destroy` | 无 | 当前界面销毁时触发；Lua 运行时继续存活，在途 HTTP 不会被取消 |
| `back` | 无 | 返回键。返回 `true` 表示已处理；否则交给 WebView 历史，最后退出 |
| `newIntent` | `url` | 深链唤起（配置 `deepLinks`）。冷启动时会在 `create` 之后补发 |
| `notification` | `{ id, data, coldStart }` | 用户点击本地通知（需要 `notify` 能力）。`id`/`data` 来自 `notify.show`；`coldStart` 表示进程此前没有绑定过界面（通常即冷启动）。冷启动在 `create` 之后触发 |
| `task` | `ctx` | 后台定时任务触发（需要 `task` 能力），无前台界面也会执行。`ctx` 含 `name`、`data`、`scheduledAt`、`firedAt`、`late`、`runId`、`done()`；必须调用 `ctx.done()` 或等待超时 |

多个同事件处理器都会执行，按注册顺序。

```lua
app.on("create", function()
  log("ready")
end)

app.on("back", function()
  if modalOpen then
    closeModal()
    return true
  end
end)
```

### `app.exit()`

结束当前 Activity。Lua 运行时与后台任务（例如在途 `http`）继续存活，之后仍可通过深链或通知重新附着界面。

### `app.info()`

返回表：`name`、`package`、`versionName`、`versionCode`、`sdk`、`platform`（固定 `"android"`）、`foreground`（当前是否有前台界面）。

## web

| 方法 | 说明 |
| --- | --- |
| `web.load(url)` | 加载页面；origin 不在白名单、或 `http://` 未开启 cleartext 时直接报错 |
| `web.reload()` | 重新加载 |
| `web.goBack()` | WebView 后退 |
| `web.canGoBack()` | 返回 boolean |
| `web.eval(script, cb)` | 执行 JS；`cb(resultString)` 可选，`resultString` 是 `evaluateJavascript` 的原始 JSON 文本 |
| `web.on(event, fn)` | 见下表 |

| 事件 | 参数 | 说明 |
| --- | --- | --- |
| `pageStarted` | `url` | 主框架开始加载 |
| `pageFinished` | `url` | 主框架加载完成 |
| `error` | `description, url` | 主框架加载失败 |

```lua
web.on("pageFinished", function(url)
  web.eval("document.title", function(title)
    log("title = " .. title)
  end)
end)
```

## bridge（Lua 侧）

### `bridge.register(name, fn)`

注册供 JS 调用的方法。`fn` 接收一个参数（JS 传入的值，可能是 `nil`），返回值会被 JSON 编码后回传给 Promise。

```lua
bridge.register("add", function(req)
  return { sum = (req.a or 0) + (req.b or 0) }
end)
```

### `bridge.emit(name, data)`

向页面推送事件，对应 JS 的 `berba.on(name, fn)`。

```lua
bridge.emit("tick", { count = 3, source = "lua" })
```

## http

### `http.get(url, callback)`

### `http.post(url, body, callback)`

仅支持 `http/https`。`body` 可以是字符串或表（表会编码为 JSON）。异步执行，回调收到结果表：

```lua
http.get("https://example.com/api", function(res)
  if res.ok then
    log(res.status, res.body)
  else
    log("失败", res.error)
  end
end)
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `ok` | bool | HTTP 2xx |
| `status` | int | 状态码，网络错误为 0 |
| `body` | string | 响应文本，最大 8MB |
| `error` | string | 仅失败时存在 |

连接/读取超时 15 秒，User-Agent 为 `berba`。重定向最多 5 跳，每一跳都重新校验协议与主机；默认拒绝本机、私网、链路本地与 CGNAT 地址，需要时在 `app.json` 设置 `"http": { "allowPrivateNetwork": true }`。明文 HTTP 需要 `"cleartext": true`。

### `http.request(opts)`

完整形态，`opts` 为表，返回请求 ID（字符串）：

```lua
local id = http.request({
  url = "https://example.com/api/items",
  method = "PUT",                       -- GET/POST/PUT/DELETE/HEAD/OPTIONS，默认 GET
  headers = { ["X-Token"] = "abc" },     -- 可选
  body = { items = { 1, 2 } },           -- 字符串或表（表编码为 JSON），可选
  timeout = 30,                          -- 秒，1-120，默认 15
  cb = function(res) log(res.status, res.body) end,
  onProgress = function(read, total)      -- 可选；total 为 Content-Length，未知为 -1
    if total > 0 then log(read * 100 // total .. "%") end
  end,
})
http.cancel(id)   -- 断开连接；回调收到 { ok=false, status=0, error="已取消" }
```

结果表在 `http.get/post` 的基础上额外提供 `headers`（响应头表，同名多值以 `, ` 连接）。`http.cancel(id)` 只对仍在途的请求返回 `true`。

## file

路径必须相对应用私有目录 `filesDir`，绝对路径、`..` 越界、NUL 都会被拒绝（反斜杠会统一转换为 `/`）；读写单文件上限均为 8MB。`file.write` 先写入同目录临时文件再替换，避免中途失败留下半个文件。

| 方法 | 返回 |
| --- | --- |
| `file.read(path)` | 内容字符串；失败返回 `nil, 错误信息` |
| `file.write(path, text)` | `true`；失败返回 `nil, 错误信息`（自动创建父目录） |
| `file.append(path, text)` | `true`；追加写入并检查 8MB 上限 |
| `file.exists(path)` | boolean；非法路径返回 `false` |
| `file.delete(path)` | 删除文件/空目录；不存在返回 `false`，失败返回 `nil, 错误信息` |
| `file.rename(from, to)` | `true`；目标已存在时覆盖，父目录自动创建 |
| `file.stat(path)` | `{ size, mtime, dir, file }`（mtime 为秒级时间戳）；不存在返回 `nil, 错误信息` |
| `file.list(dir)` | 排序后的文件名数组；失败返回 `nil, 错误信息` |

```lua
file.write("notes/a.txt", "hello")
file.append("notes/a.txt", "\nworld")
local text = file.read("notes/a.txt")
local info = file.stat("notes/a.txt")   -- info.size, info.mtime
local names = file.list("notes")
file.rename("notes/a.txt", "notes/b.txt")
```

## store

基于 `SharedPreferences` 的键值存储，值以 JSON 保存，可存字符串、数字、布尔、表。

```lua
store.set("token", { value = "abc", exp = 1730000000 })
local t = store.get("token")   -- 表；不存在返回 nil
store.set("token", nil)        -- 删除
local existed = store.remove("token")  -- 删除并返回原本是否存在
local keys = store.keys()              -- 排序后的 key 数组
```

key 不能为空且长度 ≤ 256。

## permission

### `permission.check(name)`

返回是否已授权。`name` 可写 `CAMERA` 或完整 `android.permission.CAMERA`。

### `permission.request(names, callback)`

`names` 为字符串或字符串数组（一次最多 32 个）。回调收到表：键是完整权限名，值为 boolean。

```lua
app.on("create", function()
  if not permission.check("CAMERA") then
    permission.request({"CAMERA"}, function(result)
      log("camera granted: " .. tostring(result["android.permission.CAMERA"]))
    end)
  end
end)
```

权限必须先写入 `app.json` 的 `permissions`，运行时申请才有意义。

## notify

需要 `app.json` 的 `capabilities` 包含 `"notify"`。该能力会自动写入 `POST_NOTIFICATIONS` 权限，并在 API 33+ 仍需运行时申请；`notify.*` 不要求前台界面。

| 方法 | 说明 |
| --- | --- |
| `notify.show(opts)` / `notify.show(title, body)` | 显示或替换通知；成功返回 id，失败返回 `nil, err` |
| `notify.cancel(id)` | 取消指定通知，返回原本是否存在 |
| `notify.cancelAll()` | 取消本应用全部通知 |
| `notify.enabled()` | 通知权限与系统通知开关均可用时返回 `true`；为 `false` 时 `show` 返回 `nil, "通知权限未授予"` |
| `notify.channel(opts)` | 创建或更新通知通道（API 26+；低版本 no-op） |

`notify.show` 选项：

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `id` | string / 正整数 | 自增 | 字符串 id 映射为通知 tag（同 id 替换）；数字 id 映射为 notificationId |
| `title` | string | 应用名 | ≤ 512 字符 |
| `body` | string | 空 | ≤ 8192 字符 |
| `channel` | string | `"default"` | 通道 id：1-64 字符，仅字母、数字、`_ - .`；缺失时自动创建 |
| `data` | 任意 JSON 值 | `nil` | 随点击事件返回；序列化后 ≤ 64KB |
| `ongoing` | bool | `false` | 常驻通知，用户不能直接划掉 |
| `autoCancel` | bool | `true` | 点击后自动移除 |

`notify.channel` 选项：`id`、`name`、`description`、`importance`（`min` / `low` / `default` / `high`）。通道创建后重要性由系统固定，后续调用只更新名称与描述。

点击通知会打开 `MainActivity` 并触发 `app.on("notification")`：热启动立即送达；冷启动在 `create` 之后送达，`coldStart = true`。每次有效点击只送达一次；token 或 payload 不合法（被其他应用伪造、缺少字段、超过 64KB）会被丢弃并记日志。

```lua
app.on("create", function()
  permission.request("POST_NOTIFICATIONS", function(result)
    if not result["android.permission.POST_NOTIFICATIONS"] then
      log("通知权限被拒绝")
    end
  end)
end)

app.on("notification", function(event)
  log("clicked", event.id, json.encode(event.data), tostring(event.coldStart))
end)

local id, err = notify.show({
  id = "daily",
  title = "今天也要加油",
  body = "点击回到应用",
  channel = "default",
  data = { from = "lua" }
})
```

通知小图标由构建在能力启用时生成（`drawable-*/ic_stat_berba.png`），无需在 `www/` 或 `lua/` 中提供。启用 `notify` 会让 Activity 使用 `launchMode="singleTask"`，保证点击走 `onNewIntent`。

## task

需要 `app.json` 的 `capabilities` 包含 `"task"`。该能力会自动加入 `RECEIVE_BOOT_COMPLETED` 与 `SCHEDULE_EXACT_ALARM`，并在 Manifest 中注册 `TaskJobService`、`TaskAlarmReceiver`、`TaskBootReceiver`；`task.*` 不要求前台界面。

| 方法 | 说明 |
| --- | --- |
| `task.schedule(name, opts)` | 创建或替换任务；成功返回任务信息表，失败返回 `nil, err` |
| `task.cancel(name)` | 取消任务并移除闹钟与待运行 Job，返回原本是否存在 |
| `task.list()` | 返回按名称排序的任务数组 |
| `task.exact()` | 当前是否允许精确闹钟（`canScheduleExactAlarms`；API 31 以下恒为 `true`） |

名字需匹配 `^[A-Za-z_][A-Za-z0-9_.-]{0,63}$`。`opts`：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `every` / `daily` / `delay` | number / string / number | 三选一。`every` 为间隔秒数，最小 900；`daily` 为 24 小时制 `HH:MM`；`delay` 为一次性延时秒数 |
| `data` | 任意 JSON 值 | 随 `app.on("task")` 返回；序列化后 ≤ 64KB，明文存储，**不要放密钥** |
| `exact` | bool | 请求精确闹钟；系统未授权时自动降级为非精确并记警告 |
| `timeout` | number | 本次运行上限，10-600 秒，默认 60 |
| `replace` | bool | 同名任务是否存在时替换，默认 `true`；为 `false` 时返回错误 |

限制：最多 64 个任务；`every`/`delay` 上限 30 天。返回信息表与 `task.list()` 元素包含 `name`、`kind`（`every`/`daily`/`delay`）、`nextRun`（epoch 秒）、`runId`、`exact`、`timeout`、`done` 及对应的 `every`/`daily`/`delay`。无任务时 `task.list()` 为空表，经桥或 `json.encode` 编码为 `{}`，前端请用 `Array.isArray`/`Object.keys` 判断。

任务触发时 `app.on("task")` 收到 `ctx`：

| 字段 | 说明 |
| --- | --- |
| `name` / `data` | 任务名与排程时写入的 `data` |
| `scheduledAt` / `firedAt` | 计划触发与实际触发的 epoch 秒 |
| `late` | 实际触发比计划晚超过 60 秒 |
| `runId` | 本次运行的递增编号，可用于去重 |
| `done()` | 结束本次运行；重复调用安全。不调用则等待 `timeout` |

执行语义：

- 每次闹钟触发后先推进 `runId` 与下一次 `nextRun` 并重排闹钟，再入队 Job；Job 未运行也不影响下一次。
- Handler 返回**不代表完成**：必须调用 `ctx.done()`，或在超时后由壳结束 Job。异步 `http` 回调中调用 `done()` 是支持的。
- 同一任务不会重放积压：开机、应用更新、改时间/时区后过期任务只补一次并标记 `late`。
- `exact` 只影响闹钟精度；JobScheduler 的执行时间仍是 best-effort（API 31+ 会尝试 expedited Job，仍受系统配额限制）。
- force-stop 会取消闹钟；Doze 与厂商省电策略下触发可能延迟。没有前台服务，也没有推送。

```lua
local info, err = task.schedule("sync", {
  every = 900,
  data = { since = 123 },
  timeout = 60,
  replace = true
})

app.on("task", function(ctx)
  if ctx.name == "sync" then
    http.request({
      url = "https://example.com/sync",
      cb = function() ctx.done() end
    })
  else
    ctx.done()
  end
end)

task.cancel("sync")
```

## native

需要 `capabilities` 包含 `"native"`，并在 `app.json` 的 `native.plugins` 中声明插件。每个插件按 `spec.json` 暴露 `native.<module>.<function>`；未声明的模块/函数不可见。插件在应用启动时加载，失败时对应函数替换为抛错桩，其他插件继续可用。

| 方法 | 说明 |
| --- | --- |
| `native.list()` | 返回已加载插件数组；元素含 `name`、`lib`、`ok`、`error`（失败时）与 `modules`（函数及 `async` 标记） |
| `native.<module>.<fn>(args)` | 同步函数；参数/结果均为 JSON。成功返回解析后的 Lua 值，失败返回 `nil, err` |
| `native.<module>.<fn>(args, callback)` | 异步函数；回调签名为 `callback(result, err)`，在 Lua 线程执行 |

- 参数上限 1MB、结果上限 8MB、`config` 上限 64KB；异步并发上限 256、超时 30 秒。
- 同步调用在 Lua 线程执行，插件必须快速返回；耗时工作应声明为 `async`。
- 结果按 JSON 解析；非 JSON 结果按字符串返回；`null` 映射为 `nil`。
- 开发期：`native` 表存在但函数抛“原生插件仅在设备上可用”。ABI、构建与示例见 [native-plugins.md](native-plugins.md)。

## ui

| 方法 | 说明 |
| --- | --- |
| `ui.toast(msg, duration)` | 提示；`duration` 为 `"short"`（默认）/`"long"`，或毫秒数（≥ 2000 视为长提示） |
| `ui.statusBar({ color, light })` | 设置状态栏颜色（`"#rrggbb"`、`"#aarrggbb"` 或整数）与图标明暗（`light = true` 使用深色图标） |
| `ui.alert(msg)` | 单按钮对话框 |
| `ui.confirm(msg, cb)` | 确认对话框；`cb(true/false)` |
| `ui.fullscreen(on)` | 隐藏/恢复状态栏与导航栏（沉浸式全屏） |
| `ui.keepScreenOn(on)` | 保持屏幕常亮，适合海报/应援等长时间展示 |

`ui.keepScreenOn` 只影响当前窗口，退出页面后系统自动恢复。

```lua
-- 海报/应援场景：横屏、全屏、保持常亮
ui.fullscreen(true)
ui.keepScreenOn(true)
device.orientation("landscape")
```

## device

| 方法 | 返回 |
| --- | --- |
| `device.clipboard()` / `device.clipboard(text)` | 无参读取剪贴板；传入字符串则写入并返回 `true` |
| `device.vibrate(ms)` | 震动 1-10000 毫秒；需要 `VIBRATE` 权限，缺失时直接报错 |
| `device.share(text)` / `device.share({ text, url })` | 打开系统分享面板 |
| `device.openUrl(url)` | 用外部浏览器/应用打开 http(s) 链接 |
| `device.orientation(mode)` | 运行时切换方向：`portrait` / `landscape` / `sensor` / `auto` |
| `device.model()` | 设备型号 |
| `device.sdk()` | API level |
| `device.platform()` | `"android"` |
| `device.locale()` | 例如 `zh_CN` |
| `device.screen()` | `{ width, height, density, dpi }` |

`device.openUrl` 只接受 `http/https`，不受 WebView 导航白名单限制；调用前请确认链接可信。

## json 与 log

```lua
local text = json.encode({ a = 1 })   -- '{"a":1}'
local t, err = json.decode(text)       -- 失败返回 nil, 错误信息
log("任意", "参数", 123)                -- 输出到 Logcat，tag = berba
```

JSON 与 Lua 表的转换规则：键为 `1..n` 连续整数的表编码为数组，其余编码为对象（空表为 `{}`）；`nil` 为 `null`。函数、userdata 编码为 `null`；存在循环引用或嵌套超过 64 层时整体输出 `null`。

## JS 侧接口

每个页面加载时注入 shim，暴露 `window.berba`：

```js
const res = await window.berba.call('add', { a: 1, b: 2 });
console.log(res.sum);

function onTick(data) { console.log(data.at); }
window.berba.on('tick', onTick);
window.berba.off('tick', onTick);
```

- `berba.call(name, args)` 返回 Promise；Lua 抛错或方法不存在时 reject。
- `berba.on(name, fn)` 订阅 Lua `bridge.emit` 的事件，返回该函数便于取消。
- 每次启动生成随机 token，只存在于 shim 闭包中（`window.berba.token` 不存在）；桥调用必须携带正确 token，且原生侧记录的当前页面 origin 在白名单内。
- origin 由原生侧在导航开始时记录，页面不能通过伪造 `location.origin` 绕过。
- 只有本地页面与 `allowedOrigins` 中的远程页面可以调用桥；未授权调用会被静默拒绝，Promise 不会完成。

shim 的注入方式：本地 HTML 响应会在 `<head>` 前插入脚本标签（`https://appassets.androidhost/__berba.js`）；所有白名单页面在 `onPageStarted` 时还会再注入一次内联脚本。页面无需手动引入 shim。

## 网页端可用能力

- `<input type="file">` 调用系统文件选择器（SAF），支持单选与 `multiple` 多选。
- 触发下载（如 `<a download>` 或响应带 `Content-Disposition`）时交由外部浏览器处理；`blob:` 链接不会被外部打开。
- `alert` / `confirm` / `prompt` 使用原生对话框；`console.*` 写入 Logcat（tag `berba`，前缀 `[js]`）。
- 多窗口禁用：`window.open` 与 `target="_blank"` 不会打开新页面；跳转请用 `web.load`（Lua 侧）或同页导航。
- `getUserMedia` 与 `navigator.geolocation` 不可依赖；需要相机/定位请走 Lua `permission` + 自有实现。
- 浏览器调试：`berba dev` 会在浏览器里用 fengari（Lua 5.3）真实执行 `lua/main.lua`，`bridge.register` 的方法可直接被页面调用，`dev/mock.js` 仍可覆盖单个方法。浏览器运行时按 `app.json` 的 `capabilities` 装载模块与事件白名单：未声明 `notify`/`task`/`native` 时对应的表不存在，`app.on("notification")`/`app.on("task")` 也会被判为未知事件，与设备一致。与设备（LuaJ，Lua 5.2）的其他差异：`bit32` 已补齐；除通知外的 `permission` 视为已授权；`notify` 使用 Web Notifications（无通道、页面关闭后点击不再回调，可用 `window.__berbaDev.notify(id, data)` 手动触发）；`task` 只能注册 `app.on("task")`，`task.schedule` 直接报“浏览器不支持后台任务”；`native.list()` 返回 `app.json` 声明的插件与函数（`ok=false`），插件函数抛“原生插件仅在设备上可用”；`store`/`file` 落在 `localStorage`；`http` 受浏览器 CORS 限制；`app.on("back")` 需手动调用 `window.__berbaDev.back()`。完整列表见 [build.md](build.md)。
