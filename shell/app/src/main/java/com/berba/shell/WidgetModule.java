package com.berba.shell;

import org.json.JSONObject;
import org.luaj.vm2.Globals;
import org.luaj.vm2.LuaError;
import org.luaj.vm2.LuaTable;
import org.luaj.vm2.LuaValue;
import org.luaj.vm2.Varargs;

import java.util.Collections;
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
        Integer targetId = targetInstance(args);
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

        // 指定 id 时只有该实例会收到这次更新；目标不存在时什么都不写，
        // 免得留下永远不会被读到的 state:<id> 垃圾键。
        List<Integer> instanceIds = controller.instanceIds();
        boolean exists = targetId == null || instanceIds.contains(targetId);

        WidgetState written = null;
        if (exists) {
            try {
                written = controller.write(json, targetId);
            } catch (RuntimeException e) {
                return LuaApis.nilError(e);
            }
        }

        List<Integer> ids = Collections.emptyList();
        if (exists) {
            ids = targetId == null ? instanceIds : Collections.singletonList(targetId);
        }

        LuaTable result = new LuaTable();
        result.set("updatedAt", LuaValue.valueOf(written == null ? 0L : written.updatedAt));
        result.set("ids", idsToLua(ids));
        result.set("truncated", LuaValue.valueOf(WidgetState.truncatedCount(cards.length())));
        // 全局更新但桌面上还没有实例时仍会写缓存，但没有任何实例被渲染，
        // rendered 必须是 false（与 widget.update 文档一致）。
        boolean rendered = targetId == null ? !instanceIds.isEmpty() : exists;
        result.set("rendered", LuaValue.valueOf(rendered));
        return result;
    }

    /** 可选的第二个参数：目标实例 id；不传表示写默认缓存，所有实例一起更新。 */
    private static Integer targetInstance(Varargs args) {
        if (args.narg() < 2 || args.arg(2).isnil()) {
            return null;
        }
        LuaValue value = args.arg(2);
        if (!value.isnumber()) {
            throw new LuaError("widget.update 的 id 必须是数字");
        }
        int id = value.toint();
        if (id <= 0) {
            throw new LuaError("widget.update 的 id 必须是正整数");
        }
        return id;
    }

    /**
     * 只保留字符串字段。
     *
     * <p>颜色的字符串格式非法（如 "red"）时忽略，由 {@link WidgetState} 回退主题色；
     * 但传了非字符串（数字/布尔/表）属于调用方 bug，和 title/value/subtitle 一样直接报错。
     */
    private static LuaTable cardOf(LuaTable card, int index) {
        LuaTable out = new LuaTable();
        out.set("title", stringField(card, "title", index));
        out.set("value", stringField(card, "value", index));
        out.set("subtitle", stringField(card, "subtitle", index));
        LuaValue color = card.get("color");
        if (!color.isnil()) {
            if (!color.isstring()) {
                throw new LuaError("widget.update 的 cards[" + index + "] 的 color 必须是字符串");
            }
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
        List<Integer> ids = controller.instanceIds();
        LuaTable out = new LuaTable();
        for (int i = 0; i < ids.size(); i++) {
            int id = ids.get(i);
            LuaTable item = new LuaTable();
            item.set("id", LuaValue.valueOf(id));
            item.set("updatedAt", LuaValue.valueOf(controller.stateFor(id).updatedAt));
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
