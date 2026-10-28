local count = tonumber(store.get("count")) or 0

app.on("create", function()
  log("hello ready")
end)

app.on("back", function()
  if count > 0 then
    ui.toast("计数器 = " .. count)
  end
end)

web.on("pageFinished", function(url)
  log("page finished: " .. tostring(url))
end)

bridge.register("add", function(req)
  local a = tonumber(req and req.a) or 0
  local b = tonumber(req and req.b) or 0
  return { sum = a + b }
end)

bridge.register("deviceInfo", function()
  local screen = device.screen()
  return {
    model = device.model(),
    sdk = device.sdk(),
    locale = device.locale(),
    width = screen.width,
    height = screen.height,
    dpi = screen.dpi
  }
end)

bridge.register("increment", function()
  count = count + 1
  store.set("count", count)
  return { count = count }
end)
