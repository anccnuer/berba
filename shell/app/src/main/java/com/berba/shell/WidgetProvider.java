package com.berba.shell;

import android.appwidget.AppWidgetManager;
import android.appwidget.AppWidgetProvider;
import android.content.Context;
import android.os.Bundle;

/**
 * 小组件入口：只做缓存渲染，绝不启动 Lua。需要新数据时由 task 或前台界面主动推送。
 */
public class WidgetProvider extends AppWidgetProvider {
    @Override
    public void onUpdate(Context context, AppWidgetManager manager, int[] appWidgetIds) {
        new WidgetController(context).renderAll();
    }

    @Override
    public void onAppWidgetOptionsChanged(Context context, AppWidgetManager manager,
                                          int appWidgetId, Bundle newOptions) {
        new WidgetController(context).render(appWidgetId);
    }

    @Override
    public void onEnabled(Context context) {
        new WidgetController(context).renderAll();
    }

    @Override
    public void onDeleted(Context context, int[] appWidgetIds) {
        // 状态是应用级共享缓存，删除单个实例不需要清理。
    }
}
