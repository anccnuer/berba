package com.berba.shell;

import android.appwidget.AppWidgetManager;
import android.appwidget.AppWidgetProvider;
import android.content.Context;
import android.os.Bundle;
import android.util.Log;

/**
 * 桌面小组件入口：只读缓存渲染，不启动 Lua。
 *
 * <p>updatePeriodMillis = 0，系统不会周期唤醒应用；需要刷新时由 Lua 通过
 * widget.update 主动推送，或由 task 定时触发。
 */
public class WidgetProvider extends AppWidgetProvider {
    private static final String TAG = "berba";

    @Override
    public void onUpdate(Context context, AppWidgetManager manager, int[] appWidgetIds) {
        if (appWidgetIds == null || appWidgetIds.length == 0) {
            return;
        }
        Log.d(TAG, "小组件 onUpdate: " + appWidgetIds.length + " 个实例");
        WidgetController controller = WidgetController.get(context);
        controller.render(context);
    }

    @Override
    public void onAppWidgetOptionsChanged(Context context, AppWidgetManager manager,
                                           int appWidgetId, Bundle options) {
        WidgetController.get(context).render(context);
    }

    @Override
    public void onDeleted(Context context, int[] appWidgetIds) {
        WidgetController.get(context).onDeleted(appWidgetIds);
    }
}
