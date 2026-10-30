# 迁移说明

本文列出运行结构与能力开关的破坏性变化，以及把旧项目改写到当前壳的样例。逐版本的对外变更见 GitHub Release 页面。

## 谁需要读

- 在能力开关出现前写的、直接调用 `notify.*` / `task.*` / `native.*` 的项目；
- 依赖 `destroy` 做进程级清理（关文件、取消请求、停定时器）的项目；
- 在入口脚本里做「每次打开界面都要重新初始化」的项目。

只写网页和 `bridge` 交互的项目一般不受影响。

## 运行时是进程级单例

Lua 入口在进程生命周期内只执行一次。旋转、切换语言、深色模式等导致的 Activity 重建都不会重跑入口，`app.on("create")` 会再执行一次。

```lua
-- 旧写法：把初始化写在入口顶层，重建时以为会重来
local tick = 0

-- 现写法：入口只做一次，界面级状态放 create，并且可重入
app.on("create", function()
  bridge.emit("page-ready", { tick = tick })
end)
```

`boot` 在入口执行完后触发一次，此时通常还没有前台界面；`create` 每次界面绑定都触发。需要界面（`ui.*`、`web.*`、`device.clipboard`、`device.share`、`device.openUrl`、`device.orientation`、`permission.request`、`app.exit`）的调用必须放在 `create` 之后，否则报「当前没有前台界面，无法调用该 API」。

## `destroy` 不再代表进程退出

`destroy` 现在只表示**当前界面销毁**：Lua 状态继续存活，在途 `http` 不会被取消，`file` / `store` / `notify.*` / `task.*` 仍然可用。

```lua
-- 旧写法：把 destroy 当退出钩子，会误杀还在跑的任务
app.on("destroy", function()
  http.cancel(pendingId)
  store.set("state", dump())
end)

-- 现写法：只做界面级解绑，进程级资源留给 OS 回收
app.on("destroy", function()
  bridge.emit("page-gone", {})
end)

-- 需要结束当前界面时显式调用
app.exit()
```

`app.exit()` 结束当前 Activity（没有前台界面时调用会报「当前没有前台界面，无法调用该 API」），但 Lua 运行时与后台任务继续存活；之后可以通过深链或通知点击重新附着界面。因此 `app.exit()` 不是「杀掉应用」的可靠手段，别用它替代清理逻辑。

## 可选能力必须显式启用

`notify`、`task`、`native` 不再无条件安装。未写入 `app.json` 时，对应模块是 `nil`；调用其函数会抛错，而 `app.on("notification")` / `app.on("task")` 会被拒绝为未知事件。

```json
{
  "capabilities": ["notify", "task"],
  "native": { "plugins": [{ "name": "echo" }] }
}
```

```lua
-- 旧写法：假定模块总存在
notify.show({ title = "同步完成" })

-- 现写法：按能力降级，或在使用处声明能力
local hasNotify = notify ~= nil
if hasNotify then
  notify.show({ title = "同步完成" })
else
  ui.toast("同步完成")
end
```

声明了 `native.plugins` 必须同时启用 `native` 能力，否则构建失败。`berba dev` 使用与设备相同的门控：未启用能力的模块在浏览器里同样不存在，`native.*` 只安装抛错桩，所以这类漂移在浏览器阶段就能暴露。

## 旧发行包只支持核心能力

壳能力来自 `payload/shell.json`。缺失该文件（旧发行包）时，`berba doctor` 提示「仅支持 core 能力」，构建带 `capabilities` 的项目会报「当前 payload 不支持能力」。

处理方式：更新 `berba` 与发行包，或用新 CLI 重新执行 `scripts/build-shell.sh`（构建 JNI shim 需要 NDK）。`berba release` 与 `berba doctor` 会校验三 ABI shim 齐全且 ELF 合规，缺库时直接失败，不会产出装不上的包。

## 迁移检查清单

- [ ] 全文搜索 `app.on("destroy"`，确认处理器不再取消在途请求或关闭进程级资源。
- [ ] 入口顶层的初始化改成 `app.on("create")` 内可重入执行。
- [ ] 所有 `notify.*` / `task.*` / `native.*` 调用点已在 `app.json` 声明对应能力。
- [ ] 依赖 `app.exit()` 退出的逻辑改为显式界面操作，后台任务用 `task.*` 重新安排。
- [ ] `berba doctor` 的能力行与 `启用能力:` 输出符合预期；`berba dev` 与设备行为一致。
- [ ] `berba build` 后 `assets/berba_capabilities.json` 的 `capabilities` 与项目声明一致。
