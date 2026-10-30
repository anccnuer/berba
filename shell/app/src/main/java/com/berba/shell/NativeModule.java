package com.berba.shell;

import android.util.Log;

import org.luaj.vm2.Globals;
import org.luaj.vm2.LuaError;
import org.luaj.vm2.LuaTable;
import org.luaj.vm2.LuaValue;
import org.luaj.vm2.Varargs;
import org.luaj.vm2.lib.VarArgFunction;

import java.nio.charset.StandardCharsets;
import java.util.List;

/**
 * 安装 native.list 与 native.&lt;module&gt;.&lt;function&gt;。
 * 插件失败时只为该插件的模块安装抛错桩，其他插件继续可用。
 */
final class NativeModule {
    private static final String TAG = "berba";

    private final NativePluginBridge bridge;
    private final List<NativePluginLoader.Loaded> plugins;

    private NativeModule(NativePluginBridge bridge, List<NativePluginLoader.Loaded> plugins) {
        this.bridge = bridge;
        this.plugins = plugins;
    }

    static void install(LuaRuntime rt, Globals g) {
        NativePluginBridge bridge = new NativePluginBridge(rt);
        List<NativePluginLoader.Loaded> loaded = NativePluginLoader.load(rt);
        NativeModule module = new NativeModule(bridge, loaded);

        LuaTable nativeTable = new LuaTable();
        nativeTable.set("list", LuaApis.fn(args -> module.list()));
        for (NativePluginLoader.Loaded plugin : loaded) {
            for (CapabilityConfig.NativeModuleInfo info : plugin.info.modules.values()) {
                if (!nativeTable.get(info.name).isnil()) {
                    Log.e(TAG, "原生模块名冲突，已忽略: " + plugin.info.name + "." + info.name);
                    continue;
                }
                nativeTable.set(info.name, module.moduleTable(plugin, info));
            }
        }
        g.set("native", nativeTable);
    }

    private LuaTable moduleTable(NativePluginLoader.Loaded plugin,
                                 CapabilityConfig.NativeModuleInfo info) {
        LuaTable table = new LuaTable();
        for (CapabilityConfig.NativeFunction fn : info.functions.values()) {
            if (plugin.ok()) {
                table.set(fn.name, function(plugin, info.name, fn));
            } else {
                table.set(fn.name, stub(plugin));
            }
        }
        return table;
    }

    private LuaValue function(final NativePluginLoader.Loaded plugin, final String module,
                              final CapabilityConfig.NativeFunction fn) {
        return LuaApis.fn(args -> {
            if (fn.async) {
                LuaValue callback = args.arg(2);
                if (!callback.isfunction()) {
                    throw new LuaError("异步原生函数 " + module + "." + fn.name + " 需要回调");
                }
                final String argsJson = LuaConvert.toJson(args.arg(1));
                try {
                    bridge.callAsync(plugin.handle, module, fn.name, argsJson,
                            (data, error) -> {
                                if (error != null) {
                                    callback.invoke(LuaValue.NIL, LuaValue.valueOf(error));
                                } else {
                                    callback.invoke(decode(data));
                                }
                            });
                } catch (Throwable t) {
                    return LuaApis.nilError(t);
                }
                return LuaValue.NIL;
            }
            String argsJson = LuaConvert.toJson(args.arg(1));
            try {
                return decode(bridge.call(plugin.handle, module, fn.name, argsJson));
            } catch (Throwable t) {
                return LuaApis.nilError(t);
            }
        });
    }

    private static LuaValue stub(final NativePluginLoader.Loaded plugin) {
        return new VarArgFunction() {
            @Override
            public Varargs invoke(Varargs args) {
                throw new LuaError("原生插件 " + plugin.info.name + " 加载失败: " + plugin.error);
            }
        };
    }

    /** 插件结果按 UTF-8 JSON 解析；不是合法 JSON 时按字符串返回。 */
    private static LuaValue decode(byte[] data) {
        if (data == null || data.length == 0) {
            return LuaValue.NIL;
        }
        String text = new String(data, StandardCharsets.UTF_8);
        try {
            return LuaConvert.parse(text);
        } catch (Exception e) {
            return LuaValue.valueOf(text);
        }
    }

    private LuaTable list() {
        LuaTable out = new LuaTable();
        int index = 1;
        for (NativePluginLoader.Loaded plugin : plugins) {
            LuaTable entry = new LuaTable();
            entry.set("name", plugin.info.name);
            entry.set("lib", plugin.info.libName);
            entry.set("ok", plugin.ok() ? LuaValue.TRUE : LuaValue.FALSE);
            if (!plugin.ok()) {
                entry.set("error", plugin.error);
            }
            LuaTable modules = new LuaTable();
            for (CapabilityConfig.NativeModuleInfo info : plugin.info.modules.values()) {
                LuaTable mod = new LuaTable();
                LuaTable functions = new LuaTable();
                for (CapabilityConfig.NativeFunction fn : info.functions.values()) {
                    LuaTable f = new LuaTable();
                    f.set("async", fn.async ? LuaValue.TRUE : LuaValue.FALSE);
                    functions.set(fn.name, f);
                }
                mod.set("functions", functions);
                modules.set(info.name, mod);
            }
            entry.set("modules", modules);
            out.set(index++, entry);
        }
        return out;
    }
}
