-- Widget 示例：create 时推一次，task 定时刷新，点击事件回传到界面。
-- 数据在设备上是模拟值；换成 http/file/store 即可显示真实数据。

local STORE_KEY = "widget.cards"
local TASK_NAME = "widget-refresh"

local function sampleCards()
  -- 模拟一次取数：真实场景用 http.request / file.read / store.get。
  local step = math.random(6000, 9500)
  return {
    { title = "天气", value = (20 + math.random(12)) .. "°", subtitle = "多云" },
    { title = "步数", value = tostring(step), subtitle = "目标 10,000", color = "#3DDC84" },
    { title = "电量", value = tostring(60 + math.random(40)) .. "%", subtitle = "后台任务刷新" },
    { title = "专注", value = "25:00", subtitle = "番茄钟", color = "#FF8FB1" },
    { title = "提醒", value = "18:30", subtitle = "每日 18:30", color = "#FFA726" },
    { title = "更多", value = "还有 1 张", subtitle = "渲染固定 5 行，多余截断" }
  }
end

local function pushCards()
  local cards = sampleCards()
  local res, err = widget.update({ title = "今日概览", cards = cards })
  if res == nil then
    log("widget.update 失败: " .. tostring(err))
    return
  end
  log("widget.update ok updatedAt=" .. tostring(res.updatedAt)
    .. " ids=" .. tostring(res.ids and #res.ids or 0))
  store.set(STORE_KEY, { updatedAt = res.updatedAt, count = #cards, sample = cards })
  bridge.emit("widget", { updatedAt = res.updatedAt, ids = res.ids })
end

local function scheduleTask()
  local info, err = task.schedule(TASK_NAME, { every = 900, timeout = 30, replace = true })
  if info == nil then
    log("task.schedule 失败: " .. tostring(err))
  else
    log("task.schedule ok nextRun=" .. tostring(info.nextRun))
  end
end

app.on("create", function()
  log("widget 示例启动，推送首屏卡片")
  pushCards()
  scheduleTask()
end)

app.on("task", function(ctx)
  if ctx.name == TASK_NAME then
    pushCards()
  end
  ctx.done()
end)

app.on("widget", function(event)
  log("widget clicked id=" .. tostring(event.id) .. " coldStart=" .. tostring(event.coldStart))
  bridge.emit("widget-click", {
    id = event.id,
    coldStart = event.coldStart,
    cards = store.get(STORE_KEY)
  })
end)

bridge.register("refresh", function()
  pushCards()
  return { ok = true, cards = store.get(STORE_KEY) }
end)

bridge.register("schedule", function(req)
  req = req or {}
  local info, err = task.schedule(TASK_NAME, {
    every = req.every or 900,
    timeout = 30,
    replace = true
  })
  if info == nil then
    return { ok = false, error = err }
  end
  return { ok = true, info = info }
end)

bridge.register("state", function()
  return {
    task = task.list(),
    exact = task.exact(),
    widgets = widget.list(),
    cached = store.get(STORE_KEY)
  }
end)

bridge.register("clear", function()
  widget.clear()
  return { ok = true }
end)
