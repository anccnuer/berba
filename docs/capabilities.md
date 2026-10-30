# 能力规格

> 本文件由 `go generate ./internal/capability` 生成，请勿手动修改。

每个模块声明函数是否异步（`异步`）、是否需要前台界面（`前台`），以及绑定到哪个 Lua 全局表。
`core` 始终安装；其余能力需要在 `app.json` 的 `capabilities` 中显式启用。

## core（core）

内置核心 API：配置、WebView、桥接、HTTP、文件、存储、权限、UI、设备与 JSON。

### `app`

浏览器实现：`full`

| 函数 | 异步 | 前台 | 说明 |
| --- | --- | --- | --- |
| `app.exit` | 否 | 是 | 结束当前界面。 |
| `app.info` | 否 | 否 | 返回应用名称、包名、版本、SDK 与平台信息。 |
| `app.on` | 否 | 否 | 注册应用生命周期事件处理器。 |

### `bridge`

浏览器实现：`full`

| 函数 | 异步 | 前台 | 说明 |
| --- | --- | --- | --- |
| `bridge.emit` | 否 | 是 | 向页面推送事件，对应 JS 的 berba.on。 |
| `bridge.register` | 否 | 否 | 注册供 JS 调用的方法。 |

### `device`

浏览器实现：`partial`

| 函数 | 异步 | 前台 | 说明 |
| --- | --- | --- | --- |
| `device.clipboard` | 否 | 是 | 读取或写入剪贴板。 |
| `device.locale` | 否 | 否 | 返回设备语言，例如 zh_CN。 |
| `device.model` | 否 | 否 | 返回设备型号。 |
| `device.openUrl` | 否 | 是 | 用外部浏览器或应用打开 http/https 链接。 |
| `device.orientation` | 否 | 是 | 运行时切换界面方向。 |
| `device.platform` | 否 | 否 | 返回固定值 android。 |
| `device.screen` | 否 | 否 | 返回屏幕宽高、密度与 DPI。 |
| `device.sdk` | 否 | 否 | 返回 API level。 |
| `device.share` | 否 | 是 | 打开系统分享面板。 |
| `device.vibrate` | 否 | 否 | 震动指定毫秒数，需要 VIBRATE 权限。 |

### `file`

浏览器实现：`partial`

| 函数 | 异步 | 前台 | 说明 |
| --- | --- | --- | --- |
| `file.append` | 否 | 否 | 追加写入文件，单文件上限 8MB。 |
| `file.delete` | 否 | 否 | 删除文件或空目录。 |
| `file.exists` | 否 | 否 | 判断路径是否存在。 |
| `file.list` | 否 | 否 | 返回排序后的目录项名称。 |
| `file.read` | 否 | 否 | 读取文本文件，单文件上限 8MB。 |
| `file.rename` | 否 | 否 | 重命名或覆盖移动文件。 |
| `file.stat` | 否 | 否 | 返回文件大小、修改时间与类型。 |
| `file.write` | 否 | 否 | 原子写入文本文件，自动创建父目录。 |

### `http`

浏览器实现：`partial`

| 函数 | 异步 | 前台 | 说明 |
| --- | --- | --- | --- |
| `http.cancel` | 否 | 否 | 取消在途请求。 |
| `http.get` | 是 | 否 | 发起 GET 请求，结果通过回调返回。 |
| `http.post` | 是 | 否 | 发起 POST 请求，结果通过回调返回。 |
| `http.request` | 是 | 否 | 发起完整形态的 HTTP 请求并返回请求 ID。 |

### `json`

浏览器实现：`full`

| 函数 | 异步 | 前台 | 说明 |
| --- | --- | --- | --- |
| `json.decode` | 否 | 否 | 解析 JSON 文本，失败返回 nil 与错误信息。 |
| `json.encode` | 否 | 否 | 把 Lua 值编码为 JSON 文本。 |

### `permission`

浏览器实现：`partial`

| 函数 | 异步 | 前台 | 说明 |
| --- | --- | --- | --- |
| `permission.check` | 否 | 否 | 检查运行时权限是否已授予。 |
| `permission.request` | 是 | 是 | 申请运行时权限，结果通过回调返回。 |

### `store`

浏览器实现：`partial`

| 函数 | 异步 | 前台 | 说明 |
| --- | --- | --- | --- |
| `store.get` | 否 | 否 | 读取键值，值以 JSON 保存。 |
| `store.keys` | 否 | 否 | 返回排序后的全部 key。 |
| `store.remove` | 否 | 否 | 删除键并返回原本是否存在。 |
| `store.set` | 否 | 否 | 写入或删除键值。 |

### `ui`

浏览器实现：`stub`

| 函数 | 异步 | 前台 | 说明 |
| --- | --- | --- | --- |
| `ui.alert` | 否 | 是 | 显示单按钮对话框。 |
| `ui.confirm` | 是 | 是 | 显示确认对话框，结果通过回调返回。 |
| `ui.fullscreen` | 否 | 是 | 隐藏或恢复系统栏。 |
| `ui.keepScreenOn` | 否 | 是 | 保持当前窗口屏幕常亮。 |
| `ui.statusBar` | 否 | 是 | 设置状态栏颜色与图标明暗。 |
| `ui.toast` | 否 | 是 | 显示短提示或长提示。 |

### `web`

浏览器实现：`partial`

| 函数 | 异步 | 前台 | 说明 |
| --- | --- | --- | --- |
| `web.canGoBack` | 否 | 是 | 返回 WebView 是否可后退。 |
| `web.eval` | 是 | 是 | 执行 JS，结果通过可选回调返回。 |
| `web.goBack` | 否 | 是 | WebView 后退。 |
| `web.load` | 否 | 是 | 加载白名单内的页面。 |
| `web.on` | 否 | 是 | 注册 WebView 页面事件处理器。 |
| `web.reload` | 否 | 是 | 重新加载当前页面。 |

### 全局函数

| 函数 | 异步 | 前台 | 说明 |
| --- | --- | --- | --- |
| `log` | 否 | 否 | 输出日志到 Logcat，tag 为 berba。 |

### 事件

| 事件 | 归属 | 前台 | 说明 |
| --- | --- | --- | --- |
| `back` | `app` | 是 | 返回键；返回 true 表示已处理。 |
| `boot` | `app` | 否 | 进程启动后触发一次；Stage 2 起由进程级运行时支持。 |
| `create` | `app` | 是 | 界面创建后触发。 |
| `destroy` | `app` | 是 | 界面销毁前触发。 |
| `error` | `web` | 是 | 主框架加载失败。 |
| `newIntent` | `app` | 是 | 深链唤起。 |
| `pageFinished` | `web` | 是 | 主框架加载完成。 |
| `pageStarted` | `web` | 是 | 主框架开始加载。 |
| `pause` | `app` | 是 | 界面进入后台。 |
| `resume` | `app` | 是 | 界面回到前台。 |

## native（capability）

原生插件：C/C++/Rust 共享库，稳定 C ABI，构建期 ELF 校验。

需要 Lua 运行时：是

### `native`

浏览器实现：`stub`

| 函数 | 异步 | 前台 | 说明 |
| --- | --- | --- | --- |
| `native.list` | 否 | 否 | 返回已加载插件及其模块与函数；插件模块以 native.<module> 暴露。 |

## notify（capability）

本地通知：显示、取消、通道与点击事件。

需要 Lua 运行时：是

权限：

- `android.permission.POST_NOTIFICATIONS`

生成资源：

- `notification_icon`

### `notify`

浏览器实现：`partial`

| 函数 | 异步 | 前台 | 说明 |
| --- | --- | --- | --- |
| `notify.cancel` | 否 | 否 | 取消指定通知。 |
| `notify.cancelAll` | 否 | 否 | 取消本应用的全部通知。 |
| `notify.channel` | 否 | 否 | 创建或更新通知通道（API 26+）。 |
| `notify.enabled` | 否 | 否 | 返回通知权限与系统开关是否可用。 |
| `notify.show` | 否 | 否 | 显示或替换通知；无权限时返回 nil 与错误信息。 |

### 事件

| 事件 | 归属 | 前台 | 说明 |
| --- | --- | --- | --- |
| `notification` | `app` | 是 | 用户点击通知；冷启动在 create 之后触发。 |

## task（capability）

后台定时任务：间隔、每日、延时触发，开机与改时间重排，无界面分发。

需要 Lua 运行时：是

权限：

- `android.permission.RECEIVE_BOOT_COMPLETED`
- `android.permission.SCHEDULE_EXACT_ALARM`

Manifest 组件：

- `task_alarm_receiver`
- `task_boot_receiver`
- `task_job_service`

### `task`

浏览器实现：`stub`

| 函数 | 异步 | 前台 | 说明 |
| --- | --- | --- | --- |
| `task.cancel` | 否 | 否 | 取消任务并移除闹钟；返回任务是否存在。 |
| `task.exact` | 否 | 否 | 当前是否允许使用精确闹钟。 |
| `task.list` | 否 | 否 | 返回按名称排序的全部任务。 |
| `task.schedule` | 否 | 否 | 创建或替换定时任务，返回下一次触发时间与 runId。 |

### 事件

| 事件 | 归属 | 前台 | 说明 |
| --- | --- | --- | --- |
| `task` | `app` | 否 | 后台任务触发；调用 ctx.done() 结束本次运行，超时也会结束。 |

## widget（capability）

桌面小组件：卡片式数据展示，进程被回收后仍显示缓存。

需要 Lua 运行时：是

Manifest 组件：

- `widget_provider`

生成资源：

- `widget_drawable`
- `widget_info`
- `widget_layout`
- `widget_preview`

### `widget`

浏览器实现：`stub`

| 函数 | 异步 | 前台 | 说明 |
| --- | --- | --- | --- |
| `widget.clear` | 否 | 否 | 清空缓存并把小组件显示为占位。 |
| `widget.list` | 否 | 否 | 返回已放置的实例 id 与缓存时间。 |
| `widget.update` | 否 | 否 | 写入缓存并渲染卡片；无实例时只缓存。 |

### 事件

| 事件 | 归属 | 前台 | 说明 |
| --- | --- | --- | --- |
| `widget` | `app` | 否 | 点击小组件；冷启动在 create 之后触发。 |

