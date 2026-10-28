app.on("create", function()
  log("Lua runtime ready")
end)

web.on("pageFinished", function(url)
  log("page finished: " .. tostring(url))
end)

bridge.register("getToken", function(req)
  return {
    token = "demo-token",
    sdk = device.sdk(),
    model = device.model()
  }
end)
