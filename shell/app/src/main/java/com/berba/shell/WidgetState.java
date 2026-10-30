package com.berba.shell;

import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * 小组件缓存模型：纯 Java（不依赖 Android），便于 JVM 单测。
 * 解析容错——任何损坏的 JSON 都退回空状态，保证 launcher 渲染不崩。
 */
final class WidgetState {
    static final int MAX_CARDS = 20;
    static final int MAX_ROWS = 5;
    static final int MAX_BYTES = 32 * 1024;
    static final int MAX_TITLE = 24;
    static final int MAX_VALUE = 16;
    static final int MAX_SUBTITLE = 32;
    static final int MAX_HEADER = 32;

    static final class Card {
        final String title;
        final String value;
        final String subtitle;
        final String color;

        private Card(String title, String value, String subtitle, String color) {
            this.title = truncate(title, MAX_TITLE);
            this.value = truncate(value, MAX_VALUE);
            this.subtitle = truncate(subtitle, MAX_SUBTITLE);
            this.color = validColor(color) ? color : "";
        }

        static Card of(String title, String value, String subtitle, String color) {
            return new Card(nullToEmpty(title), nullToEmpty(value), nullToEmpty(subtitle),
                    nullToEmpty(color).trim());
        }
    }

    static final class State {
        final long updatedAt;
        final String title;
        final List<Card> cards;

        private State(long updatedAt, String title, List<Card> cards) {
            this.updatedAt = updatedAt <= 0 ? 0 : updatedAt;
            this.title = truncate(nullToEmpty(title), MAX_HEADER);
            this.cards = Collections.unmodifiableList(new ArrayList<>(cards));
        }

        static State of(long updatedAt, String title, List<Card> cards) {
            List<Card> kept = new ArrayList<>();
            if (cards != null) {
                for (Card card : cards) {
                    if (card == null || kept.size() >= MAX_CARDS) {
                        continue;
                    }
                    kept.add(card);
                }
            }
            return new State(updatedAt, title, kept);
        }

        boolean isEmpty() {
            return updatedAt <= 0 && title.isEmpty() && cards.isEmpty();
        }
    }

    private static final State EMPTY = State.of(0, "", null);

    private WidgetState() {
    }

    static State empty() {
        return EMPTY;
    }

    /** 解析持久化状态；失败返回空状态。 */
    static State parse(String json) {
        if (json == null || json.trim().isEmpty()) {
            return EMPTY;
        }
        try {
            Object root = Json.parse(json);
            if (!(root instanceof Map)) {
                return EMPTY;
            }
            Map<?, ?> obj = (Map<?, ?>) root;
            long updatedAt = asLong(obj.get("updatedAt"));
            String title = asString(obj.get("title"));
            List<Card> cards = new ArrayList<>();
            Object rawCards = obj.get("cards");
            if (rawCards instanceof List) {
                for (Object item : (List<?>) rawCards) {
                    if (cards.size() >= MAX_CARDS) {
                        break;
                    }
                    if (!(item instanceof Map)) {
                        continue;
                    }
                    Map<?, ?> card = (Map<?, ?>) item;
                    cards.add(Card.of(
                            asString(card.get("title")),
                            asString(card.get("value")),
                            asString(card.get("subtitle")),
                            asString(card.get("color"))));
                }
            }
            return State.of(updatedAt, title, cards);
        } catch (RuntimeException e) {
            return EMPTY;
        }
    }

    /** 序列化；键顺序固定，保证同一状态字节一致。 */
    static String toJson(State state) {
        State value = state == null ? EMPTY : state;
        StringBuilder sb = new StringBuilder(128);
        sb.append("{\"updatedAt\":").append(value.updatedAt);
        sb.append(",\"title\":").append(quote(value.title));
        sb.append(",\"cards\":[");
        for (int i = 0; i < value.cards.size(); i++) {
            if (i > 0) {
                sb.append(',');
            }
            Card card = value.cards.get(i);
            sb.append('{');
            sb.append("\"title\":").append(quote(card.title)).append(',');
            sb.append("\"value\":").append(quote(card.value)).append(',');
            sb.append("\"subtitle\":").append(quote(card.subtitle)).append(',');
            sb.append("\"color\":").append(quote(card.color));
            sb.append('}');
        }
        sb.append("]}");
        return sb.toString();
    }

    static String truncate(String text, int max) {
        if (text == null) {
            return "";
        }
        if (max <= 0 || text.length() <= max) {
            return text;
        }
        int end = max - 1;
        if (end > 0 && Character.isHighSurrogate(text.charAt(end - 1))) {
            end--;
        }
        return text.substring(0, end) + "…";
    }

    /** 只接受 #RRGGBB 或 #AARRGGBB。 */
    static boolean validColor(String color) {
        if (color == null) {
            return false;
        }
        String s = color.trim();
        if (s.length() != 7 && s.length() != 9) {
            return false;
        }
        if (s.charAt(0) != '#') {
            return false;
        }
        for (int i = 1; i < s.length(); i++) {
            if (Character.digit(s.charAt(i), 16) < 0) {
                return false;
            }
        }
        return true;
    }

    private static long asLong(Object value) {
        if (value instanceof Number) {
            return ((Number) value).longValue();
        }
        return 0;
    }

    private static String asString(Object value) {
        return value instanceof String ? (String) value : "";
    }

    private static String nullToEmpty(String value) {
        return value == null ? "" : value;
    }

    private static String quote(String text) {
        StringBuilder sb = new StringBuilder(text.length() + 2);
        sb.append('"');
        for (int i = 0; i < text.length(); i++) {
            char c = text.charAt(i);
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
                default:
                    if (c < 0x20) {
                        sb.append("\\u");
                        String hex = Integer.toHexString(c);
                        for (int p = hex.length(); p < 4; p++) {
                            sb.append('0');
                        }
                        sb.append(hex);
                    } else {
                        sb.append(c);
                    }
            }
        }
        sb.append('"');
        return sb.toString();
    }

    /** 极简 JSON 解析器：只支持状态文件需要的结构，避免依赖 org.json（JVM 单测不可用）。 */
    static final class Json {
        private final String text;
        private int pos;

        private Json(String text) {
            this.text = text;
        }

        static Object parse(String text) {
            Json parser = new Json(text);
            Object value = parser.value();
            parser.skipSpace();
            if (parser.pos != text.length()) {
                throw new IllegalArgumentException("JSON 末尾存在多余内容");
            }
            return value;
        }

        private Object value() {
            skipSpace();
            if (pos >= text.length()) {
                throw new IllegalArgumentException("JSON 意外结束");
            }
            char c = text.charAt(pos);
            switch (c) {
                case '{':
                    return object();
                case '[':
                    return array();
                case '"':
                    return string();
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
                    return number();
            }
        }

        private Map<String, Object> object() {
            Map<String, Object> map = new LinkedHashMap<>();
            pos++; // {
            skipSpace();
            if (peek() == '}') {
                pos++;
                return map;
            }
            while (true) {
                skipSpace();
                if (peek() != '"') {
                    throw new IllegalArgumentException("对象键必须是字符串");
                }
                String key = string();
                skipSpace();
                if (peek() != ':') {
                    throw new IllegalArgumentException("对象缺少冒号");
                }
                pos++;
                map.put(key, value());
                skipSpace();
                char c = peek();
                pos++;
                if (c == '}') {
                    return map;
                }
                if (c != ',') {
                    throw new IllegalArgumentException("对象分隔符非法");
                }
            }
        }

        private List<Object> array() {
            List<Object> list = new ArrayList<>();
            pos++; // [
            skipSpace();
            if (peek() == ']') {
                pos++;
                return list;
            }
            while (true) {
                list.add(value());
                skipSpace();
                char c = peek();
                pos++;
                if (c == ']') {
                    return list;
                }
                if (c != ',') {
                    throw new IllegalArgumentException("数组分隔符非法");
                }
            }
        }

        private String string() {
            pos++; // "
            StringBuilder sb = new StringBuilder();
            while (true) {
                if (pos >= text.length()) {
                    throw new IllegalArgumentException("字符串未闭合");
                }
                char c = text.charAt(pos++);
                if (c == '"') {
                    return sb.toString();
                }
                if (c != '\\') {
                    sb.append(c);
                    continue;
                }
                if (pos >= text.length()) {
                    throw new IllegalArgumentException("转义序列未完成");
                }
                char esc = text.charAt(pos++);
                switch (esc) {
                    case '"':
                    case '\\':
                    case '/':
                        sb.append(esc);
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
                        if (pos + 4 > text.length()) {
                            throw new IllegalArgumentException("Unicode 转义不完整");
                        }
                        sb.append((char) Integer.parseInt(text.substring(pos, pos + 4), 16));
                        pos += 4;
                        break;
                    default:
                        throw new IllegalArgumentException("未知转义: \\" + esc);
                }
            }
        }

        private Double number() {
            int start = pos;
            while (pos < text.length()) {
                char c = text.charAt(pos);
                if ((c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.'
                        || c == 'e' || c == 'E') {
                    pos++;
                } else {
                    break;
                }
            }
            if (start == pos) {
                throw new IllegalArgumentException("无法识别的 JSON 值");
            }
            return Double.valueOf(text.substring(start, pos));
        }

        private void expect(String word) {
            if (!text.startsWith(word, pos)) {
                throw new IllegalArgumentException("无法识别的 JSON 字面量");
            }
            pos += word.length();
        }

        private char peek() {
            if (pos >= text.length()) {
                throw new IllegalArgumentException("JSON 意外结束");
            }
            return text.charAt(pos);
        }

        private void skipSpace() {
            while (pos < text.length()) {
                char c = text.charAt(pos);
                if (c == ' ' || c == '\n' || c == '\r' || c == '\t') {
                    pos++;
                } else {
                    return;
                }
            }
        }
    }
}
