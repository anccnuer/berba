package com.berba.shell;

import android.os.Build;
import android.util.DisplayMetrics;
import android.util.Log;

import org.json.JSONTokener;
import org.luaj.vm2.Globals;
import org.luaj.vm2.LuaError;
import org.luaj.vm2.LuaTable;
import org.luaj.vm2.LuaValue;
import org.luaj.vm2.Varargs;
import org.luaj.vm2.lib.VarArgFunction;

import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.Locale;

/** 白名单 Lua API 的安装入口。 */
final class LuaApis {
    private static final String TAG = "berba";
    private static final int HTTP_TIMEOUT_MS = 15000;
    private static final int MAX_HTTP_BODY = 8 * 1024 * 1024;
    private static final long MAX_FILE_SIZE = 8L * 1024 * 1024;

    private LuaApis() {
    }

    private interface Body {
        Varargs call(Varargs args) throws Exception;
    }

    private static LuaValue fn(final Body body) {
        return new VarArgFunction() {
            @Override
            public Varargs invoke(Varargs args) {
                try {
                    return body.call(args);
                } catch (LuaError e) {
                    throw e;
                } catch (Throwable t) {
                    throw new LuaError(LuaRuntime.message(t));
                }
            }
        };
    }

    private static Varargs nilError(Throwable t) {
        return LuaValue.varargsOf(LuaValue.NIL, LuaValue.valueOf(LuaRuntime.message(t)));
    }

    static void install(final LuaRuntime rt, Globals g) {
        g.set("log", fn(args -> {
            StringBuilder sb = new StringBuilder();
            for (int i = 1; i <= args.narg(); i++) {
                if (i > 1) {
                    sb.append(' ');
                }
                sb.append(args.arg(i).tojstring());
            }
            Log.i(TAG, sb.toString());
            return LuaValue.NONE;
        }));
        installApp(rt, g);
        installWeb(rt, g);
        installBridge(rt, g);
        installHttp(rt, g);
        installFile(rt, g);
        installStore(rt, g);
        installPermission(rt, g);
        installUi(rt, g);
        installDevice(rt, g);
        installJson(rt, g);
    }

    private static void installApp(final LuaRuntime rt, Globals g) {
        LuaTable app = new LuaTable();
        app.set("on", fn(args -> {
            String event = args.checkjstring(1);
            LuaValue handler = args.arg(2);
            if (!handler.isfunction()) {
                throw new LuaError("app.on 第二个参数必须是函数");
            }
            if (!LuaRuntime.isAppEvent(event)) {
                throw new LuaError("未知事件: " + event);
            }
            rt.addHandler(event, handler);
            return LuaValue.NIL;
        }));
        app.set("exit", fn(args -> {
            rt.activity().finishOnMain();
            return LuaValue.NIL;
        }));
        app.set("info", fn(args -> {
            AppConfig c = rt.config();
            LuaTable t = new LuaTable();
            t.set("name", c.name);
            t.set("package", c.packageName);
            t.set("versionName", c.versionName);
            t.set("versionCode", c.versionCode);
            t.set("sdk", Build.VERSION.SDK_INT);
            t.set("platform", "android");
            return t;
        }));
        g.set("app", app);
    }

    private static void installWeb(final LuaRuntime rt, Globals g) {
        LuaTable web = new LuaTable();
        web.set("load", fn(args -> {
            rt.activity().loadFromLua(args.checkjstring(1));
            return LuaValue.NIL;
        }));
        web.set("reload", fn(args -> {
            rt.activity().reloadOnMain();
            return LuaValue.NIL;
        }));
        web.set("goBack", fn(args -> {
            rt.activity().goBackOnMain();
            return LuaValue.NIL;
        }));
        web.set("canGoBack", fn(args -> rt.activity().canGoBackOnMain()
                ? LuaValue.TRUE : LuaValue.FALSE));
        web.set("eval", fn(args -> {
            String script = args.checkjstring(1);
            final LuaValue callback = args.arg(2);
            rt.activity().evaluateJs(script, callback.isfunction() ? callback : null);
            return LuaValue.NIL;
        }));
        web.set("on", fn(args -> {
            String event = args.checkjstring(1);
            LuaValue handler = args.arg(2);
            if (!handler.isfunction()) {
                throw new LuaError("web.on 第二个参数必须是函数");
            }
            if (!LuaRuntime.isWebEvent(event)) {
                throw new LuaError("未知事件: " + event);
            }
            rt.addHandler(event, handler);
            return LuaValue.NIL;
        }));
        g.set("web", web);
    }

    private static void installBridge(final LuaRuntime rt, Globals g) {
        LuaTable bridge = new LuaTable();
        bridge.set("register", fn(args -> {
            String name = args.checkjstring(1);
            LuaValue handler = args.arg(2);
            if (!handler.isfunction()) {
                throw new LuaError("bridge.register 第二个参数必须是函数");
            }
            rt.registerBridge(name, handler);
            return LuaValue.NIL;
        }));
        bridge.set("emit", fn(args -> {
            String name = args.checkjstring(1);
            LuaValue data = args.arg(2);
            rt.emitToJs(name, LuaConvert.toJson(data));
            return LuaValue.NIL;
        }));
        g.set("bridge", bridge);
    }

    private static void installHttp(final LuaRuntime rt, Globals g) {
        LuaTable http = new LuaTable();
        http.set("get", fn(args -> {
            final String url = args.checkjstring(1);
            final LuaValue callback = args.arg(2);
            request(rt, "GET", url, null, callback);
            return LuaValue.NIL;
        }));
        http.set("post", fn(args -> {
            final String url = args.checkjstring(1);
            LuaValue body = args.arg(2);
            final LuaValue callback = args.arg(3);
            String payload;
            if (body.isnil()) {
                payload = "";
            } else if (body.isstring()) {
                payload = body.tojstring();
            } else {
                payload = LuaConvert.toJson(body);
            }
            request(rt, "POST", url, payload, callback);
            return LuaValue.NIL;
        }));
        g.set("http", http);
    }

    private static void request(final LuaRuntime rt, final String method, final String url,
                                final String body, final LuaValue callback) {
        if (!callback.isfunction()) {
            throw new LuaError("http." + method.toLowerCase(Locale.ROOT) + " 需要回调函数");
        }
        final URL parsed;
        try {
            parsed = new URL(url);
        } catch (Exception e) {
            throw new LuaError("URL 不合法: " + url);
        }
        if (!"http".equals(parsed.getProtocol()) && !"https".equals(parsed.getProtocol())) {
            throw new LuaError("只支持 http/https");
        }
        rt.submitIo(() -> {
            final LuaTable result = doRequest(method, parsed, body);
            rt.post(() -> callback.invoke(result));
        });
    }

    private static LuaTable doRequest(String method, URL url, String body) {
        LuaTable result = new LuaTable();
        HttpURLConnection conn = null;
        try {
            conn = (HttpURLConnection) url.openConnection();
            conn.setRequestMethod(method);
            conn.setConnectTimeout(HTTP_TIMEOUT_MS);
            conn.setReadTimeout(HTTP_TIMEOUT_MS);
            conn.setInstanceFollowRedirects(true);
            conn.setRequestProperty("Accept", "*/*");
            conn.setRequestProperty("User-Agent", "berba");
            if ("POST".equals(method)) {
                conn.setDoOutput(true);
                String trimmed = body == null ? "" : body.trim();
                String type = trimmed.startsWith("{") || trimmed.startsWith("[")
                        ? "application/json; charset=utf-8" : "text/plain; charset=utf-8";
                conn.setRequestProperty("Content-Type", type);
                byte[] bytes = (body == null ? "" : body).getBytes(StandardCharsets.UTF_8);
                conn.setFixedLengthStreamingMode(bytes.length);
                try (java.io.OutputStream os = conn.getOutputStream()) {
                    os.write(bytes);
                }
            }
            int status = conn.getResponseCode();
            InputStream in = status >= 400 ? conn.getErrorStream() : conn.getInputStream();
            String text = in == null ? "" : readLimited(in, MAX_HTTP_BODY);
            result.set("ok", status >= 200 && status < 300 ? LuaValue.TRUE : LuaValue.FALSE);
            result.set("status", status);
            result.set("body", text);
        } catch (Exception e) {
            result.set("ok", LuaValue.FALSE);
            result.set("status", 0);
            result.set("body", "");
            result.set("error", LuaRuntime.message(e));
        } finally {
            if (conn != null) {
                conn.disconnect();
            }
        }
        return result;
    }

    private static String readLimited(InputStream in, int limit) throws IOException {
        try (InputStream input = in) {
            ByteArrayOutputStream out = new ByteArrayOutputStream();
            byte[] buf = new byte[8192];
            int total = 0;
            int n;
            while ((n = input.read(buf)) > 0) {
                total += n;
                if (total > limit) {
                    throw new IOException("响应超过 " + (limit / 1024 / 1024) + "MB 限制");
                }
                out.write(buf, 0, n);
            }
            return out.toString(StandardCharsets.UTF_8.name());
        }
    }

    private static File resolveFile(LuaRuntime rt, String rel) throws IOException {
        String s = rel == null ? "" : rel.trim().replace('\\', '/');
        if (s.indexOf('\0') >= 0) {
            throw new IOException("路径包含非法字符");
        }
        if (s.startsWith("/")) {
            throw new IOException("必须使用相对路径");
        }
        File root = rt.activity().getFilesDir().getCanonicalFile();
        File f = new File(root, s).getCanonicalFile();
        String rootPath = root.getPath();
        if (!f.getPath().equals(rootPath) && !f.getPath().startsWith(rootPath + File.separator)) {
            throw new IOException("路径越界");
        }
        return f;
    }

    private static void installFile(final LuaRuntime rt, Globals g) {
        LuaTable file = new LuaTable();
        file.set("read", fn(args -> {
            File f = resolveFile(rt, args.checkjstring(1));
            if (!f.isFile()) {
                return nilError(new IOException("文件不存在"));
            }
            if (f.length() > MAX_FILE_SIZE) {
                return nilError(new IOException("文件超过 8MB 限制"));
            }
            try (FileInputStream in = new FileInputStream(f)) {
                return LuaValue.valueOf(readLimited(in, (int) MAX_FILE_SIZE));
            } catch (IOException e) {
                return nilError(e);
            }
        }));
        file.set("write", fn(args -> {
            File f = resolveFile(rt, args.checkjstring(1));
            String data = args.checkjstring(2);
            File parent = f.getParentFile();
            if (parent != null && !parent.isDirectory() && !parent.mkdirs()) {
                return nilError(new IOException("无法创建目录"));
            }
            try (FileOutputStream out = new FileOutputStream(f)) {
                out.write(data.getBytes(StandardCharsets.UTF_8));
                return LuaValue.TRUE;
            } catch (IOException e) {
                return nilError(e);
            }
        }));
        file.set("exists", fn(args -> {
            try {
                return resolveFile(rt, args.checkjstring(1)).exists() ? LuaValue.TRUE : LuaValue.FALSE;
            } catch (IOException e) {
                return LuaValue.FALSE;
            }
        }));
        file.set("list", fn(args -> {
            File dir = resolveFile(rt, args.checkjstring(1));
            if (!dir.isDirectory()) {
                return nilError(new IOException("不是目录"));
            }
            String[] names = dir.list();
            if (names == null) {
                return nilError(new IOException("无法读取目录"));
            }
            Arrays.sort(names);
            LuaTable t = new LuaTable();
            for (int i = 0; i < names.length; i++) {
                t.set(i + 1, names[i]);
            }
            return t;
        }));
        g.set("file", file);
    }

    private static void installStore(final LuaRuntime rt, Globals g) {
        LuaTable store = new LuaTable();
        store.set("get", fn(args -> {
            String key = args.checkjstring(1);
            String raw = rt.activity().prefs().getString(key, null);
            if (raw == null) {
                return LuaValue.NIL;
            }
            return LuaConvert.parse(raw);
        }));
        store.set("set", fn(args -> {
            String key = args.checkjstring(1);
            if (key.trim().isEmpty() || key.length() > 256) {
                throw new LuaError("store key 不合法");
            }
            LuaValue value = args.arg(2);
            if (value.isnil()) {
                rt.activity().prefs().edit().remove(key).apply();
            } else {
                rt.activity().prefs().edit().putString(key, LuaConvert.toJson(value)).apply();
            }
            return LuaValue.TRUE;
        }));
        g.set("store", store);
    }

    static String normalizePermission(String raw) {
        String s = raw == null ? "" : raw.trim();
        if (s.isEmpty()) {
            return "";
        }
        if (s.contains(".")) {
            return s;
        }
        return "android.permission." + s.replace('-', '_').toUpperCase(Locale.ROOT);
    }

    private static void installPermission(final LuaRuntime rt, Globals g) {
        LuaTable permission = new LuaTable();
        permission.set("check", fn(args -> {
            String name = normalizePermission(args.checkjstring(1));
            if (name.isEmpty()) {
                throw new LuaError("权限名不能为空");
            }
            return rt.activity().hasPermission(name) ? LuaValue.TRUE : LuaValue.FALSE;
        }));
        permission.set("request", fn(args -> {
            LuaValue names = args.arg(1);
            List<String> list = new ArrayList<>();
            if (names.isstring()) {
                list.add(normalizePermission(names.tojstring()));
            } else if (names.istable()) {
                LuaTable t = names.checktable();
                for (int i = 1; i <= t.length(); i++) {
                    LuaValue v = t.get(i);
                    if (v.isstring()) {
                        list.add(normalizePermission(v.tojstring()));
                    }
                }
            }
            list.removeAll(Collections.singleton(""));
            if (list.isEmpty()) {
                throw new LuaError("permission.request 需要权限名");
            }
            if (list.size() > 32) {
                throw new LuaError("一次最多请求 32 个权限");
            }
            final LuaValue callback = args.arg(2);
            rt.activity().requestPermissionsFromLua(list, callback.isfunction() ? callback : null);
            return LuaValue.NIL;
        }));
        g.set("permission", permission);
    }

    private static void installUi(final LuaRuntime rt, Globals g) {
        LuaTable ui = new LuaTable();
        ui.set("toast", fn(args -> {
            rt.activity().toastOnMain(args.checkjstring(1));
            return LuaValue.NIL;
        }));
        ui.set("alert", fn(args -> {
            rt.activity().showAlert(args.checkjstring(1));
            return LuaValue.NIL;
        }));
        ui.set("confirm", fn(args -> {
            String message = args.checkjstring(1);
            final LuaValue callback = args.arg(2);
            rt.activity().showConfirm(message, callback.isfunction() ? callback : null);
            return LuaValue.NIL;
        }));
        ui.set("fullscreen", fn(args -> {
            rt.activity().setFullscreenOnMain(args.checkboolean(1));
            return LuaValue.NIL;
        }));
        ui.set("keepScreenOn", fn(args -> {
            rt.activity().setKeepScreenOnOnMain(args.checkboolean(1));
            return LuaValue.NIL;
        }));
        g.set("ui", ui);
    }

    private static void installDevice(final LuaRuntime rt, Globals g) {
        LuaTable device = new LuaTable();
        device.set("clipboard", fn(args -> LuaValue.valueOf(rt.activity().clipboardText())));
        device.set("orientation", fn(args -> {
            rt.activity().setOrientationOnMain(args.checkjstring(1));
            return LuaValue.NIL;
        }));
        device.set("model", fn(args -> LuaValue.valueOf(Build.MODEL)));
        device.set("sdk", fn(args -> LuaValue.valueOf(Build.VERSION.SDK_INT)));
        device.set("platform", fn(args -> LuaValue.valueOf("android")));
        device.set("locale", fn(args -> LuaValue.valueOf(Locale.getDefault().toString())));
        device.set("screen", fn(args -> {
            DisplayMetrics m = rt.activity().getResources().getDisplayMetrics();
            LuaTable t = new LuaTable();
            t.set("width", m.widthPixels);
            t.set("height", m.heightPixels);
            t.set("density", m.density);
            t.set("dpi", m.densityDpi);
            return t;
        }));
        g.set("device", device);
    }

    private static void installJson(final LuaRuntime rt, Globals g) {
        LuaTable json = new LuaTable();
        json.set("encode", fn(args -> LuaValue.valueOf(LuaConvert.toJson(args.arg(1)))));
        json.set("decode", fn(args -> {
            String text = args.checkjstring(1);
            try {
                Object value = new JSONTokener(text).nextValue();
                return LuaConvert.fromJson(value);
            } catch (Exception e) {
                return nilError(e);
            }
        }));
        g.set("json", json);
    }
}
