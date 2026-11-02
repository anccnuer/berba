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

    /** 行点击的 requestCode 偏移，避免与整块点击（requestCode = appWidgetId）撞车。 */
    private static final int ROW_REQUEST_BASE = 10000;

    private final Context context;
    private final AppWidgetManager manager;
    private final ComponentName provider;
    private final SharedPreferences prefs;
    private final Handler main = new Handler(Looper.getMainLooper());
    /**
     * 资源 id 缓存。
     *
     * <p>理论上只有主线程的 {@link #doRender} 会访问（Lua 线程只走 {@code write → main.post}），
     * 这里用 ConcurrentHashMap 作为兜底，避免日后有人从别的线程调用时踩到并发问题。
     */
    private final Map<String, Integer> resourceIds = new ConcurrentHashMap<>();

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

    /**
     * 一次有效小组件点击。
     *
     * <p>{@code index} 为被点击的行（0 起）；整块点击为 {@code -1}。
     */
    static final class Click {
        final int id;
        final int index;
        boolean coldStart;

        Click(int id, int index) {
            this.id = id;
            this.index = index;
        }
    }

    /**
     * 解析并清理 berba-widget://open/<id>[/<index>] Intent；无效来源返回 null。
     *
     * <p>没有行号表示整块点击，有行号表示点中第几行卡片。
     */
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
        String[] segments = path.split("/");
        try {
            if (segments.length < 2) {
                Log.w(TAG, "忽略小组件 Intent 缺少 id: " + data);
                return null;
            }
            int id = Integer.parseInt(segments[1]);
            int index = -1;
            if (segments.length > 2) {
                index = Integer.parseInt(segments[2]);
                if (index < 0 || index >= WidgetState.ROW_COUNT) {
                    Log.w(TAG, "忽略小组件 index 越界的 Intent: " + data);
                    return null;
                }
            }
            intent.setData(null);
            return new Click(id, index);
        } catch (NumberFormatException e) {
            Log.w(TAG, "忽略小组件 id/index 不合法的 Intent: " + data);
            return null;
        }
    }

    SharedPreferences prefs() {
        return prefs;
    }

    /**
     * 读指定键的缓存。原文存在但解析失败时记日志并删掉坏键：否则损坏的缓存会
     * 让小组件永远渲染空白，而且没有任何线索可查。损坏返回 null，由调用方决定回退策略。
     */
    private WidgetState readState(String key) {
        String raw = prefs.getString(key, null);
        if (raw == null || raw.isEmpty()) {
            return WidgetState.empty();
        }
        WidgetState parsed = WidgetState.parseOrNull(raw);
        if (parsed == null) {
            Log.w(TAG, "小组件缓存损坏，已清理 " + key + "（长度 " + raw.length() + "）");
            prefs.edit().remove(key).apply();
            return null;
        }
        return parsed;
    }

    /** 实例自己的缓存优先，缺失或损坏时回退到默认缓存。 */
    WidgetState stateFor(int appWidgetId) {
        WidgetState own = readState(WidgetState.instanceKey(appWidgetId));
        if (own != null) {
            return own;
        }
        WidgetState shared = readState(WidgetState.PREFS_KEY);
        return shared == null ? WidgetState.empty() : shared;
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

    /**
     * 校验 → 持久化 → 主线程渲染；超限抛 IllegalArgumentException。
     *
     * @param targetId 目标实例 id；null 表示写默认缓存，所有没有自己缓存的实例都会读到
     * @return 写入后的状态，调用方直接取 updatedAt 等字段即可，不必回读
     */
    WidgetState write(String json, Integer targetId) {
        WidgetState state = WidgetState.parse(json);
        state.updatedAt = System.currentTimeMillis();
        state.checkWritable();
        String key = targetId == null
                ? WidgetState.PREFS_KEY : WidgetState.instanceKey(targetId);
        prefs.edit().putString(key, state.toJson()).apply();
        render(context);
        return state;
    }

    /** 清空默认缓存与所有已知实例缓存，并把小组件显示为占位。 */
    void clear() {
        SharedPreferences.Editor editor = prefs.edit().remove(WidgetState.PREFS_KEY);
        for (int id : instanceIds()) {
            editor.remove(WidgetState.instanceKey(id));
        }
        editor.apply();
        render(context);
    }

    void onDeleted(int[] appWidgetIds) {
        if (appWidgetIds == null || appWidgetIds.length == 0) {
            return;
        }
        Log.i(TAG, "移除了 " + appWidgetIds.length + " 个小组件实例");
        render(context);
    }

    /** 最后一个实例被移除：清空缓存，删掉再加回来不再显示旧内容。 */
    void onDisabled() {
        Log.i(TAG, "所有小组件实例已移除，清空缓存");
        clear();
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
        int[] ids = manager.getAppWidgetIds(provider);
        for (int appWidgetId : ids) {
            // 每个实例可能有自己的缓存，必须在循环里逐个读。
            WidgetState state = stateFor(appWidgetId);
            if (state.hiddenCardCount() > 0) {
                Log.w(TAG, "小组件 " + appWidgetId + " 只渲染 " + WidgetState.ROW_COUNT
                        + " 行，截断 " + state.hiddenCardCount() + " 张卡片");
            }
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
            setRowClick(ctx, views, rowId, appWidgetId, i);
        }

        int rootId = resourceId("id", ID_ROOT);
        if (rootId != 0) {
            views.setOnClickPendingIntent(rootId, rootPending(ctx, appWidgetId));
        }
    }

    /** 整块点击：不带行号，Lua 侧收到的 index 为 -1。 */
    private static PendingIntent rootPending(Context ctx, int appWidgetId) {
        Intent intent = new Intent(ctx, MainActivity.class);
        intent.setData(Uri.parse(SCHEME + "://open/" + appWidgetId));
        intent.addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP | Intent.FLAG_ACTIVITY_CLEAR_TOP);
        return PendingIntent.getActivity(ctx, appWidgetId, intent,
                PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);
    }

    /** 单行点击：带上行号，Lua 侧能区分点的是哪一张卡片。 */
    private static void setRowClick(Context ctx, RemoteViews views, int rowId,
                                    int appWidgetId, int index) {
        if (rowId == 0) {
            return;
        }
        Intent intent = new Intent(ctx, MainActivity.class);
        intent.setData(Uri.parse(SCHEME + "://open/" + appWidgetId + "/" + index));
        intent.addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP | Intent.FLAG_ACTIVITY_CLEAR_TOP);
        PendingIntent pending = PendingIntent.getActivity(ctx,
                ROW_REQUEST_BASE + appWidgetId * WidgetState.ROW_COUNT + index, intent,
                PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);
        views.setOnClickPendingIntent(rowId, pending);
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
