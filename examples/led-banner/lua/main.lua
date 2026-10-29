local DEFAULT = {
  text = "哥哥好厉害！",
  size = 60,
  speed = 320,
  color = "#ffef5c",
  background = "#08080c",
  glow = true
}

local active = false

local function clamp(value, min, max, fallback)
  local n = tonumber(value) or fallback
  if n < min then
    n = min
  elseif n > max then
    n = max
  end
  return math.floor(n + 0.5)
end

local function color(value, fallback)
  if type(value) == "string" and value:match("^#%x%x%x%x%x%x$") then
    return value:lower()
  end
  return fallback
end

local function sanitize(input)
  if type(input) ~= "table" then
    input = {}
  end
  local text = type(input.text) == "string" and input.text or DEFAULT.text
  if #text > 120 then
    text = DEFAULT.text
  end
  return {
    text = text,
    size = clamp(input.size, 20, 90, DEFAULT.size),
    speed = clamp(input.speed, 80, 900, DEFAULT.speed),
    color = color(input.color, DEFAULT.color),
    background = color(input.background, DEFAULT.background),
    glow = input.glow ~= false
  }
end

local function closeBanner()
  if not active then
    return false
  end
  active = false
  device.orientation("portrait")
  ui.fullscreen(false)
  ui.keepScreenOn(false)
  bridge.emit("bannerExit", { ok = true })
  return true
end

app.on("create", function()
  log("led banner ready")
end)

app.on("back", function()
  return closeBanner()
end)

web.on("pageFinished", function(url)
  log("page finished: " .. tostring(url))
end)

bridge.register("getSettings", function()
  return sanitize(store.get("banner"))
end)

bridge.register("saveSettings", function(req)
  local s = sanitize(req)
  store.set("banner", s)
  return s
end)

bridge.register("enterBanner", function()
  if active then
    return { ok = true }
  end
  active = true
  ui.fullscreen(true)
  ui.keepScreenOn(true)
  device.orientation("landscape")
  return { ok = true }
end)

bridge.register("exitBanner", function()
  closeBanner()
  return { ok = true }
end)
