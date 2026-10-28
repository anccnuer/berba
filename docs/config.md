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
  "permissions": ["CAMERA", "ACCESS_FINE_LOCATION"],
  "metaData": { "com.example.channel": "internal" },
  "web": {
    "dir": "www",
    "startUrl": "https://appassets.androidhost/index.html",
    "spa": true,
    "allowedOrigins": []
  },
  "lua": { "dir": "lua", "entry": "main.lua" },
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
| `icon` | string | 空 | PNG/JPG 路径；为空时按应用名首字母生成默认图标 |
| `orientation` | string | `auto` | `auto` / `portrait` / `landscape` |
| `theme` | string | `light` | `light` / `dark` / `device`，映射到 Android 框架主题 |
| `cleartext` | bool | `false` | 是否允许明文 HTTP（不推荐） |
| `permissions` | string[] | `[]` | 权限别名或完整 `android.permission.X`；`INTERNET` 会自动追加 |
| `metaData` | object | `{}` | 写入 `<application>` 的 `<meta-data>`，键值均为字符串 |
| `web.dir` | string | `www` | 前端资源目录（相对项目根目录） |
| `web.startUrl` | string | `https://appassets.androidhost/index.html` | 启动页面 |
| `web.spa` | bool | `false` | 本地路由无扩展名时回退到 `index.html` |
| `web.allowedOrigins` | string[] | `[]` | 额外允许的 origin（用于远程页面、导航与 JS 桥） |
| `lua.dir` | string | `lua` | Lua 目录（相对项目根目录） |
| `lua.entry` | string | `main.lua` | Lua 入口，相对 `lua.dir` |
| `debug` | bool | `false` | 开启 WebView 远程调试与 `android:debuggable` |

也可以构建时用 `berba build --debug` 临时开启调试，无需修改配置。

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

## 启动页面与 origin

**本地页面**：把 `web.startUrl` 设为 `https://appassets.androidhost/...`。该域名由 WebView 拦截并映射到 `assets/www/`，不会产生网络请求；入口文件必须存在于 `www.dir` 中，路径越界会被拒绝。本地 origin 始终在白名单内。

**远程页面**：`web.startUrl` 可以是 `https://` 地址；使用 `http://` 时必须同时设置 `"cleartext": true`。远程页面的 origin 必须加入 `web.allowedOrigins`，否则导航与 JS 桥都会被拒绝：

```json
{
  "web": {
    "startUrl": "https://app.example.com/index.html",
    "allowedOrigins": ["https://app.example.com", "https://api.example.com"]
  }
}
```

`allowedOrigins` 只接受 origin（协议 + 主机 + 可选端口），不能带路径；统一转为小写去重。

## Lua

若 `lua.dir` 存在且入口文件可读，构建时会把整个目录写入 APK 的 `assets/lua/`，并启用 Lua 运行时。没有 Lua 目录时应用照常启动，只是 `window.berba.call` 会返回“未注册的桥接方法”。

`lua.entry` 与 `require` 的路径都会被规范化，禁止通过 `../` 越出 `lua.dir`。
