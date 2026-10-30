package com.berba.shell;

import org.luaj.vm2.Globals;
import org.luaj.vm2.LuaError;
import org.luaj.vm2.LuaTable;
import org.luaj.vm2.LuaValue;
import org.luaj.vm2.Varargs;

import java.util.concurrent.atomic.AtomicInteger;

/** 安装 notify.*：无需前台界面即可显示与取消通知。 */
final class NotifyModule {
    private final NotifyController controller;
    private final AtomicInteger sequence = new AtomicInteger(1);

    private NotifyModule(NotifyController controller) {
        this.controller = controller;
    }

    static void install(LuaRuntime rt, Globals g) {
        NotifyModule module = new NotifyModule(rt.notifyController());
        LuaTable notify = new LuaTable();
        notify.set("show", LuaApis.fn(module::show));
        notify.set("cancel", LuaApis.fn(module::cancel));
        notify.set("cancelAll", LuaApis.fn(args -> {
            module.controller.cancelAll();
            return LuaValue.NIL;
        }));
        notify.set("enabled", LuaApis.fn(args ->
                module.controller.enabled() ? LuaValue.TRUE : LuaValue.FALSE));
        notify.set("channel", LuaApis.fn(module::channel));
        g.set("notify", notify);
    }

    private Varargs show(Varargs args) {
        LuaValue options = args.arg(1);
        String title;
        String body;
        String channel = NotifyController.DEFAULT_CHANNEL;
        String dataJson = "null";
        boolean ongoing = false;
        boolean autoCancel = true;
        LuaValue idValue = LuaValue.NIL;

        if (options.istable()) {
            LuaTable t = options.checktable();
            title = t.get("title").optjstring("");
            body = t.get("body").optjstring("");
            channel = parseChannelId(t.get("channel").optjstring(NotifyController.DEFAULT_CHANNEL));
            dataJson = LuaConvert.toJson(t.get("data"));
            ongoing = t.get("ongoing").optboolean(false);
            autoCancel = t.get("autoCancel").optboolean(true);
            idValue = t.get("id");
        } else if (options.isstring() || options.isnumber()) {
            title = options.tojstring();
            body = args.arg(2).optjstring("");
        } else {
            throw new LuaError("notify.show 需要 table 或标题字符串");
        }

        if (!controller.enabled()) {
            return LuaApis.nilError(new IllegalStateException("通知权限未授予"));
        }
        Key key = keyFor(idValue, true);
        try {
            Object id = controller.show(key.tag, key.id, title, body, channel,
                    dataJson, ongoing, autoCancel);
            return idToLua(id);
        } catch (RuntimeException e) {
            return LuaApis.nilError(e);
        }
    }

    private Varargs cancel(Varargs args) {
        Key key = keyFor(args.arg(1), false);
        boolean ok = controller.cancel(key.tag, key.id);
        return LuaValue.valueOf(ok);
    }

    private Varargs channel(Varargs args) {
        LuaTable t = args.checktable(1);
        String id = parseChannelId(t.get("id").optjstring(NotifyController.DEFAULT_CHANNEL));
        String name = t.get("name").optjstring("");
        String description = t.get("description").optjstring("");
        String importance = t.get("importance").optjstring("default");
        controller.ensureChannel(id, name, description, importance);
        return LuaValue.TRUE;
    }

    private Key keyFor(LuaValue value, boolean allowAuto) {
        if (value.isnil()) {
            if (!allowAuto) {
                throw new LuaError("notify.cancel 需要 id");
            }
            int next = sequence.getAndIncrement();
            if (next <= 0) {
                sequence.set(2);
                next = 1;
            }
            return new Key(null, next);
        }
        if (value.isnumber()) {
            double d = value.todouble();
            int id = (int) d;
            if (d != id || id <= 0) {
                throw new LuaError("notify id 必须是正整数或非空字符串");
            }
            return new Key(null, id);
        }
        if (value.isstring()) {
            String tag = value.tojstring();
            if (tag.isEmpty() || tag.length() > 256) {
                throw new LuaError("notify 字符串 id 长度需在 1-256 之间");
            }
            return new Key(tag, 0);
        }
        throw new LuaError("notify id 必须是正整数或非空字符串");
    }

    private static String parseChannelId(String raw) {
        String id = raw == null ? NotifyController.DEFAULT_CHANNEL : raw.trim();
        if (id.isEmpty()) {
            id = NotifyController.DEFAULT_CHANNEL;
        }
        if (id.length() > 64) {
            throw new LuaError("notify channel id 不能超过 64 字符");
        }
        for (int i = 0; i < id.length(); i++) {
            char c = id.charAt(i);
            boolean ok = (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
                    || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.';
            if (!ok) {
                throw new LuaError("notify channel id 只能包含字母、数字、_ - .");
            }
        }
        return id;
    }

    private static LuaValue idToLua(Object id) {
        if (id instanceof String) {
            return LuaValue.valueOf((String) id);
        }
        return LuaValue.valueOf(((Number) id).intValue());
    }

    private static final class Key {
        final String tag;
        final int id;

        Key(String tag, int id) {
            this.tag = tag;
            this.id = id;
        }
    }
}
