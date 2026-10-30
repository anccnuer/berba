local seq = 0

app.on("task", function(ctx)
  seq = seq + 1
  log("task: " .. ctx.name .. " runId=" .. tostring(ctx.runId)
    .. " late=" .. tostring(ctx.late))
  bridge.emit("task", {
    name = ctx.name,
    runId = ctx.runId,
    late = ctx.late,
    scheduledAt = ctx.scheduledAt,
    firedAt = ctx.firedAt,
    data = ctx.data
  })
  ctx.done()
end)

bridge.register("state", function()
  return {
    exact = task.exact(),
    tasks = task.list()
  }
end)

bridge.register("schedule", function(req)
  req = req or {}
  seq = seq + 1
  local name = req.name or "demo"
  local mode = req.mode or "every"
  local opts = {
    data = { source = "task-example", seq = seq },
    exact = req.exact == true,
    timeout = req.timeout or 60,
    replace = true
  }
  if mode == "daily" then
    opts.daily = req.daily or "08:30"
  elseif mode == "delay" then
    opts.delay = req.delay or 60
  else
    opts.every = req.every or 900
  end
  local info, err = task.schedule(name, opts)
  if info == nil then
    return { ok = false, error = err }
  end
  return { ok = true, info = info }
end)

bridge.register("cancel", function(req)
  local name = (req and req.name) or "demo"
  return { ok = task.cancel(name), name = name }
end)
