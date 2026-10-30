# app.json 配置

`app.json` 位于项目根目录。`berba init` 会生成一份可运行的默认配置。

```json
{
  "name": "示例应用",
  "package": "com.example.demo",
  "versionName": "1.0.0",
  "versionCode": 1,
  "icon": "icon.png",
  "orientation": "portrait",
  "theme": "light",
  "cleartext": false,
  "capabilities": ["notify"],
  "permissions": ["CAMERA", "ACCESS_FINE_LOCATION"],
  "metaData": { "com.example.channel": "internal" },
  "deepLinks": ["myapp://open"],
  "web": {
    "dir": "www",
    "startUrl": "https://appassets.androidhost/index.html",
    "spa": true,
    "allowedOrigins": []
  },
  "lua": { "dir": "lua", "entry": "main.lua" },
  "http": { "allowPrivateNetwork": false },
  "debug": false
}
```

## 字段

| 字段 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `name` | string | 必填 | 应用显示名称，写入 `strings.xml` 的 `app_name` |
| `package` | string | 必填 | 应用包名，同时作为 APK 的 applicationId |
| `versionName` | string | 必填 | 版本名，显示给用户 |
| `versionCode` | int | `1` | 版本号，必须 ≥ 1；升级时递增 |
| `icon` | string | 空 | PNG/JPG 路径；为空时按应用名生成一个默认图标 |
| `orientation` | string | `auto` | `auto` / `portrait` / `landscape` |
| `theme` | string | `light` | `light` / `dark` / `device`，映射到 Android 框架主题 |
| `cleartext` | bool | `false` | 是否允许明文 HTTP（不推荐） |
| `capabilities` | string[] | `[]` | 显式启用的能力，当前支持 `notify`、`task`、`native`；`core` 始终启用，最多 32 个 |
| `permissions` | string[] | `[]` | 权限别名或完整 `android.permission.X`；`INTERNET` 会自动追加 |
| `metaData` | object | `{}` | 写入 `<application>` 的 `<meta-data>`，键值均为字符串 |
| `deepLinks` | string[] | `[]` | 自定义 scheme 深链，如 `"myapp://open"`；生成 VIEW intent-filter 并启用 `launchMode="singleTask"` |
| `web.dir` | string | `www` | 前端资源目录（相对项目根目录，不得越出项目根目录） |
| `web.startUrl` | string | `https://appassets.androidhost/index.html` | 启动页面 |
| `web.spa` | bool | `false` | 本地路由无扩展名时回退到 `index.html` |
| `web.allowedOrigins` | string[] | `[]` | 额外允许的 origin（用于远程页面、导航与 JS 桥） |
| `lua.dir` | string | `lua` | Lua 目录（相对项目根目录，不得越出项目根目录） |
| `lua.entry` | string | `main.lua` | Lua 入口，相对 `lua.dir` |
| `http.allowPrivateNetwork` | bool | `false` | 允许 Lua `http` 访问本机、局域网与链路本地地址 |
| `debug` | bool | `false` | 开启 WebView 远程调试与 `android:debuggable` |

也可以构建时用 `berba build --debug` 临时开启调试，无需修改配置。debug 产物会命名为 `<package>-<versionName>-debug.apk`，不会覆盖 release 产物。

配置使用严格 JSON 解析：未知字段（包括拼写错误）会直接导致构建失败。

## 包名

必须匹配 `^[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z][a-zA-Z0-9_]*)+$`，且不能以 `android.`、`com.android.`、`java.`、`javax.` 开头。包名决定安装身份，发布后不要修改。

## 图标

图标会被居中裁成正方形，再缩放到 mdpi/hdpi/xhdpi/xxhdpi/xxxhdpi（48/72/96/144/192 px），写入 `mipmap-*` 并由 Manifest 引用 `@mipmap/ic_launcher`。同时生成 API 26+ 的自适应图标（`mipmap-anydpi-v26`，前景取图标中心 72dp 安全区，背景取图标边缘平均色）。建议提供 512×512 或更大的方形 PNG。

## 权限

`permissions` 支持完整名称：

```json
{ "permissions": ["android.permission.CAMERA", "android.permission.VIBRATE"] }
```

也支持以下内置别名（大小写不敏感，`-` 与 `_` 等价）：

| 别名 | 完整权限 |
| --- | --- |
| `INTERNET` | `android.permission.INTERNET` |
| `ACCESS_NETWORK_STATE` | `android.permission.ACCESS_NETWORK_STATE` |
| `CAMERA` | `android.permission.CAMERA` |
| `RECORD_AUDIO` | `android.permission.RECORD_AUDIO` |
| `ACCESS_FINE_LOCATION` | `android.permission.ACCESS_FINE_LOCATION` |
| `ACCESS_COARSE_LOCATION` | `android.permission.ACCESS_COARSE_LOCATION` |
| `ACCESS_BACKGROUND_LOCATION` | `android.permission.ACCESS_BACKGROUND_LOCATION` |
| `READ_EXTERNAL_STORAGE` | `android.permission.READ_EXTERNAL_STORAGE` |
| `WRITE_EXTERNAL_STORAGE` | `android.permission.WRITE_EXTERNAL_STORAGE` |
| `READ_MEDIA_IMAGES` | `android.permission.READ_MEDIA_IMAGES` |
| `READ_MEDIA_VIDEO` | `android.permission.READ_MEDIA_VIDEO` |
| `READ_MEDIA_AUDIO` | `android.permission.READ_MEDIA_AUDIO` |
| `POST_NOTIFICATIONS` | `android.permission.POST_NOTIFICATIONS` |
| `VIBRATE` | `android.permission.VIBRATE` |
| `WAKE_LOCK` | `android.permission.WAKE_LOCK` |
| `BLUETOOTH` | `android.permission.BLUETOOTH` |
| `BLUETOOTH_CONNECT` | `android.permission.BLUETOOTH_CONNECT` |
| `NFC` | `android.permission.NFC` |
| `CALL_PHONE` | `android.permission.CALL_PHONE` |
| `READ_CONTACTS` | `android.permission.READ_CONTACTS` |
| `READ_CALENDAR` | `android.permission.READ_CALENDAR` |
| `BODY_SENSORS` | `android.permission.BODY_SENSORS` |
| `ACTIVITY_RECOGNITION` | `android.permission.ACTIVITY_RECOGNITION` |
| `SYSTEM_ALERT_WINDOW` | `android.permission.SYSTEM_ALERT_WINDOW` |

未识别的别名会直接报错；需要其他权限时请写完整的 `android.permission.X`（仅限大写字母、数字、下划线）。

运行时申请权限请使用 Lua 的 `permission.request`，见 [lua-api.md](lua-api.md)。

## 能力

`capabilities` 按需启用超出核心 API 的系统能力：

```json
{ "capabilities": ["notify"] }
```

| 能力 | 说明 |
| --- | --- |
| `notify` | 本地通知：自动加入 `POST_NOTIFICATIONS`、生成 `drawable-*/ic_stat_berba.png` 小图标，并让 Activity 使用 `launchMode="singleTask"` |
| `task` | 后台定时任务：自动加入 `RECEIVE_BOOT_COMPLETED`、`SCHEDULE_EXACT_ALARM`，注册 `TaskJobService`、`TaskAlarmReceiver`、`TaskBootReceiver`；需要 `lua` 目录 |
| `native` | 原生插件：加载 `native.plugins` 声明的 C ABI 共享库，设置 `extractNativeLibs="true"`；需要 `lua` 目录 |
| `widget` | 桌面小组件：注册 `WidgetProvider`，生成 `layout/berba_widget.xml`、`xml/berba_widget_info.xml` 与圆角背景；需要 `lua` 目录 |

能力名必须匹配 `^[a-z][a-z0-9_]{0,31}$`，重复项自动去重；`core` 无需也无法写入。未实现的能力名会导致构建失败。能力会自动合并 Manifest 权限、组件与资源，无需在 `permissions` 中重复声明这些权限。完整 API 见 [lua-api.md](lua-api.md#notify) 与自动生成的 [capabilities.md](capabilities.md)。`notify`、`task`、`native` 与 `widget` 都要求 Lua 运行时。

## 原生插件

启用 `native` 能力后，可在 `native.plugins` 中声明 C/C++/Rust 插件：

```json
{
  "capabilities": ["native"],
  "native": {
    "plugins": [
      {
        "name": "echo",
        "dir": "native/echo",
        "config": { "greeting": "hi" }
      }
    ]
  }
}
```

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `name` | string | 必填 | `^[a-z][a-z0-9_]{0,31}$`，不能为 `jni`；库名为 `libberba_<name>.so` |
| `dir` | string | `native/<name>` | 插件目录，必须在项目内；符号链接逃逸会被拒绝 |
| `config` | object | `{}` | 传给 `berba_plugin_init` 的内联 JSON，上限 64KB；会进入 APK，**不要放密钥** |

- 声明 `native.plugins` 时必须同时启用 `native` 能力；启用 `native` 但没有插件只会告警。
- 每个插件目录需要 `spec.json` 与 `lib/<abi>/libberba_<name>.so`；支持 `arm64-v8a`、`armeabi-v7a`、`x86_64`，APK 取所有插件的 ABI 交集。
- C++ 插件必须静态链接 STL；`DT_NEEDED` 仅允许 `libc.so`、`libm.so`、`libdl.so`、`liblog.so`。
- 完整 ABI、构建与示例见 [native-plugins.md](native-plugins.md)。

## 深链

`deepLinks` 声明自定义 scheme，供其他应用或浏览器唤起本应用：

```json
{ "deepLinks": ["myapp://open", "notes://view/doc"] }
```

- 每条链接解析出 `scheme` 与 `host`，按 `scheme+host` 生成 `<intent-filter>`（VIEW + DEFAULT + BROWSABLE）；`host` 为空时匹配该 scheme 的全部链接。
- scheme 必须以小写字母开头（仅小写字母、数字、`+ . -`），禁止 `http/https/file/content/data`；重复条目自动去重，最多 16 条。
- 声明深链后 Activity 自动使用 `launchMode="singleTask"`，保证第二次唤起走 `onNewIntent`（启用 `notify` 能力同样会开启该模式）。
- Lua 侧通过 `app.on("newIntent", fn)` 收到完整 URL 字符串（冷启动时会在 `create` 之后补发）。深链内容不可信，务必校验后再使用。

## 启动页面与 origin

**本地页面**：把 `web.startUrl` 设为 `https://appassets.androidhost/...`。该域名由 WebView 拦截并映射到 `assets/www/`，不会产生网络请求；入口文件必须存在于 `www.dir` 中，路径越界会被拒绝。使用本地启动页时，本地 origin 会自动加入白名单。

**远程页面**：`web.startUrl` 可以是 `https://` 地址；使用 `http://` 时必须同时设置 `"cleartext": true`。启动页自身的 origin 会自动加入白名单；页面需要访问其他 origin（API、iframe 等）时，仍需写入 `web.allowedOrigins`：

```json
{
  "web": {
    "startUrl": "https://app.example.com/index.html",
    "allowedOrigins": ["https://app.example.com", "https://api.example.com"]
  }
}
```

`allowedOrigins` 只接受 origin（协议 + 主机 + 可选端口），不能带路径；统一转为小写去重。

## HTTP

Lua 的 `http.*` 默认拒绝 loopback、局域网、链路本地、CGNAT 等私网地址，并逐跳校验重定向目标。确实需要访问本机服务（例如本地调试网关）时显式开启：

```json
{ "http": { "allowPrivateNetwork": true } }
```

开启后仍受 `cleartext` 约束：`http://` 地址只有在 `"cleartext": true` 时才可访问。

## Lua

若 `lua.dir` 存在且入口文件可读，构建时会把整个目录写入 APK 的 `assets/lua/`，并启用 Lua 运行时。整个 `lua.dir` 不存在时应用照常启动，只是 `window.berba.call` 会返回“未注册的桥接方法”；显式配置了 `lua.dir` 却不存在时会输出警告，目录存在但入口文件缺失会直接构建失败。

`lua.entry` 与 `require` 的路径都会被规范化，禁止通过 `../` 越出 `lua.dir`。
