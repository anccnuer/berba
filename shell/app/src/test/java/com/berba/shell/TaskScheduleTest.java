package com.berba.shell;

import java.util.Calendar;
import java.util.TimeZone;

/** TaskSchedule 的 JVM 单测，由 build-shell 脚本调用。 */
public final class TaskScheduleTest {
    private TaskScheduleTest() {
    }

    public static void main(String[] args) {
        TimeZone original = TimeZone.getDefault();
        try {
            testInterval();
            testDelay();
            testDaily();
            testDailyAcrossSpringForward();
            testDailyAcrossFallBack();
            testLate();
            testOverflow();
            testInvalidDaily();
        } finally {
            TimeZone.setDefault(original);
        }
        System.out.println("TaskSchedule OK");
    }

    private static void testInterval() {
        TimeZone.setDefault(TimeZone.getTimeZone("Asia/Shanghai"));
        long now = at("Asia/Shanghai", 2026, 1, 15, 9, 30);
        TaskRecord record = record(TaskRecord.KIND_EVERY, 900, "", 0);
        check(TaskSchedule.nextRun(record, now) == now + 900_000L, "every 应推进 900 秒");
    }

    private static void testDelay() {
        long now = at("UTC", 2026, 1, 15, 9, 30);
        TaskRecord record = record(TaskRecord.KIND_DELAY, 0, "", 60);
        check(TaskSchedule.nextRun(record, now) == now + 60_000L, "delay 应推进 60 秒");
    }

    private static void testDaily() {
        String tz = "Asia/Shanghai";
        TaskRecord record = record(TaskRecord.KIND_DAILY, 0, "08:30", 0);
        long before = at(tz, 2026, 5, 20, 7, 0);
        long next = TaskSchedule.nextRun(record, before);
        check(field(next, tz, Calendar.DAY_OF_MONTH) == 20, "当天 08:30 前应安排当天");
        check(field(next, tz, Calendar.HOUR_OF_DAY) == 8, "daily 小时");
        check(field(next, tz, Calendar.MINUTE) == 30, "daily 分钟");

        long after = at(tz, 2026, 5, 20, 9, 0);
        long tomorrow = TaskSchedule.nextRun(record, after);
        check(field(tomorrow, tz, Calendar.DAY_OF_MONTH) == 21, "当天 08:30 后应安排次日");

        long exact = at(tz, 2026, 5, 20, 8, 30);
        check(field(TaskSchedule.nextRun(record, exact), tz, Calendar.DAY_OF_MONTH) == 21,
                "恰好在 HH:MM 时应安排次日");
    }

    private static void testDailyAcrossSpringForward() {
        String tz = "America/New_York";
        TimeZone.setDefault(TimeZone.getTimeZone(tz));
        TaskRecord record = record(TaskRecord.KIND_DAILY, 0, "08:30", 0);
        long before = at(tz, 2026, 3, 8, 0, 30);
        long next = TaskSchedule.nextRun(record, before);
        check(field(next, tz, Calendar.DAY_OF_MONTH) == 8, "DST 当天应安排当天 08:30");
        check(field(next, tz, Calendar.HOUR_OF_DAY) == 8, "DST 后本地小时仍为 8");
        check(field(next, tz, Calendar.MINUTE) == 30, "DST 后本地分钟仍为 30");
        int offset = field(next, tz, Calendar.ZONE_OFFSET) + field(next, tz, Calendar.DST_OFFSET);
        check(offset == -4 * 3600 * 1000, "春季 DST 后应为 EDT(-4h)");
        check(next - before == 7 * 3600 * 1000L, "跨春季 DST 实际间隔应为 7 小时");
    }

    private static void testDailyAcrossFallBack() {
        String tz = "America/New_York";
        TimeZone.setDefault(TimeZone.getTimeZone(tz));
        TaskRecord record = record(TaskRecord.KIND_DAILY, 0, "08:30", 0);
        long before = at(tz, 2026, 11, 1, 0, 30);
        long next = TaskSchedule.nextRun(record, before);
        check(field(next, tz, Calendar.DAY_OF_MONTH) == 1, "秋季 DST 当天应安排当天 08:30");
        int offset = field(next, tz, Calendar.ZONE_OFFSET) + field(next, tz, Calendar.DST_OFFSET);
        check(offset == -5 * 3600 * 1000, "秋季 DST 后应为 EST(-5h)");
    }

    private static void testLate() {
        long scheduled = 1_000_000_000L;
        check(!TaskSchedule.isLate(scheduled, scheduled), "准点不应标记 late");
        check(!TaskSchedule.isLate(scheduled, scheduled + TaskSchedule.LATE_THRESHOLD_MS),
                "阈值边界不应标记 late");
        check(TaskSchedule.isLate(scheduled, scheduled + TaskSchedule.LATE_THRESHOLD_MS + 1),
                "超过阈值应标记 late");
    }

    private static void testOverflow() {
        TaskRecord record = record(TaskRecord.KIND_EVERY, TaskSchedule.MAX_SPAN_SEC, "", 0);
        long next = TaskSchedule.nextRun(record, Long.MAX_VALUE - 1000);
        check(next == Long.MAX_VALUE, "时间溢出应饱和到 Long.MAX_VALUE");
    }

    private static void testInvalidDaily() {
        check(!TaskSchedule.validDaily("24:00"), "24:00 应非法");
        check(!TaskSchedule.validDaily("8:30"), "8:30 应非法");
        check(!TaskSchedule.validDaily("08:60"), "08:60 应非法");
        check(TaskSchedule.validDaily("00:00") && TaskSchedule.validDaily("23:59"),
                "00:00 与 23:59 应合法");
        boolean threw = false;
        try {
            TaskSchedule.nextDaily("9:00", System.currentTimeMillis());
        } catch (IllegalArgumentException e) {
            threw = true;
        }
        check(threw, "非法 daily 应抛 IllegalArgumentException");
    }

    private static TaskRecord record(int kind, long every, String daily, long delay) {
        return new TaskRecord("t", 1, kind, every, daily, delay, "null", false, 60,
                0, 0, 1, 0, false);
    }

    private static long at(String tz, int year, int month, int day, int hour, int minute) {
        Calendar c = Calendar.getInstance(TimeZone.getTimeZone(tz));
        c.clear();
        c.set(year, month - 1, day, hour, minute, 0);
        return c.getTimeInMillis();
    }

    private static int field(long millis, String tz, int field) {
        Calendar c = Calendar.getInstance(TimeZone.getTimeZone(tz));
        c.setTimeInMillis(millis);
        return c.get(field);
    }

    private static void check(boolean ok, String message) {
        if (!ok) {
            throw new AssertionError(message);
        }
    }
}
