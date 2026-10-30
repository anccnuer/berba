package com.berba.shell;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.util.Log;

/** 开机、应用更新、改时间/时区与精确闹钟授权变化后恢复任务。 */
public final class TaskBootReceiver extends BroadcastReceiver {
    private static final String TAG = "berba";

    @Override
    public void onReceive(Context context, Intent intent) {
        if (intent == null) {
            return;
        }
        String action = intent.getAction();
        if (!Intent.ACTION_BOOT_COMPLETED.equals(action)
                && !Intent.ACTION_MY_PACKAGE_REPLACED.equals(action)
                && !Intent.ACTION_TIME_CHANGED.equals(action)
                && !Intent.ACTION_TIMEZONE_CHANGED.equals(action)
                && !TaskScheduler.ACTION_EXACT_PERMISSION.equals(action)) {
            return;
        }
        Log.i(TAG, "恢复定时任务: " + action);
        TaskScheduler.rescheduleAll(context, action);
    }
}
