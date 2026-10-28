package com.berba.shell;

import android.os.Handler;
import android.os.HandlerThread;
import android.os.Looper;
import android.util.Log;

import org.luaj.vm2.Globals;
import org.luaj.vm2.LuaError;
import org.luaj.vm2.LuaTable;
import org.luaj.vm2.LuaValue;
import org.luaj.vm2.Varargs;
import org.luaj.vm2.lib.VarArgFunction;

import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/**
 * Lua 沙箱运行时：单工作线程，白名单库，所有回调在本线程执行。
 */
final class LuaRuntime {
    private static final String TAG = "berba";
    private static final Set<String> APP_EVENTS = new HashSet<>();
    private static final Set<String> WEB_EVENTS = new HashSet<>();

    static {
        for (String e : new String[]{"create", "pause", "resume", "destroy", "back", "newIntent"}) {
            APP_EVENTS.add(e);
        }
        for (String e : new String[]{"pageStarted", "pageFinished", "error"}) {
            WEB_EVENTS.add(e);
        }
    }

    interface BridgeCallback {
        void onResult(boolean ok, String payload);
    }

    private final MainActivity activity;
    private final AppConfig config;
    private final HandlerThread thread;
    private Handler handler;
    private Globals globals;
    private final Map<String, LuaValue> bridgeHandlers = new HashMap<>();
    private final Map<String, List<LuaValue>> eventHandlers = new HashMap<>();
    private final Map<String, LuaValue> moduleCache = new HashMap<>();
    private final Set<String> moduleLoading = new HashSet<>();
    private final ExecutorService io = Executors.newCachedThreadPool();
    private volatile boolean started;
    private volatile boolean shuttingDown;

    LuaRuntime(MainActivity activity, AppConfig config) {
        this.activity = activity;
        this.config = config;
        this.thread = new HandlerThread("berba-lua");
    }

    MainActivity activity() {
        return activity;
    }

    AppConfig config() {
        return config;
    }

    ExecutorService io() {
        return io;
    }

    void start(Runnable onReady) {
        thread.start();
        handler = new Handler(thread.getLooper());
        handler.post(() -> {
            try {
                globals = createGlobals();
                LuaApis.install(this, globals);
                String entry = config.luaEntry;
                if (entry != null) {
                    LuaValue chunk = globals.load(readAsset("lua/" + entry), "@assets/lua/" + entry);
                    chunk.call();
                }
                started = true;
            } catch (Throwable t) {
                Log.e(TAG, "Lua 初始化失败", t);
                activity.toastOnMain("Lua 初始化失败: " + message(t), true);
            }
            if (onReady != null) {
                onReady.run();
            }
        });
    }

    private Globals createGlobals() {
        Globals g = LuaSandbox.create();
        g.set("require", new VarArgFunction() {
            @Override
            public Varargs invoke(Varargs args) {
                try {
                    return loadModule(args.checkjstring(1));
                } catch (Throwable t) {
                    throw new LuaError(message(t));
                }
            }
        });
        g.set("print", new VarArgFunction() {
            @Override
            public Varargs invoke(Varargs args) {
                StringBuilder sb = new StringBuilder();
                for (int i = 1; i <= args.narg(); i++) {
                    if (i > 1) {
                        sb.append('\t');
                    }
                    sb.append(args.arg(i).tojstring());
                }
                Log.i(TAG, sb.toString());
                return LuaValue.NONE;
            }
        });
        return g;
    }

    /** 从 assets/lua 加载模块并缓存，支持 a.b -> a/b.lua 与 a/b/init.lua。 */
    LuaValue loadModule(String name) throws IOException {
        if (name == null || name.isEmpty()) {
            throw new LuaError("require 名称不能为空");
        }
        LuaValue cached = moduleCache.get(name);
        if (cached != null) {
            return cached;
        }
        if (moduleLoading.contains(name)) {
            return LuaValue.TRUE;
        }
        String rel = name.replace('.', '/');
        String source = null;
        String path = null;
        for (String candidate : new String[]{rel + ".lua", rel + "/init.lua"}) {
            try {
                source = readAsset("lua/" + candidate);
                path = candidate;
                break;
            } catch (IOException ignored) {
            }
        }
        if (source == null) {
            throw new LuaError("模块不存在: " + name);
        }
        moduleLoading.add(name);
        try {
            LuaValue result = globals.load(source, "@assets/lua/" + path).call();
            LuaValue module = result.isnil() ? LuaValue.TRUE : result;
            moduleCache.put(name, module);
            return module;
        } finally {
            moduleLoading.remove(name);
        }
    }

    String readAsset(String path) throws IOException {
        try (InputStream in = activity.getAssets().open(path)) {
            java.io.ByteArrayOutputStream out = new java.io.ByteArrayOutputStream();
            byte[] buf = new byte[8192];
            int n;
            while ((n = in.read(buf)) > 0) {
                out.write(buf, 0, n);
            }
            return out.toString(StandardCharsets.UTF_8.name());
        }
    }

    void post(Runnable r) {
        if (handler == null) {
            return;
        }
        handler.post(() -> {
            try {
                r.run();
            } catch (Throwable t) {
                Log.e(TAG, "Lua 任务异常", t);
            }
        });
    }

    void onAppEvent(String event, LuaValue... args) {
        post(() -> dispatchEvent(event, args));
    }

    /** 执行事件处理器后把 completion 交回主线程。 */
    void onAppEvent(String event, Runnable completion, LuaValue... args) {
        post(() -> {
            dispatchEvent(event, args);
            if (completion != null) {
                activity.runOnUiThread(completion);
            }
        });
    }

    /** 返回键：若没有处理器返回 true，则回调主线程执行默认返回。 */
    void onBack(Runnable defaultBack) {
        post(() -> {
            boolean handled = false;
            for (LuaValue fn : snapshotHandlers("back")) {
                LuaValue r = invokeSafely(fn, LuaValue.NONE);
                if (r != null && r.toboolean()) {
                    handled = true;
                }
            }
            if (!handled && defaultBack != null) {
                activity.runOnUiThread(defaultBack);
            }
        });
    }

    boolean hasHandlers(String event) {
        synchronized (eventHandlers) {
            List<LuaValue> list = eventHandlers.get(event);
            return list != null && !list.isEmpty();
        }
    }

    private List<LuaValue> snapshotHandlers(String event) {
        synchronized (eventHandlers) {
            List<LuaValue> list = eventHandlers.get(event);
            return list == null ? new ArrayList<>() : new ArrayList<>(list);
        }
    }

    void addHandler(String event, LuaValue fn) {
        synchronized (eventHandlers) {
            List<LuaValue> list = eventHandlers.get(event);
            if (list == null) {
                list = new ArrayList<>();
                eventHandlers.put(event, list);
            }
            list.add(fn);
        }
    }

    private void dispatchEvent(String event, LuaValue[] args) {
        Varargs varargs = args == null || args.length == 0 ? LuaValue.NONE : LuaValue.varargsOf(args);
        for (LuaValue fn : snapshotHandlers(event)) {
            invokeSafely(fn, varargs);
        }
    }

    private LuaValue invokeSafely(LuaValue fn, Varargs args) {
        try {
            return fn.invoke(args).arg1();
        } catch (Throwable t) {
            Log.e(TAG, "Lua 回调错误: " + message(t), t);
            return null;
        }
    }

    void registerBridge(String name, LuaValue fn) {
        if (name == null || name.trim().isEmpty()) {
            throw new LuaError("bridge.register 名称不能为空");
        }
        if (name.length() > 128) {
            throw new LuaError("bridge.register 名称过长");
        }
        synchronized (bridgeHandlers) {
            bridgeHandlers.put(name, fn);
        }
    }

    void callBridge(String name, String argsJson, BridgeCallback cb) {
        post(() -> {
            LuaValue fn;
            synchronized (bridgeHandlers) {
                fn = bridgeHandlers.get(name);
            }
            if (fn == null) {
                cb.onResult(false, "未注册的桥接方法: " + name);
                return;
            }
            try {
                LuaValue arg = argsJson == null || argsJson.isEmpty()
                        ? LuaValue.NIL : LuaConvert.parse(argsJson);
                LuaValue result = fn.invoke(arg).arg1();
                cb.onResult(true, LuaConvert.toJson(result));
            } catch (Throwable t) {
                cb.onResult(false, message(t));
            }
        });
    }

    void emitToJs(String name, String payloadJson) {
        activity.runOnUiThread(() -> activity.evaluateJs(
                "window.__berbaEvent(" + org.json.JSONObject.quote(name) + ","
                        + org.json.JSONObject.quote(payloadJson == null ? "null" : payloadJson) + ");",
                null));
    }

    void submitIo(Runnable r) {
        io.execute(() -> {
            try {
                r.run();
            } catch (Throwable t) {
                Log.e(TAG, "IO 任务异常", t);
            }
        });
    }

    void shutdown() {
        if (handler == null || shuttingDown) {
            return;
        }
        shuttingDown = true;
        handler.post(() -> {
            dispatchEvent("destroy", null);
            LuaApis.cancelAllHttp();
            io.shutdownNow();
            thread.quitSafely();
        });
    }

    static boolean isAppEvent(String name) {
        return APP_EVENTS.contains(name);
    }

    static boolean isWebEvent(String name) {
        return WEB_EVENTS.contains(name);
    }

    static String message(Throwable t) {
        if (t instanceof LuaError) {
            LuaError le = (LuaError) t;
            String m = le.getMessage();
            return m == null ? "Lua 错误" : m;
        }
        String m = t.getMessage();
        return m == null ? t.getClass().getSimpleName() : m;
    }

    LuaTable newTable() {
        return new LuaTable();
    }

    Globals globals() {
        return globals;
    }
}
