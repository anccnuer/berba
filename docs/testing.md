# 测试与验收

berba 的 CI 无法连接真机，因此采用**自动化产物校验 + 人工 smoke 清单**两层策略。自动化部分由 `scripts/` 与 GitHub Actions 覆盖（见文末），本文聚焦发布前必须在真机/模拟器上手动走查的清单。

## 何时需要跑人工清单

- 重打 `payload/classes.dex`（壳有任何改动）后，发布前必须整份走查。
- 仅改 Go（`internal/*`）时，至少走查 1（安装启动）、2（桥往返）、6（构建可复现）。
- 修改 `JsBridge`/`MainActivity`/`LuaApis`/`AssetServer` 任一鉴权或网络路径时，3、8、9 为阻断项。

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
- [ ] 退出后 Lua 收到 `destroy`（在其之后再销毁 WebView，无 “released instance” 崩溃）。

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
- [ ] `http.cancel` 能中断在途请求，返回“已取消”；退出时 `cancelAllHttp` 清理。

## 自动化覆盖（无需真机）

| 检查 | 命令 / 位置 |
| --- | --- |
| Go 单测 + 竞态 | `go test -race ./...` |
| 纯 Java 策略类（OriginPolicy/NetworkPolicy/LuaSandbox 等） | `scripts/build-shell.sh` 内 JVM 冒烟，输出 `LuaSandbox OK` / `NetworkPolicy OK` |
| 签名与对齐 | `apksigner verify --min-sdk-version 24`、`zipalign -c 4` |
| Manifest 断言 | `aapt2 dump badging` / `aapt2 dump xmltree` 校验包名、版本、权限、cleartext、深链 scheme、launchMode |
| 产物内容 | 解包断言 `assets/www`、`assets/lua`、`app_config.json` 与 `.sha256` 一致 |
| 可复现 | CI 连续构建两次比对 SHA-256 |
| 发行包完整性 | `berba doctor` 校验 aapt2/android.jar/dex 哈希 |

## 可选：设备农场

如需自动化真机验证，可将清单 1–5、7–9 接入 Firebase Test Lab 或等价服务，作为**非阻断**补充信号；发布阻断仍以本文的人工清单 + 上表自动化为准。
