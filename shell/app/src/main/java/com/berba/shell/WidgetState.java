package com.berba.shell;

import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.TreeMap;

/**
 * 小组件状态：卡片结构、JSON 解析/序列化与截断逻辑。
 *
 * <p>纯 Java（不依赖 android.* 与 org.json），便于在 JVM 单测中直接验证；
 * 进程被回收后 WidgetProvider 只读这份缓存渲染，不启动 Lua。
 */
final class WidgetState {
    static final String PREFS_NAME = "berba_widget";
    static final String PREFS_KEY = "state";

    /** 缓存序列化上限。 */
    static final int MAX_BYTES = 32 * 1024;
    /** 单次写入最多保留的卡片数。 */
    static final int MAX_CARDS = 20;
    /** RemoteViews 固定渲染的行槽位数。 */
    static final int ROW_COUNT = 5;
    static final int TITLE_MAX = 24;
    static final int VALUE_MAX = 16;
    static final int SUBTITLE_MAX = 32;

    /** 卡片未指定颜色时由渲染层回退到主题色。 */
    static final int NO_COLOR = 0;

    static final class Card {
        String title = "";
        String value = "";
        String subtitle = "";
        int color = NO_COLOR;

        Card() {
        }

        Card(String title, String value, String subtitle, int color) {
            this.title = title;
            this.value = value;
            this.subtitle = subtitle;
            this.color = color;
        }
    }

    long updatedAt;
    String title = "";
    final List<Card> cards = new ArrayList<>();

    static WidgetState empty() {
        return new WidgetState();
    }

    /** 只保留渲染槽位内的卡片；超出部分由调用方记日志。 */
    List<Card> visibleCards() {
        if (cards.size() <= ROW_COUNT) {
            return cards;
        }
        return Collections.unmodifiableList(cards.subList(0, ROW_COUNT));
    }

    int hiddenCardCount() {
        return cards.size() > ROW_COUNT ? cards.size() - ROW_COUNT : 0;
    }

    int byteSize() {
        return toJson().getBytes(StandardCharsets.UTF_8).length;
    }

    /** 校验写入上限；超限抛 IllegalArgumentException（中文消息，供 Lua 侧透出）。 */
    void checkWritable() {
        if (cards.size() > MAX_CARDS) {
            throw new IllegalArgumentException(
                    "小组件最多支持 " + MAX_CARDS + " 张卡片（实际 " + cards.size() + " 张）");
        }
        int size = byteSize();
        if (size > MAX_BYTES) {
            throw new IllegalArgumentException(
                    "小组件状态超过 " + (MAX_BYTES / 1024) + "KB 上限（实际 " + size + " 字节）");
        }
    }

    static String truncate(String value, int max) {
        if (value == null) {
            return "";
        }
        if (value.length() <= max) {
            return value;
        }
        return value.substring(0, Math.max(0, max - 1)) + "…";
    }

    /** 解析 #RRGGBB / #AARRGGBB；非法返回 NO_COLOR。 */
    static int parseColor(String raw) {
        if (raw == null) {
            return NO_COLOR;
        }
        String value = raw.trim();
        int length = value.length();
        if (length != 7 && length != 9) {
            return NO_COLOR;
        }
        if (value.charAt(0) != '#') {
            return NO_COLOR;
        }
        for (int i = 1; i < length; i++) {
            char c = value.charAt(i);
            boolean hex = (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F');
            if (!hex) {
                return NO_COLOR;
            }
        }
        try {
            return (int) Long.parseLong(value.substring(1), 16);
        } catch (NumberFormatException e) {
            return NO_COLOR;
        }
    }

    /** 严格解析；非法 JSON 抛 IllegalArgumentException。 */
    static WidgetState parse(String json) {
        Object value = Json.parse(json == null ? "" : json);
        if (!(value instanceof Map)) {
            throw new IllegalArgumentException("小组件缓存必须是 JSON 对象");
        }
        Map<?, ?> object = (Map<?, ?>) value;
        WidgetState state = new WidgetState();
        Object rawUpdated = object.get("updatedAt");
        if (rawUpdated != null && !(rawUpdated instanceof Number)) {
            throw new IllegalArgumentException("小组件 updatedAt 必须是数字");
        }
        if (rawUpdated instanceof Number) {
            state.updatedAt = ((Number) rawUpdated).longValue();
        }
        Object rawTitle = object.get("title");
        if (rawTitle instanceof String) {
            state.title = truncate((String) rawTitle, TITLE_MAX);
        }
        Object rawCards = object.get("cards");
        if (rawCards == null) {
            return state;
        }
        if (!(rawCards instanceof List)) {
            throw new IllegalArgumentException("小组件 cards 必须是数组");
        }
        List<?> list = (List<?>) rawCards;
        for (int i = 0; i < list.size(); i++) {
            Object item = list.get(i);
            if (!(item instanceof Map)) {
                throw new IllegalArgumentException("小组件 cards[" + i + "] 必须是对象");
            }
            Map<?, ?> card = (Map<?, ?>) item;
            WidgetState.Card out = new WidgetState.Card();
            out.title = cardString(card, "title", TITLE_MAX, i);
            out.value = cardString(card, "value", VALUE_MAX, i);
            out.subtitle = cardString(card, "subtitle", SUBTITLE_MAX, i);
            Object rawColor = card.get("color");
            out.color = rawColor instanceof String ? parseColor((String) rawColor) : NO_COLOR;
            state.cards.add(out);
        }
        return state;
    }

    private static String cardString(Map<?, ?> card, String key, int max, int index) {
        Object raw = card.get(key);
        if (raw == null) {
            return "";
        }
        if (!(raw instanceof String)) {
            throw new IllegalArgumentException(
                    "小组件 cards[" + index + "] 的 " + key + " 必须是字符串");
        }
        return truncate((String) raw, max);
    }

    /** 宽松解析：缓存损坏时回退为空状态，而不是让渲染失败。 */
    static WidgetState parseLenient(String json) {
        try {
            return parse(json);
        } catch (RuntimeException e) {
            return empty();
        }
    }

    String toJson() {
        Map<String, Object> object = new TreeMap<>();
        object.put("updatedAt", updatedAt);
        object.put("title", title);
        List<Map<String, Object>> list = new ArrayList<>();
        for (WidgetState.Card card : cards) {
            Map<String, Object> item = new TreeMap<>();
            item.put("title", card.title);
            item.put("value", card.value);
            item.put("subtitle", card.subtitle);
            item.put("color", card.color == NO_COLOR ? null : String.format("#%08X", card.color));
            list.add(item);
        }
        object.put("cards", list);
        return Json.write(object);
    }

    /** 极简 JSON 解析/序列化，避免在纯 Java 路径上依赖 org.json。 */
    private static final class Json {
        private static final int MAX_DEPTH = 32;

        private final String src;
        private int pos;

        private Json(String src) {
            this.src = src;
        }

        static Object parse(String text) {
            Json reader = new Json(text);
            reader.skipWhitespace();
            Object value = reader.readValue(0);
            reader.skipWhitespace();
            if (reader.pos != text.length()) {
                throw reader.error("尾部存在多余内容");
            }
            return value;
        }

        static String write(Object value) {
            StringBuilder sb = new StringBuilder();
            writeValue(sb, value);
            return sb.toString();
        }

        private static void writeValue(StringBuilder sb, Object value) {
            if (value == null) {
                sb.append("null");
            } else if (value instanceof String) {
                writeString(sb, (String) value);
            } else if (value instanceof Boolean) {
                sb.append(((Boolean) value) ? "true" : "false");
            } else if (value instanceof Double || value instanceof Float) {
                double d = ((Number) value).doubleValue();
                if (d == Math.rint(d) && !Double.isInfinite(d) && Math.abs(d) < 9.007199254740992E15) {
                    sb.append(Long.toString((long) d));
                } else {
                    sb.append(Double.toString(d));
                }
            } else if (value instanceof Number) {
                sb.append(value.toString());
            } else if (value instanceof Map) {
                sb.append('{');
                boolean first = true;
                for (Map.Entry<?, ?> entry : ((Map<?, ?>) value).entrySet()) {
                    if (!first) {
                        sb.append(',');
                    }
                    first = false;
                    writeString(sb, String.valueOf(entry.getKey()));
                    sb.append(':');
                    writeValue(sb, entry.getValue());
                }
                sb.append('}');
            } else if (value instanceof List) {
                sb.append('[');
                List<?> list = (List<?>) value;
                for (int i = 0; i < list.size(); i++) {
                    if (i > 0) {
                        sb.append(',');
                    }
                    writeValue(sb, list.get(i));
                }
                sb.append(']');
            } else {
                throw new IllegalArgumentException("无法序列化的小组件字段类型: "
                        + (value == null ? "null" : value.getClass().getName()));
            }
        }

        private static void writeString(StringBuilder sb, String value) {
            sb.append('"');
            for (int i = 0; i < value.length(); i++) {
                char c = value.charAt(i);
                switch (c) {
                    case '"':
                        sb.append("\\\"");
                        break;
                    case '\\':
                        sb.append("\\\\");
                        break;
                    case '\n':
                        sb.append("\\n");
                        break;
                    case '\r':
                        sb.append("\\r");
                        break;
                    case '\t':
                        sb.append("\\t");
                        break;
                    case '\b':
                        sb.append("\\b");
                        break;
                    case '\f':
                        sb.append("\\f");
                        break;
                    default:
                        if (c < 0x20) {
                            sb.append(String.format("\\u%04x", (int) c));
                        } else {
                            sb.append(c);
                        }
                        break;
                }
            }
            sb.append('"');
        }

        private Object readValue(int depth) {
            if (depth > MAX_DEPTH) {
                throw error("嵌套层级过深");
            }
            if (pos >= src.length()) {
                throw error("内容不完整");
            }
            char c = src.charAt(pos);
            switch (c) {
                case '{':
                    return readObject(depth);
                case '[':
                    return readArray(depth);
                case '"':
                    return readString();
                case 't':
                    expect("true");
                    return Boolean.TRUE;
                case 'f':
                    expect("false");
                    return Boolean.FALSE;
                case 'n':
                    expect("null");
                    return null;
                default:
                    return readNumber();
            }
        }

        private Map<String, Object> readObject(int depth) {
            Map<String, Object> map = new TreeMap<>();
            pos++;
            skipWhitespace();
            if (pos < src.length() && src.charAt(pos) == '}') {
                pos++;
                return map;
            }
            while (true) {
                skipWhitespace();
                if (pos >= src.length() || src.charAt(pos) != '"') {
                    throw error("对象键必须是字符串");
                }
                String key = readString();
                skipWhitespace();
                if (pos >= src.length() || src.charAt(pos) != ':') {
                    throw error("对象缺少 ':'");
                }
                pos++;
                skipWhitespace();
                map.put(key, readValue(depth + 1));
                skipWhitespace();
                if (pos >= src.length()) {
                    throw error("对象未闭合");
                }
                char c = src.charAt(pos);
                if (c == ',') {
                    pos++;
                } else if (c == '}') {
                    pos++;
                    return map;
                } else {
                    throw error("对象缺少 ',' 或 '}'");
                }
            }
        }

        private List<Object> readArray(int depth) {
            List<Object> list = new ArrayList<>();
            pos++;
            skipWhitespace();
            if (pos < src.length() && src.charAt(pos) == ']') {
                pos++;
                return list;
            }
            while (true) {
                skipWhitespace();
                list.add(readValue(depth + 1));
                skipWhitespace();
                if (pos >= src.length()) {
                    throw error("数组未闭合");
                }
                char c = src.charAt(pos);
                if (c == ',') {
                    pos++;
                } else if (c == ']') {
                    pos++;
                    return list;
                } else {
                    throw error("数组缺少 ',' 或 ']'");
                }
            }
        }

        private String readString() {
            pos++;
            StringBuilder sb = new StringBuilder();
            while (true) {
                if (pos >= src.length()) {
                    throw error("字符串未闭合");
                }
                char c = src.charAt(pos++);
                if (c == '"') {
                    return sb.toString();
                }
                if (c != '\\') {
                    sb.append(c);
                    continue;
                }
                if (pos >= src.length()) {
                    throw error("转义字符不完整");
                }
                char esc = src.charAt(pos++);
                switch (esc) {
                    case '"':
                        sb.append('"');
                        break;
                    case '\\':
                        sb.append('\\');
                        break;
                    case '/':
                        sb.append('/');
                        break;
                    case 'b':
                        sb.append('\b');
                        break;
                    case 'f':
                        sb.append('\f');
                        break;
                    case 'n':
                        sb.append('\n');
                        break;
                    case 'r':
                        sb.append('\r');
                        break;
                    case 't':
                        sb.append('\t');
                        break;
                    case 'u':
                        if (pos + 4 > src.length()) {
                            throw error("\\u 转义不完整");
                        }
                        String hex = src.substring(pos, pos + 4);
                        try {
                            sb.append((char) Integer.parseInt(hex, 16));
                        } catch (NumberFormatException e) {
                            throw error("\\u 转义不合法: " + hex);
                        }
                        pos += 4;
                        break;
                    default:
                        throw error("未知转义字符: \\" + esc);
                }
            }
        }

        private Object readNumber() {
            int start = pos;
            if (pos < src.length() && (src.charAt(pos) == '-' || src.charAt(pos) == '+')) {
                pos++;
            }
            boolean fractional = false;
            boolean exponent = false;
            while (pos < src.length()) {
                char c = src.charAt(pos);
                if (c >= '0' && c <= '9') {
                    pos++;
                } else if (c == '.' && !fractional && !exponent) {
                    fractional = true;
                    pos++;
                } else if ((c == 'e' || c == 'E') && !exponent) {
                    exponent = true;
                    pos++;
                    if (pos < src.length() && (src.charAt(pos) == '-' || src.charAt(pos) == '+')) {
                        pos++;
                    }
                } else {
                    break;
                }
            }
            if (start == pos) {
                throw error("无法识别的字符: " + src.charAt(pos));
            }
            String text = src.substring(start, pos);
            try {
                if (fractional || exponent) {
                    return Double.parseDouble(text);
                }
                return Long.parseLong(text);
            } catch (NumberFormatException e) {
                throw error("数字格式不合法: " + text);
            }
        }

        private void expect(String literal) {
            if (!src.startsWith(literal, pos)) {
                throw error("缺少字面量 " + literal);
            }
            pos += literal.length();
        }

        private void skipWhitespace() {
            while (pos < src.length()) {
                char c = src.charAt(pos);
                if (c == ' ' || c == '\t' || c == '\n' || c == '\r') {
                    pos++;
                } else {
                    break;
                }
            }
        }

        private IllegalArgumentException error(String message) {
            return new IllegalArgumentException("小组件缓存 JSON 不合法（位置 " + pos + "）: " + message);
        }
    }
}
