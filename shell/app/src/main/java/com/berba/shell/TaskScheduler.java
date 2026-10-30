package com.berba.shell;

import android.app.AlarmManager;
import android.app.PendingIntent;
import android.app.job.JobInfo;
import android.app.job.JobScheduler;
import android.content.ComponentName;
import android.content.Context;
import android.content.Intent;
import android.net.Uri;
import android.os.Build;
import android.util.Log;

import java.io.IOException;
import java.util.List;

/** 一次性闹钟 + JobScheduler 的调度；闹钟每次触发后重排。 */
final class TaskScheduler {
    static final String ACTION_ALARM = "com.berba.shell.action.TASK_ALARM";
    static final String ACTION_EXACT_PERMISSION =
            "android.app.action.SCHEDULE_EXACT_ALARM_PERMISSION_STATE_CHANGED";
    static final String EXTRA_NAME = "com.berba.shell.extra.TASK_NAME";
    static final String EXTRA_RUN_ID = "com.berba.shell.extra.TASK_RUN_ID";
    static final String DATA_SCHEME = "berba-task";

    private static final String TAG = "berba";

    private TaskScheduler() {
    }

    static boolean canExact(Context context) {
        AlarmManager am = (AlarmManager) context.getSystemService(Context.ALARM_SERVICE);
        return am != null && canExact(am);
    }

    private static boolean canExact(AlarmManager am) {
        if (Build.VERSION.SDK_INT < 31) {
            return true;
        }
        return Api31.canScheduleExactAlarms(am);
    }

    static void arm(Context context, TaskRecord record) {
        if (record == null || record.done || record.nextRun <= 0) {
            return;
        }
        AlarmManager am = (AlarmManager) context.getSystemService(Context.ALARM_SERVICE);
        if (am == null) {
            Log.w(TAG, "AlarmManager 不可用，任务未排程: " + record.name);
            return;
        }
        PendingIntent pending = alarmIntent(context, record.name, record.runId);
        boolean exact = record.exact && canExact(am);
        if (record.exact && !exact) {
            Log.w(TAG, "任务 " + record.name + " 需要精确闹钟但当前未授权，降级为非精确");
        }
        try {
            if (exact) {
                am.setExactAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, record.nextRun, pending);
            } else {
                am.setAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, record.nextRun, pending);
            }
        } catch (SecurityException e) {
            Log.w(TAG, "精确闹钟被系统拒绝，降级为非精确: " + record.name, e);
            try {
                am.setAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, record.nextRun, pending);
            } catch (RuntimeException fallback) {
                Log.e(TAG, "任务闹钟排程失败: " + record.name, fallback);
            }
        }
    }

    static void cancel(Context context, String name, int jobId, long runId) {
        AlarmManager am = (AlarmManager) context.getSystemService(Context.ALARM_SERVICE);
        if (am != null) {
            PendingIntent pending = alarmIntent(context, name, runId);
            am.cancel(pending);
            pending.cancel();
        }
        JobScheduler jobs = (JobScheduler) context.getSystemService(Context.JOB_SCHEDULER_SERVICE);
        if (jobs != null) {
            jobs.cancel(jobId);
        }
        new TaskStore(context).removePending(jobId);
    }

    /** 先写 Job 快照再入队；Job 启动时按 runId 校验是否已过期。 */
    static void enqueueJob(Context context, TaskRecord record,
                           long scheduledAt, long firedAt, boolean late) {
        TaskStore store = new TaskStore(context);
        TaskStore.Pending pending = new TaskStore.Pending(
                record.name, record.runId, scheduledAt, firedAt, late);
        if (!store.putPending(record.jobId, pending)) {
            Log.e(TAG, "写入任务 Job 快照失败，跳过本次执行: " + record.name);
            return;
        }
        JobScheduler jobs = (JobScheduler) context.getSystemService(Context.JOB_SCHEDULER_SERVICE);
        if (jobs == null) {
            store.removePending(record.jobId);
            Log.w(TAG, "JobScheduler 不可用，任务未入队: " + record.name);
            return;
        }
        ComponentName component = new ComponentName(context, TaskJobService.class);
        int result = JobScheduler.RESULT_FAILURE;
        if (Build.VERSION.SDK_INT >= 31) {
            try {
                result = jobs.schedule(Api31.expedited(context, record.jobId, component));
            } catch (RuntimeException e) {
                Log.w(TAG, "expedited Job 排程失败，改用普通 Job: " + record.name, e);
            }
        }
        if (result != JobScheduler.RESULT_SUCCESS) {
            JobInfo info = new JobInfo.Builder(record.jobId, component)
                    .setOverrideDeadline(1000)
                    .build();
            result = jobs.schedule(info);
        }
        if (result != JobScheduler.RESULT_SUCCESS) {
            store.removePending(record.jobId);
            Log.e(TAG, "任务 Job 入队失败: " + record.name + " (" + result + ")");
        }
    }

    /** 开机、应用更新、改时间/时区、精确闹钟授权变化后恢复闹钟；错过的任务只补一次。 */
    static void rescheduleAll(Context context, String action) {
        TaskStore store = new TaskStore(context);
        List<TaskRecord> records = store.list();
        boolean timeChanged = Intent.ACTION_TIME_CHANGED.equals(action)
                || Intent.ACTION_TIMEZONE_CHANGED.equals(action);
        long now = System.currentTimeMillis();
        for (TaskRecord record : records) {
            if (record.done || record.nextRun <= 0) {
                continue;
            }
            TaskRecord next = record.copy();
            boolean overdue = record.nextRun <= now;
            if (!overdue) {
                if (timeChanged && record.kind == TaskRecord.KIND_DAILY) {
                    long candidate = TaskSchedule.nextDaily(record.daily, now);
                    if (candidate != record.nextRun) {
                        next.nextRun = candidate;
                        next.runId++;
                        if (!save(store, next)) {
                            continue;
                        }
                    }
                }
                arm(context, next);
                continue;
            }

            long scheduledAt = record.nextRun;
            boolean late = TaskSchedule.isLate(scheduledAt, now);
            next.lastFiredAt = now;
            next.runId++;
            if (next.kind == TaskRecord.KIND_DELAY) {
                next.done = true;
                next.nextRun = 0;
            } else if (timeChanged && next.kind == TaskRecord.KIND_DAILY) {
                next.nextRun = TaskSchedule.nextDaily(next.daily, now);
            } else {
                next.nextRun = TaskSchedule.nextRun(next, now);
            }
            if (!save(store, next)) {
                continue;
            }
            enqueueJob(context, next, scheduledAt, now, late);
            if (!next.done) {
                arm(context, next);
            }
        }
    }

    private static boolean save(TaskStore store, TaskRecord record) {
        try {
            store.put(record);
            return true;
        } catch (IOException e) {
            Log.e(TAG, "更新任务失败: " + record.name, e);
            return false;
        }
    }

    private static PendingIntent alarmIntent(Context context, String name, long runId) {
        Intent intent = new Intent(context, TaskAlarmReceiver.class);
        intent.setAction(ACTION_ALARM);
        intent.setData(Uri.parse(DATA_SCHEME + "://" + Uri.encode(name)));
        intent.putExtra(EXTRA_NAME, name);
        intent.putExtra(EXTRA_RUN_ID, runId);
        int flags = PendingIntent.FLAG_UPDATE_CURRENT;
        if (Build.VERSION.SDK_INT >= 23) {
            flags |= PendingIntent.FLAG_IMMUTABLE;
        }
        return PendingIntent.getBroadcast(context, 0, intent, flags);
    }

    private static final class Api31 {
        private Api31() {
        }

        static boolean canScheduleExactAlarms(AlarmManager am) {
            return am.canScheduleExactAlarms();
        }

        static JobInfo expedited(Context context, int jobId, ComponentName component) {
            JobInfo.Builder builder = new JobInfo.Builder(jobId, component);
            builder.setExpedited(true);
            return builder.build();
        }
    }
}
