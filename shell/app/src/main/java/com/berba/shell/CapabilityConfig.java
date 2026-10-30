package com.berba.shell;

import android.content.Context;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.ByteArrayOutputStream;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.TreeSet;

/**
 * assets/berba_capabilities.json 的运行时视图。
 * 资产缺失或损坏时按 core-only 运行；壳未实现的能力由 LuaRuntime 安装抛错桩。
 */
final class CapabilityConfig {
    private static final String TAG = "berba";
    static final int SCHEMA = 1;

    static final Set<String> CORE_APP_EVENTS = Collections.unmodifiableSet(
            new LinkedHashSet<>(Arrays.asList(
                    "back", "boot", "create", "destroy", "newIntent", "pause", "resume")));
    static final Set<String> CORE_WEB_EVENTS = Collections.unmodifiableSet(
            new LinkedHashSet<>(Arrays.asList("error", "pageFinished", "pageStarted")));

    static final class Module {
        final String browser;
        final List<String> functions;

        Module(String browser, List<String> functions) {
            this.browser = browser == null ? "" : browser;
            this.functions = functions;
        }
    }

    static final class NativeFunction {
        final String name;
        final boolean async;

        NativeFunction(String name, boolean async) {
            this.name = name;
            this.async = async;
        }
    }

    static final class NativeModuleInfo {
        final String name;
        final Map<String, NativeFunction> functions;

        NativeModuleInfo(String name, Map<String, NativeFunction> functions) {
            this.name = name;
            this.functions = functions;
        }
    }

    static final class NativePluginInfo {
        final String name;
        final String libName;
        final String config;
        final Map<String, NativeModuleInfo> modules;

        NativePluginInfo(String name, String libName, String config,
                         Map<String, NativeModuleInfo> modules) {
            this.name = name;
            this.libName = libName;
            this.config = config;
            this.modules = modules;
        }
    }

    private final List<String> capabilities;
    private final Map<String, Module> modules;
    private final Set<String> globals;
    private final Set<String> appEvents;
    private final Set<String> webEvents;
    private final int nativeAbiVersion;
    private final List<NativePluginInfo> nativePlugins;

    private CapabilityConfig(List<String> capabilities,
                             Map<String, Module> modules, Set<String> globals,
                             Set<String> appEvents, Set<String> webEvents,
                             int nativeAbiVersion, List<NativePluginInfo> nativePlugins) {
        this.capabilities = capabilities;
        this.modules = modules;
        this.globals = globals;
        this.appEvents = appEvents;
        this.webEvents = webEvents;
        this.nativeAbiVersion = nativeAbiVersion;
        this.nativePlugins = nativePlugins;
    }

    static CapabilityConfig load(Context ctx) {
        try {
            return parse(readAsset(ctx, "berba_capabilities.json"));
        } catch (Exception e) {
            Log.e(TAG, "能力资产缺失或损坏，按 core-only 运行: " + e);
            return coreOnly();
        }
    }

    static CapabilityConfig coreOnly() {
        return new CapabilityConfig(Collections.emptyList(),
                Collections.emptyMap(), Collections.emptySet(),
                new LinkedHashSet<>(CORE_APP_EVENTS), new LinkedHashSet<>(CORE_WEB_EVENTS),
                0, Collections.emptyList());
    }

    private static CapabilityConfig parse(String text) throws Exception {
        JSONObject json = new JSONObject(text);
        int schema = json.optInt("schema", 0);
        if (schema != SCHEMA) {
            throw new IllegalStateException("不支持的能力资产 schema: " + schema);
        }

        Set<String> caps = new TreeSet<>();
        JSONArray capArray = json.optJSONArray("capabilities");
        if (capArray != null) {
            for (int i = 0; i < capArray.length(); i++) {
                String name = capArray.optString(i, "").trim();
                if (!name.isEmpty()) {
                    caps.add(name);
                }
            }
        }

        Map<String, Module> modules = new LinkedHashMap<>();
        JSONObject modulesJson = json.optJSONObject("modules");
        if (modulesJson != null) {
            for (String name : sortedKeys(modulesJson)) {
                JSONObject mod = modulesJson.optJSONObject(name);
                if (mod == null) {
                    continue;
                }
                List<String> functions = new ArrayList<>();
                JSONObject fns = mod.optJSONObject("functions");
                if (fns != null) {
                    functions.addAll(sortedKeys(fns));
                }
                modules.put(name, new Module(mod.optString("browser", ""), functions));
            }
        }

        Set<String> globals = new TreeSet<>(sortedKeys(json.optJSONObject("globals")));

        Set<String> appEvents = new LinkedHashSet<>(CORE_APP_EVENTS);
        Set<String> webEvents = new LinkedHashSet<>(CORE_WEB_EVENTS);
        JSONObject events = json.optJSONObject("events");
        if (events != null) {
            addKeys(events.optJSONObject("app"), appEvents);
            addKeys(events.optJSONObject("web"), webEvents);
        }

        int nativeAbiVersion = 0;
        List<NativePluginInfo> nativePlugins = new ArrayList<>();
        JSONObject nativeJson = json.optJSONObject("native");
        if (nativeJson != null) {
            nativeAbiVersion = nativeJson.optInt("abiVersion", 0);
            JSONArray plugins = nativeJson.optJSONArray("plugins");
            if (plugins != null) {
                for (int i = 0; i < plugins.length(); i++) {
                    JSONObject plugin = plugins.optJSONObject(i);
                    if (plugin == null) {
                        continue;
                    }
                    String name = plugin.optString("name", "").trim();
                    if (name.isEmpty()) {
                        continue;
                    }
                    String libName = plugin.optString("libName", "").trim();
                    if (libName.isEmpty()) {
                        libName = "libberba_" + name + ".so";
                    }
                    String config = "{}";
                    Object rawConfig = plugin.opt("config");
                    if (rawConfig instanceof JSONObject || rawConfig instanceof JSONArray) {
                        config = rawConfig.toString();
                    } else if (rawConfig instanceof String && !((String) rawConfig).trim().isEmpty()) {
                        config = (String) rawConfig;
                    }
                    Map<String, NativeModuleInfo> pluginModules = new LinkedHashMap<>();
                    JSONObject modsJson = plugin.optJSONObject("modules");
                    for (String modName : sortedKeys(modsJson)) {
                        JSONObject mod = modsJson.optJSONObject(modName);
                        if (mod == null) {
                            continue;
                        }
                        Map<String, NativeFunction> functions = new LinkedHashMap<>();
                        JSONObject fns = mod.optJSONObject("functions");
                        for (String fnName : sortedKeys(fns)) {
                            JSONObject fn = fns.optJSONObject(fnName);
                            boolean async = fn != null && fn.optBoolean("async", false);
                            functions.put(fnName, new NativeFunction(fnName, async));
                        }
                        pluginModules.put(modName, new NativeModuleInfo(modName, functions));
                    }
                    nativePlugins.add(new NativePluginInfo(name, libName, config, pluginModules));
                }
            }
        }
        Collections.sort(nativePlugins, (a, b) -> a.name.compareTo(b.name));

        return new CapabilityConfig(new ArrayList<>(caps), modules, globals,
                appEvents, webEvents, nativeAbiVersion, nativePlugins);
    }

    private static void addKeys(JSONObject obj, Set<String> out) {
        out.addAll(sortedKeys(obj));
    }

    private static List<String> sortedKeys(JSONObject obj) {
        List<String> keys = new ArrayList<>();
        if (obj != null) {
            JSONArray names = obj.names();
            if (names != null) {
                for (int i = 0; i < names.length(); i++) {
                    String key = names.optString(i, "");
                    if (!key.isEmpty()) {
                        keys.add(key);
                    }
                }
            }
        }
        Collections.sort(keys);
        return keys;
    }

    private static String readAsset(Context ctx, String path) throws Exception {
        try (InputStream in = ctx.getAssets().open(path)) {
            ByteArrayOutputStream out = new ByteArrayOutputStream();
            byte[] buf = new byte[8192];
            int n;
            while ((n = in.read(buf)) > 0) {
                out.write(buf, 0, n);
            }
            return out.toString(StandardCharsets.UTF_8.name());
        }
    }

    boolean hasCapability(String name) {
        return name != null && capabilities.contains(name);
    }

    List<String> capabilities() {
        return Collections.unmodifiableList(capabilities);
    }

    Map<String, Module> modules() {
        return Collections.unmodifiableMap(modules);
    }

    Set<String> globals() {
        return Collections.unmodifiableSet(globals);
    }

    boolean isAppEvent(String name) {
        return name != null && appEvents.contains(name);
    }

    boolean isWebEvent(String name) {
        return name != null && webEvents.contains(name);
    }

    int nativeAbiVersion() {
        return nativeAbiVersion;
    }

    List<NativePluginInfo> nativePlugins() {
        return Collections.unmodifiableList(nativePlugins);
    }
}
