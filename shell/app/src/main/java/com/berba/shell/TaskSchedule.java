package com.berba.shell;

import java.util.Calendar;
import java.util.TimeZone;

/**
 * 任务时间计算。仅使用 minSdk 24 可用的 Calendar/TimeZone，不依赖 java.time。
 * 所有时间均为 epoch 毫秒，返回 Long.MAX_VALUE 表示饱和。
 */
final class TaskSchedule {
    static final long MIN_EVERY_SEC = 900;
    static final long MAX_SPAN_SEC = 30L * 24 * 3600;
    static final int MIN_TIMEOUT_SEC = 10;
    static final int MAX_TIMEOUT_SEC = 600;
    static final long LATE_THRESHOLD_MS = 60 * 1000;

    private TaskSchedule() {
    }

    static boolean validDaily(String value) {
        if (value == null || value.length() != 5 || value.charAt(2) != ':') {
            return false;
        }
        int hour = twoDigits(value, 0);
        int minute = twoDigits(value, 3);
        return hour >= 0 && hour <= 23 && minute >= 0 && minute <= 59;
    }

    private static int twoDigits(String value, int offset) {
        char a = value.charAt(offset);
        char b = value.charAt(offset + 1);
        if (a < '0' || a > '9' || b < '0' || b > '9') {
            return -1;
        }
        return (a - '0') * 10 + (b - '0');
    }

    /** 根据任务类型计算 fromMillis 之后的下一次触发时间。 */
    static long nextRun(TaskRecord record, long fromMillis) {
        switch (record.kind) {
            case TaskRecord.KIND_EVERY:
                return addSaturated(fromMillis, record.everySec * 1000L);
            case TaskRecord.KIND_DAILY:
                return nextDaily(record.daily, fromMillis);
            case TaskRecord.KIND_DELAY:
                return addSaturated(fromMillis, record.delaySec * 1000L);
            default:
                throw new IllegalArgumentException("未知任务类型: " + record.kind);
        }
    }

    /** 下一次本地 HH:MM，严格晚于 fromMillis；跨 DST 时按日历日推进。 */
    static long nextDaily(String hhmm, long fromMillis) {
        if (!validDaily(hhmm)) {
            throw new IllegalArgumentException("daily 必须是 HH:MM");
        }
        int hour = twoDigits(hhmm, 0);
        int minute = twoDigits(hhmm, 3);
        Calendar c = Calendar.getInstance(TimeZone.getDefault());
        c.setTimeInMillis(fromMillis);
        c.set(Calendar.HOUR_OF_DAY, hour);
        c.set(Calendar.MINUTE, minute);
        c.set(Calendar.SECOND, 0);
        c.set(Calendar.MILLISECOND, 0);
        long candidate = c.getTimeInMillis();
        if (candidate <= fromMillis) {
            c.add(Calendar.DAY_OF_MONTH, 1);
            candidate = c.getTimeInMillis();
        }
        return candidate;
    }

    static boolean isLate(long scheduledAt, long firedAt) {
        return firedAt - scheduledAt > LATE_THRESHOLD_MS;
    }

    private static long addSaturated(long base, long delta) {
        if (delta <= 0) {
            return base;
        }
        if (base > Long.MAX_VALUE - delta) {
            return Long.MAX_VALUE;
        }
        return base + delta;
    }
}
