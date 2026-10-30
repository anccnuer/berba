package com.berba.shell;

import android.Manifest;
import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Context;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.net.Uri;
import android.os.Build;
import android.util.Log;

import org.json.JSONObject;
import org.json.JSONTokener;

import java.nio.charset.StandardCharsets;
import java.util.UUID;
import java.util.concurrent.atomic.AtomicInteger;

/** 通知的通道、ID 映射、PendingIntent 与点击 payload 校验。不持有 Activity。 */
final class NotifyController {
    static final String ACTION_CLICK = "com.berba.shell.action.NOTIFICATION_CLICK";
    static final String EXTRA_PAYLOAD = "com.berba.shell.extra.NOTIFY_PAYLOAD";
    static final String EXTRA_TOKEN = "com.berba.shell.extra.NOTIFY_TOKEN";
    static final String NOTIFY_SCHEME = "berba-notify";
    static final String DEFAULT_CHANNEL = "default";
    static final int MAX_PAYLOAD_BYTES = 64 * 1024;

    private static final String TAG = "berba";

    /** 一次有效通知点击。id/data 为 org.json 解析出的值。 */
    static final class Click {
        final Object id;
        final Object data;
        boolean coldStart;

        Click(Object id, Object data) {
            this.id = id;
            this.data = data;
        }
    }

    private final Context context;
    private final NotificationManager manager;
    private final String appName;
    private final AtomicInteger sequence = new AtomicInteger(1);

    NotifyController(Context context) {
        this.context = context.getApplicationContext();
        this.manager =
                (NotificationManager) this.context.getSystemService(Context.NOTIFICATION_SERVICE);
        CharSequence label = this.context.getApplicationInfo().loadLabel(
                this.context.getPackageManager());
        String name = label == null ? this.context.getPackageName() : label.toString();
        this.appName = name.trim().isEmpty() ? this.context.getPackageName() : name.trim();
        ensureDefaultChannel();
    }

    boolean enabled() {
        if (manager == null) {
            return false;
        }
        if (Build.VERSION.SDK_INT >= 33
                && context.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS)
                != PackageManager.PERMISSION_GRANTED) {
            return false;
        }
        return manager.areNotificationsEnabled();
    }

    /** 显示或替换通知；返回 string 或 Integer 形式的 id。 */
    Object show(String tag, int id, String title, String body, String channelId,
                String dataJson, boolean ongoing, boolean autoCancel) {
        if (!enabled()) {
            throw new IllegalStateException("通知权限未授予");
        }
        String text = title == null ? "" : title;
        if (text.trim().isEmpty()) {
            text = appName;
        }
        if (text.length() > 512) {
            throw new IllegalArgumentException("通知标题超过 512 字符");
        }
        String content = body == null ? "" : body;
        if (content.length() > 8192) {
            throw new IllegalArgumentException("通知正文超过 8192 字符");
        }
        if (channelId == null || channelId.isEmpty()) {
            channelId = DEFAULT_CHANNEL;
        }
        ensureChannelIfMissing(channelId);

        String payload;
        try {
            JSONObject envelope = new JSONObject();
            if (tag != null) {
                envelope.put("id", tag);
            } else {
                envelope.put("id", id);
            }
            envelope.put("data", new JSONTokener(dataJson == null ? "null" : dataJson).nextValue());
            payload = envelope.toString();
        } catch (Exception e) {
            throw new IllegalArgumentException("通知 data 无法序列化: " + e.getMessage());
        }
        int size = payload.getBytes(StandardCharsets.UTF_8).length;
        if (size > MAX_PAYLOAD_BYTES) {
            throw new IllegalArgumentException("通知 payload 超过 64KB（实际 " + size + " 字节）");
        }

        Intent click = new Intent(ACTION_CLICK);
        click.setClass(context, MainActivity.class);
        click.setData(Uri.parse(NOTIFY_SCHEME + "://click/" + UUID.randomUUID()));
        click.putExtra(EXTRA_PAYLOAD, payload);
        click.putExtra(EXTRA_TOKEN, NotificationToken.get(context));
        click.addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP | Intent.FLAG_ACTIVITY_CLEAR_TOP);
        int requestCode = tag != null ? tag.hashCode() : id;
        PendingIntent pending = PendingIntent.getActivity(context, requestCode, click,
                PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);

        Notification.Builder builder = Build.VERSION.SDK_INT >= 26
                ? new Notification.Builder(context, channelId)
                : new Notification.Builder(context);
        builder.setSmallIcon(smallIcon())
                .setContentTitle(text)
                .setContentIntent(pending)
                .setAutoCancel(autoCancel)
                .setOngoing(ongoing)
                .setWhen(System.currentTimeMillis());
        if (!content.isEmpty()) {
            builder.setContentText(content);
        }
        Notification notification = builder.build();
        if (tag != null) {
            manager.notify(tag, id, notification);
            return tag;
        }
        manager.notify(id, notification);
        return id;
    }

    boolean cancel(String tag, int id) {
        if (manager == null) {
            return false;
        }
        manager.cancel(tag, id);
        return true;
    }

    void cancelAll() {
        if (manager != null) {
            manager.cancelAll();
        }
    }

    /** 创建通道；已存在时只更新名称/描述，重要性保持首次设置。 */
    void ensureChannel(String id, String name, String description, String importance) {
        if (Build.VERSION.SDK_INT < 26) {
            return;
        }
        if (manager == null) {
            throw new IllegalStateException("通知服务不可用");
        }
        String channelName = name == null || name.trim().isEmpty() ? id : name.trim();
        String desc = description == null ? "" : description;
        int level = parseImportance(importance);
        NotificationChannel existing = manager.getNotificationChannel(id);
        NotificationChannel channel;
        if (existing != null) {
            channel = new NotificationChannel(id, channelName, existing.getImportance());
            channel.setDescription(desc);
        } else {
            channel = new NotificationChannel(id, channelName, level);
            channel.setDescription(desc);
        }
        manager.createNotificationChannel(channel);
    }

    private void ensureDefaultChannel() {
        if (Build.VERSION.SDK_INT < 26 || manager == null) {
            return;
        }
        if (manager.getNotificationChannel(DEFAULT_CHANNEL) == null) {
            NotificationChannel channel = new NotificationChannel(
                    DEFAULT_CHANNEL, appName, NotificationManager.IMPORTANCE_DEFAULT);
            channel.setDescription("应用通知");
            manager.createNotificationChannel(channel);
        }
    }

    private void ensureChannelIfMissing(String id) {
        if (Build.VERSION.SDK_INT < 26 || manager == null) {
            return;
        }
        if (manager.getNotificationChannel(id) == null) {
            ensureChannel(id, id, "", "default");
        }
    }

    private static int parseImportance(String importance) {
        String value = importance == null ? "default" : importance.trim().toLowerCase();
        switch (value) {
            case "min":
                return NotificationManager.IMPORTANCE_MIN;
            case "low":
                return NotificationManager.IMPORTANCE_LOW;
            case "high":
                return NotificationManager.IMPORTANCE_HIGH;
            case "default":
            case "":
                return NotificationManager.IMPORTANCE_DEFAULT;
            default:
                throw new IllegalArgumentException(
                        "importance 只能是 min/low/default/high: " + importance);
        }
    }

    private int smallIcon() {
        int icon = context.getResources().getIdentifier(
                "ic_stat_berba", "drawable", context.getPackageName());
        return icon != 0 ? icon : android.R.drawable.ic_dialog_info;
    }

    /** 解析并清理通知点击 Intent；无效来源返回 null。 */
    Click parseClick(Intent intent) {
        if (intent == null) {
            return null;
        }
        String payload = intent.getStringExtra(EXTRA_PAYLOAD);
        String token = intent.getStringExtra(EXTRA_TOKEN);
        if (payload == null && token == null) {
            return null;
        }
        String action = intent.getAction();
        clearExtras(intent);
        if (payload == null || token == null) {
            Log.w(TAG, "忽略缺少 payload/token 的通知 Intent");
            return null;
        }
        if (!ACTION_CLICK.equals(action)) {
            Log.w(TAG, "忽略 action 不匹配的通知 Intent");
            return null;
        }
        if (!NotificationToken.matches(context, token)) {
            Log.w(TAG, "忽略 token 不匹配的通知 Intent");
            return null;
        }
        if (payload.getBytes(StandardCharsets.UTF_8).length > MAX_PAYLOAD_BYTES) {
            Log.w(TAG, "忽略超过 64KB 的通知 payload");
            return null;
        }
        try {
            JSONObject envelope = new JSONObject(payload);
            if (!envelope.has("id") || !envelope.has("data")) {
                Log.w(TAG, "忽略缺少 id/data 的通知 payload");
                return null;
            }
            return new Click(envelope.get("id"), envelope.get("data"));
        } catch (Exception e) {
            Log.w(TAG, "忽略无法解析的通知 payload: " + e);
            return null;
        }
    }

    private static void clearExtras(Intent intent) {
        intent.removeExtra(EXTRA_PAYLOAD);
        intent.removeExtra(EXTRA_TOKEN);
        Uri data = intent.getData();
        if (data != null && NOTIFY_SCHEME.equalsIgnoreCase(data.getScheme())) {
            intent.setData(null);
        }
    }
}
