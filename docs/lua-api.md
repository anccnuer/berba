# Lua API 与 JS 桥

运行时为 LuaJ 3.0.1（Lua 5.2 语义），只加载白名单库：`base`、`table`、`string`、`math`、`coroutine`、`bit32`。**没有** `io`、`os`、`package`、`debug`、`utf8`、`luajava`，`dofile`、`loadfile` 被移除。Lua 代码无法直接访问 Java 或文件系统以外的系统能力。

所有 Lua 回调在单一 Lua 工作线程执行；`ui`、`web`、`app.exit` 等 UI 操作会自动切到主线程。耗时操作用 `http`（内部线程池）或 `file`（同步，限制 8MB）。

## 入口与 require

入口文件默认 `lua/main.lua`，启动时执行一次。`require("a.b")` 会依次查找：

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
| `create` | 无 | Lua 就绪后触发。所有 `create` 回调执行完，若没有调用 `web.load`，会自动加载 `startUrl` |
| `pause` | 无 | Activity `onPause` |
| `resume` | 无 | Activity `onResume` |
| `destroy` | 无 | Activity 销毁前 |
| `back` | 无 | 返回键。返回 `true` 表示已处理；否则交给 WebView 历史，最后退出 |

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

结束当前 Activity。

### `app.info()`

返回表：`name`、`package`、`versionName`、`versionCode`、`sdk`、`platform`（固定 `"android"`）。

## web

| 方法 | 说明 |
| --- | --- |
| `web.load(url)` | 加载页面；origin 必须在白名单，否则被忽略 |
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

向页面推送事件，对应 JS 的 `guiapp.on(name, fn)`。

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

连接/读取超时 15 秒，自动跟随重定向，User-Agent 为 `guiapp`。明文 HTTP 需要 manifest 允许 cleartext。

## file

路径必须相对应用私有目录 `filesDir`，绝对路径、`..` 越界、NUL 都会被拒绝（反斜杠会统一转换为 `/`）；单文件上限 8MB。

| 方法 | 返回 |
| --- | --- |
| `file.read(path)` | 内容字符串；失败返回 `nil, 错误信息` |
| `file.write(path, text)` | `true`；失败返回 `nil, 错误信息`（自动创建父目录） |
| `file.exists(path)` | boolean；非法路径返回 `false` |
| `file.list(dir)` | 排序后的文件名数组；失败返回 `nil, 错误信息` |

```lua
file.write("notes/a.txt", "hello")
local text = file.read("notes/a.txt")
local names = file.list("notes")
```

## store

基于 `SharedPreferences` 的键值存储，值以 JSON 保存，可存字符串、数字、布尔、表。

```lua
store.set("token", { value = "abc", exp = 1730000000 })
local t = store.get("token")   -- 表；不存在返回 nil
store.set("token", nil)        -- 删除
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

## ui

| 方法 | 说明 |
| --- | --- |
| `ui.toast(msg)` | 短提示 |
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
| `device.clipboard()` | 剪贴板文本 |
| `device.orientation(mode)` | 运行时切换方向：`portrait` / `landscape` / `sensor` / `auto` |
| `device.model()` | 设备型号 |
| `device.sdk()` | API level |
| `device.platform()` | `"android"` |
| `device.locale()` | 例如 `zh_CN` |
| `device.screen()` | `{ width, height, density, dpi }` |

## json 与 log

```lua
local text = json.encode({ a = 1 })   -- '{"a":1}'
local t, err = json.decode(text)       -- 失败返回 nil, 错误信息
log("任意", "参数", 123)                -- 输出到 Logcat，tag = guiapp
```

JSON 与 Lua 表的转换规则：键为 `1..n` 连续整数的表编码为数组，其余编码为对象；`nil` 为 `null`。函数、userdata 等无法编码的值会报错。

## JS 侧接口

每个页面加载时注入 shim，暴露 `window.guiapp`：

```js
const res = await window.guiapp.call('add', { a: 1, b: 2 });
console.log(res.sum);

function onTick(data) { console.log(data.at); }
window.guiapp.on('tick', onTick);
window.guiapp.off('tick', onTick);
```

- `guiapp.call(name, args)` 返回 Promise；Lua 抛错或方法不存在时 reject。
- `guiapp.on(name, fn)` 订阅 Lua `bridge.emit` 的事件，返回该函数便于取消。
- 每次启动生成随机 token，桥调用必须携带正确 token 且当前页面 origin 在白名单内。
- 只有本地页面与 `allowedOrigins` 中的远程页面可以调用桥；未授权调用会被静默拒绝，Promise 不会完成。

shim 的注入方式：本地 HTML 响应会在 `<head>` 前插入脚本标签（`https://appassets.androidhost/__guiapp.js`）；所有白名单页面在 `onPageStarted` 时还会再注入一次内联脚本。页面无需手动引入 shim，也无法通过其他 origin 获得可用 token。
