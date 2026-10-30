package com.berba.shell;

import android.app.PendingIntent;
import android.appwidget.AppWidgetManager;
import android.content.ComponentName;
import android.content.Context;
import android.content.Intent;
import android.net.Uri;
import android.util.Log;
import android.widget.RemoteViews;

import java.nio.charset.StandardCharsets;
import java.util.Calendar;
import java.util.List;
import java.util.Locale;

/**
 * 小组件渲染与缓存：只读 SharedPreferences，不在渲染路径启动 Lua，
 * 进程被回收或 launcher 重启后仍能显示最后一次数据。
 */
final class WidgetController {
    static final String PREFS = "berba_widget";
    static final String KEY_STATE = "state";
    static final String SCHEME = "berba-widget";
    static final String ACTION_OPEN = "com.berba.shell.action.WIDGET_OPEN";

    private static final String TAG = "berba";

    /** 一次小组件点击。 */
    static final class Click {
        final int widgetId;
        boolean coldStart;

        Click(int widgetId) {
            this.widgetId = widgetId;
        }
    }

    private final Context context;

    WidgetController(Context context) {
        this.context = context.getApplicationContext();
    }

    WidgetState.State load() {
        return WidgetState.parse(prefs().getString(KEY_STATE, ""));
    }

    int[] ids() {
        AppWidgetManager manager = AppWidgetManager.getInstance(context);
        if (manager == null) {
            return new int[0];
        }
        ComponentName provider = new ComponentName(context, WidgetProvider.class);
        try {
            return manager.getAppWidgetIds(provider);
        } catch (RuntimeException e) {
            Log.w(TAG, "读取小组件实例失败", e);
            return new int[0];
        }
    }

    /** 校验并持久化状态，随后推送全部实例；超出上限抛 IllegalArgumentException。 */
    long write(WidgetState.State state) {
        String json = WidgetState.toJson(state);
        int size = json.getBytes(StandardCharsets.UTF_8).length;
        if (size > WidgetState.MAX_BYTES) {
            throw new IllegalArgumentException(
                    "小组件状态超过 " + (WidgetState.MAX_BYTES / 1024) + "KB（实际 " + size + " 字节）");
        }
        if (!prefs().edit().putString(KEY_STATE, json).commit()) {
            throw new IllegalStateException("小组件状态写入失败");
        }
        renderAll();
        return state.updatedAt;
    }

    void clear() {
        write(WidgetState.empty());
    }

    void renderAll() {
        int[] ids = ids();
        if (ids.length == 0) {
            return;
        }
        WidgetState.State state = load();
        AppWidgetManager manager = AppWidgetManager.getInstance(context);
        if (manager == null) {
            return;
        }
        for (int id : ids) {
            manager.updateAppWidget(id, buildViews(state, id));
        }
    }

    void render(int widgetId) {
        AppWidgetManager manager = AppWidgetManager.getInstance(context);
        if (manager == null) {
            return;
        }
        manager.updateAppWidget(widgetId, buildViews(load(), widgetId));
    }

    private android.content.SharedPreferences prefs() {
        return context.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
    }

    /** 解析点击 Intent；非小组件 Intent 返回 null。 */
    static Click parseClick(Intent intent) {
        if (intent == null || intent.getData() == null) {
            return null;
        }
        Uri data = intent.getData();
        if (!SCHEME.equalsIgnoreCase(data.getScheme())) {
            return null;
        }
        String last = data.getLastPathSegment();
        if (last == null) {
            return null;
        }
        try {
            int id = Integer.parseInt(last.trim());
            if (id <= 0) {
                return null;
            }
            return new Click(id);
        } catch (NumberFormatException e) {
            return null;
        }
    }

    private RemoteViews buildViews(WidgetState.State state, int widgetId) {
        int layout = id("berba_widget", "layout");
        if (layout == 0) {
            Log.w(TAG, "缺少小组件布局资源，跳过渲染");
            return null;
        }
        RemoteViews views = new RemoteViews(context.getPackageName(), layout);

        views.setTextViewText(id("berba_widget_title", "id"),
                state.title.isEmpty() ? appLabel() : state.title);
        views.setTextViewText(id("berba_widget_updated", "id"), updatedLabel(state.updatedAt));

        List<WidgetState.Card> cards = state.cards;
        for (int i = 0; i < WidgetState.MAX_ROWS; i++) {
            int row = id("berba_widget_row" + i, "id");
            if (row == 0) {
                continue;
            }
            if (i >= cards.size()) {
                views.setViewVisibility(row, android.view.View.GONE);
                continue;
            }
            WidgetState.Card card = cards.get(i);
            views.setViewVisibility(row, android.view.View.VISIBLE);
            views.setTextViewText(id("berba_widget_row" + i + "_title", "id"), card.title);
            views.setTextViewText(id("berba_widget_row" + i + "_value", "id"), card.value);
            views.setTextViewText(id("berba_widget_row" + i + "_subtitle", "id"), card.subtitle);
            if (!card.color.isEmpty()) {
                try {
                    views.setTextColor(id("berba_widget_row" + i + "_value", "id"),
                            android.graphics.Color.parseColor(card.color));
                } catch (IllegalArgumentException ignored) {
                    // 颜色在写入时已校验；这里只做兜底。
                }
            }
        }

        int root = id("berba_widget_root", "id");
        if (root != 0) {
            views.setOnClickPendingIntent(root, openIntent(widgetId));
        }
        return views;
    }

    private PendingIntent openIntent(int widgetId) {
        Intent intent = new Intent(context, MainActivity.class);
        intent.setAction(ACTION_OPEN);
        intent.setData(Uri.parse(SCHEME + "://open/" + widgetId));
        intent.addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP | Intent.FLAG_ACTIVITY_CLEAR_TOP);
        int flags = PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE;
        return PendingIntent.getActivity(context, widgetId, intent, flags);
    }

    private String appLabel() {
        CharSequence label = context.getApplicationInfo().loadLabel(context.getPackageManager());
        return label == null ? context.getPackageName() : label.toString();
    }

    private static String updatedLabel(long updatedAt) {
        if (updatedAt <= 0) {
            return "暂无数据";
        }
        Calendar time = Calendar.getInstance();
        time.setTimeInMillis(updatedAt);
        return String.format(Locale.getDefault(), "更新于 %02d:%02d",
                time.get(Calendar.HOUR_OF_DAY), time.get(Calendar.MINUTE));
    }

    private int id(String name, String type) {
        return context.getResources().getIdentifier(name, type, context.getPackageName());
    }
}
