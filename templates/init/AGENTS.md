# AGENTS.md

本文件面向在本项目中工作的 AI coding agent，说明项目能力边界与可用 API。开始修改前请完整阅读；**不要假设存在本文档未列出的能力**。

本项目由 [berba](https://github.com/anccnuer/berba) 打包为 Android APK：静态前端（`www/`）+ Lua 逻辑（`lua/`）+ berba Android 壳。产物面向侧载/企业分发，minSdk 24（Android 7.0+），targetSdk 35，APK Signature Scheme v2 签名。

## 1. 快速事实

| 项 | 值 |
| --- | --- |
| 构建 | `berba build` → `dist/<package>-<versionName>.apk` + `.sha256` |
| 开发 | `berba dev --open`：浏览器内执行页面与 Lua，桥调用优先 `dev/mock.js`，其次 Lua |
| 监听 | `berba watch [--install]`：文件变更自动重建，可选安装到设备 |
| 自检 | `berba doctor`（工具链与 payload 完整性，含 aapt2/android.jar/dex 哈希） |
| 安装 | `berba install`（需要 adb，失败不影响构建） |
| 入口页面 | `app.json` 的 `web.startUrl`，默认 `https://appassets.androidhost/index.html` |
| 入口脚本 | `app.json` 的 `lua.entry`，默认 `main.lua`（位于 `lua/`） |
| 前端构建 | **没有** Node/npm 构建步骤；`www/` 必须是可直接访问的静态文件 |
| 签名密钥 | `.berba/keys/release.pem`，首次构建自动生成，**必须离线备份** |

所有命令都在项目根目录执行。若 `berba` 不在 PATH，使用发行包中的可执行文件路径（如 `..\berba.exe build`）。

## 2. 目录与可编辑范围

```
app.json          应用配置（唯一配置入口）
icon.png          可选图标，PNG/JPG，建议 512x512
www/              前端静态资源，整体打包进 assets/www
lua/              Lua 逻辑，整体打包进 assets/lua
dev/mock.js       可选，berba dev 的桥 mock（不会进入 APK）
.berba/           构建缓存与签名密钥（不要读取、修改或提交）
dist/             构建产物（不要手改）
AGENTS.md         本文件
```

- 只通过 `app.json`、`www/`、`lua/`、图标文件表达应用行为。
- `dev/` 只用于本地调试，可自由创建 `dev/mock.js`；该目录不会打包进 APK。
- 不存在可编辑的 Android 工程、Manifest 或 Gradle；唯一例外是原生插件：确需 C/C++/Rust 时按上游 [`docs/native-plugins.md`](https://github.com/anccnuer/berba/blob/main/docs/native-plugins.md) 在 `native/` 下提供 C ABI 共享库，不要生成 Android 工程。
- 不要提交 `.berba/` 与 `dist/`（脚手架已自带 `.gitignore`）。

## 3. 能力边界

### 3.1 能做

- 任意静态前端：HTML/CSS/JS、预构建的框架产物、本地图片/字体/音视频、SPA 路由。
- 本地页面（`https://appassets.androidhost/*` → `assets/www/*`）或远程页面（需 `allowedOrigins`）。
- Lua 5.2（LuaJ 3.0.1）胶水逻辑：事件、定时/异步回调（无 sleep）、JSON、HTTP(S)、本地键值存储与私有文件。
- JS ↔ Lua 双向调用：`window.berba.call` / `bridge.register`，`bridge.emit` / `window.berba.on`。
- `berba dev` 浏览器内调试：同一套页面与 Lua（含 `require`）直接运行，桥与事件可用；设备专属能力可在 `dev/mock.js` 伪造。
- 运行时权限申请；Toast/对话框；沉浸式全屏；屏幕常亮；运行时切换屏幕方向；状态栏颜色；读写剪贴板；震动；系统分享；外部浏览器打开 http(s) 链接。
- 本地通知（`capabilities: ["notify"]`）：通道、显示/取消、点击事件；系统通知权限仍需运行时申请。
- 后台定时任务（`capabilities: ["task"]`）：一次性/周期/每日任务，闹钟 + JobScheduler 触发 `app.on("task")`，重启与改时间后自动重排；无前台界面时仍可运行（best-effort）。
- 原生插件（`capabilities: ["native"]` + `native.plugins`）：C ABI v1 共享库暴露 `native.<module>.<function>`，支持同步与跨线程异步；构建期校验 ELF 架构/16KB 对齐/导出符号/依赖白名单。库要先用 NDK 编译到 `<dir>/lib/<abi>/libberba_<name>.so`，多个插件取 ABI 交集，交集为空即构建失败。插件是受信任代码，绕过 Lua 沙箱；C++ 需静态链接 STL（ABI 细节见上游 [`docs/native-plugins.md`](https://github.com/anccnuer/berba/blob/main/docs/native-plugins.md)，用法见 6.15）。
- 私有文件读写/追加/重命名/删除/stat，键值存储（含 `store.keys`）；读取设备信息。
- 配置应用名、包名、版本、图标、主题（light/dark/device）、屏幕方向、cleartext、meta-data、能力（`capabilities`）。

### 3.2 不能做（不要尝试，也不要向用户承诺）

- **没有任意 Java/Android API**：Lua 无 `luajava`，不能反射、不能调用未封装的系统能力；原生能力只能通过 `native` 能力的 C ABI 显式暴露，不能从 Lua 直接加载 `.so` 或调用 JNI。
- **Lua 库受限**：只有 `base`、`table`、`string`、`math`、`coroutine`、`bit32`；没有 `io`、`os`、`package`、`debug`、`utf8`、`load`、`dofile`、`loadfile`。设备是 Lua 5.2 语义：没有 `//`、位运算运算符、`utf8` 库。**dev 的 fengari 是 Lua 5.3，会接受这些写法；请始终以设备 5.2 为准。**
- **后台能力仅限 `task` 能力**：没有前台服务、推送/FCM、常驻进程或自定义 Service/Receiver/开机脚本；`task` 基于一次性闹钟 + JobScheduler，`am force-stop` 后闹钟被清除，Doze/OEM 策略下为 best-effort。通知只能由 Lua 在进程存活时主动显示（或由 `task` 回调触发），进程被系统回收后不会自行补发。
- **系统集成有限**：`<input type="file">` 走系统文件选择器（SAF），下载/`device.share`/`device.openUrl` 交由系统处理；无自定义 Provider、无前台服务。新窗口/`target="_blank"`、`window.open` 仍被禁用（多窗口已关闭）。
- **网页媒体/定位不可用**：壳未实现 `getUserMedia`（相机/麦克风）与 `navigator.geolocation` 授权，二者不可依赖。`permission.request` 只授予 Manifest 权限，不等于网页 API 可用。剪贴板读写使用 `device.clipboard()` / `device.clipboard(text)`。
- **网页对话框可用但会阻塞页面**：`alert`/`confirm`/`prompt` 映射为原生对话框；`console.*` 写入 Logcat（`adb logcat -s berba`，前缀 `[js]`）。交互复杂时优先用 `ui.*` 或 DOM 弹层。
- **无多 Activity、无自定义 Manifest/资源/主题**：主题只能是 `light`/`dark`/`device` 映射的框架主题。
- **无 AAB/Google Play、无热更新、无 v1 签名、无 iOS**。
- **明文网络默认禁止**：只有 `"cleartext": true` 才允许 `http://`；混合内容永远禁止。
- **远程页面受限**：启动页自身的 origin 会自动加入白名单；页面访问其他远程 origin 时必须写入 `web.allowedOrigins`，否则导航与 JS 桥都会被拒绝。只有白名单页面能使用桥。
- **不要硬编码密钥/令牌**：`www/` 与 `lua/` 会原样进入 APK，可被解包读取。
- **dev 不等于设备**：浏览器运行时是 Lua 5.3、除通知外的权限视为已授予、`notify` 基于 Web Notifications、`task` 不能真正后台调度、`native` 仅在设备可用、`store`/`file` 落在 `localStorage`、`http` 受 CORS 限制。不要在 dev 通过后跳过 `berba build` 上机验证（差异见 6.14）。

## 4. app.json

```json
{
  "name": "示例应用",
  "package": "com.example.demo",
  "versionName": "1.0.0",
  "versionCode": 1,
  "icon": "icon.png",
  "orientation": "auto",
  "theme": "light",
  "cleartext": false,
  "capabilities": ["notify", "task"],
  "permissions": ["CAMERA"],
  "metaData": { "com.example.channel": "internal" },
  "web": {
    "dir": "www",
    "startUrl": "https://appassets.androidhost/index.html",
    "spa": false,
    "allowedOrigins": []
  },
  "lua": { "dir": "lua", "entry": "main.lua" },
  "http": { "allowPrivateNetwork": false },
  "debug": false
}
```

| 字段 | 说明 |
| --- | --- |
| `name` | 应用显示名（`app_name`） |
| `package` | 包名，`^[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z][a-zA-Z0-9_]*)+$`，不得以 `android.`/`com.android.`/`java.`/`javax.` 开头；发布后不可改 |
| `versionName` | 展示版本号 |
| `versionCode` | 整数 ≥ 1；**覆盖升级必须递增** |
| `icon` | PNG/JPG 路径；缺省按应用名首字母生成 |
| `orientation` | `auto` / `portrait` / `landscape`（Manifest 初始方向） |
| `theme` | `light` / `dark` / `device` |
| `cleartext` | 是否允许明文 HTTP，默认 `false` |
| `http.allowPrivateNetwork` | 允许 Lua `http` 访问本机/局域网，默认 `false` |
| `capabilities` | 显式启用的能力，当前支持 `notify`、`task`、`native`；`core` 始终启用 |
| `native.plugins` | 原生插件数组 `{ name, dir, config }`；`dir` 默认 `native/<name>` 且必须在项目目录内，库固定为 `<dir>/lib/<abi>/libberba_<name>.so`，`name` 需与插件 `spec.json` 一致 |
| `permissions` | 权限别名或完整 `android.permission.X`；`INTERNET` 自动追加 |
| `metaData` | `<application>` 下的 meta-data，键值均为字符串 |
| `deepLinks` | 自定义 scheme 深链（如 `"myapp://open"`），生成 VIEW intent-filter；声明后 Activity 使用 `launchMode="singleTask"` |
| `web.dir` | 前端目录，默认 `www` |
| `web.startUrl` | 启动页面；本地页用 `https://appassets.androidhost/...` |
| `web.spa` | 无扩展名的本地路径回退到 `index.html` |
| `web.allowedOrigins` | 额外允许的 origin（协议+主机+可选端口，不能带路径） |
| `lua.dir` / `lua.entry` | Lua 目录与入口；`lua.dir` 不存在时 Lua 被禁用（显式配置会告警），目录存在但入口缺失会构建失败 |
| `debug` | WebView 远程调试 + `android:debuggable`；也可用 `berba build --debug` |

常用权限别名（大小写不敏感，`-` 与 `_` 等价）：`INTERNET`、`ACCESS_NETWORK_STATE`、`CAMERA`、`RECORD_AUDIO`、`ACCESS_FINE_LOCATION`、`ACCESS_COARSE_LOCATION`、`ACCESS_BACKGROUND_LOCATION`、`READ_EXTERNAL_STORAGE`、`WRITE_EXTERNAL_STORAGE`、`READ_MEDIA_IMAGES`、`READ_MEDIA_VIDEO`、`READ_MEDIA_AUDIO`、`POST_NOTIFICATIONS`、`VIBRATE`、`WAKE_LOCK`、`BLUETOOTH`、`BLUETOOTH_CONNECT`、`NFC`、`CALL_PHONE`、`READ_CONTACTS`、`READ_CALENDAR`、`BODY_SENSORS`、`ACTIVITY_RECOGNITION`、`SYSTEM_ALERT_WINDOW`。未识别别名会构建失败，其他权限写完整名称。

## 5. JS API

shim 由壳自动注入（本地 HTML 自动插入 `<script src="https://appassets.androidhost/__berba.js">`，远程白名单页在 `onPageStarted` 再注入一次）。**不要在页面里手动引入 shim，也不要重定义 `window.berba`。**

`berba dev` 由开发服务器注入 `/__berba.js`，页面代码无需改动；调试用的 mock 写在 `dev/mock.js`（`window.__berbaMock`），同名方法优先于 Lua，且不会进入 APK。不要把调试 mock 写进 `www/`。

```js
// 调用 Lua 注册的方法，返回 Promise
const res = await window.berba.call('hello', { user: 'a' });
console.log(res.message);

// 订阅 Lua → JS 事件；on 返回传入的函数，可直接传给 off
function onTick(data) { console.log(data.count); }
window.berba.on('tick', onTick);
window.berba.off('tick', onTick);
```

- `berba.call(name, args)`：`args` 必须是可 `JSON.stringify` 的值，序列化后 ≤ 1MB；`undefined` 视为 `null`。
- Lua 抛错或方法未注册时 Promise reject，`err.message` 为错误文本。
- Lua 返回 `nil` → resolve `null`；Lua 表按 JSON 规则转换。
- 当前页面 origin 不在白名单时，调用被静默丢弃，**Promise 永远不会 settle**；不要依赖超时判断业务错误。
- token 只存在于 shim 闭包，`window.berba.token` 不存在；origin 由原生侧按导航 URL 记录。安全边界是 origin 白名单，不要允许不可信页面。

## 6. Lua API

全局白名单：`app`、`web`、`bridge`、`http`、`file`、`store`、`permission`、`notify`（需能力）、`task`（需能力）、`native`（需能力）、`ui`、`device`、`json`、`log`、`require`。设备上所有回调在单一 Lua 工作线程执行（dev 浏览器为事件循环）；`ui`/`web`/`app.exit`/`device.orientation` 内部自动切主线程。

运行时是进程级单例：入口每个进程只执行一次，Activity 旋转/重建不会重跑。没有前台界面时，`ui`/`web`/`app.exit`/`device.clipboard`/`device.share`/`device.openUrl`/`device.orientation`/`permission.request` 会报“当前没有前台界面”；`file`/`store`/`http`/`permission.check`/`notify.*`/`task.*` 仍可用。Activity 销毁不会取消在途 HTTP。

### 6.1 入口与 require

入口文件每个进程启动时执行一次。`require("a.b")` 依次查找 `assets/lua/a/b.lua`、`assets/lua/a/b/init.lua`，只执行一次并缓存返回值（无返回值时为 `true`）；循环 require 返回 `true`。只能加载 `lua/` 目录内文件。

### 6.2 app

| API | 说明 |
| --- | --- |
| `app.on(event, fn)` | `boot` / `create` / `pause` / `resume` / `destroy` / `back` / `newIntent` / `notification` / `task`；同事件多个处理器按注册顺序执行 |
| `app.exit()` | 结束当前界面；Lua 运行时与后台任务继续存活 |
| `app.info()` | 返回 `{name, package, versionName, versionCode, sdk, platform, foreground}` |

- `boot`：进程启动、入口执行完后触发一次，可能没有前台界面。
- `create`：每次 Activity 绑定后触发；所有处理器执行完，若没有调用 `web.load`，自动加载 `startUrl`。
- `destroy`：当前界面销毁时触发；不结束 Lua 运行时，也不取消在途 HTTP。
- `back`：任一处理器返回 `true` 表示已处理；否则交给 WebView 历史，最后退出。
- `newIntent(url)`：深链唤起（需在 `app.json` 配置 `deepLinks`）。冷启动时在 `create` 之后补发。内容不可信，使用前必须校验。
- `notification(event)`：用户点击本地通知（需 `notify` 能力）。`event = { id, data, coldStart }`；`coldStart` 表示进程此前没有绑定过界面，冷启动在 `create` 之后送达。
- `task(ctx)`：定时任务触发（需 `task` 能力），可能没有前台界面。`ctx = { name, data, scheduledAt, firedAt, late, runId, done }`，单位均为秒；**必须调用 `ctx.done()`**（异步完成时在回调里调用），否则到达 `timeout` 才结束。详见 6.12。

```lua
app.on("back", function()
  if panelOpen then closePanel(); return true end
end)
```

### 6.3 web

| API | 说明 |
| --- | --- |
| `web.load(url)` | 加载页面；origin 不在白名单或明文 HTTP 未开启时直接报错 |
| `web.reload()` / `web.goBack()` | 重载 / 后退 |
| `web.canGoBack()` | 返回 boolean |
| `web.eval(script, cb)` | 执行 JS；`cb(rawJson)` 收到 `evaluateJavascript` 的原始 JSON 文本 |
| `web.on(event, fn)` | `pageStarted(url)` / `pageFinished(url)` / `error(description, url)`，仅主框架 |

```lua
web.on("pageFinished", function(url)
  web.eval("document.title", function(raw)
    local title, err = json.decode(raw)   -- raw 是 JSON 文本，如 "\"标题\""
    if title then log("title", title) end
  end)
end)
```

### 6.4 bridge

| API | 说明 |
| --- | --- |
| `bridge.register(name, fn)` | 注册供 `window.berba.call` 调用的方法；`name` 非空且 ≤ 128 字符 |
| `bridge.emit(name, data)` | 推送事件给 `window.berba.on(name, fn)` |

```lua
bridge.register("add", function(req)
  return { sum = ((req and req.a) or 0) + ((req and req.b) or 0) }
end)
bridge.emit("tick", { count = 3 })
```

### 6.5 http

`http.get(url, callback)`、`http.post(url, body, callback)`、`http.request(opts)`、`http.cancel(id)`。仅 `http/https`；异步；最多 5 跳重定向且逐跳校验；UA 为 `berba`；响应上限 8MB；默认拒绝本机/私网地址（需 `http.allowPrivateNetwork`）；`body` 为字符串或表（表编码为 JSON，`{`/`[` 开头按 `application/json` 发送，否则 `text/plain`）。

`http.request(opts)` 的 `opts`：`url`、`method`（GET/POST/PUT/DELETE/HEAD/OPTIONS，默认 GET）、`headers`、`body`、`timeout`（秒，1-120，默认 15）、`cb`（必填）、`onProgress(read, total)`（可选，total 未知为 -1）。返回请求 ID（字符串），`http.cancel(id)` 断开在途请求并触发 `cb({ ok=false, status=0, error="已取消" })`。

回调结果：`{ ok = bool, status = int, body = string, headers = table, error = string? }`；`ok` 表示 2xx，网络错误 `status = 0` 且带 `error`。明文 HTTP 需 `cleartext: true`。

```lua
http.get("https://example.com/api", function(res)
  if res.ok then log(res.status, res.body) else log(res.error or res.status) end
end)
```

### 6.6 file

路径相对应用私有目录 `filesDir`；绝对路径、`..` 越界、NUL 被拒绝；反斜杠转为 `/`；读写单文件均 ≤ 8MB；`write` 先写临时文件再替换。

| API | 返回 |
| --- | --- |
| `file.read(path)` | 文本；失败 `nil, err` |
| `file.write(path, text)` | `true`；失败 `nil, err`；自动创建父目录 |
| `file.append(path, text)` | `true`；追加，超过 8MB 报错 |
| `file.exists(path)` | boolean（非法路径为 `false`） |
| `file.delete(path)` | 删除；不存在 `false`，失败 `nil, err` |
| `file.rename(from, to)` | `true`；覆盖目标并创建父目录 |
| `file.stat(path)` | `{ size, mtime, dir, file }`；不存在 `nil, err` |
| `file.list(dir)` | 排序后的名称数组；失败 `nil, err` |

```lua
file.write("notes/a.txt", "hello")
file.append("notes/a.txt", "\nworld")
local text = file.read("notes/a.txt")
```

### 6.7 store

`store.get(key)` / `store.set(key, value)` / `store.remove(key)` / `store.keys()`：SharedPreferences，值以 JSON 保存（字符串/数字/布尔/表）；`set(key, nil)` 删除；key 非空且 ≤ 256。`remove` 返回原本是否存在，`keys` 返回排序后的 key 数组。

### 6.8 permission

- `permission.check(name)` → boolean；`name` 可用别名或完整名。
- `permission.request(names, cb)`：`names` 为字符串或数组（≤ 32）；`cb(result)` 的键为完整权限名，值为 boolean。

权限必须先在 `app.json.permissions` 中声明，运行时申请才有意义。

```lua
permission.request({"CAMERA"}, function(r)
  log("camera", tostring(r["android.permission.CAMERA"]))
end)
```

### 6.9 ui

| API | 说明 |
| --- | --- |
| `ui.toast(msg, duration)` | 提示；`"short"`（默认）/`"long"`/毫秒数 |
| `ui.statusBar({ color, light })` | 状态栏颜色（`"#rrggbb"` 等）与图标明暗 |
| `ui.alert(msg)` | 单按钮对话框 |
| `ui.confirm(msg, cb)` | `cb(true/false)` |
| `ui.fullscreen(on)` | 沉浸式全屏开关（`onResume` 自动恢复） |
| `ui.keepScreenOn(on)` | 屏幕常亮（仅当前窗口） |

### 6.10 device

| API | 返回 |
| --- | --- |
| `device.clipboard()` / `device.clipboard(text)` | 读取 / 写入剪贴板 |
| `device.vibrate(ms)` | 震动 1-10000ms，需要 `VIBRATE` 权限 |
| `device.share(text)` / `device.share({ text, url })` | 系统分享面板 |
| `device.openUrl(url)` | 外部浏览器打开 http(s) 链接 |
| `device.orientation(mode)` | 运行时切换：`portrait` / `landscape` / `sensor` / `auto` |
| `device.model()` / `device.sdk()` / `device.platform()` | 型号 / API level / `"android"` |
| `device.locale()` | 如 `zh_CN` |
| `device.screen()` | `{ width, height, density, dpi }` |

### 6.11 notify（需 `capabilities: ["notify"]`）

| API | 说明 |
| --- | --- |
| `notify.show(opts)` / `notify.show(title, body)` | 显示或替换通知；成功返回 id，失败返回 `nil, err` |
| `notify.cancel(id)` / `notify.cancelAll()` | 取消指定 / 全部通知 |
| `notify.enabled()` | 权限与系统通知开关均可用时为 `true` |
| `notify.channel({ id, name, description, importance })` | 创建/更新通道（API 26+；重要性首次创建后不可变） |

`notify.show` 的 `opts`：`id`（string 映射 tag / 正整数映射 notificationId / 缺省自增）、`title`（默认应用名，≤512）、`body`（≤8192）、`channel`（默认 `"default"`）、`data`（随点击事件返回，≤64KB）、`ongoing`、`autoCancel`（默认 `true`）。能力会自动写入 `POST_NOTIFICATIONS`；API 33+ 仍需 `permission.request`，未授权时 `show` 返回 `nil, "通知权限未授予"`。

```lua
notify.channel({ id = "alerts", name = "提醒", importance = "high" })
notify.show({ id = "daily", title = "提醒", body = "点击查看", channel = "alerts",
  data = { page = "today" } })

app.on("notification", function(event)
  log("clicked", event.id, json.encode(event.data), tostring(event.coldStart))
end)
```

点击通知走显式 `MainActivity` Intent，携带每次安装随机 token；伪造或无效 payload 会被丢弃。启用能力后 Activity 自动为 `launchMode="singleTask"`，热启动走 `onNewIntent`。

### 6.12 task（需 `capabilities: ["task"]`）

| API | 说明 |
| --- | --- |
| `task.schedule(name, opts)` | 创建或替换任务；返回信息表，失败 `nil, err` |
| `task.cancel(name)` | 取消任务、闹钟与待运行 Job；返回原本是否存在 |
| `task.list()` | 按名称排序的任务数组 |
| `task.exact()` | 当前是否允许精确闹钟（API 31 以下恒为 `true`） |

名字匹配 `^[A-Za-z_][A-Za-z0-9_.-]{0,63}$`。`opts`：`every`（间隔秒，≥900）/ `daily`（`"HH:MM"`）/ `delay`（一次性秒数）三选一；`data`（随事件返回，≤64KB，明文存储）；`exact`（无权限时自动降级）；`timeout`（10-600 秒，默认 60）；`replace`（默认 `true`，同名且为 `false` 时报错）。最多 64 个任务，`every`/`delay` 上限 30 天。无任务时 `task.list()` 为空表，编码到 JS 是 `{}`，前端用 `Array.isArray` 判断。

触发时 `app.on("task", function(ctx) ... end)` 的 `ctx` 含 `name`、`data`、`scheduledAt`、`firedAt`（epoch 秒）、`late`（晚于计划 60 秒以上）、`runId`、`done()`。**Handler 返回不代表完成**：必须在异步回调里调用 `ctx.done()`，否则等到 `timeout` 才结束；重复调用安全。开机、应用更新、改时间/时区后过期任务只补一次并标记 `late`，不重放积压。`exact` 只影响闹钟精度，JobScheduler 执行仍是 best-effort；force-stop 会取消闹钟，Doze/厂商策略下可能延迟。

```lua
task.schedule("sync", { every = 900, data = { since = 123 } })

app.on("task", function(ctx)
  if ctx.name == "sync" then
    http.request({ url = "https://example.com/sync", cb = function() ctx.done() end })
  else
    ctx.done()
  end
end)
```

### 6.13 widget（需 `capabilities: ["widget"]`）

| API | 说明 |
| --- | --- |
| `widget.update({ title, cards })` | 写入缓存并刷新所有实例；`cards` 为 `{ title, value, subtitle, color }` 数组，最多 20 张；返回 `{ updatedAt, ids }` |
| `widget.update(cards)` | 省略标题的简写，参数直接作为卡片数组 |
| `widget.clear()` | 清空缓存，小组件显示为占位 |
| `widget.list()` | 已放置实例数组，元素含 `id` 与 `updatedAt`；未放置时为空表 |

桌面小组件固定渲染 5 行卡片（`title` ≤ 24、`value` ≤ 16、`subtitle` ≤ 32 字符，超出以 `…` 截断），`color` 接受 `#RRGGBB` / `#AARRGGBB`。状态存 `SharedPreferences("berba_widget")`，上限 32KB；`WidgetProvider` 只读缓存渲染，**不会启动 Lua**，所以进程被回收或 launcher 重启后仍显示最后一次数据。系统不自动刷新（`updatePeriodMillis=0`），需要定时更新时用 `task` 调用 `widget.update`。点击卡片会打开应用并触发 `widget` 事件（`{ id, coldStart }`，冷启动在 `create` 之后）。RemoteViews 不能放 WebView、不能动态加行，也不支持可滚动集合与配置 Activity。

```lua
widget.update({
  title = "今日概览",
  cards = {
    { title = "天气", value = "26°", subtitle = "多云" },
    { title = "步数", value = "8,421", subtitle = "目标 10,000", color = "#3DDC84" }
  }
})

app.on("widget", function(ev)
  log("widget", ev.id, tostring(ev.coldStart))
end)
```

### 6.14 json 与 log

```lua
local text = json.encode({ a = 1 })      -- '{"a":1}'
local t, err = json.decode(text)          -- 失败返回 nil, err
log("tag", 123)                            -- Logcat，tag = berba
```

JSON 转换：键为 `1..n` 连续整数的表编码为数组，否则为对象（空表为 `{}`）；`nil` → `null`；函数/userdata 编码为 `null`；存在循环引用或嵌套超过 64 层时整个结果为 `"null"`。

### 6.15 dev 浏览器运行时

`berba dev` 在项目启用 Lua 时用 fengari 执行 `lua.entry`（Lua 5.3），设备壳是 LuaJ（Lua 5.2）。`require`、`bridge.register`、`bridge.emit`、`app.on("create")`、`web.on("pageFinished")` 行为一致；入口抛错时 pending 的 `berba.call` 会 reject。

| 能力 | 浏览器行为 |
| --- | --- |
| 能力开关 | 模块与事件白名单来自 `app.json` 的 `capabilities`：未声明 `notify`/`task`/`native` 时对应的表不存在，`app.on("notification")`/`app.on("task")` 报未知事件（与设备一致） |
| `app.info()` | `platform = "browser"`、`sdk = 0`，其余来自 `app.json` |
| `app.on("back")` | 浏览器拦不到返回键，用 `window.__berbaDev.back()` 手动触发 |
| `app.on("newIntent")` | 不触发 |
| `app.on("notification")` | 需 `notify` 能力；浏览器通知点击时触发；页面关闭后不再回调，可用 `window.__berbaDev.notify(id, data)` 手动触发 |
| `app.on("task")` | 需 `task` 能力；可以注册，但浏览器不会自动触发；需在设备上验证 |
| `task.*` | `schedule` 直接报「浏览器不支持后台任务」；`cancel` 返回 `false`；`list` 为空数组；`exact` 为 `false` |
| `native.*` | `native.list()` 返回 `app.json` 声明的插件/模块/函数（`ok = false`），插件函数抛「原生插件仅在设备上可用」；用 `dev/mock.js` 伪造结果 |
| `pause` / `resume` | 绑定页面 `visibilitychange` |
| `permission.*` | 除 `POST_NOTIFICATIONS` 使用浏览器权限外，其余视为已授权 |
| `notify.*` | 基于 Web Notifications：`channel` 校验参数但不生效；`permission.request("POST_NOTIFICATIONS")` 会弹浏览器授权（通常需要用户手势，建议由按钮触发） |
| `store` / `file` | 落在 `localStorage`，清站点数据即丢失；单文件仍限 8MB |
| `http.*` | 受 CORS 限制；明文 `http://` 不拦截；`onProgress` 的总长可能为 `-1` |
| `ui.statusBar` | no-op |
| `ui.fullscreen` / `ui.keepScreenOn` | 需要用户手势 / Wake Lock（https 或 localhost） |
| `device.clipboard()` | 读取返回空串；写入正常 |
| `device.share` / `device.orientation` | 仅部分浏览器可用，失败时打印警告 |
| Lua 版本 | 5.3、32 位整数、已补 `bit32`；`math.type` 等 5.3 专有特性在设备上不可用 |

- `--host 0.0.0.0` 会把项目源码与 Lua 执行能力暴露到局域网，仅真机联调时临时使用，用完改回 `127.0.0.1`。
- 浏览器通过不代表设备通过：提交前仍需 `berba build` 并上机验证权限、文件、方向与全屏。

### 6.16 native（需 `capabilities: ["native"]`）

模块与函数名来自插件 `spec.json`，构建期合并进能力资产；未声明的模块/函数不可见。

| 形式 | 说明 |
| --- | --- |
| `local v, err = native.<module>.<fn>(args)` | 同步；参数/结果均为 JSON，成功返回解析后的 Lua 值，失败返回 `nil, err` |
| `native.<module>.<fn>(args, cb)` | 异步（`spec.json` 标记 `async`）；`cb(result, err)` 在 Lua 线程执行 |
| `native.list()` | 已加载插件数组：`name`、`lib`、`ok`、`error`（失败时）与 `modules`（函数及 `async` 标记） |

```lua
local value, err = native.echo.greet({ name = "berba" })
native.echo.greet_later({ name = "berba" }, function(result, e)
  if e then log("失败: " .. e) else log(json.encode(result)) end
end)
```

- 上限：参数 1MB、结果 8MB、`config` 64KB；异步并发 256、超时 30 秒。同步调用在 Lua 线程执行，插件必须快速返回，耗时工作要声明 `async`。
- 插件在应用启动时加载；单个插件加载或 `init` 失败时，它的函数被替换为抛错桩（`原生插件 <name> 加载失败: ...`），其他插件照常可用。没有启用 `native` 能力的项目里 `native` 表根本不存在，用 `if native then ... end` 兜底。
- 修改插件源码后必须重新编译 `<dir>/lib/<abi>/`，`berba build` 不会替你调用 NDK。
- 插件是受信任的原生代码，与壳同进程、拥有应用全部权限；它返回的数据在 Lua/页面侧仍要校验与转义。
- 设备上的 `log` 走 Logcat（tag `berba`），插件内通过宿主 `log` 写入（tag `berba-plugin`）。

## 7. 关键约定与陷阱

1. **数组判定**：Lua 表只有键恰好为 `1..n` 连续整数时才编码为 JSON 数组；空表编码为 `{}`。需要空数组时用 `json.encode` 前自行构造或返回对象。
2. **异步模型**：Lua 单线程，没有 `sleep`/定时器；用 `http` 回调或 JS 侧 `setTimeout` + `berba.call` 实现延时。桥调用是异步的，不要假设顺序。
3. **回调错误**：Lua 回调抛错只会 reject 对应 Promise 或写日志，不会崩溃应用；需要自行 `pcall` 兜底。
4. **`web.eval` 回调拿到的是 JSON 文本**，需 `json.decode`，不是 Lua 值。
5. **页面路径大小写敏感**，全部使用相对路径；`spa: true` 只对无扩展名路径回退 `index.html`。
6. **白名单外的导航被拦截**：外部 `http(s)` 链接若 origin 未列入 `allowedOrigins` 会静默失败；`target="_blank"`/`window.open` 不可用。
7. **安全区域**：targetSdk 35 为 edge-to-edge，页面请使用 `viewport-fit=cover` 与 `env(safe-area-inset-*)` 避免被状态栏/导航栏遮挡。
8. **调试**：先用 `berba dev --open` 在浏览器验证页面与 Lua（占位能力用 `dev/mock.js`）；`berba build --debug` 后可用 `chrome://inspect` 连接 WebView；Lua `log` 输出在 Logcat（tag `berba`）。release 构建无法远程调试，页面 `console` 也不会出现在 Logcat。
9. **升级与身份**：`package` 不可修改；覆盖升级必须提高 `versionCode`；签名密钥丢失则无法升级，只能卸载重装。
10. **构建即校验**：修改 `app.json`、`www/`、`lua/` 后必须运行 `berba build`；构建会自检签名、包名与 versionCode，并输出 SHA256。
11. **Lua 版本以设备为准**：dev 浏览器是 5.3，可能放过设备 5.2 不支持的写法（`//`、位运算符、`math.type` 等）；`bit32` 两端都有，但不要依赖 5.3 语法。开发阶段请同时跑 `berba build` 验证。

## 8. 修改后的验证清单

1. `berba dev --open` 打开页面：`berba.call` 往返、`bridge.emit` 推送、`pageFinished` 事件正常，控制台没有「未实现/未注册」的桥调用。
2. `berba build` 成功，输出 APK 与 SHA256。
3. 可选 `berba doctor` 确认工具链完整。
4. 安装后检查：启动页加载、`berba.call` 往返、`bridge.emit` 推送、`pageFinished` 事件、返回键行为。
5. 涉及权限时：首次申请、拒绝后再次申请、未声明权限的负例。
6. 涉及通知时：API 33+ 授权/拒绝、通道、热启动与冷启动点击、取消；确认点击只触发一次。
7. 涉及定时任务时：`delay` 触发一次、`every` 自动重排、`daily` 按本地时间；异步回调中 `ctx.done()`、超时结束、重复 `done`、运行中 `cancel`；重启/改时间后任务仍在。
8. 涉及存储/文件时：重启应用后数据仍在；路径越界被拒。
9. 涉及全屏/常亮/方向时：切换应用再回来状态正确。
10. 涉及原生插件时：用 NDK 重新编译 `<dir>/lib/<abi>/` 与 `payload/native/` 后 `berba build`；上机确认 `native.list()` 中插件 `ok = true`、同步调用返回、异步回调恰好一次、init 失败插件的调用报出错误文本，且其他插件仍可用。浏览器里只能验证元数据可见性（见 6.14）。

## 9. 安全要求

- 不在 `www/`、`lua/`、`app.json` 中写入密钥、令牌或隐私数据（APK 可解包）。
- 不通过 `allowedOrigins` 放行不可信站点；不无故开启 `cleartext`。
- 不尝试绕过桥的 origin/token 校验，不把 `__berbaNative` 暴露给非白名单页面。
- 所有文件访问保持相对 `filesDir` 的路径，拒绝用户输入中的 `..` 与绝对路径。
