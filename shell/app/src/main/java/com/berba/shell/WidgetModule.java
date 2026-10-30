package com.berba.shell;

import org.luaj.vm2.Globals;
import org.luaj.vm2.LuaError;
import org.luaj.vm2.LuaTable;
import org.luaj.vm2.LuaValue;
import org.luaj.vm2.Varargs;

import java.util.ArrayList;
import java.util.List;

/** 安装 widget.*：写入卡片缓存并推送 RemoteViews，无需前台界面。 */
final class WidgetModule {
    private final WidgetController controller;

    private WidgetModule(WidgetController controller) {
        this.controller = controller;
    }

    static void install(LuaRuntime rt, Globals g) {
        WidgetController controller = rt.widgetController();
        if (controller == null) {
            throw new LuaError("widget 能力未启用");
        }
        WidgetModule module = new WidgetModule(controller);
        LuaTable widget = new LuaTable();
        widget.set("update", LuaApis.fn(module::update));
        widget.set("clear", LuaApis.fn(args -> {
            module.controller.clear();
            return LuaValue.TRUE;
        }));
        widget.set("list", LuaApis.fn(module::list));
        g.set("widget", widget);
    }

    private Varargs update(Varargs args) {
        LuaValue options = args.arg(1);
        if (!options.istable()) {
            throw new LuaError("widget.update 需要 table");
        }
        LuaTable table = options.checktable();
        String title = table.get("title").optjstring("");

        LuaValue rawCards = table.get("cards");
        LuaTable source = rawCards.istable() ? rawCards.checktable() : table;
        int count = source.length();
        List<WidgetState.Card> cards = new ArrayList<>();
        for (int i = 1; i <= count && cards.size() < WidgetState.MAX_CARDS; i++) {
            LuaValue item = source.get(i);
            if (!item.istable()) {
                continue;
            }
            LuaTable card = item.checktable();
            cards.add(WidgetState.Card.of(
                    card.get("title").optjstring(""),
                    card.get("value").optjstring(""),
                    card.get("subtitle").optjstring(""),
                    card.get("color").optjstring("")));
        }

        long now = System.currentTimeMillis();
        long updatedAt;
        try {
            updatedAt = controller.write(WidgetState.State.of(now, title, cards));
        } catch (RuntimeException e) {
            return LuaApis.nilError(e);
        }

        LuaTable result = new LuaTable();
        result.set("updatedAt", LuaValue.valueOf((double) updatedAt));
        LuaTable ids = new LuaTable();
        int[] current = controller.ids();
        for (int i = 0; i < current.length; i++) {
            ids.set(i + 1, LuaValue.valueOf(current[i]));
        }
        result.set("ids", ids);
        return result;
    }

    private Varargs list(Varargs args) {
        long updatedAt = controller.load().updatedAt;
        LuaTable result = new LuaTable();
        int[] current = controller.ids();
        for (int i = 0; i < current.length; i++) {
            LuaTable item = new LuaTable();
            item.set("id", LuaValue.valueOf(current[i]));
            item.set("updatedAt", LuaValue.valueOf((double) updatedAt));
            result.set(i + 1, item);
        }
        return result;
    }
}
