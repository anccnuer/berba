package com.berba.shell;

import android.app.job.JobParameters;
import android.app.job.JobService;
import android.util.Log;

import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

/**
 * 后台任务执行入口：把任务事件投递到进程级 Lua 运行时，
 * 只有 ctx.done() 或超时才 jobFinished；handler 返回不代表完成。
 */
public class TaskJobService extends JobService {
    private static final String TAG = "berba";

    private final Map<Integer, TaskInvocation> running = new ConcurrentHashMap<>();

    @Override
    public boolean onStartJob(final JobParameters params) {
        final int jobId = params.getJobId();
        final TaskStore store = new TaskStore(this);
        final TaskStore.Pending pending = store.getPending(jobId);
        if (pending == null) {
            Log.w(TAG, "忽略缺少快照的任务 Job: " + jobId);
            jobFinished(params, false);
            return true;
        }
        final TaskRecord record = store.get(pending.name);
        if (record == null || record.runId != pending.runId) {
            Log.w(TAG, "忽略过期任务 Job: " + pending.name + " runId=" + pending.runId);
            store.removePending(jobId);
            jobFinished(params, false);
            return true;
        }

        final boolean oneShot = record.kind == TaskRecord.KIND_DELAY;
        final TaskInvocation invocation = new TaskInvocation(pending, record.dataJson,
                record.timeoutSec, () -> {
            running.remove(jobId);
            store.removePending(jobId);
            if (oneShot) {
                store.removeIfRunId(record.name, pending.runId);
            }
            jobFinished(params, false);
        });
        running.put(jobId, invocation);
        invocation.start();
        try {
            RuntimeManager.get(this).dispatchTask(invocation);
        } catch (Throwable t) {
            Log.e(TAG, "任务分发失败: " + pending.name, t);
            invocation.finish("分发失败");
        }
        return true;
    }

    @Override
    public boolean onStopJob(JobParameters params) {
        TaskInvocation invocation = running.remove(params.getJobId());
        if (invocation != null) {
            invocation.finish("系统停止");
        }
        // 下一次闹钟已在触发时排好，不需要系统重试。
        return false;
    }
}
