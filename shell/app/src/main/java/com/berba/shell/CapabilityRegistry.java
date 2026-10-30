package com.berba.shell;

import org.luaj.vm2.Globals;

/**
 * 由 go generate ./internal/capability 生成，请勿手动修改。
 * 规格声明了 installer 但实现类缺失时，编译会直接失败。
 */
final class CapabilityRegistry {
    static final int SCHEMA = 1;

    private CapabilityRegistry() {
    }

    static boolean isImplemented(String capability) {
        if (capability == null) {
            return false;
        }
        switch (capability) {
            case "native":
            case "notify":
            case "task":
            case "widget":
                return true;
            default:
                return false;
        }
    }

    static void install(LuaRuntime rt, Globals g, CapabilityConfig cfg) {
        LuaApis.install(rt, g); // core

        if (cfg.hasCapability("native")) {
            NativeModule.install(rt, g); // 能力 native
        }
        if (cfg.hasCapability("notify")) {
            NotifyModule.install(rt, g); // 能力 notify
        }
        if (cfg.hasCapability("task")) {
            TaskModule.install(rt, g); // 能力 task
        }
        if (cfg.hasCapability("widget")) {
            WidgetModule.install(rt, g); // 能力 widget
        }
    }
}
