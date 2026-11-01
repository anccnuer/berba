---
sidebar_position: 1
---

# berba 简介

**把网页装进 APK，把灵感留在掌心。**

berba 是一个轻量的 Android 应用构建器：用 HTML/CSS/JS 写界面，用 Lua 写逻辑，一条命令产出可安装的 APK。终端用户不需要 JDK、Android SDK 或 Android Studio——解压发行包即可开始。

## 它适合做什么

- 小工具与效率应用：计算器、清单、笔记、快捷键面板；
- 展示与互动页面：应援横幅、卡片集、相册（Web 的排版能力直接复用）；
- 带后台节奏的轻应用：定时任务、本地通知、桌面小组件；
- 需要触碰原生能力时：C ABI v1 插件体系，构建期 ELF 校验。

它不适合：需要前台服务、推送、复杂动画引擎或 Google Play/AAB 的重型应用。

## 30 秒上手

```bat
berba.exe init myapp --package com.example.myapp
cd myapp
..\berba.exe dev --open     :: 浏览器里改页面、改 Lua，刷新即见
..\berba.exe build          :: 产出 dist\<package>-<version>.apk
..\berba.exe install        :: adb 安装到已连接设备
```

Linux/macOS 把 `berba.exe` 与反斜杠相应替换；`berba doctor` 可随时体检工具链。

## 能力一览

| 能力 | 说明 | 入口 |
| --- | --- | --- |
| `core` | 永远启用：WebView、桥、`file`/`store`/`http`/`permission`/`ui`/`device` | [Lua API](lua-api.md) |
| `notify` | 本地通知、通道、冷/热点击事件 | [lua-api.md#notify](lua-api.md#notify) |
| `task` | `AlarmManager` + `JobScheduler` 定时任务，无界面执行 | [lua-api.md#task](lua-api.md#task) |
| `widget` | 桌面卡片小组件，Lua 推数据、缓存渲染 | [lua-api.md#widget](lua-api.md#widget) |
| `native` | C ABI v1 原生插件，同步/跨线程异步 | [native-plugins.md](native-plugins.md) |

可选能力必须在 `app.json` 的 `capabilities` 中显式声明，未声明即不存在——边界写进清单，而不是运行时猜。

## 文档地图

| 我想…… | 去读 |
| --- | --- |
| 构建、`dev`/`watch` 与故障排查 | [构建与调试](build.md) |
| 写 `app.json`，搞清每个字段 | [配置参考](config.md) |
| 查 Lua API 与 JS 桥协议 | [Lua API](lua-api.md) |
| 看能力与 API 的机器可读清单 | [能力清单](capabilities.md) |
| 把旧项目迁到能力开关之后 | [迁移指南](migration.md) |
| 写 C 原生插件 | [原生插件](native-plugins.md) |
| 了解安全边界 | [安全模型](security.md) |
| 发布前逐项验收 | [测试与验收](testing.md) |

示例工程在仓库 [`examples/`](https://github.com/anccnuer/berba/tree/main/examples)：`hello`、`led-banner`、`notify`、`task`、`native-echo`、`widget`，每个都可直接 `berba build`。

> [!NOTE]
> 仓库 `docs/` 是文档唯一真源；官网（独立仓库 [berba-website](https://github.com/anccnuer/berba-website)，Docusaurus）通过 `npm run sync-docs` 从本目录同步副本，请勿在官网仓库里直接改文档。
