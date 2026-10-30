package com.berba.shell;

import java.nio.charset.StandardCharsets;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicLong;

/**
 * JNI shim 的 Java 侧：符号加载、UTF-8 参数边界、异步完成路由与 30 秒超时。
 * 原生调用本身在 Lua 线程（同步）或 IO 线程（异步）执行。
 */
final class NativePluginBridge {
    static final int MAX_PENDING = 256;
    static final int MAX_ARGS = 1 << 20;
    static final int MAX_RESULT = 8 << 20;
    static final long TIMEOUT_MS = 30_000L;

    interface Completion {
        void done(byte[] result, String error);
    }

    private static final class Pending {
        final NativePluginBridge bridge;
        final Completion completion;

        Pending(NativePluginBridge bridge, Completion completion) {
            this.bridge = bridge;
            this.completion = completion;
        }
    }

    private static final AtomicLong SEQ = new AtomicLong(1);
    private static final Map<Long, Pending> PENDING = new ConcurrentHashMap<>();
    private static volatile String loadError;

    private final LuaRuntime rt;

    NativePluginBridge(LuaRuntime rt) {
        this.rt = rt;
    }

    static synchronized void ensureLoaded() {
        if (loadError != null) {
            throw new IllegalStateException(loadError);
        }
        try {
            System.loadLibrary("berba_jni");
        } catch (Throwable t) {
            loadError = "JNI shim 不可用: " + LuaRuntime.message(t);
            throw new IllegalStateException(loadError);
        }
    }

    static boolean isLoaded() {
        return loadError == null;
    }

    static String loadError() {
        return loadError;
    }

    long load(String name, String path, String config) {
        return nativeLoad(name, path, encode(config, 64 * 1024, "插件 config"));
    }

    String describe(long handle) {
        byte[] data = nativeDescribe(handle);
        return data == null ? null : new String(data, StandardCharsets.UTF_8);
    }

    /** 同步调用；原生函数必须快速返回。 */
    byte[] call(long handle, String module, String function, String argsJson) {
        return nativeCall(handle, module, function, encode(argsJson, MAX_ARGS, "插件参数"));
    }

    /** 异步调用；completion 在 Lua 线程执行。插件可能从任意线程完成，也可能同步返回。 */
    void callAsync(long handle, String module, String function, String argsJson,
                   Completion completion) {
        final byte[] args = encode(argsJson, MAX_ARGS, "插件参数");
        final long id = SEQ.getAndIncrement();
        Pending pending = new Pending(this, completion);
        synchronized (PENDING) {
            if (PENDING.size() >= MAX_PENDING) {
                throw new IllegalStateException("原生插件并发调用已达上限 " + MAX_PENDING);
            }
            PENDING.put(id, pending);
        }
        rt.postDelayed(() -> timeout(id), TIMEOUT_MS);
        rt.submitIo(() -> {
            try {
                nativeCallAsync(handle, module, function, args, id);
            } catch (Throwable t) {
                fail(id, LuaRuntime.message(t));
            }
        });
    }

    private static byte[] encode(String text, int limit, String what) {
        if (text == null) {
            return null;
        }
        byte[] bytes = text.getBytes(StandardCharsets.UTF_8);
        if (bytes.length > limit) {
            throw new IllegalArgumentException(what + "超过 " + (limit / 1024) + "KB");
        }
        return bytes;
    }

    /** 原生线程回调：先移除 pending，再 post 回 Lua 线程；迟到或重复完成直接丢弃。 */
    private static void dispatch(long id, int status, byte[] data) {
        Pending pending = PENDING.remove(id);
        if (pending == null) {
            return;
        }
        if (status == 0) {
            pending.bridge.rt.post(() -> pending.completion.done(data, null));
        } else {
            final String error = "原生插件调用失败（错误码 " + status + "）";
            pending.bridge.rt.post(() -> pending.completion.done(null, error));
        }
    }

    private void timeout(long id) {
        Pending pending = PENDING.remove(id);
        if (pending != null) {
            pending.completion.done(null, "原生插件调用超时（"
                    + (TIMEOUT_MS / 1000) + " 秒）");
        }
    }

    private void fail(long id, String message) {
        Pending pending = PENDING.remove(id);
        if (pending != null) {
            rt.post(() -> pending.completion.done(null, message));
        }
    }

    private static native long nativeLoad(String name, String path, byte[] config);

    private static native byte[] nativeDescribe(long handle);

    private static native byte[] nativeCall(long handle, String module, String function,
                                            byte[] args);

    private static native void nativeCallAsync(long handle, String module, String function,
                                               byte[] args, long taskId);
}
