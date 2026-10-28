app.on("create", function()
  log("Lua runtime ready")
end)

web.on("pageFinished", function(url)
  log("page finished: " .. tostring(url))
end)

bridge.register("hello", function(req)
  return {
    message = "hello from lua",
    sdk = device.sdk(),
    model = device.model()
  }
end)
