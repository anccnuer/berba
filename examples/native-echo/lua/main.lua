app.on("create", function()
  log("native-echo ready")
end)

local function name_of(req)
  if req and type(req.name) == "string" then
    return req.name
  end
  return "world"
end

bridge.register("greet", function(req)
  local value, err = native.echo.greet({ name = name_of(req) })
  if err then
    return { ok = false, error = err }
  end
  return { ok = true, value = value }
end)

bridge.register("greetLater", function(req)
  native.echo.greet_later({ name = name_of(req) }, function(value, err)
    if err then
      bridge.emit("nativeResult", { ok = false, error = err })
    else
      bridge.emit("nativeResult", { ok = true, value = value })
    end
  end)
  return { pending = true }
end)

bridge.register("list", function()
  return native.list()
end)
