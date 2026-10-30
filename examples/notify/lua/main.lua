local seq = 0

app.on("create", function()
  local ok, err = pcall(notify.channel, {
    id = "demo",
    name = "示例通知",
    description = "Notify 示例应用的通知通道",
    importance = "default"
  })
  if not ok then
    log("创建通知通道失败: " .. tostring(err))
  end
end)

app.on("notification", function(event)
  log("notification: " .. json.encode(event))
  bridge.emit("notification", {
    id = event.id,
    data = event.data,
    coldStart = event.coldStart
  })
end)

bridge.register("state", function()
  return {
    enabled = notify.enabled(),
    permission = permission.check("POST_NOTIFICATIONS")
  }
end)

bridge.register("requestPermission", function()
  permission.request("POST_NOTIFICATIONS", function(result)
    bridge.emit("permission", {
      granted = result["android.permission.POST_NOTIFICATIONS"] == true,
      result = result
    })
  end)
  return { started = true }
end)

bridge.register("show", function(req)
  seq = seq + 1
  req = req or {}
  local data = { text = req.text or "来自 Lua 的通知", seq = seq }
  local id, err = notify.show({
    id = req.id or "demo",
    title = req.title or "berba 通知",
    body = req.body or ("第 " .. seq .. " 条本地通知"),
    channel = "demo",
    data = data,
    autoCancel = true
  })
  if id == nil then
    return { ok = false, error = err }
  end
  return { ok = true, id = id, data = data }
end)

bridge.register("cancel", function(req)
  req = req or {}
  local id = req.id or "demo"
  return { ok = notify.cancel(id), id = id }
end)

bridge.register("cancelAll", function()
  notify.cancelAll()
  return { ok = true }
end)
