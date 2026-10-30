package com.berba.shell;

import android.content.Context;
import android.content.Intent;
import android.util.Log;

import org.luaj.vm2.LuaTable;
import org.luaj.vm2.LuaValue;

import java.util.ArrayList;
import java.util.List;

/**
 * 进程级单例：持有配置与 LuaRuntime，并把 Activity 生命周期映射为 UiSession。
 * Activity 销毁只 detach，不结束 Lua 运行时。
 */
final class RuntimeManager {
    private static final String TAG = "berba";

    private final Context appContext;
    private final AppConfig config;
    private final CapabilityConfig capabilities;
    private final NotifyController notify;
    private final WidgetController widget;
    private final LuaRuntime runtime;
    private final List<NotifyController.Click> pendingClicks = new ArrayList<>();
    private final List<WidgetController.Click> pendingWidgets = new ArrayList<>();
    private volatile UiSession current;
    private volatile boolean attachedOnce;

    private RuntimeManager(Context ctx) {
        this.appContext = ctx.getApplicationContext();
        this.config = AppConfig.load(appContext);
        this.capabilities = CapabilityConfig.load(appContext);
        this.notify = capabilities.hasCapability("notify")
                ? new NotifyController(appContext) : null;
        this.widget = capabilities.hasCapability("widget")
                ? WidgetController.get(appContext) : null;
        this.runtime = new LuaRuntime(appContext, config, capabilities, notify, widget);
        runtime.start();
    }

    static synchronized RuntimeManager get(Context ctx) {
        if (instance == null) {
            instance = new RuntimeManager(ctx);
        }
        return instance;
    }

    private static volatile RuntimeManager instance;

    AppConfig config() {
        return config;
    }

    LuaRuntime runtime() {
        return runtime;
    }

    UiSession attach(MainActivity activity, Runnable afterCreate) {
        attachedOnce = true;
        UiSession session = new UiSession(this, activity);
        current = session;
        runtime.attach(session, afterCreate);
        return session;
    }

    void detach(UiSession session) {
        if (session == null) {
            return;
        }
        if (current == session) {
            current = null;
        }
        runtime.detach(session);
    }

    boolean isCurrent(UiSession session) {
        return session != null && current == session;
    }

    /** 解析通知点击 Intent 并排队；返回是否为有效点击。 */
    boolean captureNotification(Intent intent) {
        if (notify == null) {
            return false;
        }
        NotifyController.Click click = notify.parseClick(intent);
        if (click == null) {
            return false;
        }
        click.coldStart = !attachedOnce;
        synchronized (pendingClicks) {
            pendingClicks.add(click);
        }
        return true;
    }

    /** 在 Lua 就绪后把排队的通知点击投递为 notification 事件。 */
    void deliverNotifications(LuaRuntime rt) {
        if (rt == null) {
            return;
        }
        List<NotifyController.Click> clicks;
        synchronized (pendingClicks) {
            if (pendingClicks.isEmpty()) {
                return;
            }
            clicks = new ArrayList<>(pendingClicks);
            pendingClicks.clear();
        }
        rt.whenReady(() -> {
            for (NotifyController.Click click : clicks) {
                LuaValue id;
                LuaValue data;
                try {
                    id = LuaConvert.fromJson(click.id);
                    data = LuaConvert.fromJson(click.data);
                } catch (Exception e) {
                    Log.w(TAG, "通知 payload 转换失败", e);
                    continue;
                }
                LuaTable event = new LuaTable();
                event.set("id", id);
                event.set("data", data);
                event.set("coldStart", LuaValue.valueOf(click.coldStart));
                rt.dispatchEvent("notification", new LuaValue[]{event});
            }
        });
    }

    /** 解析小组件点击 Intent 并排队；返回是否为有效点击。 */
    boolean captureWidget(Intent intent) {
        if (widget == null) {
            return false;
        }
        WidgetController.Click click = widget.parseClick(intent);
        if (click == null) {
            return false;
        }
        click.coldStart = !attachedOnce;
        synchronized (pendingWidgets) {
            pendingWidgets.add(click);
        }
        return true;
    }

    /** 在 Lua 就绪后把排队的小组件点击投递为 widget 事件。 */
    void deliverWidgets(LuaRuntime rt) {
        if (rt == null) {
            return;
        }
        List<WidgetController.Click> clicks;
        synchronized (pendingWidgets) {
            if (pendingWidgets.isEmpty()) {
                return;
            }
            clicks = new ArrayList<>(pendingWidgets);
            pendingWidgets.clear();
        }
        rt.whenReady(() -> {
            for (WidgetController.Click click : clicks) {
                LuaTable event = new LuaTable();
                event.set("id", LuaValue.valueOf(click.id));
                event.set("coldStart", LuaValue.valueOf(click.coldStart));
                rt.dispatchEvent("widget", new LuaValue[]{event});
            }
        });
    }

    /** 把一次后台任务运行投递到 Lua 线程；无处理器或未启用时立即结束。 */
    void dispatchTask(TaskInvocation invocation) {
        if (invocation == null) {
            return;
        }
        if (!capabilities.hasCapability("task")) {
            invocation.finish("任务能力未启用");
            return;
        }
        runtime.whenReady(() -> runtime.dispatchTask(invocation));
    }

    static synchronized void shutdownForTests() {
        if (instance == null) {
            return;
        }
        instance.current = null;
        instance.runtime.shutdownForTests();
        WidgetController.release(instance.appContext);
        instance = null;
    }
}
