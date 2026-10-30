package com.berba.shell;

import android.graphics.Bitmap;
import android.net.Uri;
import android.util.Log;
import android.webkit.RenderProcessGoneDetail;
import android.webkit.WebResourceError;
import android.webkit.WebResourceRequest;
import android.webkit.WebResourceResponse;
import android.webkit.WebView;
import android.webkit.WebViewClient;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.util.HashMap;
import java.util.Locale;
import java.util.Map;

/**
 * 本地页面服务器 + 导航白名单。
 * https://appassets.androidhost/* -> assets/www/*，并注入 JS 桥。
 */
final class AssetServer extends WebViewClient {
    private static final String TAG = "berba";
    private static final int MAX_ASSET = 64 * 1024 * 1024;

    private final UiSession session;
    private final AppConfig config;
    private final byte[] shim;
    private final byte[] shimTag;

    AssetServer(UiSession session, AppConfig config, JsBridge bridge) {
        this.session = session;
        this.config = config;
        this.shim = bridge.shim().getBytes(StandardCharsets.UTF_8);
        this.shimTag = bridge.shimTag().getBytes(StandardCharsets.US_ASCII);
    }

    @Override
    public WebResourceResponse shouldInterceptRequest(WebView view, WebResourceRequest request) {
        Uri uri = request.getUrl();
        if (!isLocal(uri)) {
            return null;
        }
        String method = request.getMethod();
        if (method != null && !"GET".equalsIgnoreCase(method) && !"HEAD".equalsIgnoreCase(method)) {
            return textResponse(405, "Method Not Allowed", "Method Not Allowed");
        }
        String path = uri.getPath();
        if (path == null || path.isEmpty()) {
            path = "/";
        }
        if (("/" + JsBridge.SHIM_PATH).equals(path)) {
            return new WebResourceResponse("application/javascript", "utf-8", 200, "OK",
                    headers(), new ByteArrayInputStream(shim));
        }
        String rel = sanitize(path);
        if (rel == null) {
            return textResponse(404, "Not Found", "Not Found");
        }
        byte[] data = readAsset("www/" + rel);
        String mime = mimeOf(rel);
        if (data == null) {
            if (config.spa && !hasExtension(rel)) {
                data = readAsset("www/index.html");
                mime = "text/html";
            }
            if (data == null) {
                return textResponse(404, "Not Found", "Not Found");
            }
        }
        if ("text/html".equals(mime)) {
            data = injectShim(data);
        }
        return new WebResourceResponse(mime, isText(mime) ? "utf-8" : null, 200, "OK",
                headers(), new ByteArrayInputStream(data));
    }

    private boolean isLocal(Uri uri) {
        String scheme = uri.getScheme();
        String host = uri.getHost();
        return "https".equalsIgnoreCase(scheme) && AppConfig.LOCAL_HOST.equalsIgnoreCase(host);
    }

    /** 去除空段与 "."，拒绝 ".."、反斜杠和 NUL；目录补 index.html。 */
    static String sanitize(String path) {
        StringBuilder sb = new StringBuilder();
        for (String seg : path.split("/")) {
            if (seg.isEmpty() || ".".equals(seg)) {
                continue;
            }
            if ("..".equals(seg) || seg.indexOf('\\') >= 0 || seg.indexOf('\0') >= 0) {
                return null;
            }
            if (sb.length() > 0) {
                sb.append('/');
            }
            sb.append(seg);
        }
        String rel = sb.toString();
        if (rel.isEmpty()) {
            return "index.html";
        }
        if (path.endsWith("/")) {
            rel = rel + "/index.html";
        }
        return rel;
    }

    private byte[] readAsset(String path) {
        try (InputStream in = session.activity().getAssets().open(path)) {
            ByteArrayOutputStream out = new ByteArrayOutputStream();
            byte[] buf = new byte[8192];
            int total = 0;
            int n;
            while ((n = in.read(buf)) > 0) {
                total += n;
                if (total > MAX_ASSET) {
                    Log.w(TAG, "资源过大: " + path);
                    return null;
                }
                out.write(buf, 0, n);
            }
            return out.toByteArray();
        } catch (IOException e) {
            return null;
        }
    }

    private byte[] injectShim(byte[] html) {
        int limit = Math.min(html.length, 8192);
        int insert = -1;
        int head = indexOfIgnoreCase(html, "<head", limit);
        if (head >= 0) {
            int gt = indexOf(html, (byte) '>', head);
            if (gt >= 0) {
                insert = gt + 1;
            }
        }
        if (insert < 0) {
            int htmlTag = indexOfIgnoreCase(html, "<html", limit);
            if (htmlTag >= 0) {
                int gt = indexOf(html, (byte) '>', htmlTag);
                if (gt >= 0) {
                    insert = gt + 1;
                }
            }
        }
        if (insert < 0) {
            insert = 0;
        }
        byte[] out = new byte[html.length + shimTag.length];
        System.arraycopy(html, 0, out, 0, insert);
        System.arraycopy(shimTag, 0, out, insert, shimTag.length);
        System.arraycopy(html, insert, out, insert + shimTag.length, html.length - insert);
        return out;
    }

    private static int indexOf(byte[] data, byte b, int from) {
        for (int i = Math.max(0, from); i < data.length; i++) {
            if (data[i] == b) {
                return i;
            }
        }
        return -1;
    }

    private static int indexOfIgnoreCase(byte[] data, String needle, int limit) {
        int n = needle.length();
        for (int i = 0; i + n <= limit; i++) {
            boolean match = true;
            for (int j = 0; j < n; j++) {
                byte c = data[i + j];
                if (c >= 'A' && c <= 'Z') {
                    c = (byte) (c + 32);
                }
                if (c != needle.charAt(j)) {
                    match = false;
                    break;
                }
            }
            if (match) {
                return i;
            }
        }
        return -1;
    }

    private static boolean hasExtension(String rel) {
        int slash = rel.lastIndexOf('/');
        int dot = rel.lastIndexOf('.');
        return dot > slash;
    }

    private static Map<String, String> headers() {
        Map<String, String> h = new HashMap<>();
        h.put("Cache-Control", "no-cache");
        h.put("X-Content-Type-Options", "nosniff");
        return h;
    }

    private static WebResourceResponse textResponse(int status, String reason, String body) {
        return new WebResourceResponse("text/plain", "utf-8", status, reason, headers(),
                new ByteArrayInputStream(body.getBytes(StandardCharsets.UTF_8)));
    }

    private static boolean isText(String mime) {
        return mime.startsWith("text/") || mime.contains("json") || mime.contains("javascript")
                || mime.contains("xml") || mime.contains("svg");
    }

    static String mimeOf(String name) {
        int dot = name.lastIndexOf('.');
        String ext = dot < 0 ? "" : name.substring(dot + 1).toLowerCase(Locale.ROOT);
        switch (ext) {
            case "html":
            case "htm":
                return "text/html";
            case "js":
            case "mjs":
                return "application/javascript";
            case "css":
                return "text/css";
            case "json":
            case "map":
                return "application/json";
            case "txt":
                return "text/plain";
            case "xml":
                return "application/xml";
            case "svg":
                return "image/svg+xml";
            case "png":
                return "image/png";
            case "jpg":
            case "jpeg":
                return "image/jpeg";
            case "gif":
                return "image/gif";
            case "webp":
                return "image/webp";
            case "ico":
                return "image/x-icon";
            case "woff":
                return "font/woff";
            case "woff2":
                return "font/woff2";
            case "ttf":
                return "font/ttf";
            case "otf":
                return "font/otf";
            case "wasm":
                return "application/wasm";
            case "mp3":
                return "audio/mpeg";
            case "mp4":
                return "video/mp4";
            case "webm":
                return "video/webm";
            default:
                return "application/octet-stream";
        }
    }

    @Override
    public boolean shouldOverrideUrlLoading(WebView view, WebResourceRequest request) {
        return !allowed(request.getUrl());
    }

    @SuppressWarnings("deprecation")
    @Override
    public boolean shouldOverrideUrlLoading(WebView view, String url) {
        return !allowed(Uri.parse(url));
    }

    private boolean allowed(Uri uri) {
        String scheme = uri.getScheme();
        if (scheme == null) {
            return false;
        }
        scheme = scheme.toLowerCase(Locale.ROOT);
        if (!"https".equals(scheme) && !"http".equals(scheme)) {
            return false;
        }
        if ("http".equals(scheme) && !config.cleartext) {
            return false;
        }
        return config.originAllowed(originOf(uri));
    }

    static String originOf(Uri uri) {
        String scheme = uri.getScheme();
        String host = uri.getHost();
        if (scheme == null || host == null) {
            return "";
        }
        scheme = scheme.toLowerCase(Locale.ROOT);
        host = host.toLowerCase(Locale.ROOT);
        int port = uri.getPort();
        boolean defaultPort = port == -1
                || ("https".equals(scheme) && port == 443)
                || ("http".equals(scheme) && port == 80);
        return defaultPort ? scheme + "://" + host : scheme + "://" + host + ":" + port;
    }

    @Override
    public void onPageStarted(WebView view, String url, Bitmap favicon) {
        super.onPageStarted(view, url, favicon);
        if (!session.isCurrent()) {
            return;
        }
        MainActivity activity = session.activity();
        Uri uri = Uri.parse(url);
        String scheme = uri.getScheme();
        if (!"http".equalsIgnoreCase(scheme) && !"https".equalsIgnoreCase(scheme)) {
            activity.setPageOrigin("");
            return;
        }
        String origin = originOf(uri);
        if (!allowed(uri)) {
            activity.setPageOrigin("");
            view.stopLoading();
            activity.onWebErrorEvent("导航被拒绝：origin 不在白名单", url);
            return;
        }
        activity.setPageOrigin(origin);
        view.evaluateJavascript(new String(shim, StandardCharsets.UTF_8), null);
        activity.onPageStartedEvent(url);
    }

    @Override
    public void onPageFinished(WebView view, String url) {
        super.onPageFinished(view, url);
        if (!session.isCurrent()) {
            return;
        }
        MainActivity activity = session.activity();
        Uri uri = Uri.parse(url);
        if (allowed(uri)) {
            activity.setPageOrigin(originOf(uri));
            // onPageStarted 时注入可能被新文档丢弃，结束时再补一次（shim 内部幂等）。
            view.evaluateJavascript(new String(shim, StandardCharsets.UTF_8), null);
        }
        activity.onPageFinishedEvent(url);
    }

    @Override
    public void doUpdateVisitedHistory(WebView view, String url, boolean isReload) {
        super.doUpdateVisitedHistory(view, url, isReload);
        if (session.isCurrent()) {
            session.activity().updateCanGoBack(view.canGoBack());
        }
    }

    @Override
    public void onReceivedHttpError(WebView view, WebResourceRequest request,
                                    WebResourceResponse errorResponse) {
        super.onReceivedHttpError(view, request, errorResponse);
        if (session.isCurrent() && request.isForMainFrame()) {
            session.activity().onWebErrorEvent("页面返回 HTTP " + errorResponse.getStatusCode(),
                    request.getUrl().toString());
        }
    }

    @Override
    public boolean onRenderProcessGone(WebView view, RenderProcessGoneDetail detail) {
        String reason = detail == null ? "unknown" : "crash=" + detail.didCrash();
        Log.e(TAG, "WebView 渲染进程退出: " + reason);
        if (session.isCurrent()) {
            session.activity().onRendererGone(reason);
        }
        return true;
    }

    @Override
    public void onReceivedError(WebView view, WebResourceRequest request, WebResourceError error) {
        super.onReceivedError(view, request, error);
        if (session.isCurrent() && request.isForMainFrame()) {
            CharSequence desc = error.getDescription();
            session.activity().onWebErrorEvent(desc == null ? "页面加载失败" : desc.toString(),
                    request.getUrl().toString());
        }
    }
}
