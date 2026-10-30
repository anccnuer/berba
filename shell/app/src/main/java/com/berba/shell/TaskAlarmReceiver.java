package com.berba.shell;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.util.Log;

import java.io.IOException;

/** 闹钟触发：先推进 runId/nextRun 并重排下一次闹钟，再入队 Job。 */
public final class TaskAlarmReceiver extends BroadcastReceiver {
    private static final String TAG = "berba";

    @Override
    public void onReceive(Context context, Intent intent) {
        if (intent == null || !TaskScheduler.ACTION_ALARM.equals(intent.getAction())) {
            return;
        }
        String name = intent.getStringExtra(TaskScheduler.EXTRA_NAME);
        long runId = intent.getLongExtra(TaskScheduler.EXTRA_RUN_ID, -1);
        if (name == null || name.isEmpty() || runId < 0) {
            Log.w(TAG, "忽略缺少任务信息的闹钟");
            return;
        }
        TaskStore store = new TaskStore(context);
        TaskRecord record = store.get(name);
        if (record == null) {
            Log.w(TAG, "闹钟对应的任务已不存在: " + name);
            return;
        }
        if (record.done || record.runId != runId) {
            Log.w(TAG, "忽略过期任务闹钟: " + name + " runId=" + runId);
            return;
        }

        long now = System.currentTimeMillis();
        long scheduledAt = record.nextRun;
        TaskRecord next = record.copy();
        next.runId++;
        next.lastFiredAt = now;
        if (record.kind == TaskRecord.KIND_DELAY) {
            next.done = true;
            next.nextRun = 0;
        } else {
            next.nextRun = TaskSchedule.nextRun(record, now);
        }
        try {
            store.put(next);
        } catch (IOException e) {
            Log.e(TAG, "推进任务状态失败，跳过本次执行: " + name, e);
            return;
        }
        TaskScheduler.enqueueJob(context, next, scheduledAt, now,
                TaskSchedule.isLate(scheduledAt, now));
        if (!next.done) {
            TaskScheduler.arm(context, next);
        }
    }
}
