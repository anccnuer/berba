package com.berba.shell;

/** WidgetState 的 JVM 单测，由 build-shell 脚本调用。 */
public final class WidgetStateTest {
    private WidgetStateTest() {
    }

    public static void main(String[] args) {
        testEmpty();
        testParseInvalid();
        testLenientFallback();
        testTruncate();
        testParsedTruncation();
        testCardsOverRowCount();
        testColorFormat();
        testColorFromJson();
        testCardLimit();
        testByteLimit();
        testRoundTrip();
        testEscapes();
        System.out.println("WidgetState OK");
    }

    private static void testEmpty() {
        WidgetState state = WidgetState.parse("{}");
        check(state.cards.isEmpty(), "空对象应没有卡片");
        check(state.updatedAt == 0, "updatedAt 默认为 0");
        check(state.title.isEmpty(), "title 默认为空");
        check(WidgetState.parse("{\"title\":\"概览\"}").title.equals("概览"), "title 应被解析");
    }

    private static void testParseInvalid() {
        expectReject("");
        expectReject("   ");
        expectReject("{");
        expectReject("{\"updatedAt\":");
        expectReject("[1,2]");
        expectReject("{\"cards\":{}}");
        expectReject("{\"cards\":[1]}");
        expectReject("{\"cards\":[{\"value\":1}]}");
        expectReject("{\"cards\":[{\"title\":}]}");
        expectReject("{\"updatedAt\":\"12\"}");
    }

    private static void expectReject(String json) {
        try {
            WidgetState.parse(json);
        } catch (IllegalArgumentException e) {
            return;
        }
        throw new AssertionError("非法 JSON 应被拒绝: " + json);
    }

    private static void testLenientFallback() {
        check(WidgetState.parseLenient(null).cards.isEmpty(), "null 缓存应回退为空");
        check(WidgetState.parseLenient("not json").cards.isEmpty(), "损坏缓存应回退为空");
        check(WidgetState.parseLenient("{\"cards\":[1]}").cards.isEmpty(), "结构错误应回退为空");
    }

    private static void testTruncate() {
        check(WidgetState.truncate(null, 4).isEmpty(), "null 截断为空");
        check(WidgetState.truncate("abc", 4).equals("abc"), "未超长不截断");
        check(WidgetState.truncate("abcdef", 4).equals("abc…"), "超长补省略号");
        check(WidgetState.truncate("abcdef", 1).equals("…"), "max=1 只留省略号");
    }

    private static void testParsedTruncation() {
        StringBuilder sb = new StringBuilder();
        for (int i = 0; i < 40; i++) {
            sb.append('x');
        }
        WidgetState state = WidgetState.parse("{\"title\":\"" + sb + "\","
                + "\"cards\":[{\"title\":\"" + sb + "\",\"value\":\"" + sb
                + "\",\"subtitle\":\"" + sb + "\"}]}");
        check(state.title.length() == WidgetState.TITLE_MAX, "title 长度 = " + state.title.length());
        WidgetState.Card card = state.cards.get(0);
        check(card.title.length() == WidgetState.TITLE_MAX, "卡片 title 长度");
        check(card.value.length() == WidgetState.VALUE_MAX, "卡片 value 长度");
        check(card.subtitle.length() == WidgetState.SUBTITLE_MAX, "卡片 subtitle 长度");
        check(card.title.endsWith("…"), "截断后带省略号");
    }

    private static void testCardsOverRowCount() {
        WidgetState state = WidgetState.parse("{\"cards\":["
                + "{\"value\":\"a\"},{\"value\":\"b\"},{\"value\":\"c\"},"
                + "{\"value\":\"d\"},{\"value\":\"e\"},{\"value\":\"f\"},{\"value\":\"g\"}]}");
        check(state.cards.size() == 7, "应保留全部 7 张卡片");
        check(state.visibleCards().size() == WidgetState.ROW_COUNT, "只渲染 5 行");
        check(state.hiddenCardCount() == 2, "应报告 2 张被截断");
        check(state.visibleCards().get(0).value.equals("a"), "首行是第一张卡片");
    }

    private static void testColorFormat() {
        check(WidgetState.parseColor("#FF8FB1") == 0xFF8FB1, "#RRGGBB");
        check(WidgetState.parseColor("#80FF8FB1") == 0x80FF8FB1, "#AARRGGBB");
        check(WidgetState.parseColor(" #FF8FB1 ") == 0xFF8FB1, "容忍首尾空白");
        check(WidgetState.parseColor("#ff8fb1") == 0xff8fb1, "小写十六进制");
        check(WidgetState.parseColor("red") == WidgetState.NO_COLOR, "颜色名非法");
        check(WidgetState.parseColor("#12345") == WidgetState.NO_COLOR, "长度不足");
        check(WidgetState.parseColor("#123456789") == WidgetState.NO_COLOR, "长度过长");
        check(WidgetState.parseColor("#GGGGGG") == WidgetState.NO_COLOR, "非十六进制字符");
        check(WidgetState.parseColor("FF8FB1") == WidgetState.NO_COLOR, "缺少 #");
        check(WidgetState.parseColor(null) == WidgetState.NO_COLOR, "null 非法");
    }

    private static void testColorFromJson() {
        WidgetState state = WidgetState.parse("{\"cards\":[{\"value\":\"a\",\"color\":\"#FF8FB1\"},"
                + "{\"value\":\"b\",\"color\":\"red\"},{\"value\":\"c\"}]}");
        check(state.cards.get(0).color == 0xFF8FB1, "合法颜色被解析");
        check(state.cards.get(1).color == WidgetState.NO_COLOR, "非法颜色回退主题色");
        check(state.cards.get(2).color == WidgetState.NO_COLOR, "缺省颜色为主题色");
    }

    private static void testCardLimit() {
        WidgetState state = stateWithCards(WidgetState.MAX_CARDS);
        state.checkWritable();
        state = stateWithCards(WidgetState.MAX_CARDS + 1);
        try {
            state.checkWritable();
        } catch (IllegalArgumentException e) {
            check(e.getMessage().contains("20"), "错误消息应说明上限: " + e.getMessage());
            return;
        }
        throw new AssertionError("超过卡片上限应报错");
    }

    private static void testByteLimit() {
        WidgetState state = WidgetState.empty();
        for (int i = 0; i < WidgetState.MAX_CARDS; i++) {
            state.cards.add(new WidgetState.Card(repeat('x', 4000), repeat('y', 4000),
                    repeat('z', 4000), WidgetState.NO_COLOR));
        }
        check(state.byteSize() > WidgetState.MAX_BYTES, "构造出的状态应超过 32KB");
        try {
            state.checkWritable();
        } catch (IllegalArgumentException e) {
            check(e.getMessage().contains("32KB"), "错误消息应说明 32KB: " + e.getMessage());
            return;
        }
        throw new AssertionError("超过 32KB 应报错");
    }

    private static void testRoundTrip() {
        WidgetState original = WidgetState.parse("{\"updatedAt\":1761800000000,\"title\":\"今日概览\","
                + "\"cards\":[{\"title\":\"天气\",\"value\":\"26°\",\"subtitle\":\"多云\","
                + "\"color\":\"#3DDC84\"},{\"title\":\"步数\",\"value\":\"8,421\"}]}");
        WidgetState parsed = WidgetState.parse(original.toJson());
        check(parsed.updatedAt == 1761800000000L, "updatedAt 往返一致");
        check(parsed.title.equals("今日概览"), "title 往返一致");
        check(parsed.cards.size() == 2, "卡片数量往返一致");
        check(parsed.cards.get(0).value.equals("26°"), "value 往返一致");
        check(parsed.cards.get(0).color == 0x3DDC84, "color 往返一致");
        check(parsed.cards.get(1).color == WidgetState.NO_COLOR, "缺省颜色保持为主题色");
        check(original.byteSize() < WidgetState.MAX_BYTES, "常规状态远小于上限");
    }

    private static void testEscapes() {
        WidgetState state = WidgetState.parse("{\"title\":\"a\\\"b\\\\c\\nd\",\"cards\":[]}");
        check(state.title.equals("a\"b\\c\nd"), "转义字符应被还原: " + state.title);
        WidgetState back = WidgetState.parse(state.toJson());
        check(back.title.equals(state.title), "转义往返一致");
    }

    private static WidgetState stateWithCards(int count) {
        WidgetState state = WidgetState.empty();
        for (int i = 0; i < count; i++) {
            state.cards.add(new WidgetState.Card("t", "v", "s", WidgetState.NO_COLOR));
        }
        return state;
    }

    private static String repeat(char c, int count) {
        StringBuilder sb = new StringBuilder(count);
        for (int i = 0; i < count; i++) {
            sb.append(c);
        }
        return sb.toString();
    }

    private static void check(boolean ok, String message) {
        if (!ok) {
            throw new AssertionError(message);
        }
    }
}
