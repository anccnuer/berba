package com.berba.shell;

import android.content.Context;
import android.content.SharedPreferences;
import android.os.Handler;
import android.os.HandlerThread;
import android.util.Log;

import org.json.JSONObject;
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
 * 进程级 Lua 沙箱运行时：单工作线程，白名单库，所有 Lua 回调在本线程执行。
 * 不持有 Activity；界面通过 UiSession attach/detach。
 */
final class LuaRuntime {
    static final String PREFS_NAME = "berba";
    private static final String TAG = "berba";

    interface BridgeCallback {
        void onResult(boolean ok, String payload);
    }

    private final Context context;
    private final AppConfig config;
    private final CapabilityConfig capabilities;
    private final NotifyController notify;
    private final HandlerThread thread;
    private volatile Handler handler;
    private Globals globals;
    private final Map<String, LuaValue> bridgeHandlers = new HashMap<>();
    private final Map<String, List<LuaValue>> eventHandlers = new HashMap<>();
    private final Map<String, LuaValue> moduleCache = new HashMap<>();
    private final Set<String> moduleLoading = new HashSet<>();
    private final List<Runnable> whenReady = new ArrayList<>();
    private final ExecutorService io = Executors.newCachedThreadPool();
    private volatile boolean started;
    private volatile boolean ready;
    private volatile Throwable initError;
    private boolean initErrorNotified;
    private volatile boolean shuttingDown;
    private volatile UiSession ui;

    LuaRuntime(Context context, AppConfig config, CapabilityConfig capabilities,
               NotifyController notify) {
        this.context = context.getApplicationContext();
        this.config = config;
        this.capabilities = capabilities;
        this.notify = notify;
        this.thread = new HandlerThread("berba-lua");
    }

    AppConfig config() {
        return config;
    }

    CapabilityConfig capabilities() {
        return capabilities;
    }

    Context context() {
        return context;
    }

    SharedPreferences prefs() {
        return context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE);
    }

    ExecutorService io() {
        return io;
    }

    NotifyController notifyController() {
        return notify;
    }

    UiSession ui() {
        return ui;
    }

    UiSession requireUi() {
        UiSession session = ui;
        if (session == null) {
            throw new LuaError("当前没有前台界面，无法调用该 API");
        }
        return session;
    }

    /** 启动 Lua 线程；重复调用无副作用。 */
    void start() {
        synchronized (this) {
            if (started) {
                return;
            }
            started = true;
        }
        thread.start();
        handler = new Handler(thread.getLooper());
        handler.post(this::init);
    }

    private void init() {
        try {
            globals = createGlobals();
            CapabilityRegistry.install(this, globals, capabilities);
            verifyCapabilities();
            String entry = config.luaEntry;
            if (entry != null) {
                LuaValue chunk = globals.load(readAsset("lua/" + entry), "@assets/lua/" + entry);
                chunk.call();
            }
            dispatchEvent("boot", null);
        } catch (Throwable t) {
            initError = t;
            Log.e(TAG, "Lua 初始化失败", t);
        } finally {
            ready = true;
            runWhenReady();
            notifyInitError();
        }
    }

    private void runWhenReady() {
        List<Runnable> pending;
        synchronized (whenReady) {
            pending = new ArrayList<>(whenReady);
            whenReady.clear();
        }
        for (Runnable r : pending) {
            runSafely(r, "Lua 就绪任务异常");
        }
    }

    private void notifyInitError() {
        UiSession session = ui;
        if (initError == null || session == null || !session.isCurrent()) {
            return;
        }
        synchronized (this) {
            if (initErrorNotified) {
                return;
            }
            initErrorNotified = true;
        }
        session.activity().toastOnMain("Lua 初始化失败: " + message(initError), true);
    }

    /** 初始化完成后在 Lua 线程执行；未就绪时排队。 */
    void whenReady(Runnable r) {
        if (r == null) {
            return;
        }
        synchronized (whenReady) {
            if (!ready) {
                whenReady.add(r);
                return;
            }
        }
        post(r);
    }

    /** 绑定界面会话；初始化完成后触发 create，再回到主线程执行 afterCreate。 */
    void attach(UiSession session, Runnable afterCreate) {
        if (session == null) {
            return;
        }
        ui = session;
        if (initError != null) {
            notifyInitError();
        }
        whenReady(() -> {
            if (!isCurrent(session)) {
                return;
            }
            dispatchEvent("create", null);
            if (afterCreate != null) {
                session.activity().runOnUiThread(() -> {
                    if (isCurrent(session)) {
                        runSafely(afterCreate, "界面创建回调异常");
                    }
                });
            }
        });
    }

    /** 解绑界面会话；只触发 destroy，不取消 HTTP、不结束运行时。 */
    void detach(UiSession session) {
        if (session == null || ui != session) {
            return;
        }
        ui = null;
        post(() -> dispatchEvent("destroy", null));
    }

    boolean isCurrent(UiSession session) {
        return session != null && ui == session && session.isCurrent();
    }

    private boolean isCurrent(long epoch) {
        UiSession session = ui;
        return session != null && session.epoch() == epoch && session.isCurrent();
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

    /** 核对能力规格中的模块/函数；壳未实现的能力安装抛错桩而不是静默缺失。 */
    private void verifyCapabilities() {
        for (Map.Entry<String, CapabilityConfig.Module> entry : capabilities.modules().entrySet()) {
            String name = entry.getKey();
            List<String> functions = entry.getValue().functions;
            LuaValue module = globals.get(name);
            if (!module.istable()) {
                Log.e(TAG, "能力模块未安装，已安装抛错桩: " + name);
                globals.set(name, stubModule(name, functions));
                continue;
            }
            LuaTable table = module.checktable();
            for (String fn : functions) {
                if (!table.get(fn).isfunction()) {
                    Log.e(TAG, "能力函数未安装，已安装抛错桩: " + name + "." + fn);
                    table.set(fn, stubFunction(name + "." + fn));
                }
            }
        }
        for (String name : capabilities.globals()) {
            if (!globals.get(name).isfunction()) {
                Log.e(TAG, "能力全局函数未安装，已安装抛错桩: " + name);
                globals.set(name, stubFunction(name));
            }
        }
        for (String capability : capabilities.capabilities()) {
            if (!CapabilityRegistry.isImplemented(capability)) {
                Log.e(TAG, "壳未实现能力: " + capability + "（相关函数将抛错）");
            }
        }
    }

    private static LuaTable stubModule(String module, List<String> functions) {
        LuaTable table = new LuaTable();
        for (String fn : functions) {
            table.set(fn, stubFunction(module + "." + fn));
        }
        return table;
    }

    private static LuaValue stubFunction(final String name) {
        return new VarArgFunction() {
            @Override
            public Varargs invoke(Varargs args) {
                throw new LuaError("能力未在当前壳中实现: " + name);
            }
        };
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
        try (InputStream in = context.getAssets().open(path)) {
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
        Handler h = handler;
        if (h == null || shuttingDown) {
            return;
        }
        h.post(() -> runSafely(r, "Lua 任务异常"));
    }

    /** 在 Lua 线程延迟执行；运行时已关闭时忽略。 */
    void postDelayed(Runnable r, long delayMs) {
        Handler h = handler;
        if (h == null || shuttingDown) {
            return;
        }
        h.postDelayed(() -> runSafely(r, "Lua 定时任务异常"), delayMs);
    }

    /** 仅在当前界面会话生效时执行，过期回调直接丢弃。 */
    void postUi(Runnable r) {
        if (r == null) {
            return;
        }
        final UiSession session = ui;
        if (session == null) {
            Log.w(TAG, "无前台界面，丢弃 UI 任务");
            return;
        }
        final long epoch = session.epoch();
        session.activity().runOnUiThread(() -> {
            if (!isCurrent(epoch)) {
                Log.w(TAG, "界面已更换，丢弃过期 UI 任务");
                return;
            }
            runSafely(r, "UI 任务异常");
        });
    }

    void onAppEvent(String event, LuaValue... args) {
        post(() -> dispatchEvent(event, args));
    }

    /** 返回键：若没有处理器返回 true，则回调主线程执行默认返回。 */
    void onBack(Runnable defaultBack) {
        final UiSession session = ui;
        post(() -> {
            boolean handled = false;
            for (LuaValue fn : snapshotHandlers("back")) {
                LuaValue r = invokeSafely(fn, LuaValue.NONE);
                if (r != null && r.toboolean()) {
                    handled = true;
                }
            }
            if (!handled && defaultBack != null && isCurrent(session)) {
                session.activity().runOnUiThread(() -> {
                    if (isCurrent(session)) {
                        runSafely(defaultBack, "返回键回调异常");
                    }
                });
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

    /** 后台任务事件：handler 抛错立即结束本次运行，未调用 done 则等待超时。 */
    void dispatchTask(TaskInvocation invocation) {
        if (invocation == null || invocation.isFinished()) {
            return;
        }
        List<LuaValue> handlers = snapshotHandlers("task");
        if (handlers.isEmpty()) {
            Log.w(TAG, "没有 task 处理器，直接结束: " + invocation.name());
            invocation.finish("无 task 处理器");
            return;
        }
        LuaTable ctx = invocation.context();
        for (LuaValue fn : handlers) {
            try {
                fn.invoke(ctx);
            } catch (Throwable t) {
                Log.e(TAG, "task 回调错误: " + invocation.name(), t);
                invocation.finish("task 回调抛出错误");
                return;
            }
            if (invocation.isFinished()) {
                return;
            }
        }
    }

    /** 在 Lua 线程同步触发事件；调用方需已处于该线程。 */
    void dispatchEvent(String event, LuaValue[] args) {
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
        final String script = "window.__berbaEvent(" + JSONObject.quote(name) + ","
                + JSONObject.quote(payloadJson == null ? "null" : payloadJson) + ");";
        postUi(() -> {
            UiSession session = ui;
            if (session != null) {
                session.activity().evaluateJs(script, null);
            }
        });
    }

    void submitIo(Runnable r) {
        io.execute(() -> runSafely(r, "IO 任务异常"));
    }

    /** 仅测试使用；生产环境 Activity 销毁不结束运行时。 */
    void shutdownForTests() {
        Handler h = handler;
        if (h == null || shuttingDown) {
            return;
        }
        shuttingDown = true;
        h.post(() -> {
            dispatchEvent("destroy", null);
            LuaApis.cancelAllHttp();
            io.shutdownNow();
            thread.quitSafely();
        });
    }

    boolean isAppEvent(String name) {
        return capabilities.isAppEvent(name);
    }

    boolean isWebEvent(String name) {
        return capabilities.isWebEvent(name);
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

    private static void runSafely(Runnable r, String label) {
        try {
            r.run();
        } catch (Throwable t) {
            Log.e(TAG, label, t);
        }
    }

    LuaTable newTable() {
        return new LuaTable();
    }

    Globals globals() {
        return globals;
    }
}
