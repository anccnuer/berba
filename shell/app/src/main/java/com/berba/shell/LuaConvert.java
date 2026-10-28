package com.berba.shell;

import org.json.JSONArray;
import org.json.JSONObject;
import org.json.JSONTokener;
import org.luaj.vm2.LuaTable;
import org.luaj.vm2.LuaValue;

import java.util.Collections;
import java.util.IdentityHashMap;
import java.util.Iterator;
import java.util.Set;

/** Lua 值与 JSON 的互转。连续 1..n 键的 table 视为数组。 */
final class LuaConvert {
    private static final int MAX_DEPTH = 64;

    private LuaConvert() {
    }

    static LuaValue fromJson(Object value) throws Exception {
        if (value == null || value == JSONObject.NULL) {
            return LuaValue.NIL;
        }
        if (value instanceof Boolean) {
            return LuaValue.valueOf((Boolean) value);
        }
        if (value instanceof Integer) {
            return LuaValue.valueOf((Integer) value);
        }
        if (value instanceof Long) {
            return LuaValue.valueOf(((Long) value).doubleValue());
        }
        if (value instanceof Number) {
            return LuaValue.valueOf(((Number) value).doubleValue());
        }
        if (value instanceof String) {
            return LuaValue.valueOf((String) value);
        }
        if (value instanceof JSONArray) {
            JSONArray arr = (JSONArray) value;
            LuaTable t = new LuaTable();
            for (int i = 0; i < arr.length(); i++) {
                t.set(i + 1, fromJson(arr.get(i)));
            }
            return t;
        }
        if (value instanceof JSONObject) {
            JSONObject obj = (JSONObject) value;
            LuaTable t = new LuaTable();
            Iterator<String> it = obj.keys();
            while (it.hasNext()) {
                String k = it.next();
                t.set(k, fromJson(obj.get(k)));
            }
            return t;
        }
        return LuaValue.valueOf(String.valueOf(value));
    }

    static LuaValue parse(String json) throws Exception {
        if (json == null || json.isEmpty()) {
            return LuaValue.NIL;
        }
        Object value = new JSONTokener(json).nextValue();
        return fromJson(value);
    }

    static String toJson(LuaValue v) {
        StringBuilder sb = new StringBuilder();
        try {
            write(sb, v, 0, Collections.newSetFromMap(new IdentityHashMap<LuaTable, Boolean>()));
        } catch (Exception e) {
            return "null";
        }
        return sb.toString();
    }

    private static void write(StringBuilder sb, LuaValue v, int depth, Set<LuaTable> path) {
        if (depth > MAX_DEPTH) {
            throw new IllegalArgumentException("table 嵌套过深或存在循环引用");
        }
        if (v == null || v.isnil()) {
            sb.append("null");
        } else if (v.isboolean()) {
            sb.append(v.toboolean() ? "true" : "false");
        } else if (v.isint()) {
            sb.append(v.toint());
        } else if (v.isnumber()) {
            double d = v.todouble();
            if (Double.isNaN(d) || Double.isInfinite(d)) {
                sb.append("null");
            } else {
                sb.append(d);
            }
        } else if (v.isstring()) {
            sb.append(JSONObject.quote(v.tojstring()));
        } else if (v.istable()) {
            LuaTable t = v.checktable();
            if (!path.add(t)) {
                throw new IllegalArgumentException("存在循环引用");
            }
            int n = t.length();
            LuaValue[] keys = t.keys();
            boolean isArray = n > 0 && keys.length == n;
            if (isArray) {
                sb.append('[');
                for (int i = 1; i <= n; i++) {
                    if (i > 1) {
                        sb.append(',');
                    }
                    write(sb, t.get(i), depth + 1, path);
                }
                sb.append(']');
            } else {
                sb.append('{');
                boolean first = true;
                for (LuaValue k : keys) {
                    if (k.isnil()) {
                        continue;
                    }
                    if (!first) {
                        sb.append(',');
                    }
                    first = false;
                    sb.append(JSONObject.quote(k.tojstring()));
                    sb.append(':');
                    write(sb, t.get(k), depth + 1, path);
                }
                sb.append('}');
            }
            path.remove(t);
        } else {
            sb.append("null");
        }
    }
}
