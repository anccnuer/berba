package com.berba.shell;

import android.content.pm.ApplicationInfo;
import android.util.Log;

import org.json.JSONObject;

import java.io.File;
import java.util.ArrayList;
import java.util.Iterator;
import java.util.List;
import java.util.Map;

/** 从 APK 的 nativeLibraryDir 逐个加载插件；单个插件失败不影响其他插件。 */
final class NativePluginLoader {
    private static final String TAG = "berba";
    static final int ABI_VERSION = 1;

    static final class Loaded {
        final CapabilityConfig.NativePluginInfo info;
        final long handle;
        final String error;

        Loaded(CapabilityConfig.NativePluginInfo info, long handle, String error) {
            this.info = info;
            this.handle = handle;
            this.error = error;
        }

        boolean ok() {
            return error == null;
        }
    }

    private NativePluginLoader() {
    }

    static List<Loaded> load(LuaRuntime rt) {
        List<Loaded> out = new ArrayList<>();
        List<CapabilityConfig.NativePluginInfo> plugins = rt.capabilities().nativePlugins();
        if (plugins.isEmpty()) {
            return out;
        }
        int abiVersion = rt.capabilities().nativeAbiVersion();
        if (abiVersion != ABI_VERSION) {
            String error = "壳原生插件 ABI 版本不兼容: " + abiVersion;
            Log.e(TAG, error);
            for (CapabilityConfig.NativePluginInfo info : plugins) {
                out.add(new Loaded(info, 0, error));
            }
            return out;
        }
        try {
            NativePluginBridge.ensureLoaded();
        } catch (Throwable t) {
            String error = LuaRuntime.message(t);
            Log.e(TAG, "加载 JNI shim 失败", t);
            for (CapabilityConfig.NativePluginInfo info : plugins) {
                out.add(new Loaded(info, 0, error));
            }
            return out;
        }

        ApplicationInfo app = rt.context().getApplicationInfo();
        String dir = app == null ? null : app.nativeLibraryDir;
        NativePluginBridge bridge = new NativePluginBridge(rt);
        for (CapabilityConfig.NativePluginInfo info : plugins) {
            if (dir == null || dir.isEmpty()) {
                out.add(new Loaded(info, 0, "系统未解压原生库（nativeLibraryDir 为空）"));
                continue;
            }
            if (!isPlainName(info.libName)) {
                out.add(new Loaded(info, 0, "插件库名非法: " + info.libName));
                continue;
            }
            File lib = new File(dir, info.libName);
            if (!lib.isFile()) {
                out.add(new Loaded(info, 0, "找不到插件库: " + lib.getAbsolutePath()));
                continue;
            }
            try {
                long handle = bridge.load(info.name, lib.getAbsolutePath(), info.config);
                verifyDescribe(bridge, info, handle);
                out.add(new Loaded(info, handle, null));
            } catch (Throwable t) {
                Log.e(TAG, "插件加载失败: " + info.name, t);
                out.add(new Loaded(info, 0, LuaRuntime.message(t)));
            }
        }
        return out;
    }

    private static boolean isPlainName(String name) {
        return name != null && !name.isEmpty()
                && name.indexOf('/') < 0 && name.indexOf('\\') < 0
                && name.indexOf('\0') < 0 && !name.equals(".") && !name.equals("..");
    }

    /** describe 与构建元数据不一致只告警：以构建元数据为准。 */
    private static void verifyDescribe(NativePluginBridge bridge,
                                       CapabilityConfig.NativePluginInfo info, long handle) {
        String text;
        try {
            text = bridge.describe(handle);
        } catch (Throwable t) {
            Log.w(TAG, "插件 describe 失败: " + info.name, t);
            return;
        }
        if (text == null || text.trim().isEmpty()) {
            return;
        }
        try {
            JSONObject json = new JSONObject(text);
            String name = json.optString("name", "");
            if (!name.isEmpty() && !name.equals(info.name)) {
                Log.w(TAG, "插件名称与构建元数据不一致: " + name + " != " + info.name);
            }
            JSONObject modules = json.optJSONObject("modules");
            if (modules == null) {
                return;
            }
            for (Map.Entry<String, CapabilityConfig.NativeModuleInfo> entry
                    : info.modules.entrySet()) {
                Object mod = modules.opt(entry.getKey());
                if (mod == null) {
                    Log.w(TAG, "插件 describe 缺少模块: " + info.name + "." + entry.getKey());
                    continue;
                }
                for (String fn : entry.getValue().functions.keySet()) {
                    if (!describesFunction(mod, fn)) {
                        Log.w(TAG, "插件 describe 缺少函数: " + info.name + "."
                                + entry.getKey() + "." + fn);
                    }
                }
            }
            Iterator<String> it = modules.keys();
            while (it.hasNext()) {
                String mod = it.next();
                if (!info.modules.containsKey(mod)) {
                    Log.w(TAG, "插件 describe 多出模块（不会暴露）: " + info.name + "." + mod);
                }
            }
        } catch (Exception e) {
            Log.w(TAG, "插件 describe 不是合法 JSON: " + info.name, e);
        }
    }

    /** describe 的模块可以是函数对象或函数名数组。 */
    private static boolean describesFunction(Object module, String fn) {
        if (module instanceof JSONObject) {
            return ((JSONObject) module).has(fn);
        }
        if (module instanceof org.json.JSONArray) {
            org.json.JSONArray list = (org.json.JSONArray) module;
            for (int i = 0; i < list.length(); i++) {
                if (fn.equals(list.optString(i, ""))) {
                    return true;
                }
            }
        }
        return false;
    }
}
