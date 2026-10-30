/*
 * berba dev 浏览器 Lua 运行时。
 *
 * 基于 fengari（Lua 5.3），把设备壳里的 berba Lua API 尽量等价地模拟到浏览器：
 * app / web / bridge / http / file / store / permission / ui / device / notify / task / native / json / log。
 *
 * 与设备的差异（dev 专属，见 docs/build.md）：
 *   - 平台标识为 "browser"，sdk 为 0；除通知权限外 permission 一律视为已授权；
 *   - file 与 store 落在 localStorage；剪贴板读取、状态栏、方向锁定等能力降级或不可用；
 *   - http 直连受浏览器 CORS 限制；明文 http 不拦截；
 *   - notify 使用 Web Notifications：无通道、页面关闭后通知点击不再回调；
 *   - task 只能注册 app.on("task")，task.schedule 直接报“浏览器不支持后台任务”；
 *   - native.list 返回插件/模块信息，所有插件函数抛“原生插件仅在设备上可用”；
 *   - 未启用的能力不安装对应模块；事件白名单来自 lua.json 的能力元数据；
 *   - 浏览器返回键无法拦截，用 window.__berbaDev.back() 手动触发 app.on("back")。
 *
 * 对外只暴露 window.__berbaLua = { init(cfg) -> Promise<{ call, has }> }。
 */
(function () {
  'use strict';

  var F = window.fengari;
  if (!F) {
    window.__berbaLua = {
      init: function () { return Promise.reject(new Error('fengari 未加载')); }
    };
    return;
  }

  var lua = F.lua;
  var lauxlib = F.lauxlib;
  var lualib = F.lualib;
  var to_luastring = F.to_luastring;
  var to_jsstring = F.to_jsstring;

  var LUA_OK = lua.LUA_OK;
  var LUA_REGISTRYINDEX = lua.LUA_REGISTRYINDEX;
  var LUA_MULTRET = lua.LUA_MULTRET;
  var LUA_TFUNCTION = lua.LUA_TFUNCTION;

  var MAX_DEPTH = 64;
  var LIMIT_BYTES = 8 * 1024 * 1024;
  var TOO_COMPLEX = { tooComplex: true };

  // ---------------------------------------------------------------- 基础工具

  function utf8(s) { return to_luastring(String(s)); }
  function jsstr(s) { return to_jsstring(s); }

  function pushStr(L, s) { lua.lua_pushstring(L, utf8(s)); }
  function pushBool(L, b) { lua.lua_pushboolean(L, b ? 1 : 0); }
  function pushNumber(L, n) {
    if (Number.isInteger(n) && n >= -2147483648 && n <= 2147483647) {
      lua.lua_pushinteger(L, n);
    } else {
      lua.lua_pushnumber(L, n);
    }
  }

  function luaError(L, msg) {
    pushStr(L, msg);
    lua.lua_error(L);
    return 0;
  }

  function argString(L, i) {
    var s = lua.lua_tolstring(L, i);
    return s === null ? null : jsstr(s);
  }
  function optString(L, i, def) {
    var s = argString(L, i);
    return s === null ? def : s;
  }
  function isNil(L, i) { return lua.lua_isnoneornil(L, i); }
  function argBool(L, i) { return lua.lua_toboolean(L, i); }
  function isFunction(L, i) { return lua.lua_type(L, i) === LUA_TFUNCTION; }

  function tracebackMsgh(L) {
    var msg = lua.lua_tolstring(L, 1);
    lauxlib.luaL_traceback(L, L, msg, 1);
    return 1;
  }

  function nativeErrorHandler(L) {
    var err = lua.lua_touserdata(L, 1);
    var msg = err && err.message ? err.message : String(err);
    pushStr(L, msg);
    return 1;
  }

  // ------------------------------------------------------------ Lua <=> JS 值

  function luaToJs(L, index, depth, seen) {
    var idx = lua.lua_absindex(L, index);
    var t = lua.lua_type(L, idx);
    switch (t) {
      case lua.LUA_TNIL: return null;
      case lua.LUA_TBOOLEAN: return lua.lua_toboolean(L, idx);
      case lua.LUA_TNUMBER: return lua.lua_tonumber(L, idx);
      case lua.LUA_TSTRING: return jsstr(lua.lua_tolstring(L, idx));
      case lua.LUA_TTABLE: return tableToJs(L, idx, depth, seen);
      default: return null;
    }
  }

  function tableToJs(L, idx, depth, seen) {
    if (depth >= MAX_DEPTH) throw TOO_COMPLEX;
    var ptr = lua.lua_topointer(L, idx);
    if (seen.indexOf(ptr) >= 0) throw TOO_COMPLEX;
    seen.push(ptr);

    var keys = [];
    var values = [];
    lua.lua_pushnil(L);
    while (lua.lua_next(L, idx) !== 0) {
      keys.push(luaToJs(L, -2, depth + 1, seen));
      values.push(luaToJs(L, -1, depth + 1, seen));
      lua.lua_pop(L, 1);
    }
    seen.pop();

    var total = keys.length;
    var isArray = total > 0;
    for (var i = 0; i < total && isArray; i++) {
      var k = keys[i];
      if (typeof k !== 'number' || Math.floor(k) !== k || k < 1 || k > total) isArray = false;
    }
    if (isArray) {
      var arr = new Array(total);
      for (var j = 0; j < total; j++) arr[keys[j] - 1] = values[j];
      return arr;
    }
    var obj = {};
    for (var n = 0; n < total; n++) {
      var key = keys[n];
      obj[typeof key === 'string' ? key : String(key)] = values[n];
    }
    return obj;
  }

  function jsToLua(L, v, depth, seen) {
    if (depth > MAX_DEPTH) throw TOO_COMPLEX;
    if (v === null || v === undefined) { lua.lua_pushnil(L); return; }
    switch (typeof v) {
      case 'boolean': pushBool(L, v); return;
      case 'number': pushNumber(L, v); return;
      case 'string': pushStr(L, v); return;
      case 'object': break;
      default: lua.lua_pushnil(L); return;
    }
    if (seen.indexOf(v) >= 0) throw TOO_COMPLEX;
    seen.push(v);

    if (Array.isArray(v)) {
      lua.lua_createtable(L, v.length, 0);
      var ai = lua.lua_gettop(L);
      for (var i = 0; i < v.length; i++) {
        jsToLua(L, v[i], depth + 1, seen);
        lua.lua_rawseti(L, ai, i + 1);
      }
    } else {
      var names = Object.keys(v);
      lua.lua_createtable(L, 0, names.length);
      var oi = lua.lua_gettop(L);
      for (var n = 0; n < names.length; n++) {
        pushStr(L, names[n]);
        jsToLua(L, v[names[n]], depth + 1, seen);
        lua.lua_rawset(L, oi);
      }
    }
    seen.pop();
  }

  function pushJsValue(L, v) {
    try {
      jsToLua(L, v, 0, []);
    } catch (e) {
      lua.lua_pushnil(L);
    }
  }

  function readJsValue(L, idx) {
    try {
      return luaToJs(L, idx, 0, []);
    } catch (e) {
      return null;
    }
  }

  function pushValueTable(L, obj) {
    pushJsValue(L, obj);
    return 1;
  }

  // 从注册表取函数并调用，参数是 JS 值；用于异步回调。
  function callRef(L, ref, args) {
    var top = lua.lua_gettop(L);
    try {
      lua.lua_pushjsfunction(L, tracebackMsgh);
      var h = lua.lua_gettop(L);
      lua.lua_rawgeti(L, LUA_REGISTRYINDEX, ref);
      for (var i = 0; i < args.length; i++) pushJsValue(L, args[i]);
      var rc = lua.lua_pcall(L, args.length, 0, h);
      if (rc !== LUA_OK) {
        console.error('[berba dev] Lua 回调出错: ' + (argString(L, -1) || ('错误码 ' + rc)));
      }
    } catch (e) {
      console.error('[berba dev] Lua 回调出错', e);
    } finally {
      lua.lua_settop(L, top);
    }
  }

  function refFunction(L, index) {
    lua.lua_pushvalue(L, index);
    return lauxlib.luaL_ref(L, LUA_REGISTRYINDEX);
  }

  // ------------------------------------------------------------------ 状态

  var L = null;
  var files = {};
  var entryPath = 'main.lua';
  var appMeta = {};
  var moduleRefs = Object.create(null);
  var moduleLoading = Object.create(null);
  var handlers = {
    boot: [], create: [], pause: [], resume: [], destroy: [], back: [], newIntent: [],
    notification: [], task: [], widget: []
  };
  var webHandlers = { pageStarted: [], pageFinished: [], error: [] };
  var bridges = Object.create(null);
  var webLoaded = false;
  var initialized = null;
  var wakeLock = null;
  var capabilityMeta = null;
  var moduleMeta = null;
  var eventMeta = null;
  var nativeMeta = null;

  function newState() {
    var state = lauxlib.luaL_newstate();
    var libs = [
      ['_G', lualib.luaopen_base],
      ['coroutine', lualib.luaopen_coroutine],
      ['table', lualib.luaopen_table],
      ['string', lualib.luaopen_string],
      ['math', lualib.luaopen_math]
    ];
    for (var i = 0; i < libs.length; i++) {
      lauxlib.luaL_requiref(state, utf8(libs[i][0]), libs[i][1], 1);
      lua.lua_pop(state, 1);
    }
    // 设备壳移除了这几个函数
    var removed = ['load', 'loadfile', 'dofile'];
    for (var n = 0; n < removed.length; n++) {
      lua.lua_pushnil(state);
      lua.lua_setglobal(state, utf8(removed[n]));
    }
    lua.lua_atnativeerror(state, nativeErrorHandler);
    return state;
  }

  // bit32：设备 LuaJ 5.2 自带，fengari 没有；按 5.2 语义（无符号 32 位）补上。
  // 注意 fengari 的 integer 是 32 位有符号，结果统一转回 0..2^32-1 的浮点数。
  var BIT32_SOURCE = [
    'local TWO32 = 4294967296.0',
    'local HALF = 2147483648.0',
    'local function tosigned(x, n)',
    "  local t = math.type(x)",
    "  if t == 'integer' then return x end",
    "  if t ~= 'float' then error('bad argument #' .. n .. ' (number expected)', 3) end",
    "  if x ~= math.floor(x) then error('bad argument #' .. n .. ' (number has no integer representation)', 3) end",
    '  x = x % TWO32',
    '  if x >= HALF then x = x - TWO32 end',
    '  return math.tointeger(x)',
    'end',
    'local function unsign(r)',
    '  if r < 0 then return r + TWO32 end',
    '  return r',
    'end',
    'local function disp(n)',
    "  local t = math.type(n)",
    "  if t ~= 'integer' and t ~= 'float' then error('bad argument (number expected)', 3) end",
    '  n = math.floor(n)',
    "  if n ~= n then error('bad argument (number has no integer representation)', 3) end",
    '  return n',
    'end',
    'local M = {}',
    'function M.band(...)',
    '  local r = -1',
    "  for i = 1, select('#', ...) do r = r & tosigned((select(i, ...)), i) end",
    '  return unsign(r)',
    'end',
    'function M.bor(...)',
    '  local r = 0',
    "  for i = 1, select('#', ...) do r = r | tosigned((select(i, ...)), i) end",
    '  return unsign(r)',
    'end',
    'function M.bxor(...)',
    '  local r = 0',
    "  for i = 1, select('#', ...) do r = r ~ tosigned((select(i, ...)), i) end",
    '  return unsign(r)',
    'end',
    'function M.bnot(a) return unsign(~tosigned(a, 1)) end',
    'function M.lshift(a, n)',
    '  local x = tosigned(a, 1); local d = disp(n)',
    '  if d >= 32 or d <= -32 then return 0 end',
    '  if d < 0 then return unsign(x >> -d) end',
    '  return unsign(x << d)',
    'end',
    'function M.rshift(a, n)',
    '  local x = tosigned(a, 1); local d = disp(n)',
    '  if d >= 32 or d <= -32 then return 0 end',
    '  if d < 0 then return unsign(x << -d) end',
    '  return unsign(x >> d)',
    'end',
    'function M.arshift(a, n)',
    '  local x = tosigned(a, 1); local d = disp(n)',
    '  if d < 0 then return M.lshift(x, -d) end',
    '  if d == 0 then return unsign(x) end',
    '  local neg = x < 0',
    '  if d >= 32 then return neg and 4294967295 or 0 end',
    '  local r = x >> d',
    '  if neg then r = r | (~((1 << (32 - d)) - 1)) end',
    '  return unsign(r)',
    'end',
    'function M.btest(...)',
    '  local r = -1',
    "  for i = 1, select('#', ...) do r = r & tosigned((select(i, ...)), i) end",
    '  return r ~= 0',
    'end',
    'function M.extract(a, field, width)',
    '  local n = tosigned(a, 1); field = field or 1; width = width or 1',
    "  if field < 0 then error('bad argument #2 (field cannot be negative)', 2) end",
    "  if width <= 0 then error('bad argument #3 (width must be positive)', 2) end",
    "  if field + width > 32 then error('trying to access non-existent bits', 2) end",
    '  return (n >> field) & ((1 << width) - 1)',
    'end',
    'function M.replace(a, v, field, width)',
    '  local n = tosigned(a, 1); v = tosigned(v, 2); field = field or 1; width = width or 1',
    "  if field < 0 then error('bad argument #3 (field cannot be negative)', 2) end",
    "  if width <= 0 then error('bad argument #4 (width must be positive)', 2) end",
    "  if field + width > 32 then error('trying to access non-existent bits', 2) end",
    '  local mask = ((1 << width) - 1) << field',
    '  return unsign((n & ~mask) | ((v << field) & mask))',
    'end',
    'return M'
  ].join('\n');

  function installBit32(L) {
    var rc = lauxlib.luaL_loadbuffer(L, utf8(BIT32_SOURCE), null, utf8('@bit32'));
    if (rc !== LUA_OK) {
      console.error('[berba dev] bit32 加载失败: ' + argString(L, -1));
      lua.lua_pop(L, 1);
      return;
    }
    rc = lua.lua_pcall(L, 0, 1, 0);
    if (rc !== LUA_OK) {
      console.error('[berba dev] bit32 执行失败: ' + argString(L, -1));
      lua.lua_pop(L, 1);
      return;
    }
    lua.lua_setglobal(L, utf8('bit32'));
  }

  // ------------------------------------------------------------- require

  function resolveModule(name) {
    var base = name.replace(/\./g, '/');
    var candidates = [base + '.lua', base + '/init.lua'];
    for (var i = 0; i < candidates.length; i++) {
      if (Object.prototype.hasOwnProperty.call(files, candidates[i])) return candidates[i];
    }
    return null;
  }

  function requireImpl(L) {
    var name = argString(L, 1);
    if (!name) return luaError(L, "bad argument #1 to 'require' (string expected)");

    if (moduleRefs[name] !== undefined) {
      lua.lua_rawgeti(L, LUA_REGISTRYINDEX, moduleRefs[name]);
      return 1;
    }
    if (moduleLoading[name]) {
      pushBool(L, true);
      return 1;
    }

    var file = resolveModule(name);
    if (!file) return luaError(L, "module '" + name + "' not found");

    moduleLoading[name] = true;
    var rc = lauxlib.luaL_loadbuffer(L, utf8(files[file]), null, utf8(file));
    if (rc === LUA_OK) rc = lua.lua_pcall(L, 0, 1, 0);
    delete moduleLoading[name];

    if (rc !== LUA_OK) {
      var msg = argString(L, -1) || ('加载模块失败: ' + file);
      lua.lua_pop(L, 1);
      return luaError(L, msg);
    }

    if (lua.lua_isnil(L, -1)) {
      lua.lua_pop(L, 1);
      pushBool(L, true);
    }
    moduleRefs[name] = lauxlib.luaL_ref(L, LUA_REGISTRYINDEX);
    lua.lua_rawgeti(L, LUA_REGISTRYINDEX, moduleRefs[name]);
    return 1;
  }

  // ------------------------------------------------------- 事件（app/web）

  function fireHandlers(event, args) {
    var list = handlers[event];
    if (!list || list.length === 0) return false;
    var handled = false;
    for (var i = 0; i < list.length; i++) {
      var top = lua.lua_gettop(L);
      try {
        lua.lua_pushjsfunction(L, tracebackMsgh);
        var h = lua.lua_gettop(L);
        lua.lua_rawgeti(L, LUA_REGISTRYINDEX, list[i]);
        for (var a = 0; a < args.length; a++) pushJsValue(L, args[a]);
        var rc = lua.lua_pcall(L, args.length, 1, h);
        if (rc === LUA_OK) {
          if (lua.lua_toboolean(L, -1)) handled = true;
        } else {
          console.error('[berba dev] app.' + event + ' 回调出错: ' + (argString(L, -1) || ('错误码 ' + rc)));
        }
      } catch (e) {
        console.error('[berba dev] app.' + event + ' 回调出错', e);
      } finally {
        lua.lua_settop(L, top);
      }
    }
    return handled;
  }

  function fireWebHandlers(event, args) {
    var list = webHandlers[event];
    if (!list || list.length === 0) return;
    for (var i = 0; i < list.length; i++) callRef(L, list[i], args);
  }

  function registerHandler(bucket, L, event, fnIndex) {
    if (!isFunction(L, fnIndex)) return luaError(L, 'bad argument #2 (function expected)');
    var ref = refFunction(L, fnIndex);
    var list = bucket[event] || (bucket[event] = []);
    list.push(ref);
    return 0;
  }

  // --------------------------------------------------------------- app

  var CORE_APP_EVENTS = ['boot', 'create', 'pause', 'resume', 'destroy', 'back', 'newIntent'];
  var CORE_WEB_EVENTS = ['pageStarted', 'pageFinished', 'error'];

  function buildEventSet(owner, core, legacy) {
    var set = {};
    var i;
    for (i = 0; i < core.length; i++) set[core[i]] = 1;
    if (eventMeta && Array.isArray(eventMeta[owner])) {
      for (i = 0; i < eventMeta[owner].length; i++) {
        if (typeof eventMeta[owner][i] === 'string') set[eventMeta[owner][i]] = 1;
      }
    } else if (!eventMeta) {
      for (i = 0; i < legacy.length; i++) set[legacy[i]] = 1;
    }
    return set;
  }

  var APP_EVENTS = null;

  function appOn(L) {
    var event = argString(L, 1);
    if (!event || !APP_EVENTS[event]) {
      return luaError(L, "bad argument #1 to 'app.on' (未知事件: " + (event || 'nil') + ')');
    }
    return registerHandler(handlers, L, event, 2);
  }

  function appExit(L) {
    window.close();
    setTimeout(function () {
      if (!window.closed) console.warn('[berba dev] window.close() 被浏览器拦截，请手动关闭标签页');
    }, 200);
    return 0;
  }

  function appInfo(L) {
    return pushValueTable(L, {
      name: appMeta.name || '',
      package: appMeta.package || '',
      versionName: appMeta.versionName || '',
      versionCode: typeof appMeta.versionCode === 'number' ? appMeta.versionCode : 0,
      sdk: 0,
      platform: 'browser',
      foreground: true
    });
  }

  // --------------------------------------------------------------- web

  var WEB_EVENTS = null;

  function webLoad(L) {
    var url = argString(L, 1);
    if (!url) return luaError(L, "bad argument #1 to 'web.load' (string expected)");
    if (!/^(https?:)?\//i.test(url) && !/^[a-z][a-z0-9+.-]*:/i.test(url)) {
      return luaError(L, "bad argument #1 to 'web.load' (不支持的地址: " + url + ')');
    }
    webLoaded = true;
    location.assign(url);
    return 0;
  }

  function webEval(L) {
    var script = argString(L, 1);
    if (script === null) return luaError(L, "bad argument #1 to 'web.eval' (string expected)");
    var cb = 0;
    if (!isNil(L, 2)) {
      if (!isFunction(L, 2)) return luaError(L, "bad argument #2 to 'web.eval' (function expected)");
      cb = refFunction(L, 2);
    }
    var result;
    var failed = null;
    try {
      result = (0, eval)(script);
    } catch (e) {
      failed = e;
      console.error('[berba dev] web.eval 执行失败:', e);
    }
    if (cb) {
      if (failed) {
        // 设备 evaluateJavascript 无错误通道，这里静默
      } else {
        var text;
        try { text = JSON.stringify(result); } catch (e) { text = 'null'; }
        if (text === undefined) text = 'null';
        callRef(L, cb, [text]);
      }
    }
    return 0;
  }

  function webOn(L) {
    var event = argString(L, 1);
    if (!event || !WEB_EVENTS[event]) {
      return luaError(L, "bad argument #1 to 'web.on' (未知事件: " + (event || 'nil') + ')');
    }
    return registerHandler(webHandlers, L, event, 2);
  }

  // ------------------------------------------------------------ bridge

  function bridgeRegister(L) {
    var name = argString(L, 1);
    if (!name) return luaError(L, "bad argument #1 to 'bridge.register' (string expected)");
    if (!isFunction(L, 2)) return luaError(L, "bad argument #2 to 'bridge.register' (function expected)");
    if (bridges[name] !== undefined) {
      lauxlib.luaL_unref(L, LUA_REGISTRYINDEX, bridges[name]);
    }
    bridges[name] = refFunction(L, 2);
    return 0;
  }

  function bridgeEmit(L) {
    var name = argString(L, 1);
    if (!name) return luaError(L, "bad argument #1 to 'bridge.emit' (string expected)");
    var data = isNil(L, 2) ? null : readJsValue(L, 2);
    if (typeof window.__berbaEmit === 'function') {
      try { window.__berbaEmit(name, data); } catch (e) { console.error(e); }
    }
    return 0;
  }

  function callBridge(name, args) {
    var ref = bridges[name];
    if (ref === undefined) throw new Error('berba dev: Lua 未注册桥方法 ' + name);
    var top = lua.lua_gettop(L);
    try {
      lua.lua_pushjsfunction(L, tracebackMsgh);
      var h = lua.lua_gettop(L);
      lua.lua_rawgeti(L, LUA_REGISTRYINDEX, ref);
      pushJsValue(L, args === undefined ? null : args);
      var rc = lua.lua_pcall(L, 1, LUA_MULTRET, h);
      if (rc !== LUA_OK) {
        var msg = argString(L, -1) || ('Lua 错误码 ' + rc);
        throw new Error(msg);
      }
      var n = lua.lua_gettop(L) - h;
      if (n <= 0) return null;
      if (n === 1) return readJsValue(L, h + 1);
      var out = [];
      for (var i = 1; i <= n; i++) out.push(readJsValue(L, h + i));
      return out;
    } finally {
      lua.lua_settop(L, top);
    }
  }

  function hasBridge(name) {
    return bridges[name] !== undefined;
  }

  // ---------------------------------------------------------------- http

  var httpSeq = 1;
  var httpPending = Object.create(null);
  var corsWarned = false;

  function httpTimeout(v, def) {
    if (typeof v !== 'number' || !isFinite(v)) return def;
    v = Math.floor(v);
    if (v < 1) v = 1;
    if (v > 120) v = 120;
    return v;
  }

  function startHttp(method, url, headers, body, timeout, cbRef, progRef) {
    var id = 'h' + (httpSeq++);
    var ctrl = typeof AbortController !== 'undefined' ? new AbortController() : null;
    var entry = { ctrl: ctrl, cancelled: false, done: false };
    httpPending[id] = entry;

    function finish(res) {
      if (entry.done) return;
      entry.done = true;
      delete httpPending[id];
      clearTimeout(timer);
      callRef(L, cbRef, [res]);
    }

    var timer = setTimeout(function () {
      if (ctrl) ctrl.abort();
      finish({ ok: false, status: 0, error: '请求超时' });
    }, timeout * 1000);

    var init = { method: method };
    if (headers) init.headers = headers;
    if (body !== null && body !== undefined) init.body = body;
    if (ctrl) init.signal = ctrl.signal;

    var request;
    try {
      request = fetch(url, init);
    } catch (e) {
      finish({ ok: false, status: 0, error: '网络错误: ' + (e && e.message ? e.message : e) });
      return id;
    }

    request.then(function (resp) {
      var headerObj = {};
      if (resp.headers && resp.headers.forEach) {
        resp.headers.forEach(function (v, k) { headerObj[k] = v; });
      }
      var total = parseInt(resp.headers.get('content-length') || '-1', 10);
      if (isNaN(total)) total = -1;

      if (!progRef || !resp.body || !resp.body.getReader) {
        return resp.text().then(function (text) {
          return { ok: resp.ok, status: resp.status, body: text, headers: headerObj };
        });
      }

      var reader = resp.body.getReader();
      var decoder = new TextDecoder('utf-8');
      var text = '';
      var read = 0;

      function pump() {
        return reader.read().then(function (step) {
          if (step.done) {
            text += decoder.decode();
            return { ok: resp.ok, status: resp.status, body: text, headers: headerObj };
          }
          read += step.value.byteLength;
          text += decoder.decode(step.value, { stream: true });
          callRef(L, progRef, [read, total]);
          if (read > LIMIT_BYTES) {
            try { reader.cancel(); } catch (e) { /* ignore */ }
            throw new Error('响应超过 8MB 上限');
          }
          return pump();
        });
      }
      return pump();
    }).then(function (res) {
      finish(res);
    }).catch(function (e) {
      if (entry.cancelled) {
        finish({ ok: false, status: 0, error: '已取消' });
        return;
      }
      var msg = e && e.message ? e.message : String(e);
      if (/Failed to fetch|NetworkError|Load failed/i.test(msg) && !corsWarned) {
        corsWarned = true;
        console.warn('[berba dev] http 请求失败，常见原因是跨域（CORS）被浏览器拦截');
      }
      finish({ ok: false, status: 0, error: '网络错误: ' + msg });
    });

    return id;
  }

  function readHeaders(L, idx) {
    if (isNil(L, idx)) return null;
    var obj = readJsValue(L, idx);
    if (obj === null || typeof obj !== 'object') return null;
    var out = {};
    Object.keys(obj).forEach(function (k) {
      var v = obj[k];
      out[k] = typeof v === 'string' ? v : String(v);
    });
    return out;
  }

  function readBody(L, idx) {
    if (isNil(L, idx)) return null;
    var t = lua.lua_type(L, idx);
    if (t === lua.LUA_TSTRING) return argString(L, idx);
    var obj = readJsValue(L, idx);
    return JSON.stringify(obj);
  }

  function httpGet(L) {
    var url = argString(L, 1);
    if (!url) return luaError(L, "bad argument #1 to 'http.get' (string expected)");
    if (!isFunction(L, 2)) return luaError(L, "bad argument #2 to 'http.get' (function expected)");
    if (!/^https?:\/\//i.test(url)) return luaError(L, "bad argument #1 to 'http.get' (仅支持 http/https)");
    startHttp('GET', url, null, null, 15, refFunction(L, 2), 0);
    return 0;
  }

  function httpPost(L) {
    var url = argString(L, 1);
    if (!url) return luaError(L, "bad argument #1 to 'http.post' (string expected)");
    if (!/^https?:\/\//i.test(url)) return luaError(L, "bad argument #1 to 'http.post' (仅支持 http/https)");
    var body = readBody(L, 2);
    if (!isFunction(L, 3)) return luaError(L, "bad argument #3 to 'http.post' (function expected)");
    startHttp('POST', url, null, body, 15, refFunction(L, 3), 0);
    return 0;
  }

  var HTTP_METHODS = { GET: 1, POST: 1, PUT: 1, DELETE: 1, HEAD: 1, OPTIONS: 1 };

  function httpRequest(L) {
    if (isNil(L, 1)) return luaError(L, "bad argument #1 to 'http.request' (table expected)");
    var opts = readJsValue(L, 1);
    if (!opts || typeof opts !== 'object') {
      return luaError(L, "bad argument #1 to 'http.request' (table expected)");
    }
    var url = typeof opts.url === 'string' ? opts.url : '';
    if (!url) return luaError(L, "http.request: url 不能为空");
    if (!/^https?:\/\//i.test(url)) return luaError(L, 'http.request: 仅支持 http/https');

    var method = (opts.method || 'GET').toUpperCase();
    if (!HTTP_METHODS[method]) return luaError(L, 'http.request: 不支持的方法 ' + method);

    var headers = null;
    if (opts.headers && typeof opts.headers === 'object') {
      headers = {};
      Object.keys(opts.headers).forEach(function (k) {
        headers[k] = String(opts.headers[k]);
      });
    }

    var body = null;
    if (opts.body !== null && opts.body !== undefined) {
      body = typeof opts.body === 'string' ? opts.body : JSON.stringify(opts.body);
    }

    var cbRef = 0;
    var progRef = 0;
    // cb/onProgress 是 Lua 函数，必须从原表取引用而不是从 JS 快照
    lua.lua_getfield(L, 1, utf8('cb'));
    if (!isNil(L, -1)) {
      if (!isFunction(L, -1)) { lua.lua_pop(L, 1); return luaError(L, 'http.request: cb 必须是函数'); }
      cbRef = refFunction(L, -1);
    }
    lua.lua_pop(L, 1);
    lua.lua_getfield(L, 1, utf8('onProgress'));
    if (!isNil(L, -1)) {
      if (!isFunction(L, -1)) { lua.lua_pop(L, 1); return luaError(L, 'http.request: onProgress 必须是函数'); }
      progRef = refFunction(L, -1);
    }
    lua.lua_pop(L, 1);

    if (!cbRef) return luaError(L, 'http.request: 缺少 cb 回调');

    var id = startHttp(method, url, headers, body, httpTimeout(opts.timeout, 15), cbRef, progRef);
    pushStr(L, id);
    return 1;
  }

  function httpCancel(L) {
    var id = argString(L, 1);
    if (id === null) return luaError(L, "bad argument #1 to 'http.cancel' (string expected)");
    var entry = httpPending[id];
    if (!entry) { pushBool(L, false); return 1; }
    entry.cancelled = true;
    if (entry.ctrl) entry.ctrl.abort();
    pushBool(L, true);
    return 1;
  }

  // ---------------------------------------------------------------- file

  var FILE_PREFIX = '__berba_file:';
  var encoder = typeof TextEncoder !== 'undefined' ? new TextEncoder() : null;

  function utf8Len(s) {
    if (encoder) return encoder.encode(s).length;
    return unescape(encodeURIComponent(s)).length;
  }

  function normalizePath(raw) {
    if (typeof raw !== 'string' || raw === '') return null;
    if (raw.indexOf('\u0000') >= 0) return null;
    var p = raw.replace(/\\/g, '/');
    if (p.charAt(0) === '/') return null;
    var parts = p.split('/');
    var out = [];
    for (var i = 0; i < parts.length; i++) {
      var seg = parts[i];
      if (seg === '' || seg === '.') continue;
      if (seg === '..') return null;
      out.push(seg);
    }
    return out.length ? out.join('/') : null;
  }

  function fileKey(p) { return FILE_PREFIX + p; }

  function fileExists(p) {
    if (localStorage.getItem(fileKey(p)) !== null) return true;
    var prefix = fileKey(p) + '/';
    for (var i = 0; i < localStorage.length; i++) {
      var k = localStorage.key(i);
      if (k && k.indexOf(prefix) === 0) return true;
    }
    return false;
  }

  function fileChildren(p) {
    var prefix = p ? fileKey(p) + '/' : FILE_PREFIX;
    var names = {};
    for (var i = 0; i < localStorage.length; i++) {
      var k = localStorage.key(i);
      if (!k || k.indexOf(prefix) !== 0) continue;
      var rest = k.slice(prefix.length);
      if (!rest) continue;
      var slash = rest.indexOf('/');
      names[slash >= 0 ? rest.slice(0, slash) : rest] = true;
    }
    return Object.keys(names).sort();
  }

  function fileRead(L) {
    var raw = argString(L, 1);
    var p = normalizePath(raw);
    if (!p) { lua.lua_pushnil(L); pushStr(L, '路径不合法'); return 2; }
    var text = localStorage.getItem(fileKey(p));
    if (text === null) { lua.lua_pushnil(L); pushStr(L, '文件不存在: ' + p); return 2; }
    pushStr(L, text);
    return 1;
  }

  function fileWrite(L) {
    var p = normalizePath(argString(L, 1));
    if (!p) { lua.lua_pushnil(L); pushStr(L, '路径不合法'); return 2; }
    var text = argString(L, 2);
    if (text === null) { lua.lua_pushnil(L); pushStr(L, '内容必须是字符串'); return 2; }
    if (utf8Len(text) > LIMIT_BYTES) {
      lua.lua_pushnil(L); pushStr(L, '文件超过 8MB 上限'); return 2;
    }
    try {
      localStorage.setItem(fileKey(p), text);
    } catch (e) {
      lua.lua_pushnil(L); pushStr(L, '写入失败: ' + (e && e.message ? e.message : e)); return 2;
    }
    pushBool(L, true);
    return 1;
  }

  function fileAppend(L) {
    var p = normalizePath(argString(L, 1));
    if (!p) { lua.lua_pushnil(L); pushStr(L, '路径不合法'); return 2; }
    var text = argString(L, 2);
    if (text === null) { lua.lua_pushnil(L); pushStr(L, '内容必须是字符串'); return 2; }
    var old = localStorage.getItem(fileKey(p));
    if (old === null) { lua.lua_pushnil(L); pushStr(L, '文件不存在: ' + p); return 2; }
    if (utf8Len(old) + utf8Len(text) > LIMIT_BYTES) {
      lua.lua_pushnil(L); pushStr(L, '文件超过 8MB 上限'); return 2;
    }
    try {
      localStorage.setItem(fileKey(p), old + text);
    } catch (e) {
      lua.lua_pushnil(L); pushStr(L, '写入失败: ' + (e && e.message ? e.message : e)); return 2;
    }
    pushBool(L, true);
    return 1;
  }

  function fileExistsFn(L) {
    var p = normalizePath(argString(L, 1));
    if (!p) { pushBool(L, false); return 1; }
    pushBool(L, fileExists(p));
    return 1;
  }

  function fileDelete(L) {
    var p = normalizePath(argString(L, 1));
    if (!p) { lua.lua_pushnil(L); pushStr(L, '路径不合法'); return 2; }
    if (localStorage.getItem(fileKey(p)) !== null) {
      localStorage.removeItem(fileKey(p));
      pushBool(L, true);
      return 1;
    }
    if (fileChildren(p).length > 0) {
      lua.lua_pushnil(L); pushStr(L, '目录不为空: ' + p); return 2;
    }
    pushBool(L, false);
    return 1;
  }

  function fileRename(L) {
    var from = normalizePath(argString(L, 1));
    var to = normalizePath(argString(L, 2));
    if (!from || !to) { lua.lua_pushnil(L); pushStr(L, '路径不合法'); return 2; }
    var text = localStorage.getItem(fileKey(from));
    if (text === null) { lua.lua_pushnil(L); pushStr(L, '文件不存在: ' + from); return 2; }
    try {
      localStorage.setItem(fileKey(to), text);
      localStorage.removeItem(fileKey(from));
    } catch (e) {
      lua.lua_pushnil(L); pushStr(L, '重命名失败: ' + (e && e.message ? e.message : e)); return 2;
    }
    pushBool(L, true);
    return 1;
  }

  function fileStat(L) {
    var p = normalizePath(argString(L, 1));
    if (!p) { lua.lua_pushnil(L); pushStr(L, '路径不合法'); return 2; }
    var text = localStorage.getItem(fileKey(p));
    if (text !== null) {
      return pushValueTable(L, {
        size: utf8Len(text),
        mtime: Math.floor(Date.now() / 1000),
        dir: false,
        file: true
      });
    }
    if (fileChildren(p).length > 0) {
      return pushValueTable(L, { size: 0, mtime: 0, dir: true, file: false });
    }
    lua.lua_pushnil(L);
    pushStr(L, '文件不存在: ' + p);
    return 2;
  }

  function fileList(L) {
    var raw = optString(L, 1, '');
    var p = raw === '' || raw === '.' ? '' : normalizePath(raw);
    if (p === null) { lua.lua_pushnil(L); pushStr(L, '路径不合法'); return 2; }
    pushValueTable(L, fileChildren(p));
    return 1;
  }

  // --------------------------------------------------------------- store

  var STORE_PREFIX = '__berba_store:';

  function storeKeyArg(L, idx) {
    var key = argString(L, idx);
    if (!key) return null;
    if (key.length > 256) return null;
    return key;
  }

  function storeGet(L) {
    var key = storeKeyArg(L, 1);
    if (!key) return luaError(L, "bad argument #1 to 'store.get' (key 为空或超过 256 字节)");
    var text = localStorage.getItem(STORE_PREFIX + key);
    if (text === null) { lua.lua_pushnil(L); return 1; }
    try {
      pushJsValue(L, JSON.parse(text));
    } catch (e) {
      lua.lua_pushnil(L);
    }
    return 1;
  }

  function storeSet(L) {
    var key = storeKeyArg(L, 1);
    if (!key) return luaError(L, "bad argument #1 to 'store.set' (key 为空或超过 256 字节)");
    if (isNil(L, 2)) {
      localStorage.removeItem(STORE_PREFIX + key);
      return 0;
    }
    var value = readJsValue(L, 2);
    try {
      localStorage.setItem(STORE_PREFIX + key, JSON.stringify(value));
    } catch (e) {
      return luaError(L, 'store.set 写入失败: ' + (e && e.message ? e.message : e));
    }
    return 0;
  }

  function storeRemove(L) {
    var key = storeKeyArg(L, 1);
    if (!key) return luaError(L, "bad argument #1 to 'store.remove' (key 为空或超过 256 字节)");
    var existed = localStorage.getItem(STORE_PREFIX + key) !== null;
    localStorage.removeItem(STORE_PREFIX + key);
    pushBool(L, existed);
    return 1;
  }

  function storeKeys(L) {
    var keys = [];
    for (var i = 0; i < localStorage.length; i++) {
      var k = localStorage.key(i);
      if (k && k.indexOf(STORE_PREFIX) === 0) keys.push(k.slice(STORE_PREFIX.length));
    }
    keys.sort();
    pushValueTable(L, keys);
    return 1;
  }

  // ----------------------------------------------------------- permission

  function fullPermission(name) {
    return name.indexOf('.') >= 0 ? name : 'android.permission.' + name;
  }

  function permissionCheck(L) {
    var name = argString(L, 1);
    if (!name) return luaError(L, "bad argument #1 to 'permission.check' (string expected)");
    if (fullPermission(name) === 'android.permission.POST_NOTIFICATIONS'
        && typeof window.Notification === 'function') {
      pushBool(L, Notification.permission === 'granted');
      return 1;
    }
    pushBool(L, true);
    return 1;
  }

  function permissionRequest(L) {
    if (isNil(L, 1)) return luaError(L, "bad argument #1 to 'permission.request' (string or table expected)");
    var names = [];
    if (lua.lua_type(L, 1) === lua.LUA_TSTRING) {
      names.push(argString(L, 1));
    } else {
      var list = readJsValue(L, 1);
      if (!Array.isArray(list) || list.length === 0 || list.length > 32) {
        return luaError(L, "bad argument #1 to 'permission.request' (1-32 个权限名)");
      }
      names = list;
    }
    if (!isFunction(L, 2)) return luaError(L, "bad argument #2 to 'permission.request' (function expected)");
    var cb = refFunction(L, 2);
    var result = {};
    var pending = null;
    names.forEach(function (n) {
      if (typeof n !== 'string' || !n) return;
      var full = fullPermission(n);
      result[full] = true;
      if (full !== 'android.permission.POST_NOTIFICATIONS') return;
      if (typeof window.Notification !== 'function') {
        result[full] = false;
      } else if (Notification.permission === 'granted') {
        result[full] = true;
      } else if (Notification.permission === 'denied') {
        result[full] = false;
        console.warn('[berba dev] 浏览器已拒绝通知权限，请在站点设置中重新允许');
      } else {
        result[full] = false;
        pending = Notification.requestPermission();
      }
    });
    if (pending && pending.then) {
      pending.then(function (state) {
        result['android.permission.POST_NOTIFICATIONS'] = state === 'granted';
        callRef(L, cb, [result]);
      }).catch(function (e) {
        console.warn('[berba dev] 请求通知权限失败', e);
        callRef(L, cb, [result]);
      });
    } else {
      setTimeout(function () { callRef(L, cb, [result]); }, 0);
    }
    return 0;
  }

  // -------------------------------------------------------------- notify

  var notifications = Object.create(null);
  var notifySeq = 1;

  function notifySupported() {
    return typeof window.Notification === 'function';
  }

  function channelIdError(id) {
    if (id.length > 64) return 'notify channel id 不能超过 64 字符';
    if (!/^[A-Za-z0-9_.-]+$/.test(id)) return 'notify channel id 只能包含字母、数字、_ - .';
    return null;
  }

  function readChannel(raw) {
    var id = typeof raw === 'string' && raw.trim() ? raw.trim() : 'default';
    var err = channelIdError(id);
    return err ? { error: err } : { id: id };
  }

  function readNotifyId(value, allowAuto) {
    if (value === null || value === undefined) {
      if (!allowAuto) return { error: 'notify.cancel 需要 id' };
      var next = notifySeq++;
      return { key: 'n:' + next, id: next };
    }
    if (typeof value === 'number') {
      var n = Math.floor(value);
      if (value !== n || n <= 0) return { error: 'notify id 必须是正整数或非空字符串' };
      return { key: 'n:' + n, id: n };
    }
    if (typeof value === 'string') {
      if (!value || value.length > 256) return { error: '通知 id 长度需在 1-256 之间' };
      return { key: 's:' + value, id: value };
    }
    return { error: 'notify id 必须是正整数或非空字符串' };
  }

  function pushNotifyId(L, id) {
    if (typeof id === 'string') pushStr(L, id);
    else pushNumber(L, id);
    return 1;
  }

  function payloadTooLarge(data) {
    try {
      var text = JSON.stringify(data);
      return text !== undefined && text.length > 64 * 1024;
    } catch (e) {
      return true;
    }
  }

  function notifyShow(L) {
    var title = '';
    var body = '';
    var data = null;
    var ongoing = false;
    var autoCancel = true;
    var rawId = null;

    if (lua.lua_type(L, 1) === lua.LUA_TTABLE) {
      var t = readJsValue(L, 1) || {};
      if (typeof t.title === 'string') title = t.title;
      if (typeof t.body === 'string') body = t.body;
      if (t.channel !== undefined && t.channel !== null) {
        var ch = readChannel(t.channel);
        if (ch.error) return luaError(L, ch.error);
      }
      if (t.data !== undefined) data = t.data;
      ongoing = t.ongoing === true;
      if (t.autoCancel !== undefined && t.autoCancel !== null) autoCancel = t.autoCancel !== false;
      rawId = t.id;
    } else if (lua.lua_type(L, 1) === lua.LUA_TSTRING
        || lua.lua_type(L, 1) === lua.LUA_TNUMBER) {
      title = argString(L, 1);
      body = optString(L, 2, '');
    } else {
      return luaError(L, 'notify.show 需要 table 或标题字符串');
    }

    if (!notifySupported()) {
      lua.lua_pushnil(L);
      pushStr(L, '当前浏览器不支持通知');
      return 2;
    }
    if (Notification.permission !== 'granted') {
      console.warn('[berba dev] 通知权限未授予，请先调用 permission.request("POST_NOTIFICATIONS")');
      lua.lua_pushnil(L);
      pushStr(L, '通知权限未授予');
      return 2;
    }
    if (payloadTooLarge(data)) {
      return luaError(L, '通知 payload 超过 64KB');
    }

    var id = readNotifyId(rawId, true);
    if (id.error) return luaError(L, id.error);

    if (!title.trim()) title = appMeta.name || '通知';
    try {
      var old = notifications[id.key];
      if (old) {
        try { old.close(); } catch (e) { /* ignore */ }
      }
      var n = new Notification(title, {
        body: body,
        tag: id.key,
        requireInteraction: ongoing
      });
      notifications[id.key] = n;
      n.onclick = function () {
        try { window.focus(); } catch (e) { /* ignore */ }
        if (autoCancel) {
          try { n.close(); } catch (e) { /* ignore */ }
        }
        fireHandlers('notification', [{ id: id.id, data: data, coldStart: false }]);
      };
      n.onclose = function () {
        if (notifications[id.key] === n) delete notifications[id.key];
      };
      return pushNotifyId(L, id.id);
    } catch (e) {
      lua.lua_pushnil(L);
      pushStr(L, '显示通知失败: ' + (e && e.message ? e.message : e));
      return 2;
    }
  }

  function notifyCancel(L) {
    var value = isNil(L, 1) ? null : readJsValue(L, 1);
    if (value === null || value === undefined) {
      return luaError(L, 'notify.cancel 需要 id');
    }
    var id = readNotifyId(value, false);
    if (id.error) return luaError(L, id.error);
    var n = notifications[id.key];
    if (!n) {
      pushBool(L, false);
      return 1;
    }
    delete notifications[id.key];
    try { n.close(); } catch (e) { /* ignore */ }
    pushBool(L, true);
    return 1;
  }

  function notifyCancelAll(L) {
    Object.keys(notifications).forEach(function (key) {
      var n = notifications[key];
      delete notifications[key];
      try { n.close(); } catch (e) { /* ignore */ }
    });
    return 0;
  }

  function notifyEnabled(L) {
    pushBool(L, notifySupported() && Notification.permission === 'granted');
    return 1;
  }

  function notifyChannel(L) {
    if (lua.lua_type(L, 1) !== lua.LUA_TTABLE) {
      return luaError(L, "bad argument #1 to 'notify.channel' (table expected)");
    }
    var t = readJsValue(L, 1) || {};
    var ch = readChannel(t.id);
    if (ch.error) return luaError(L, ch.error);
    var importance = typeof t.importance === 'string' ? t.importance : 'default';
    if (importance !== 'min' && importance !== 'low'
        && importance !== 'default' && importance !== 'high') {
      return luaError(L, 'importance 只能是 min/low/default/high: ' + importance);
    }
    // 浏览器没有通道概念；保持设备侧相同的入参校验即可。
    pushBool(L, true);
    return 1;
  }

  // -------------------------------------------------------------- task

  // 浏览器没有后台调度；保留表与事件注册，排程时明确报错。
  function taskSchedule(L) {
    return luaError(L, '浏览器不支持后台任务');
  }

  function taskCancel(L) {
    pushBool(L, false);
    return 1;
  }

  function taskList(L) {
    lua.lua_newtable(L);
    return 1;
  }

  function taskExact(L) {
    pushBool(L, false);
    return 1;
  }

  // -------------------------------------------------------------- widget
  // 浏览器没有桌面小组件：只记录调用，返回空实例列表，避免静默成功。

  function widgetUpdate(L) {
    if (isNil(L, 1) || lua.lua_type(L, 1) !== lua.LUA_TTABLE) {
      return luaError(L, "bad argument #1 to 'widget.update' (table expected)");
    }
    var input = readJsValue(L, 1);
    var cards = [];
    if (input && Array.isArray(input.cards)) {
      cards = input.cards;
    } else if (Array.isArray(input)) {
      cards = input;
    }
    cards = cards.slice(0, 20);
    console.log('[berba dev] widget.update：浏览器环境不渲染桌面小组件（' + cards.length + ' 张卡片）');
    return pushValueTable(L, { updatedAt: Date.now(), ids: [] });
  }

  function widgetClear(L) {
    console.log('[berba dev] widget.clear：浏览器环境无桌面小组件');
    pushBool(L, true);
    return 1;
  }

  function widgetList(L) {
    return pushValueTable(L, []);
  }

  // -------------------------------------------------------------- native

  function unsupportedNativeFn(label) {
    return function (L) {
      return luaError(L, '原生插件仅在设备上可用（' + label + '）');
    };
  }

  function nativeList(L) {
    var plugins = nativeMeta && Array.isArray(nativeMeta.plugins) ? nativeMeta.plugins : [];
    var out = [];
    for (var i = 0; i < plugins.length; i++) {
      var p = plugins[i] || {};
      var entry = {
        name: p.name || '',
        lib: p.libName || ('libberba_' + (p.name || '') + '.so'),
        ok: false,
        error: '原生插件仅在设备上可用',
        modules: {}
      };
      var mods = p.modules || {};
      Object.keys(mods).sort().forEach(function (modName) {
        var fns = (mods[modName] && mods[modName].functions) || {};
        var functions = {};
        Object.keys(fns).sort().forEach(function (fnName) {
          functions[fnName] = { async: !!(fns[fnName] && fns[fnName].async) };
        });
        entry.modules[modName] = { functions: functions };
      });
      out.push(entry);
    }
    return pushValueTable(L, out);
  }

  function installNative(L) {
    lua.lua_newtable(L);
    var t = lua.lua_gettop(L);
    lua.lua_pushjsfunction(L, nativeList);
    lua.lua_setfield(L, t, utf8('list'));

    var plugins = nativeMeta && Array.isArray(nativeMeta.plugins) ? nativeMeta.plugins : [];
    var seen = Object.create(null);
    for (var i = 0; i < plugins.length; i++) {
      var mods = (plugins[i] && plugins[i].modules) || {};
      Object.keys(mods).sort().forEach(function (modName) {
        if (modName === 'list') return;
        if (seen[modName]) {
          console.error('[berba dev] 原生模块名冲突，已忽略: ' + modName);
          return;
        }
        seen[modName] = true;
        lua.lua_newtable(L);
        var mt = lua.lua_gettop(L);
        var fns = (mods[modName] && mods[modName].functions) || {};
        Object.keys(fns).sort().forEach(function (fnName) {
          lua.lua_pushjsfunction(L, unsupportedNativeFn('native.' + modName + '.' + fnName));
          lua.lua_setfield(L, mt, utf8(fnName));
        });
        lua.lua_setfield(L, t, utf8(modName));
      });
    }
    lua.lua_setglobal(L, utf8('native'));
  }

  // ---------------------------------------------------------------- ui

  function showToast(text, duration) {
    var host = document.body || document.documentElement;
    if (!host) return;
    var el = document.createElement('div');
    el.textContent = text;
    el.style.cssText = [
      'position:fixed', 'left:50%', 'bottom:32px', 'transform:translateX(-50%)',
      'max-width:78vw', 'background:rgba(32,32,38,.94)', 'color:#fff',
      'padding:10px 18px', 'border-radius:999px', 'z-index:2147483647',
      'box-shadow:0 6px 24px rgba(0,0,0,.35)', 'font:14px/1.4 system-ui,-apple-system,sans-serif',
      'opacity:0', 'transition:opacity .18s ease', 'pointer-events:none', 'text-align:center'
    ].join(';');
    host.appendChild(el);
    requestAnimationFrame(function () { el.style.opacity = '1'; });
    setTimeout(function () {
      el.style.opacity = '0';
      setTimeout(function () { if (el.parentNode) el.parentNode.removeChild(el); }, 220);
    }, duration);
  }

  function uiToast(L) {
    var msg = argString(L, 1);
    if (msg === null) return luaError(L, "bad argument #1 to 'ui.toast' (string expected)");
    var d = 2000;
    var raw = optString(L, 2, 'short');
    if (raw === 'long') d = 3500;
    else if (raw !== 'short') {
      var n = Number(raw);
      d = isFinite(n) && n > 0 ? n : 2000;
    }
    showToast(msg, d);
    return 0;
  }

  function uiStatusBar(L) {
    console.warn('[berba dev] ui.statusBar 在浏览器中无效');
    return 0;
  }

  function uiAlert(L) {
    var msg = argString(L, 1);
    window.alert(msg === null ? '' : msg);
    return 0;
  }

  function uiConfirm(L) {
    var msg = argString(L, 1);
    var ok = window.confirm(msg === null ? '' : msg);
    if (!isNil(L, 2)) {
      if (!isFunction(L, 2)) return luaError(L, "bad argument #2 to 'ui.confirm' (function expected)");
      var cb = refFunction(L, 2);
      setTimeout(function () { callRef(L, cb, [ok]); }, 0);
    }
    return 0;
  }

  function uiFullscreen(L) {
    var on = argBool(L, 1);
    var el = document.documentElement;
    if (on) {
      if (el.requestFullscreen && !document.fullscreenElement) {
        var p = el.requestFullscreen();
        if (p && p.catch) p.catch(function (e) { console.warn('[berba dev] 进入全屏失败（需要用户手势）', e); });
      }
    } else if (document.fullscreenElement && document.exitFullscreen) {
      var q = document.exitFullscreen();
      if (q && q.catch) q.catch(function (e) { console.warn('[berba dev] 退出全屏失败', e); });
    }
    return 0;
  }

  function uiKeepScreenOn(L) {
    var on = argBool(L, 1);
    if (!navigator.wakeLock) {
      if (on) console.warn('[berba dev] 当前浏览器不支持 Wake Lock（需要 https 或 localhost），屏幕常亮无效');
      return 0;
    }
    if (on) {
      if (wakeLock) return 0;
      navigator.wakeLock.request('screen').then(function (sentinel) {
        wakeLock = sentinel;
        sentinel.addEventListener('release', function () { wakeLock = null; });
      }).catch(function (e) {
        console.warn('[berba dev] Wake Lock 申请失败', e);
      });
    } else if (wakeLock) {
      wakeLock.release().catch(function () { /* ignore */ });
      wakeLock = null;
    }
    return 0;
  }

  // ------------------------------------------------------------- device

  function deviceClipboard(L) {
    if (isNil(L, 1)) {
      console.warn('[berba dev] 浏览器不允许同步读取剪贴板，device.clipboard() 返回空串');
      pushStr(L, '');
      return 1;
    }
    var text = argString(L, 1);
    if (text === null) return luaError(L, "bad argument #1 to 'device.clipboard' (string expected)");
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).catch(function (e) {
        console.warn('[berba dev] 写入剪贴板失败', e);
      });
    } else {
      console.warn('[berba dev] 当前浏览器不支持写入剪贴板');
    }
    pushBool(L, true);
    return 1;
  }

  function deviceVibrate(L) {
    var ms = lua.lua_tonumber(L, 1);
    if (!isFinite(ms) || ms < 1 || ms > 10000) {
      return luaError(L, "bad argument #1 to 'device.vibrate' (1-10000 毫秒)");
    }
    if (navigator.vibrate) {
      try { navigator.vibrate(Math.floor(ms)); } catch (e) { console.warn('[berba dev] 震动失败', e); }
    } else {
      console.warn('[berba dev] 当前浏览器不支持震动');
    }
    return 0;
  }

  function deviceShare(L) {
    var payload = {};
    if (lua.lua_type(L, 1) === lua.LUA_TTABLE) {
      var t = readJsValue(L, 1) || {};
      if (typeof t.text === 'string') payload.text = t.text;
      if (typeof t.url === 'string') payload.url = t.url;
    } else {
      var text = argString(L, 1);
      if (text === null) return luaError(L, "bad argument #1 to 'device.share' (string or table expected)");
      payload.text = text;
    }
    if (!navigator.share) {
      console.warn('[berba dev] 当前浏览器不支持系统分享面板');
      return 0;
    }
    navigator.share(payload).catch(function (e) {
      if (e && e.name === 'AbortError') return;
      console.warn('[berba dev] 分享失败', e);
    });
    return 0;
  }

  function deviceOpenUrl(L) {
    var url = argString(L, 1);
    if (!url || !/^https?:\/\//i.test(url)) {
      return luaError(L, "bad argument #1 to 'device.openUrl' (只接受 http/https)");
    }
    var win = window.open(url, '_blank', 'noopener');
    if (!win) console.warn('[berba dev] 打开新标签页被浏览器拦截: ' + url);
    return 0;
  }

  function deviceOrientation(L) {
    var mode = argString(L, 1);
    if (mode !== 'portrait' && mode !== 'landscape' && mode !== 'sensor' && mode !== 'auto') {
      return luaError(L, "bad argument #1 to 'device.orientation' (portrait/landscape/sensor/auto)");
    }
    var so = screen.orientation;
    if (!so || !so.lock) {
      console.warn('[berba dev] 当前浏览器不支持方向锁定');
      return 0;
    }
    if (mode === 'auto') {
      if (so.unlock) so.unlock();
      return 0;
    }
    var p = so.lock(mode === 'sensor' ? 'any' : mode);
    if (p && p.catch) p.catch(function (e) { console.warn('[berba dev] 方向锁定失败', e); });
    return 0;
  }

  function deviceModel(L) {
    var ua = navigator.userAgent || '';
    var label = 'browser';
    if (/Android/i.test(ua)) label = 'Android browser';
    else if (/iPhone|iPad|iPod/i.test(ua)) label = 'iOS browser';
    else if (/Windows/i.test(ua)) label = 'Windows browser';
    else if (/Macintosh/i.test(ua)) label = 'macOS browser';
    else if (/Linux/i.test(ua)) label = 'Linux browser';
    pushStr(L, label);
    return 1;
  }

  function deviceSdk(L) {
    lua.lua_pushinteger(L, 0);
    return 1;
  }

  function devicePlatform(L) {
    pushStr(L, 'browser');
    return 1;
  }

  function deviceLocale(L) {
    var lang = navigator.language || (navigator.languages && navigator.languages[0]) || 'en';
    pushStr(L, String(lang).replace(/-/g, '_'));
    return 1;
  }

  function deviceScreen(L) {
    var dpr = window.devicePixelRatio || 1;
    return pushValueTable(L, {
      width: Math.round(screen.width * dpr),
      height: Math.round(screen.height * dpr),
      density: dpr,
      dpi: Math.round(160 * dpr)
    });
  }

  // ------------------------------------------------------------- json/log

  function jsonEncode(L) {
    if (isNil(L, 1)) { pushStr(L, 'null'); return 1; }
    var value;
    try {
      value = luaToJs(L, 1, 0, []);
    } catch (e) {
      pushStr(L, 'null');
      return 1;
    }
    var text;
    try {
      text = JSON.stringify(value);
    } catch (e) {
      text = 'null';
    }
    if (text === undefined) text = 'null';
    pushStr(L, text);
    return 1;
  }

  function jsonDecode(L) {
    var text = argString(L, 1);
    if (text === null) {
      lua.lua_pushnil(L);
      pushStr(L, "bad argument #1 to 'json.decode' (string expected)");
      return 2;
    }
    var value;
    try {
      value = JSON.parse(text);
    } catch (e) {
      lua.lua_pushnil(L);
      pushStr(L, 'JSON 解析失败: ' + (e && e.message ? e.message : e));
      return 2;
    }
    pushJsValue(L, value);
    return 1;
  }

  function logImpl(L) {
    var n = lua.lua_gettop(L);
    var parts = ['[berba lua]'];
    for (var i = 1; i <= n; i++) {
      var t = lua.lua_type(L, i);
      if (t === lua.LUA_TSTRING) {
        parts.push(argString(L, i));
      } else if (t === lua.LUA_TNUMBER) {
        parts.push(String(lua.lua_tonumber(L, i)));
      } else if (t === lua.LUA_TBOOLEAN) {
        parts.push(lua.lua_toboolean(L, i) ? 'true' : 'false');
      } else if (t === lua.LUA_TNIL) {
        parts.push('nil');
      } else {
        var v = readJsValue(L, i);
        try { parts.push(JSON.stringify(v)); } catch (e) { parts.push(String(v)); }
      }
    }
    console.log.apply(console, parts);
    return 0;
  }

  // ------------------------------------------------------------ 注册全局

  function setModule(L, name, members) {
    lua.lua_newtable(L);
    var t = lua.lua_gettop(L);
    Object.keys(members).forEach(function (key) {
      lua.lua_pushjsfunction(L, members[key]);
      lua.lua_setfield(L, t, utf8(key));
    });
    lua.lua_setglobal(L, utf8(name));
  }

  function capabilityEnabled(name) {
    if (!capabilityMeta) return true;
    return capabilityMeta[name] === true;
  }

  function stubFunction(label) {
    return function (L) {
      return luaError(L, '能力未在当前壳中实现: ' + label);
    };
  }

  // 与设备壳一致：能力元数据声明了但运行时未安装的模块/函数安装抛错桩。
  function applyModuleMeta(L) {
    if (!moduleMeta) return;
    Object.keys(moduleMeta).sort().forEach(function (name) {
      var fns = Array.isArray(moduleMeta[name]) ? moduleMeta[name] : [];
      lua.lua_getglobal(L, utf8(name));
      if (!lua.lua_istable(L, -1)) {
        lua.lua_pop(L, 1);
        console.error('[berba dev] 能力模块未安装，已安装抛错桩: ' + name);
        var members = {};
        fns.forEach(function (fn) { members[fn] = stubFunction(name + '.' + fn); });
        setModule(L, name, members);
        return;
      }
      fns.forEach(function (fn) {
        lua.lua_getfield(L, -1, utf8(fn));
        if (!lua.lua_isfunction(L, -1)) {
          lua.lua_pop(L, 1);
          console.error('[berba dev] 能力函数未安装，已安装抛错桩: ' + name + '.' + fn);
          lua.lua_pushjsfunction(L, stubFunction(name + '.' + fn));
          lua.lua_setfield(L, -2, utf8(fn));
        } else {
          lua.lua_pop(L, 1);
        }
      });
      lua.lua_pop(L, 1);
    });
  }

  function checkCapabilities() {
    if (!capabilityMeta) return;
    Object.keys(capabilityMeta).sort().forEach(function (name) {
      if (name !== 'native' && name !== 'notify' && name !== 'task' && name !== 'widget') {
        console.error('[berba dev] 壳未实现能力: ' + name + '（相关函数将抛错）');
      }
    });
  }

  function registerGlobals(L) {
    APP_EVENTS = buildEventSet('app', CORE_APP_EVENTS, ['notification', 'task', 'widget']);
    WEB_EVENTS = buildEventSet('web', CORE_WEB_EVENTS, []);
    setModule(L, 'app', { on: appOn, exit: appExit, info: appInfo });
    setModule(L, 'web', {
      load: webLoad,
      reload: function () { location.reload(); return 0; },
      goBack: function () { history.back(); return 0; },
      canGoBack: function (L) { pushBool(L, history.length > 1); return 1; },
      eval: webEval,
      on: webOn
    });
    setModule(L, 'bridge', { register: bridgeRegister, emit: bridgeEmit });
    setModule(L, 'http', {
      get: httpGet, post: httpPost, request: httpRequest, cancel: httpCancel
    });
    setModule(L, 'file', {
      read: fileRead, write: fileWrite, append: fileAppend, exists: fileExistsFn,
      delete: fileDelete, rename: fileRename, stat: fileStat, list: fileList
    });
    setModule(L, 'store', {
      get: storeGet, set: storeSet, remove: storeRemove, keys: storeKeys
    });
    setModule(L, 'permission', { check: permissionCheck, request: permissionRequest });
    if (capabilityEnabled('notify')) {
      setModule(L, 'notify', {
        show: notifyShow, cancel: notifyCancel, cancelAll: notifyCancelAll,
        enabled: notifyEnabled, channel: notifyChannel
      });
    }
    if (capabilityEnabled('task')) {
      setModule(L, 'task', {
        schedule: taskSchedule, cancel: taskCancel, list: taskList, exact: taskExact
      });
    }
    if (capabilityEnabled('widget')) {
      setModule(L, 'widget', {
        update: widgetUpdate, clear: widgetClear, list: widgetList
      });
    }
    if (capabilityEnabled('native')) {
      installNative(L);
    }
    setModule(L, 'ui', {
      toast: uiToast, statusBar: uiStatusBar, alert: uiAlert, confirm: uiConfirm,
      fullscreen: uiFullscreen, keepScreenOn: uiKeepScreenOn
    });
    setModule(L, 'device', {
      clipboard: deviceClipboard, vibrate: deviceVibrate, share: deviceShare,
      openUrl: deviceOpenUrl, orientation: deviceOrientation, model: deviceModel,
      sdk: deviceSdk, platform: devicePlatform, locale: deviceLocale, screen: deviceScreen
    });
    setModule(L, 'json', { encode: jsonEncode, decode: jsonDecode });

    lua.lua_pushjsfunction(L, logImpl);
    lua.lua_setglobal(L, utf8('log'));

    lua.lua_pushjsfunction(L, requireImpl);
    lua.lua_setglobal(L, utf8('require'));

    installBit32(L);
    applyModuleMeta(L);
    checkCapabilities();
  }

  // ---------------------------------------------------------------- 启动

  function boot() {
    var rc;
    try {
      L = newState();
      registerGlobals(L);
    } catch (e) {
      console.error('[berba dev] Lua 状态初始化失败', e);
      throw e;
    }

    if (!Object.prototype.hasOwnProperty.call(files, entryPath)) {
      throw new Error('Lua 入口不存在: ' + entryPath);
    }

    lua.lua_pushjsfunction(L, tracebackMsgh);
    var h = lua.lua_gettop(L);
    rc = lauxlib.luaL_loadbuffer(L, utf8(files[entryPath]), null, utf8(entryPath));
    if (rc === LUA_OK) rc = lua.lua_pcall(L, 0, 0, h);
    var failure = rc === LUA_OK ? null : (argString(L, -1) || ('错误码 ' + rc));
    lua.lua_settop(L, 0);
    if (failure !== null) {
      console.error('[berba dev] Lua 入口执行失败:\n' + failure);
      throw new Error(failure);
    }

    fireHandlers('boot', []);
    fireHandlers('create', []);
    if (!webLoaded) fireWebHandlers('pageFinished', [location.href]);
    console.log('[berba dev] Lua 运行时就绪: ' + entryPath);
    return { call: callBridge, has: hasBridge };
  }

  function parseCapabilityMeta(cfg) {
    if (!Array.isArray(cfg.capabilities)) return null;
    var set = {};
    cfg.capabilities.forEach(function (name) {
      if (typeof name === 'string' && name) set[name] = true;
    });
    return set;
  }

  function parseModuleMeta(cfg) {
    var src = cfg.modules;
    if (!src || typeof src !== 'object') return null;
    var out = {};
    Object.keys(src).forEach(function (name) {
      var value = src[name];
      if (Array.isArray(value)) {
        out[name] = value.filter(function (fn) { return typeof fn === 'string'; });
      } else if (value && typeof value === 'object') {
        out[name] = Object.keys(value.functions || {});
      } else {
        out[name] = [];
      }
    });
    return out;
  }

  function init(cfg) {
    if (initialized) return initialized;
    cfg = cfg || {};
    files = cfg.files || {};
    entryPath = String(cfg.entry || 'main.lua').replace(/^\/+/, '');
    appMeta = cfg.app || {};
    capabilityMeta = parseCapabilityMeta(cfg);
    moduleMeta = parseModuleMeta(cfg);
    eventMeta = cfg.events && typeof cfg.events === 'object' ? cfg.events : null;
    nativeMeta = cfg.native && typeof cfg.native === 'object' ? cfg.native : null;

    initialized = new Promise(function (resolve, reject) {
      function start() {
        try {
          resolve(boot());
        } catch (e) {
          reject(e);
        }
      }
      if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', start);
      } else {
        start();
      }
    });
    initialized.catch(function () { /* 由调用方处理 */ });
    return initialized;
  }

  // 浏览器里没有返回键；手动触发 app.on("back")，返回是否已处理。
  window.__berbaDev = window.__berbaDev || {};
  window.__berbaDev.back = function () {
    if (!L) {
      console.warn('[berba dev] Lua 尚未就绪，无法触发 back');
      return false;
    }
    return fireHandlers('back', []);
  };
  window.__berbaDev.hasBridge = hasBridge;
  // 浏览器通知在页面关闭后不再回调，用这个手动触发 app.on("notification")。
  window.__berbaDev.notify = function (id, data) {
    if (!L) {
      console.warn('[berba dev] Lua 尚未就绪，无法触发 notification');
      return false;
    }
    return fireHandlers('notification', [{
      id: id === undefined ? null : id,
      data: data === undefined ? null : data,
      coldStart: false
    }]);
  };

  document.addEventListener('visibilitychange', function () {
    if (!L) return;
    fireHandlers(document.hidden ? 'pause' : 'resume', []);
  });
  window.addEventListener('pagehide', function () {
    if (L) fireHandlers('destroy', []);
  });

  window.__berbaLua = { init: init };
})();
