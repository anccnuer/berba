package com.berba.shell;

import android.os.Handler;
import android.os.Looper;
import android.util.Log;

import org.luaj.vm2.LuaTable;
import org.luaj.vm2.LuaValue;

import java.util.concurrent.atomic.AtomicBoolean;

/** 一次任务运行：ctx.done() 或超时各结束一次，重复调用安全。 */
final class TaskInvocation {
    private static final String TAG = "berba";

    private final String name;
    private final LuaValue data;
    private final long scheduledAt;
    private final long firedAt;
    private final long runId;
    private final boolean late;
    private final int timeoutSec;
    private final Runnable onFinish;
    private final Handler handler = new Handler(Looper.getMainLooper());
    private final AtomicBoolean finished = new AtomicBoolean();
    private final Runnable timeoutTask = () -> finish("超时");

    TaskInvocation(TaskStore.Pending pending, String dataJson, int timeoutSec, Runnable onFinish) {
        this.name = pending.name;
        this.scheduledAt = pending.scheduledAt;
        this.firedAt = pending.firedAt;
        this.runId = pending.runId;
        this.late = pending.late;
        this.timeoutSec = timeoutSec;
        this.onFinish = onFinish;
        LuaValue parsed;
        try {
            parsed = LuaConvert.parse(dataJson);
        } catch (Exception e) {
            Log.w(TAG, "任务 data 解析失败: " + name, e);
            parsed = LuaValue.NIL;
        }
        this.data = parsed;
    }

    String name() {
        return name;
    }

    long runId() {
        return runId;
    }

    boolean isFinished() {
        return finished.get();
    }

    void start() {
        handler.postDelayed(timeoutTask, timeoutSec * 1000L);
    }

    LuaTable context() {
        LuaTable ctx = new LuaTable();
        ctx.set("name", LuaValue.valueOf(name));
        ctx.set("data", data);
        ctx.set("scheduledAt", LuaValue.valueOf(scheduledAt / 1000.0));
        ctx.set("firedAt", LuaValue.valueOf(firedAt / 1000.0));
        ctx.set("late", LuaValue.valueOf(late));
        ctx.set("runId", LuaValue.valueOf((double) runId));
        ctx.set("done", LuaApis.fn(args -> {
            finish("done");
            return LuaValue.NONE;
        }));
        return ctx;
    }

    void finish(String reason) {
        if (!finished.compareAndSet(false, true)) {
            return;
        }
        handler.removeCallbacks(timeoutTask);
        if (!"done".equals(reason)) {
            Log.w(TAG, "任务 " + name + " 结束: " + reason);
        }
        if (onFinish != null) {
            try {
                onFinish.run();
            } catch (Throwable t) {
                Log.e(TAG, "任务完成回调失败: " + name, t);
            }
        }
    }
}
