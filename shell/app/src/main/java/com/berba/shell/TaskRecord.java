package com.berba.shell;

/** 一条持久化任务定义；nextRun/runId 在每次触发前推进。 */
final class TaskRecord {
    static final int KIND_EVERY = 1;
    static final int KIND_DAILY = 2;
    static final int KIND_DELAY = 3;

    final String name;
    final int jobId;
    final int kind;
    final long everySec;
    final String daily;
    final long delaySec;
    final String dataJson;
    final boolean exact;
    final int timeoutSec;
    final long createdAt;
    long nextRun;
    long runId;
    long lastFiredAt;
    boolean done;

    TaskRecord(String name, int jobId, int kind, long everySec, String daily, long delaySec,
               String dataJson, boolean exact, int timeoutSec, long createdAt,
               long nextRun, long runId, long lastFiredAt, boolean done) {
        this.name = name;
        this.jobId = jobId;
        this.kind = kind;
        this.everySec = everySec;
        this.daily = daily;
        this.delaySec = delaySec;
        this.dataJson = dataJson == null ? "null" : dataJson;
        this.exact = exact;
        this.timeoutSec = timeoutSec;
        this.createdAt = createdAt;
        this.nextRun = nextRun;
        this.runId = runId;
        this.lastFiredAt = lastFiredAt;
        this.done = done;
    }

    TaskRecord copy() {
        return new TaskRecord(name, jobId, kind, everySec, daily, delaySec, dataJson,
                exact, timeoutSec, createdAt, nextRun, runId, lastFiredAt, done);
    }

    String kindName() {
        switch (kind) {
            case KIND_EVERY:
                return "every";
            case KIND_DAILY:
                return "daily";
            default:
                return "delay";
        }
    }
}
