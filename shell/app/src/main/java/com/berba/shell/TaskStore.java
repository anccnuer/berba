package com.berba.shell;

import android.content.Context;
import android.content.SharedPreferences;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;
import org.json.JSONTokener;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;

/** 任务定义与待运行 Job 快照的持久化（独立 SharedPreferences）。 */
final class TaskStore {
    static final int SCHEMA = 1;
    static final int MAX_TASKS = 64;
    static final int MAX_DATA_BYTES = 64 * 1024;
    static final String PREFS_NAME = "berba_tasks";

    private static final String TAG = "berba";
    private static final String KEY_TASKS = "tasks";
    private static final String PENDING_PREFIX = "job:";

    static final class Pending {
        final String name;
        final long runId;
        final long scheduledAt;
        final long firedAt;
        final boolean late;

        Pending(String name, long runId, long scheduledAt, long firedAt, boolean late) {
            this.name = name;
            this.runId = runId;
            this.scheduledAt = scheduledAt;
            this.firedAt = firedAt;
            this.late = late;
        }
    }

    private final SharedPreferences prefs;

    TaskStore(Context context) {
        this.prefs = context.getApplicationContext()
                .getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE);
    }

    synchronized List<TaskRecord> list() {
        List<TaskRecord> out = new ArrayList<>();
        String text = prefs.getString(KEY_TASKS, null);
        if (text == null || text.isEmpty()) {
            return out;
        }
        try {
            JSONObject root = new JSONObject(text);
            if (root.optInt("schema", 0) != SCHEMA) {
                Log.e(TAG, "任务存储 schema 不受支持，按空任务处理");
                return out;
            }
            JSONArray tasks = root.optJSONArray("tasks");
            if (tasks == null) {
                return out;
            }
            for (int i = 0; i < tasks.length(); i++) {
                JSONObject o = tasks.optJSONObject(i);
                TaskRecord record = parseRecord(o);
                if (record != null) {
                    out.add(record);
                }
            }
        } catch (JSONException e) {
            Log.e(TAG, "任务存储损坏，按空任务处理", e);
            return new ArrayList<>();
        }
        out.sort(Comparator.comparing(r -> r.name));
        return out;
    }

    synchronized TaskRecord get(String name) {
        if (name == null) {
            return null;
        }
        for (TaskRecord record : list()) {
            if (record.name.equals(name)) {
                return record;
            }
        }
        return null;
    }

    synchronized int count() {
        return list().size();
    }

    synchronized int allocateJobId() {
        boolean[] used = new boolean[MAX_TASKS + 1];
        for (TaskRecord record : list()) {
            if (record.jobId > 0 && record.jobId <= MAX_TASKS) {
                used[record.jobId] = true;
            }
        }
        for (int id = 1; id <= MAX_TASKS; id++) {
            if (!used[id]) {
                return id;
            }
        }
        throw new IllegalStateException("任务数量已达上限 " + MAX_TASKS);
    }

    synchronized void put(TaskRecord record) throws IOException {
        if (record == null || record.name == null || record.name.isEmpty()) {
            throw new IOException("任务名不能为空");
        }
        int dataSize = record.dataJson.getBytes(StandardCharsets.UTF_8).length;
        if (dataSize > MAX_DATA_BYTES) {
            throw new IOException("任务 data 超过 64KB（实际 " + dataSize + " 字节）");
        }
        List<TaskRecord> records = list();
        boolean replaced = false;
        for (int i = 0; i < records.size(); i++) {
            if (records.get(i).name.equals(record.name)) {
                records.set(i, record);
                replaced = true;
                break;
            }
        }
        if (!replaced) {
            if (records.size() >= MAX_TASKS) {
                throw new IOException("任务数量已达上限 " + MAX_TASKS);
            }
            records.add(record);
        }
        write(records);
    }

    synchronized boolean remove(String name) {
        List<TaskRecord> records = list();
        boolean removed = false;
        for (int i = records.size() - 1; i >= 0; i--) {
            if (records.get(i).name.equals(name)) {
                records.remove(i);
                removed = true;
            }
        }
        if (removed) {
            try {
                write(records);
            } catch (IOException e) {
                Log.e(TAG, "删除任务失败", e);
                return false;
            }
        }
        return removed;
    }

    synchronized boolean removeIfRunId(String name, long runId) {
        TaskRecord record = get(name);
        if (record == null || record.runId != runId) {
            return false;
        }
        return remove(name);
    }

    synchronized boolean putPending(int jobId, Pending pending) {
        try {
            JSONObject o = new JSONObject();
            o.put("name", pending.name);
            o.put("runId", pending.runId);
            o.put("scheduledAt", pending.scheduledAt);
            o.put("firedAt", pending.firedAt);
            o.put("late", pending.late);
            return prefs.edit().putString(PENDING_PREFIX + jobId, o.toString()).commit();
        } catch (JSONException e) {
            Log.e(TAG, "写入任务 Job 快照失败", e);
            return false;
        }
    }

    synchronized Pending getPending(int jobId) {
        String text = prefs.getString(PENDING_PREFIX + jobId, null);
        if (text == null || text.isEmpty()) {
            return null;
        }
        try {
            JSONObject o = new JSONObject(text);
            String name = o.optString("name", "");
            if (name.isEmpty()) {
                return null;
            }
            return new Pending(name, o.optLong("runId", 0),
                    o.optLong("scheduledAt", 0), o.optLong("firedAt", 0),
                    o.optBoolean("late", false));
        } catch (JSONException e) {
            Log.e(TAG, "读取任务 Job 快照失败", e);
            return null;
        }
    }

    synchronized void removePending(int jobId) {
        prefs.edit().remove(PENDING_PREFIX + jobId).apply();
    }

    private void write(List<TaskRecord> records) throws IOException {
        JSONObject root = new JSONObject();
        JSONArray array = new JSONArray();
        try {
            root.put("schema", SCHEMA);
            for (TaskRecord record : records) {
                JSONObject o = new JSONObject();
                o.put("name", record.name);
                o.put("jobId", record.jobId);
                o.put("kind", record.kindName());
                o.put("every", record.everySec);
                o.put("daily", record.daily == null ? "" : record.daily);
                o.put("delay", record.delaySec);
                o.put("data", rawJson(record.dataJson));
                o.put("exact", record.exact);
                o.put("timeout", record.timeoutSec);
                o.put("createdAt", record.createdAt);
                o.put("nextRun", record.nextRun);
                o.put("runId", record.runId);
                o.put("lastFiredAt", record.lastFiredAt);
                o.put("done", record.done);
                array.put(o);
            }
            root.put("tasks", array);
        } catch (JSONException e) {
            throw new IOException("任务序列化失败: " + e.getMessage());
        }
        if (!prefs.edit().putString(KEY_TASKS, root.toString()).commit()) {
            throw new IOException("任务存储写入失败");
        }
    }

    private static Object rawJson(String json) {
        if (json == null || json.isEmpty()) {
            return JSONObject.NULL;
        }
        try {
            Object value = new JSONTokener(json).nextValue();
            return value == null ? JSONObject.NULL : value;
        } catch (JSONException e) {
            return JSONObject.NULL;
        }
    }

    private static TaskRecord parseRecord(JSONObject o) {
        if (o == null) {
            return null;
        }
        String name = o.optString("name", "");
        if (name.isEmpty()) {
            return null;
        }
        int kind;
        switch (o.optString("kind", "every")) {
            case "daily":
                kind = TaskRecord.KIND_DAILY;
                break;
            case "delay":
                kind = TaskRecord.KIND_DELAY;
                break;
            default:
                kind = TaskRecord.KIND_EVERY;
                break;
        }
        Object data = o.opt("data");
        String dataJson;
        if (data == null || data == JSONObject.NULL) {
            dataJson = "null";
        } else if (data instanceof JSONObject || data instanceof JSONArray) {
            dataJson = data.toString();
        } else if (data instanceof String) {
            dataJson = JSONObject.quote((String) data);
        } else {
            dataJson = String.valueOf(data);
        }
        return new TaskRecord(name,
                o.optInt("jobId", 0),
                kind,
                o.optLong("every", 0),
                o.optString("daily", ""),
                o.optLong("delay", 0),
                dataJson,
                o.optBoolean("exact", false),
                o.optInt("timeout", TaskSchedule.MIN_TIMEOUT_SEC),
                o.optLong("createdAt", 0),
                o.optLong("nextRun", 0),
                o.optLong("runId", 0),
                o.optLong("lastFiredAt", 0),
                o.optBoolean("done", false));
    }
}
