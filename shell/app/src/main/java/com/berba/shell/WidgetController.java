package com.berba.shell;

import android.app.PendingIntent;
import android.appwidget.AppWidgetManager;
import android.content.ComponentName;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.net.Uri;
import android.os.Handler;
import android.os.Looper;
import android.util.Log;
import android.view.View;
import android.widget.RemoteViews;

import java.text.SimpleDateFormat;
import java.util.ArrayList;
import java.util.Date;
import java.util.HashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

/**
 * 小组件渲染与缓存：写缓存、渲染 RemoteViews、把点击转成 berba-widget:// Intent。
 *
 * <p>进程可能没有 Lua 运行时（launcher 拉起 WidgetProvider），因此这里只读
 * SharedPreferences 缓存，绝不启动 Lua。资源名由构建期生成，用 getIdentifier 解析。
 */
final class WidgetController {
    private static final String TAG = "berba";
    static final String SCHEME = "berba-widget";

    private static final String ID_ROOT = "widget_root";
    private static final String ID_TITLE = "widget_title";
    private static final String ID_UPDATED = "widget_updated";
    private static final String ID_ROW = "widget_row";

    private static final int THEME_VALUE_COLOR = 0xFFFFFFFF;

    private final Context context;
    private final AppWidgetManager manager;
    private final ComponentName provider;
    private final SharedPreferences prefs;
    private final Handler main = new Handler(Looper.getMainLooper());
    private final Map<String, Integer> resourceIds = new HashMap<>();

    private WidgetController(Context context) {
        this.context = context.getApplicationContext();
        this.manager = (AppWidgetManager) this.context.getSystemService(Context.APPWIDGET_SERVICE);
        this.provider = new ComponentName(this.context, WidgetProvider.class);
        this.prefs = this.context.getSharedPreferences(WidgetState.PREFS_NAME, Context.MODE_PRIVATE);
    }

    /** 每个进程共享一个 controller：Lua 线程与 WidgetProvider 都走这里。 */
    private static final Map<Context, WidgetController> INSTANCES = new ConcurrentHashMap<>();

    static WidgetController get(Context context) {
        Context app = context.getApplicationContext();
        WidgetController controller = INSTANCES.get(app);
        if (controller != null) {
            return controller;
        }
        synchronized (INSTANCES) {
            controller = INSTANCES.get(app);
            if (controller == null) {
                controller = new WidgetController(app);
                INSTANCES.put(app, controller);
            }
            return controller;
        }
    }

    static synchronized void release(Context context) {
        if (context != null) {
            INSTANCES.remove(context.getApplicationContext());
        }
    }

    /** 一次有效小组件点击。 */
    static final class Click {
        final int id;
        boolean coldStart;

        Click(int id) {
            this.id = id;
        }
    }

    /** 解析并清理 berba-widget://open/<id> Intent；无效来源返回 null。 */
    Click parseClick(Intent intent) {
        if (intent == null) {
            return null;
        }
        Uri data = intent.getData();
        if (data == null || !SCHEME.equalsIgnoreCase(data.getScheme())) {
            return null;
        }
        String host = data.getHost();
        String path = data.getPath();
        if (!"open".equals(host) || path == null || path.length() < 2) {
            Log.w(TAG, "忽略非法的小组件 Intent: " + data);
            return null;
        }
        int id;
        try {
            id = Integer.parseInt(path.substring(1));
        } catch (NumberFormatException e) {
            Log.w(TAG, "忽略小组件 id 不合法的 Intent: " + data);
            return null;
        }
        intent.setData(null);
        return new Click(id);
    }

    SharedPreferences prefs() {
        return prefs;
    }

    WidgetState state() {
        return WidgetState.parseLenient(prefs.getString(WidgetState.PREFS_KEY, null));
    }

    List<Integer> instanceIds() {
        if (manager == null) {
            return new ArrayList<>();
        }
        int[] ids = manager.getAppWidgetIds(provider);
        List<Integer> out = new ArrayList<>();
        for (int id : ids) {
            out.add(id);
        }
        java.util.Collections.sort(out);
        return out;
    }

    /** 校验 → 持久化 → 主线程渲染；超限抛 IllegalArgumentException。 */
    void write(String json) {
        WidgetState state = WidgetState.parse(json);
        state.updatedAt = System.currentTimeMillis();
        state.checkWritable();
        String stored = state.toJson();
        int size = stored.getBytes(java.nio.charset.StandardCharsets.UTF_8).length;
        if (size > WidgetState.MAX_BYTES) {
            throw new IllegalArgumentException(
                    "小组件状态超过 " + (WidgetState.MAX_BYTES / 1024) + "KB 上限（实际 " + size + " 字节）");
        }
        prefs.edit().putString(WidgetState.PREFS_KEY, stored).apply();
        render(context);
    }

    /** 清空缓存并把小组件显示为占位。 */
    void clear() {
        prefs.edit().remove(WidgetState.PREFS_KEY).apply();
        render(context);
    }

    void onDeleted(int[] appWidgetIds) {
        if (appWidgetIds == null || appWidgetIds.length == 0) {
            return;
        }
        Log.i(TAG, "移除了 " + appWidgetIds.length + " 个小组件实例");
        render(context);
    }

    /** AppWidgetManager.updateAppWidget 可在任意线程调用，统一 post 到主线程。 */
    void render(Context context) {
        main.post(() -> doRender(context.getApplicationContext()));
    }

    private void doRender(Context ctx) {
        if (manager == null) {
            Log.w(TAG, "AppWidgetManager 不可用，跳过小组件渲染");
            return;
        }
        int layout = resourceId("layout", "berba_widget");
        if (layout == 0) {
            Log.e(TAG, "缺少 @layout/berba_widget，请确认 app.json 启用了 widget 能力");
            return;
        }
        WidgetState state = state();
        if (state.hiddenCardCount() > 0) {
            Log.w(TAG, "小组件只渲染 " + WidgetState.ROW_COUNT + " 行，截断 "
                    + state.hiddenCardCount() + " 张卡片");
        }
        int[] ids = manager.getAppWidgetIds(provider);
        for (int appWidgetId : ids) {
            try {
                RemoteViews views = new RemoteViews(ctx.getPackageName(), layout);
                buildViews(ctx, views, state, appWidgetId);
                manager.updateAppWidget(appWidgetId, views);
            } catch (RuntimeException e) {
                Log.w(TAG, "小组件渲染失败: " + appWidgetId, e);
            }
        }
    }

    private void buildViews(Context ctx, RemoteViews views, WidgetState state, int appWidgetId) {
        boolean hasTitle = !state.title.isEmpty();
        int titleId = resourceId("id", ID_TITLE);
        views.setViewVisibility(titleId, hasTitle ? View.VISIBLE : View.GONE);
        if (hasTitle) {
            views.setTextViewText(titleId, state.title);
        }
        boolean hasUpdated = state.updatedAt > 0;
        int updatedId = resourceId("id", ID_UPDATED);
        views.setViewVisibility(updatedId, hasUpdated ? View.VISIBLE : View.GONE);
        if (hasUpdated) {
            views.setTextViewText(updatedId, formatTime(state.updatedAt));
        }

        List<WidgetState.Card> cards = state.visibleCards();
        for (int i = 0; i < WidgetState.ROW_COUNT; i++) {
            String row = ID_ROW + i;
            int rowId = resourceId("id", row);
            if (i >= cards.size()) {
                views.setViewVisibility(rowId, View.GONE);
                continue;
            }
            WidgetState.Card card = cards.get(i);
            views.setViewVisibility(rowId, View.VISIBLE);
            int titleTextId = resourceId("id", row + "_title");
            int valueTextId = resourceId("id", row + "_value");
            views.setTextViewText(titleTextId, card.title);
            views.setTextViewText(valueTextId, valueText(card));
            int color = card.color == WidgetState.NO_COLOR ? THEME_VALUE_COLOR : card.color;
            views.setTextColor(valueTextId, color);
        }

        Intent intent = new Intent(ctx, MainActivity.class);
        intent.setData(Uri.parse(SCHEME + "://open/" + appWidgetId));
        intent.addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP | Intent.FLAG_ACTIVITY_CLEAR_TOP);
        PendingIntent pending = PendingIntent.getActivity(ctx, appWidgetId, intent,
                PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);
        views.setOnClickPendingIntent(resourceId("id", ID_ROOT), pending);
    }

    /** value 为主文本，subtitle 追加在后；布局 maxLines=1 时超出部分由系统省略。 */
    private static String valueText(WidgetState.Card card) {
        if (card.subtitle.isEmpty()) {
            return card.value;
        }
        return card.value.isEmpty() ? card.subtitle : card.value + " · " + card.subtitle;
    }

    private static String formatTime(long epochMillis) {
        return new SimpleDateFormat("MM-dd HH:mm", Locale.getDefault()).format(new Date(epochMillis));
    }

    private int resourceId(String type, String name) {
        String key = type + "/" + name;
        Integer cached = resourceIds.get(key);
        if (cached != null) {
            return cached;
        }
        int value = context.getResources().getIdentifier(name, type, context.getPackageName());
        if (value == 0) {
            Log.w(TAG, "缺少小组件资源: " + key);
        }
        resourceIds.put(key, value);
        return value;
    }
}
