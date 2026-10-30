package com.berba.shell;

import org.json.JSONObject;
import org.luaj.vm2.Globals;
import org.luaj.vm2.LuaError;
import org.luaj.vm2.LuaTable;
import org.luaj.vm2.LuaValue;
import org.luaj.vm2.Varargs;

import java.util.List;

/** 安装 widget.*：写缓存、清空与查询实例，不需要前台界面。 */
final class WidgetModule {
    private final LuaRuntime rt;
    private final WidgetController controller;

    private WidgetModule(LuaRuntime rt, WidgetController controller) {
        this.rt = rt;
        this.controller = controller;
    }

    static void install(LuaRuntime rt, Globals g) {
        WidgetModule module = new WidgetModule(rt, rt.widgetController());
        LuaTable widget = new LuaTable();
        widget.set("update", LuaApis.fn(module::update));
        widget.set("clear", LuaApis.fn(module::clear));
        widget.set("list", LuaApis.fn(module::list));
        g.set("widget", widget);
    }

    private Varargs update(Varargs args) {
        LuaTable options = args.checktable(1);
        String title = options.get("title").optjstring("");
        LuaValue cardsValue = options.get("cards");
        if (cardsValue.isnil()) {
            throw new LuaError("widget.update 需要 cards");
        }
        if (!cardsValue.istable()) {
            throw new LuaError("widget.update 的 cards 必须是数组");
        }
        LuaTable cards = cardsValue.checktable();
        if (cards.length() > WidgetState.MAX_CARDS) {
            throw new LuaError("widget.update 最多支持 " + WidgetState.MAX_CARDS
                    + " 张卡片（实际 " + cards.length() + " 张）");
        }
        LuaTable array = new LuaTable();
        for (int i = 1; i <= cards.length(); i++) {
            LuaValue item = cards.get(i);
            if (!item.istable()) {
                throw new LuaError("widget.update 的 cards[" + i + "] 必须是 table");
            }
            LuaTable card = item.checktable();
            array.set(i, cardOf(card, i));
        }

        LuaTable root = new LuaTable();
        root.set("title", LuaValue.valueOf(title));
        root.set("cards", array);

        // 空 table 会被 LuaConvert 编码成 {} 而不是 []，这里显式构造空数组。
        String json = array.length() == 0
                ? "{\"title\":" + JSONObject.quote(title) + ",\"cards\":[]}"
                : LuaConvert.toJson(root);

        try {
            controller.write(json);
        } catch (RuntimeException e) {
            return LuaApis.nilError(e);
        }

        LuaTable result = new LuaTable();
        result.set("updatedAt", LuaValue.valueOf(controller.state().updatedAt));
        result.set("ids", idsToLua(controller.instanceIds()));
        return result;
    }

    /** 只保留字符串字段；颜色非法时忽略（由 WidgetState 回退主题色）。 */
    private static LuaTable cardOf(LuaTable card, int index) {
        LuaTable out = new LuaTable();
        out.set("title", stringField(card, "title", index));
        out.set("value", stringField(card, "value", index));
        out.set("subtitle", stringField(card, "subtitle", index));
        LuaValue color = card.get("color");
        if (color.isstring()) {
            out.set("color", color);
        }
        return out;
    }

    private static LuaValue stringField(LuaTable card, String key, int index) {
        LuaValue value = card.get(key);
        if (value.isnil()) {
            return LuaValue.valueOf("");
        }
        if (!value.isstring()) {
            throw new LuaError("widget.update 的 cards[" + index + "] 的 " + key + " 必须是字符串");
        }
        return value;
    }

    private Varargs clear(Varargs args) {
        controller.clear();
        return LuaValue.NIL;
    }

    private Varargs list(Varargs args) {
        WidgetState state = controller.state();
        List<Integer> ids = controller.instanceIds();
        LuaTable out = new LuaTable();
        for (int i = 0; i < ids.size(); i++) {
            LuaTable item = new LuaTable();
            item.set("id", LuaValue.valueOf(ids.get(i)));
            item.set("updatedAt", LuaValue.valueOf(state.updatedAt));
            out.set(i + 1, item);
        }
        return out;
    }

    private static LuaTable idsToLua(List<Integer> ids) {
        LuaTable out = new LuaTable();
        for (int i = 0; i < ids.size(); i++) {
            out.set(i + 1, LuaValue.valueOf(ids.get(i)));
        }
        return out;
    }
}
