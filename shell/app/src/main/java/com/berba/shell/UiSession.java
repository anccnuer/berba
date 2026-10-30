package com.berba.shell;

import java.util.concurrent.atomic.AtomicLong;

/** 一次 Activity（界面）会话；epoch 用于丢弃旋转/重建后的过期回调。 */
final class UiSession {
    private static final AtomicLong EPOCHS = new AtomicLong(1);

    private final RuntimeManager manager;
    private final MainActivity activity;
    private final long epoch;

    UiSession(RuntimeManager manager, MainActivity activity) {
        this.manager = manager;
        this.activity = activity;
        this.epoch = EPOCHS.getAndIncrement();
    }

    long epoch() {
        return epoch;
    }

    MainActivity activity() {
        return activity;
    }

    LuaRuntime runtime() {
        return manager.runtime();
    }

    boolean isCurrent() {
        return manager.isCurrent(this);
    }
}
