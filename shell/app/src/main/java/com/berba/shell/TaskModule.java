package com.berba.shell;

import org.luaj.vm2.Globals;
import org.luaj.vm2.LuaError;
import org.luaj.vm2.LuaTable;
import org.luaj.vm2.LuaValue;
import org.luaj.vm2.Varargs;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.List;
import java.util.regex.Pattern;

/** 安装 task.*：无需前台界面即可排程、取消与查询任务。 */
final class TaskModule {
    private static final Pattern NAME_RE = Pattern.compile("^[A-Za-z_][A-Za-z0-9_.-]{0,63}$");

    private final LuaRuntime rt;
    private final TaskStore store;

    private TaskModule(LuaRuntime rt) {
        this.rt = rt;
        this.store = new TaskStore(rt.context());
    }

    static void install(LuaRuntime rt, Globals g) {
        TaskModule module = new TaskModule(rt);
        LuaTable task = new LuaTable();
        task.set("schedule", LuaApis.fn(module::schedule));
        task.set("cancel", LuaApis.fn(module::cancel));
        task.set("list", LuaApis.fn(module::list));
        task.set("exact", LuaApis.fn(args ->
                TaskScheduler.canExact(rt.context()) ? LuaValue.TRUE : LuaValue.FALSE));
        g.set("task", task);
    }

    private Varargs schedule(Varargs args) {
        String name = args.arg(1).optjstring("");
        if (!NAME_RE.matcher(name).matches()) {
            throw new LuaError("task 名称需匹配 ^[A-Za-z_][A-Za-z0-9_.-]{0,63}$");
        }
        LuaValue options = args.arg(2);
        if (!options.istable()) {
            throw new LuaError("task.schedule 需要任务配置 table");
        }
        LuaTable t = options.checktable();

        boolean hasEvery = !t.get("every").isnil();
        boolean hasDaily = !t.get("daily").isnil();
        boolean hasDelay = !t.get("delay").isnil();
        int kinds = (hasEvery ? 1 : 0) + (hasDaily ? 1 : 0) + (hasDelay ? 1 : 0);
        if (kinds != 1) {
            throw new LuaError("task.schedule 必须在 every、daily、delay 中恰好指定一个");
        }

        int kind;
        long everySec = 0;
        long delaySec = 0;
        String daily = "";
        if (hasEvery) {
            kind = TaskRecord.KIND_EVERY;
            everySec = intValue(t.get("every"), "every",
                    TaskSchedule.MIN_EVERY_SEC, TaskSchedule.MAX_SPAN_SEC);
        } else if (hasDaily) {
            kind = TaskRecord.KIND_DAILY;
            daily = t.get("daily").optjstring("");
            if (!TaskSchedule.validDaily(daily)) {
                throw new LuaError("daily 必须是 24 小时制 HH:MM");
            }
        } else {
            kind = TaskRecord.KIND_DELAY;
            delaySec = intValue(t.get("delay"), "delay", 1, TaskSchedule.MAX_SPAN_SEC);
        }

        int timeoutSec = t.get("timeout").optint(60);
        if (timeoutSec < TaskSchedule.MIN_TIMEOUT_SEC || timeoutSec > TaskSchedule.MAX_TIMEOUT_SEC) {
            throw new LuaError("timeout 必须在 10-600 秒之间");
        }
        boolean exact = t.get("exact").optboolean(false);
        boolean replace = t.get("replace").optboolean(true);
        String dataJson = LuaConvert.toJson(t.get("data"));
        int dataSize = dataJson.getBytes(StandardCharsets.UTF_8).length;
        if (dataSize > TaskStore.MAX_DATA_BYTES) {
            throw new LuaError("任务 data 超过 64KB（实际 " + dataSize + " 字节）");
        }

        TaskRecord existing = store.get(name);
        if (existing != null && !replace) {
            return LuaApis.nilError(new IllegalStateException("任务已存在: " + name));
        }
        int jobId;
        long createdAt;
        long runId;
        if (existing == null) {
            if (store.count() >= TaskStore.MAX_TASKS) {
                return LuaApis.nilError(new IllegalStateException(
                        "任务数量已达上限 " + TaskStore.MAX_TASKS));
            }
            jobId = store.allocateJobId();
            createdAt = System.currentTimeMillis();
            runId = 1;
        } else {
            jobId = existing.jobId;
            createdAt = existing.createdAt;
            runId = existing.runId + 1;
        }

        long now = System.currentTimeMillis();
        TaskRecord record = new TaskRecord(name, jobId, kind, everySec, daily, delaySec,
                dataJson, exact, timeoutSec, createdAt, 0, runId, 0, false);
        record.nextRun = TaskSchedule.nextRun(record, now);
        try {
            store.put(record);
        } catch (IOException e) {
            return LuaApis.nilError(e);
        }
        TaskScheduler.arm(rt.context(), record);
        return info(record);
    }

    private Varargs cancel(Varargs args) {
        String name = args.checkjstring(1);
        TaskRecord record = store.get(name);
        if (record == null) {
            return LuaValue.FALSE;
        }
        if (!store.remove(name)) {
            return LuaValue.FALSE;
        }
        TaskScheduler.cancel(rt.context(), name, record.jobId, record.runId);
        return LuaValue.TRUE;
    }

    private Varargs list(Varargs args) {
        List<TaskRecord> records = store.list();
        LuaTable out = new LuaTable();
        int index = 1;
        for (TaskRecord record : records) {
            out.set(index++, info(record));
        }
        return out;
    }

    private static long intValue(LuaValue value, String field, long min, long max) {
        if (!value.isnumber()) {
            throw new LuaError(field + " 必须是整数");
        }
        double d = value.todouble();
        long n = (long) d;
        if (d != n || n < min || n > max) {
            throw new LuaError(field + " 必须在 " + min + "-" + max + " 之间");
        }
        return n;
    }

    private static LuaTable info(TaskRecord record) {
        LuaTable t = new LuaTable();
        t.set("name", LuaValue.valueOf(record.name));
        t.set("kind", LuaValue.valueOf(record.kindName()));
        t.set("every", LuaValue.valueOf((double) record.everySec));
        t.set("daily", LuaValue.valueOf(record.daily == null ? "" : record.daily));
        t.set("delay", LuaValue.valueOf((double) record.delaySec));
        t.set("exact", LuaValue.valueOf(record.exact));
        t.set("timeout", LuaValue.valueOf(record.timeoutSec));
        t.set("nextRun", LuaValue.valueOf(record.nextRun / 1000.0));
        t.set("runId", LuaValue.valueOf((double) record.runId));
        t.set("done", LuaValue.valueOf(record.done));
        return t;
    }
}
