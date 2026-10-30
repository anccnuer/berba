package com.berba.shell;

import android.Manifest;
import android.content.Context;
import android.content.pm.PackageManager;
import android.graphics.Color;
import android.os.Build;
import android.os.VibrationEffect;
import android.os.Vibrator;
import android.os.VibratorManager;
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
import java.util.Map;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicInteger;

/** 白名单 Lua API 的安装入口。 */
final class LuaApis {
    private static final String TAG = "berba";
    private static final int MAX_HTTP_BODY = 8 * 1024 * 1024;
    private static final long MAX_FILE_SIZE = 8L * 1024 * 1024;

    private LuaApis() {
    }

    interface Body {
        Varargs call(Varargs args) throws Exception;
    }

    static LuaValue fn(final Body body) {
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

    static Varargs nilError(Throwable t) {
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
            if (!rt.isAppEvent(event)) {
                throw new LuaError("未知事件: " + event);
            }
            rt.addHandler(event, handler);
            return LuaValue.NIL;
        }));
        app.set("exit", fn(args -> {
            rt.requireUi().activity().finishOnMain();
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
            t.set("foreground", LuaValue.valueOf(rt.ui() != null));
            return t;
        }));
        g.set("app", app);
    }

    private static void installWeb(final LuaRuntime rt, Globals g) {
        LuaTable web = new LuaTable();
        web.set("load", fn(args -> {
            rt.requireUi().activity().loadFromLua(args.checkjstring(1));
            return LuaValue.NIL;
        }));
        web.set("reload", fn(args -> {
            rt.requireUi().activity().reloadOnMain();
            return LuaValue.NIL;
        }));
        web.set("goBack", fn(args -> {
            rt.requireUi().activity().goBackOnMain();
            return LuaValue.NIL;
        }));
        web.set("canGoBack", fn(args -> rt.requireUi().activity().canGoBackCached()
                ? LuaValue.TRUE : LuaValue.FALSE));
        web.set("eval", fn(args -> {
            String script = args.checkjstring(1);
            final LuaValue callback = args.arg(2);
            rt.requireUi().activity().evaluateJs(script, callback.isfunction() ? callback : null);
            return LuaValue.NIL;
        }));
        web.set("on", fn(args -> {
            String event = args.checkjstring(1);
            LuaValue handler = args.arg(2);
            if (!handler.isfunction()) {
                throw new LuaError("web.on 第二个参数必须是函数");
            }
            if (!rt.isWebEvent(event)) {
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

    private static final List<String> HTTP_METHODS =
            Arrays.asList("GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS");
    private static final int MAX_HTTP_TIMEOUT_S = 120;
    private static final AtomicInteger HTTP_SEQ = new AtomicInteger(1);
    private static final Map<String, HttpURLConnection> ACTIVE_HTTP = new ConcurrentHashMap<>();
    private static final Set<String> CANCELED_HTTP = ConcurrentHashMap.newKeySet();

    private static void installHttp(final LuaRuntime rt, Globals g) {
        LuaTable http = new LuaTable();
        http.set("get", fn(args -> {
            String id = nextHttpId();
            request(rt, id, "GET", args.checkjstring(1), null, null, 15,
                    args.arg(2), LuaValue.NIL);
            return LuaValue.NIL;
        }));
        http.set("post", fn(args -> {
            LuaValue body = args.arg(2);
            String payload;
            if (body.isnil()) {
                payload = "";
            } else if (body.isstring()) {
                payload = body.tojstring();
            } else {
                payload = LuaConvert.toJson(body);
            }
            request(rt, nextHttpId(), "POST", args.checkjstring(1), payload, null, 15,
                    args.arg(3), LuaValue.NIL);
            return LuaValue.NIL;
        }));
        http.set("request", fn(args -> {
            LuaTable opts = args.checktable(1);
            String url = opts.get("url").checkjstring();
            String method = opts.get("method").optjstring("GET").toUpperCase(Locale.ROOT);
            if (!HTTP_METHODS.contains(method)) {
                throw new LuaError("http.request 不支持的 method: " + method);
            }
            LuaTable headers = opts.get("headers").istable() ? opts.get("headers").checktable() : null;
            LuaValue bodyValue = opts.get("body");
            String body = null;
            if (!bodyValue.isnil()) {
                body = bodyValue.isstring() ? bodyValue.tojstring() : LuaConvert.toJson(bodyValue);
            }
            int timeout = opts.get("timeout").optint(15);
            if (timeout < 1 || timeout > MAX_HTTP_TIMEOUT_S) {
                throw new LuaError("timeout 需在 1-" + MAX_HTTP_TIMEOUT_S + " 秒");
            }
            LuaValue callback = opts.get("cb");
            if (!callback.isfunction()) {
                throw new LuaError("http.request 需要 cb 回调");
            }
            String id = nextHttpId();
            request(rt, id, method, url, body, headers, timeout, callback, opts.get("onProgress"));
            return LuaValue.valueOf(id);
        }));
        http.set("cancel", fn(args -> {
            String id = args.checkjstring(1);
            // 先标记再断开：请求可能尚未注册到 ACTIVE_HTTP，标记可避免取消丢失。
            CANCELED_HTTP.add(id);
            HttpURLConnection conn = ACTIVE_HTTP.remove(id);
            if (conn != null) {
                conn.disconnect();
            }
            return conn != null ? LuaValue.TRUE : LuaValue.FALSE;
        }));
        g.set("http", http);
    }

    private static String nextHttpId() {
        return "h" + HTTP_SEQ.getAndIncrement();
    }

    /** 取消所有在途请求，供运行时关闭时调用。 */
    static void cancelAllHttp() {
        for (Map.Entry<String, HttpURLConnection> entry : ACTIVE_HTTP.entrySet()) {
            CANCELED_HTTP.add(entry.getKey());
            try {
                entry.getValue().disconnect();
            } catch (Throwable ignored) {
            }
        }
        ACTIVE_HTTP.clear();
    }

    private static void request(final LuaRuntime rt, final String id, final String method,
                                final String url, final String body, final LuaTable headers,
                                final int timeoutSeconds, final LuaValue callback,
                                final LuaValue progress) {
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
        if ("http".equals(parsed.getProtocol()) && !rt.config().cleartext) {
            throw new LuaError("明文 HTTP 未开启（在 app.json 设置 cleartext: true）");
        }
        final AppConfig config = rt.config();
        rt.submitIo(() -> {
            final LuaTable result = doRequest(rt, id, config, method, parsed, body, headers,
                    timeoutSeconds * 1000, 0, progress);
            rt.post(() -> callback.invoke(result));
        });
    }

    private static final int MAX_REDIRECTS = 5;

    private static LuaTable doRequest(LuaRuntime rt, String id, AppConfig config, String method,
                                      URL url, String body, LuaTable headers, int timeoutMs,
                                      int redirects, LuaValue progress) {
        if (CANCELED_HTTP.contains(id)) {
            return httpError("已取消");
        }
        if (!"http".equals(url.getProtocol()) && !"https".equals(url.getProtocol())) {
            return httpError("重定向只支持 http/https");
        }
        if ("http".equals(url.getProtocol()) && !config.cleartext) {
            return httpError("明文 HTTP 未开启（cleartext: false）");
        }
        if (!config.allowPrivateNetwork && NetworkPolicy.isPrivateHost(url.getHost())) {
            return httpError("默认禁止访问本机或私网地址（可设置 http.allowPrivateNetwork 放行）");
        }
        LuaTable result = new LuaTable();
        HttpURLConnection conn = null;
        try {
            conn = (HttpURLConnection) url.openConnection();
            conn.setRequestMethod(method);
            conn.setConnectTimeout(timeoutMs);
            conn.setReadTimeout(timeoutMs);
            conn.setInstanceFollowRedirects(false);

            boolean hasAccept = false;
            boolean hasUserAgent = false;
            boolean hasContentType = false;
            if (headers != null) {
                for (LuaValue key : headers.keys()) {
                    String name = key.tojstring();
                    LuaValue value = headers.get(key);
                    if (value.isnil()) {
                        continue;
                    }
                    conn.setRequestProperty(name, value.tojstring());
                    hasAccept |= "Accept".equalsIgnoreCase(name);
                    hasUserAgent |= "User-Agent".equalsIgnoreCase(name);
                    hasContentType |= "Content-Type".equalsIgnoreCase(name);
                }
            }
            if (!hasAccept) {
                conn.setRequestProperty("Accept", "*/*");
            }
            if (!hasUserAgent) {
                conn.setRequestProperty("User-Agent", "berba");
            }
            if (body != null) {
                if (!hasContentType) {
                    String trimmed = body.trim();
                    conn.setRequestProperty("Content-Type",
                            trimmed.startsWith("{") || trimmed.startsWith("[")
                                    ? "application/json; charset=utf-8"
                                    : "text/plain; charset=utf-8");
                }
                conn.setDoOutput(true);
                byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
                conn.setFixedLengthStreamingMode(bytes.length);
                try (java.io.OutputStream os = conn.getOutputStream()) {
                    os.write(bytes);
                }
            }

            ACTIVE_HTTP.put(id, conn);
            int status = conn.getResponseCode();
            if (isRedirect(status)) {
                String location = conn.getHeaderField("Location");
                InputStream redirectBody = status >= 400 ? conn.getErrorStream() : conn.getInputStream();
                if (redirectBody != null) {
                    redirectBody.close();
                }
                if (location == null || location.trim().isEmpty()) {
                    return httpError("重定向缺少 Location");
                }
                if (redirects >= MAX_REDIRECTS) {
                    return httpError("重定向次数过多");
                }
                URL next = new URL(url, location);
                boolean keepMethod = status == 307 || status == 308;
                ACTIVE_HTTP.remove(id);
                conn.disconnect();
                return doRequest(rt, id, config, keepMethod ? method : "GET", next,
                        keepMethod ? body : null, keepMethod ? headers : null, timeoutMs,
                        redirects + 1, progress);
            }
            InputStream in = status >= 400 ? conn.getErrorStream() : conn.getInputStream();
            int totalLength = conn.getContentLength();
            ReadProgress reporter = null;
            if (progress.isfunction()) {
                reporter = (read, total) -> rt.post(() -> {
                    try {
                        progress.invoke(LuaValue.valueOf(read), LuaValue.valueOf(total));
                    } catch (Throwable t) {
                        Log.e(TAG, "http onProgress 回调错误", t);
                    }
                });
            }
            String text = in == null ? "" : readLimited(in, MAX_HTTP_BODY, totalLength, reporter);
            result.set("ok", status >= 200 && status < 300 ? LuaValue.TRUE : LuaValue.FALSE);
            result.set("status", status);
            result.set("body", text);
            result.set("headers", headersTable(conn));
        } catch (Exception e) {
            return httpError(CANCELED_HTTP.contains(id) ? "已取消" : LuaRuntime.message(e));
        } finally {
            ACTIVE_HTTP.remove(id);
            CANCELED_HTTP.remove(id);
            if (conn != null) {
                conn.disconnect();
            }
        }
        return result;
    }

    private static LuaTable headersTable(HttpURLConnection conn) {
        LuaTable headers = new LuaTable();
        for (Map.Entry<String, List<String>> entry : conn.getHeaderFields().entrySet()) {
            if (entry.getKey() == null) {
                continue;
            }
            StringBuilder sb = new StringBuilder();
            for (String value : entry.getValue()) {
                if (sb.length() > 0) {
                    sb.append(", ");
                }
                sb.append(value);
            }
            headers.set(entry.getKey(), sb.toString());
        }
        return headers;
    }

    private static boolean isRedirect(int status) {
        return status == 301 || status == 302 || status == 303 || status == 307 || status == 308;
    }

    private static LuaTable httpError(String message) {
        LuaTable result = new LuaTable();
        result.set("ok", LuaValue.FALSE);
        result.set("status", 0);
        result.set("body", "");
        result.set("headers", new LuaTable());
        result.set("error", message);
        return result;
    }

    private interface ReadProgress {
        void onProgress(int read, int total);
    }

    private static String readLimited(InputStream in, int limit, int totalLength,
                                      ReadProgress progress) throws IOException {
        try (InputStream input = in) {
            ByteArrayOutputStream out = new ByteArrayOutputStream();
            byte[] buf = new byte[8192];
            int total = 0;
            int lastReport = 0;
            int n;
            while ((n = input.read(buf)) > 0) {
                total += n;
                if (total > limit) {
                    throw new IOException("响应超过 " + (limit / 1024 / 1024) + "MB 限制");
                }
                out.write(buf, 0, n);
                if (progress != null && total - lastReport >= 256 * 1024) {
                    lastReport = total;
                    progress.onProgress(total, totalLength);
                }
            }
            if (progress != null && total != lastReport) {
                progress.onProgress(total, totalLength);
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
        File root = rt.context().getFilesDir().getCanonicalFile();
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
                return LuaValue.valueOf(readLimited(in, (int) MAX_FILE_SIZE, -1, null));
            } catch (IOException e) {
                return nilError(e);
            }
        }));
        file.set("write", fn(args -> {
            File f = resolveFile(rt, args.checkjstring(1));
            byte[] bytes = args.checkjstring(2).getBytes(StandardCharsets.UTF_8);
            if (bytes.length > MAX_FILE_SIZE) {
                return nilError(new IOException("文件超过 8MB 限制"));
            }
            File parent = f.getParentFile();
            if (parent != null && !parent.isDirectory() && !parent.mkdirs()) {
                return nilError(new IOException("无法创建目录"));
            }
            File tmp = new File(parent, "." + f.getName() + ".tmp");
            try (FileOutputStream out = new FileOutputStream(tmp)) {
                out.write(bytes);
                out.getFD().sync();
            } catch (IOException e) {
                tmp.delete();
                return nilError(e);
            }
            if (f.exists() && !f.delete()) {
                tmp.delete();
                return nilError(new IOException("无法覆盖目标文件"));
            }
            if (!tmp.renameTo(f)) {
                tmp.delete();
                return nilError(new IOException("写入失败"));
            }
            return LuaValue.TRUE;
        }));
        file.set("append", fn(args -> {
            File f = resolveFile(rt, args.checkjstring(1));
            byte[] bytes = args.checkjstring(2).getBytes(StandardCharsets.UTF_8);
            if (f.length() + bytes.length > MAX_FILE_SIZE) {
                return nilError(new IOException("文件超过 8MB 限制"));
            }
            File parent = f.getParentFile();
            if (parent != null && !parent.isDirectory() && !parent.mkdirs()) {
                return nilError(new IOException("无法创建目录"));
            }
            try (FileOutputStream out = new FileOutputStream(f, true)) {
                out.write(bytes);
                return LuaValue.TRUE;
            } catch (IOException e) {
                return nilError(e);
            }
        }));
        file.set("delete", fn(args -> {
            File f = resolveFile(rt, args.checkjstring(1));
            if (f.delete()) {
                return LuaValue.TRUE;
            }
            return f.exists() ? nilError(new IOException("删除失败")) : LuaValue.FALSE;
        }));
        file.set("rename", fn(args -> {
            File from = resolveFile(rt, args.checkjstring(1));
            File to = resolveFile(rt, args.checkjstring(2));
            if (!from.exists()) {
                return nilError(new IOException("源文件不存在"));
            }
            File parent = to.getParentFile();
            if (parent != null && !parent.isDirectory() && !parent.mkdirs()) {
                return nilError(new IOException("无法创建目录"));
            }
            if (to.exists() && !to.delete()) {
                return nilError(new IOException("目标已存在且无法覆盖"));
            }
            if (!from.renameTo(to)) {
                return nilError(new IOException("重命名失败"));
            }
            return LuaValue.TRUE;
        }));
        file.set("stat", fn(args -> {
            File f = resolveFile(rt, args.checkjstring(1));
            if (!f.exists()) {
                return nilError(new IOException("文件不存在"));
            }
            LuaTable t = new LuaTable();
            t.set("size", LuaValue.valueOf(f.length()));
            t.set("mtime", LuaValue.valueOf(f.lastModified() / 1000));
            t.set("dir", LuaValue.valueOf(f.isDirectory()));
            t.set("file", LuaValue.valueOf(f.isFile()));
            return t;
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
            String raw = rt.prefs().getString(key, null);
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
                rt.prefs().edit().remove(key).apply();
            } else {
                rt.prefs().edit().putString(key, LuaConvert.toJson(value)).apply();
            }
            return LuaValue.TRUE;
        }));
        store.set("remove", fn(args -> {
            String key = args.checkjstring(1);
            if (key.length() > 256) {
                throw new LuaError("store key 不合法");
            }
            boolean existed = rt.prefs().contains(key);
            rt.prefs().edit().remove(key).apply();
            return existed ? LuaValue.TRUE : LuaValue.FALSE;
        }));
        store.set("keys", fn(args -> {
            List<String> keys = new ArrayList<>(rt.prefs().getAll().keySet());
            Collections.sort(keys);
            LuaTable t = new LuaTable();
            for (int i = 0; i < keys.size(); i++) {
                t.set(i + 1, keys.get(i));
            }
            return t;
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
            return rt.context().checkSelfPermission(name) == PackageManager.PERMISSION_GRANTED
                    ? LuaValue.TRUE : LuaValue.FALSE;
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
            rt.requireUi().activity().requestPermissionsFromLua(list, callback.isfunction() ? callback : null);
            return LuaValue.NIL;
        }));
        g.set("permission", permission);
    }

    private static void installUi(final LuaRuntime rt, Globals g) {
        LuaTable ui = new LuaTable();
        ui.set("toast", fn(args -> {
            String message = args.checkjstring(1);
            LuaValue duration = args.arg(2);
            boolean longDuration = false;
            if (duration.isstring()) {
                longDuration = "long".equalsIgnoreCase(duration.tojstring());
            } else if (duration.isnumber()) {
                longDuration = duration.toint() >= 2000;
            }
            rt.requireUi().activity().toastOnMain(message, longDuration);
            return LuaValue.NIL;
        }));
        ui.set("statusBar", fn(args -> {
            LuaTable opts = args.checktable(1);
            int color = parseColor(opts.get("color"), Color.TRANSPARENT);
            boolean light = opts.get("light").optboolean(false);
            rt.requireUi().activity().setStatusBarOnMain(color, light);
            return LuaValue.NIL;
        }));
        ui.set("alert", fn(args -> {
            rt.requireUi().activity().showAlert(args.checkjstring(1));
            return LuaValue.NIL;
        }));
        ui.set("confirm", fn(args -> {
            String message = args.checkjstring(1);
            final LuaValue callback = args.arg(2);
            rt.requireUi().activity().showConfirm(message, callback.isfunction() ? callback : null);
            return LuaValue.NIL;
        }));
        ui.set("fullscreen", fn(args -> {
            rt.requireUi().activity().setFullscreenOnMain(args.checkboolean(1));
            return LuaValue.NIL;
        }));
        ui.set("keepScreenOn", fn(args -> {
            rt.requireUi().activity().setKeepScreenOnOnMain(args.checkboolean(1));
            return LuaValue.NIL;
        }));
        g.set("ui", ui);
    }

    private static int parseColor(LuaValue value, int fallback) {
        if (value.isnil()) {
            return fallback;
        }
        if (value.isnumber()) {
            return value.toint();
        }
        if (value.isstring()) {
            try {
                return Color.parseColor(value.tojstring());
            } catch (IllegalArgumentException e) {
                throw new LuaError("颜色格式不合法: " + value.tojstring());
            }
        }
        throw new LuaError("颜色必须是字符串（如 \"#ffffff\"）或整数");
    }

    private static void installDevice(final LuaRuntime rt, Globals g) {
        LuaTable device = new LuaTable();
        device.set("clipboard", fn(args -> {
            MainActivity activity = rt.requireUi().activity();
            LuaValue value = args.arg(1);
            if (!value.isnil()) {
                if (!activity.setClipboardTextOnMain(value.tojstring())) {
                    throw new LuaError("写入剪贴板失败");
                }
                return LuaValue.TRUE;
            }
            return LuaValue.valueOf(activity.clipboardText());
        }));
        device.set("vibrate", fn(args -> {
            int ms = args.arg(1).optint(200);
            if (ms < 1 || ms > 10000) {
                throw new LuaError("vibrate 时长需在 1-10000 毫秒");
            }
            if (!vibrate(rt.context(), ms)) {
                throw new LuaError("缺少 VIBRATE 权限（在 app.json 的 permissions 中声明）");
            }
            return LuaValue.NIL;
        }));
        device.set("share", fn(args -> {
            String text = "";
            String url = "";
            LuaValue value = args.arg(1);
            if (value.istable()) {
                text = value.get("text").optjstring("");
                url = value.get("url").optjstring("");
            } else if (value.isstring()) {
                text = value.tojstring();
            }
            url = args.arg(2).optjstring(url);
            if (text.trim().isEmpty() && url.trim().isEmpty()) {
                throw new LuaError("device.share 需要 text 或 url");
            }
            rt.requireUi().activity().shareOnMain(text, url);
            return LuaValue.NIL;
        }));
        device.set("openUrl", fn(args -> {
            rt.requireUi().activity().openUrlOnMain(args.checkjstring(1));
            return LuaValue.NIL;
        }));
        device.set("orientation", fn(args -> {
            rt.requireUi().activity().setOrientationOnMain(args.checkjstring(1));
            return LuaValue.NIL;
        }));
        device.set("model", fn(args -> LuaValue.valueOf(Build.MODEL)));
        device.set("sdk", fn(args -> LuaValue.valueOf(Build.VERSION.SDK_INT)));
        device.set("platform", fn(args -> LuaValue.valueOf("android")));
        device.set("locale", fn(args -> LuaValue.valueOf(Locale.getDefault().toString())));
        device.set("screen", fn(args -> {
            DisplayMetrics m = rt.context().getResources().getDisplayMetrics();
            LuaTable t = new LuaTable();
            t.set("width", m.widthPixels);
            t.set("height", m.heightPixels);
            t.set("density", m.density);
            t.set("dpi", m.densityDpi);
            return t;
        }));
        g.set("device", device);
    }

    private static boolean vibrate(Context context, int ms) {
        if (context.checkSelfPermission(Manifest.permission.VIBRATE)
                != PackageManager.PERMISSION_GRANTED) {
            return false;
        }
        Vibrator vibrator;
        if (Build.VERSION.SDK_INT >= 31) {
            VibratorManager manager =
                    (VibratorManager) context.getSystemService(Context.VIBRATOR_MANAGER_SERVICE);
            vibrator = manager == null ? null : manager.getDefaultVibrator();
        } else {
            vibrator = (Vibrator) context.getSystemService(Context.VIBRATOR_SERVICE);
        }
        if (vibrator == null || !vibrator.hasVibrator()) {
            return true;
        }
        if (Build.VERSION.SDK_INT >= 26) {
            vibrator.vibrate(VibrationEffect.createOneShot(ms, VibrationEffect.DEFAULT_AMPLITUDE));
        } else {
            vibrator.vibrate(ms);
        }
        return true;
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
