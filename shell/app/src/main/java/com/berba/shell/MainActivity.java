package com.berba.shell;

import android.annotation.SuppressLint;
import android.app.Activity;
import android.app.AlertDialog;
import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Context;
import android.content.Intent;
import android.content.pm.ActivityInfo;
import android.content.res.Configuration;
import android.graphics.Color;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.os.SystemClock;
import android.util.Log;
import android.view.View;
import android.view.Window;
import android.view.WindowInsets;
import android.view.WindowInsetsController;
import android.view.WindowManager;
import android.webkit.JsPromptResult;
import android.webkit.JsResult;
import android.webkit.ValueCallback;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.widget.EditText;
import android.widget.FrameLayout;
import android.widget.ScrollView;
import android.widget.TextView;
import android.widget.Toast;
import android.window.OnBackInvokedCallback;
import android.window.OnBackInvokedDispatcher;

import org.json.JSONObject;
import org.luaj.vm2.LuaTable;
import org.luaj.vm2.LuaValue;

import java.io.PrintWriter;
import java.io.StringWriter;
import java.util.HashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;

/**
 * APK 宿主 Activity：代码构建 WebView，加载 assets/www，启动 Lua 运行时。
 */
public class MainActivity extends Activity {
    private static final String TAG = "berba";
    private static final int FILE_CHOOSER_REQUEST = 2001;

    private RuntimeManager manager;
    private UiSession session;
    private AppConfig config;
    private WebView webView;
    private JsBridge bridge;
    private LuaRuntime lua;
    private final Handler mainHandler = new Handler(Looper.getMainLooper());
    private final Map<Integer, PermRequest> pendingPermissions = new HashMap<>();
    private int nextRequestCode = 1001;
    private volatile boolean luaLoadRequested;
    private boolean fullscreen;
    private boolean resumed;
    private volatile String pageOrigin = "";
    private volatile String pendingDeepLink = "";
    private volatile boolean canGoBack;
    private Object backRegistration;
    private long lastBackAt;
    private ValueCallback<Uri[]> pendingFileChooser;
    private boolean statusBarCustom;
    private int statusBarColor;
    private boolean statusBarLight;

    /** 把 API 33 类型隔离到独立类，避免旧系统验证 MainActivity 时解析新类。 */
    private static final class BackApi33 {
        private BackApi33() {
        }

        static Object register(MainActivity activity) {
            OnBackInvokedCallback callback = activity::handleBack;
            activity.getOnBackInvokedDispatcher().registerOnBackInvokedCallback(
                    OnBackInvokedDispatcher.PRIORITY_DEFAULT, callback);
            return callback;
        }

        static void unregister(MainActivity activity, Object registration) {
            if (registration instanceof OnBackInvokedCallback) {
                activity.getOnBackInvokedDispatcher().unregisterOnBackInvokedCallback(
                        (OnBackInvokedCallback) registration);
            }
        }
    }

    private static final class PermRequest {
        final List<String> names;
        final LuaValue callback;

        PermRequest(List<String> names, LuaValue callback) {
            this.names = names;
            this.callback = callback;
        }
    }

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        try {
            setup();
        } catch (Throwable t) {
            Log.e(TAG, "应用启动失败", t);
            showFatalError(t);
        }
    }

    private void setup() {
        manager = RuntimeManager.get(this);
        config = manager.config();
        lua = manager.runtime();

        FrameLayout root = new FrameLayout(this);
        webView = WebViewGuard.create(this);
        webView.setLayoutParams(new FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.MATCH_PARENT, FrameLayout.LayoutParams.MATCH_PARENT));
        configureWebView();
        manager.captureNotification(getIntent());
        manager.captureWidget(getIntent());
        pendingDeepLink = deepLinkUrl(getIntent());
        session = manager.attach(this, this::onSessionCreated);
        bridge = new JsBridge(session);
        webView.addJavascriptInterface(bridge, JsBridge.NAME);
        webView.setWebViewClient(new AssetServer(session, config, bridge));
        webView.setWebChromeClient(new BridgeChromeClient(session));
        webView.setDownloadListener((url, userAgent, contentDisposition, mimeType, contentLength)
                -> handleDownload(url));
        root.addView(webView);
        applyInsets(root);
        setContentView(root);
        // 必须在 decor 创建后调用：Android 16 的 PhoneWindow.getInsetsController()
        // 在 mDecor 为 null 时会直接 NPE。
        setupWindow();
        registerBackCallback();
    }

    /** 启动期异常没有 WebView/Lua 可用，用纯文本页展示堆栈，便于无 adb 时截图反馈。 */
    private void showFatalError(Throwable error) {
        try {
            if (manager != null && session != null) {
                manager.detach(session);
                session = null;
            }
            lua = null;
            if (webView != null) {
                webView.removeJavascriptInterface(JsBridge.NAME);
                webView.destroy();
                webView = null;
            }
        } catch (Throwable ignored) {
        }
        try {
            StringWriter buffer = new StringWriter();
            PrintWriter writer = new PrintWriter(buffer);
            error.printStackTrace(writer);
            writer.flush();

            TextView text = new TextView(this);
            text.setText("应用启动失败\n请截图本页反馈（错误详情如下）\n\n"
                    + "设备: " + Build.MANUFACTURER + " " + Build.MODEL
                    + " / Android " + Build.VERSION.RELEASE + " (API " + Build.VERSION.SDK_INT + ")\n\n"
                    + buffer);
            text.setTextColor(Color.BLACK);
            text.setBackgroundColor(Color.WHITE);
            text.setTextIsSelectable(true);
            int pad = Math.round(16 * getResources().getDisplayMetrics().density);
            text.setPadding(pad, pad, pad, pad);

            ScrollView scroll = new ScrollView(this);
            scroll.setBackgroundColor(Color.WHITE);
            scroll.addView(text);
            setContentView(scroll);
        } catch (Throwable fatal) {
            Log.e(TAG, "无法展示启动错误", fatal);
            throw new RuntimeException(error);
        }
    }

    private void registerBackCallback() {
        if (Build.VERSION.SDK_INT >= 33) {
            backRegistration = BackApi33.register(this);
        }
    }

    private void setupWindow() {
        Window window = getWindow();
        if (Build.VERSION.SDK_INT >= 30) {
            window.setDecorFitsSystemWindows(false);
            window.setStatusBarColor(Color.TRANSPARENT);
            window.setNavigationBarColor(Color.TRANSPARENT);
        }
        if (Build.VERSION.SDK_INT >= 28) {
            // 允许内容在沉浸式全屏时延伸至刘海/挖孔，否则全屏后该区域会留白。
            WindowManager.LayoutParams attrs = window.getAttributes();
            attrs.layoutInDisplayCutoutMode =
                    WindowManager.LayoutParams.LAYOUT_IN_DISPLAY_CUTOUT_MODE_SHORT_EDGES;
            window.setAttributes(attrs);
        }
        applySystemBarAppearance(window);
    }

    @SuppressWarnings("deprecation")
    private void applySystemBarAppearance(Window window) {
        boolean dark = isDarkTheme();
        boolean lightStatus = statusBarCustom ? statusBarLight : !dark;
        if (statusBarCustom) {
            window.setStatusBarColor(statusBarColor);
        }
        if (Build.VERSION.SDK_INT >= 30) {
            WindowInsetsController controller = insetsController(window);
            if (controller == null) {
                return;
            }
            int mask = WindowInsetsController.APPEARANCE_LIGHT_STATUS_BARS
                    | WindowInsetsController.APPEARANCE_LIGHT_NAVIGATION_BARS;
            int appearance = 0;
            if (lightStatus) {
                appearance |= WindowInsetsController.APPEARANCE_LIGHT_STATUS_BARS;
            }
            if (!dark) {
                appearance |= WindowInsetsController.APPEARANCE_LIGHT_NAVIGATION_BARS;
            }
            controller.setSystemBarsAppearance(appearance, mask);
            return;
        }
        if (statusBarCustom) {
            View decor = window.getDecorView();
            int flags = decor.getSystemUiVisibility();
            if (lightStatus) {
                flags |= View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR;
            } else {
                flags &= ~View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR;
            }
            decor.setSystemUiVisibility(flags);
        }
    }

    private static WindowInsetsController insetsController(Window window) {
        try {
            return window.getInsetsController();
        } catch (RuntimeException e) {
            Log.w(TAG, "WindowInsetsController 不可用", e);
            return null;
        }
    }

    private boolean isDarkTheme() {
        String theme = config == null ? "light" : config.theme;
        if ("dark".equals(theme)) {
            return true;
        }
        if ("device".equals(theme)) {
            return (getResources().getConfiguration().uiMode & Configuration.UI_MODE_NIGHT_MASK)
                    == Configuration.UI_MODE_NIGHT_YES;
        }
        return false;
    }

    @SuppressWarnings("deprecation")
    private void applyInsets(View view) {
        if (Build.VERSION.SDK_INT < 30) {
            return;
        }
        view.setOnApplyWindowInsetsListener((v, insets) -> {
            android.graphics.Insets ime = insets.getInsets(WindowInsets.Type.ime());
            // 全屏时系统栏已隐藏，但仍会残留刘海/挖孔 inset；
            // 此时只保留输入法留白，其余方向铺满整个屏幕。
            if (fullscreen) {
                v.setPadding(0, 0, 0, ime.bottom);
                return insets;
            }
            android.graphics.Insets bars = insets.getInsets(
                    WindowInsets.Type.systemBars() | WindowInsets.Type.displayCutout());
            v.setPadding(bars.left, bars.top, bars.right, Math.max(bars.bottom, ime.bottom));
            return insets;
        });
        view.requestApplyInsets();
    }

    private void configureWebView() {
        WebSettings s = webView.getSettings();
        s.setJavaScriptEnabled(true);
        s.setDomStorageEnabled(true);
        s.setDatabaseEnabled(true);
        s.setAllowFileAccess(false);
        s.setAllowContentAccess(false);
        s.setAllowFileAccessFromFileURLs(false);
        s.setAllowUniversalAccessFromFileURLs(false);
        s.setMixedContentMode(WebSettings.MIXED_CONTENT_NEVER_ALLOW);
        s.setJavaScriptCanOpenWindowsAutomatically(false);
        s.setSupportMultipleWindows(false);
        s.setMediaPlaybackRequiresUserGesture(true);
        s.setBuiltInZoomControls(false);
        s.setDisplayZoomControls(false);
        webView.setOverScrollMode(View.OVER_SCROLL_NEVER);
        if (config.debug) {
            WebView.setWebContentsDebuggingEnabled(true);
        }
    }

    /** create 事件完成后再加载 startUrl（若 Lua 侧未主动 web.load）。 */
    private void onSessionCreated() {
        if (session == null || !session.isCurrent() || lua == null) {
            return;
        }
        if (!luaLoadRequested && webView != null) {
            webView.loadUrl(config.startUrl);
        }
        if (resumed) {
            lua.onAppEvent("resume");
        }
        deliverDeepLink();
        manager.deliverNotifications(lua);
        manager.deliverWidgets(lua);
    }

    private void deliverDeepLink() {
        String url = pendingDeepLink;
        if (url == null || url.isEmpty() || lua == null
                || session == null || !session.isCurrent()) {
            return;
        }
        pendingDeepLink = "";
        lua.onAppEvent("newIntent", LuaValue.valueOf(url));
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);
        if (manager != null && manager.captureNotification(intent)) {
            if (lua != null && session != null && session.isCurrent()) {
                manager.deliverNotifications(lua);
            }
            return;
        }
        if (manager != null && manager.captureWidget(intent)) {
            if (lua != null && session != null && session.isCurrent()) {
                manager.deliverWidgets(lua);
            }
            return;
        }
        String url = deepLinkUrl(intent);
        if (url.isEmpty()) {
            return;
        }
        if (lua != null && session != null && session.isCurrent()) {
            lua.onAppEvent("newIntent", LuaValue.valueOf(url));
        } else {
            pendingDeepLink = url;
        }
    }

    private static String deepLinkUrl(Intent intent) {
        if (intent == null) {
            return "";
        }
        Uri data = intent.getData();
        if (data == null
                || NotifyController.NOTIFY_SCHEME.equalsIgnoreCase(data.getScheme())
                || WidgetController.SCHEME.equalsIgnoreCase(data.getScheme())) {
            return "";
        }
        return data.toString();
    }

    @Override
    protected void onPause() {
        super.onPause();
        resumed = false;
        if (webView != null) {
            webView.onPause();
        }
        if (lua != null && session != null && session.isCurrent()) {
            lua.onAppEvent("pause");
        }
    }

    @Override
    protected void onResume() {
        super.onResume();
        resumed = true;
        if (fullscreen) {
            // 附着后再应用一次，覆盖进入前后台或早期调用时 controller 尚不可用的情况。
            applyFullscreen(true);
        } else if (Build.VERSION.SDK_INT >= 30) {
            // 窗口已附着，此时 controller 才可用（主题为 device 时也靠这里更新）。
            applySystemBarAppearance(getWindow());
        }
        if (webView != null) {
            webView.onResume();
        }
        if (lua != null && session != null && session.isCurrent()) {
            lua.onAppEvent("resume");
        }
    }

    @Override
    protected void onDestroy() {
        if (backRegistration != null) {
            BackApi33.unregister(this, backRegistration);
            backRegistration = null;
        }
        // 只解绑当前界面；Lua 运行时与在途 HTTP 在进程内继续存活。
        if (manager != null && session != null) {
            manager.detach(session);
            session = null;
        }
        lua = null;
        synchronized (pendingPermissions) {
            pendingPermissions.clear();
        }
        if (pendingFileChooser != null) {
            pendingFileChooser.onReceiveValue(null);
            pendingFileChooser = null;
        }
        if (webView != null) {
            webView.removeJavascriptInterface(JsBridge.NAME);
            webView.destroy();
            webView = null;
        }
        super.onDestroy();
    }

    @Override
    public void onConfigurationChanged(Configuration newConfig) {
        super.onConfigurationChanged(newConfig);
        if (fullscreen) {
            mainHandler.post(() -> applyFullscreen(true));
        } else if (Build.VERSION.SDK_INT >= 30) {
            mainHandler.post(() -> applySystemBarAppearance(getWindow()));
        }
    }

    @SuppressLint("MissingSuperCall")
    @Override
    public void onBackPressed() {
        handleBack();
    }

    private void handleBack() {
        long now = SystemClock.uptimeMillis();
        if (now - lastBackAt < 500) {
            return;
        }
        lastBackAt = now;
        if (lua != null && lua.hasHandlers("back")) {
            lua.onBack(this::defaultBack);
        } else {
            defaultBack();
        }
    }

    private void defaultBack() {
        if (!uiActive()) {
            return;
        }
        if (webView != null && webView.canGoBack()) {
            webView.goBack();
        } else {
            finish();
        }
    }

    private boolean uiActive() {
        return session != null && session.isCurrent();
    }

    AppConfig config() {
        return config;
    }

    // ---- 页面状态 ----

    void setPageOrigin(String origin) {
        pageOrigin = origin == null ? "" : origin;
    }

    String pageOrigin() {
        return pageOrigin;
    }

    void updateCanGoBack(boolean value) {
        canGoBack = value;
    }

    // ---- WebView 控制（可在任意线程调用） ----

    void loadFromLua(final String url) {
        if (!isHttpUrl(url)) {
            throw new IllegalArgumentException("web.load 只支持 http/https");
        }
        android.net.Uri uri = android.net.Uri.parse(url);
        if ("http".equalsIgnoreCase(uri.getScheme()) && !config.cleartext) {
            throw new IllegalArgumentException("明文 HTTP 未开启（cleartext: false）");
        }
        String origin = AssetServer.originOf(uri);
        if (!config.originAllowed(origin)) {
            throw new IllegalArgumentException("origin 不在白名单: " + origin);
        }
        luaLoadRequested = true;
        final UiSession s = session;
        runOnUiThread(() -> {
            if (s != null && s.isCurrent() && webView != null) {
                webView.loadUrl(url);
            }
        });
    }

    void reloadOnMain() {
        final UiSession s = session;
        runOnUiThread(() -> {
            if (s != null && s.isCurrent() && webView != null) {
                webView.reload();
            }
        });
    }

    void goBackOnMain() {
        final UiSession s = session;
        runOnUiThread(() -> {
            if (s != null && s.isCurrent() && webView != null && webView.canGoBack()) {
                webView.goBack();
            }
        });
    }

    boolean canGoBackCached() {
        return canGoBack;
    }

    void evaluateJs(final String script, final LuaValue callback) {
        final UiSession s = session;
        final LuaRuntime runtime = lua;
        runOnUiThread(() -> {
            if (s == null || !s.isCurrent() || webView == null) {
                return;
            }
            webView.evaluateJavascript(script, value -> {
                if (callback != null && runtime != null) {
                    runtime.post(() -> {
                        if (!s.isCurrent()) {
                            return;
                        }
                        try {
                            callback.invoke(LuaValue.valueOf(value == null ? "null" : value));
                        } catch (Throwable t) {
                            Log.e(TAG, "web.eval 回调错误", t);
                        }
                    });
                }
            });
        });
    }

    void deliverBridgeResult(final String reqId, final boolean ok, final String payload) {
        final UiSession s = session;
        runOnUiThread(() -> {
            if (s == null || !s.isCurrent() || webView == null) {
                return;
            }
            String js = "window.__berbaResolve(" + JSONObject.quote(reqId) + ","
                    + (ok ? "true" : "false") + ","
                    + JSONObject.quote(payload == null ? "null" : payload) + ");";
            webView.evaluateJavascript(js, null);
        });
    }

    void onPageStartedEvent(final String url) {
        if (lua != null && uiActive()) {
            lua.onAppEvent("pageStarted", LuaValue.valueOf(url));
        }
    }

    void onPageFinishedEvent(final String url) {
        if (lua != null && uiActive()) {
            lua.onAppEvent("pageFinished", LuaValue.valueOf(url));
        }
    }

    void onWebErrorEvent(final String description, final String url) {
        if (lua != null && uiActive()) {
            lua.onAppEvent("error", LuaValue.valueOf(description), LuaValue.valueOf(url));
        }
    }

    // ---- UI ----

    void toastOnMain(final String message, final boolean longDuration) {
        final UiSession s = session;
        runOnUiThread(() -> {
            if (s != null && s.isCurrent()) {
                Toast.makeText(this, message,
                        longDuration ? Toast.LENGTH_LONG : Toast.LENGTH_SHORT).show();
            }
        });
    }

    void setStatusBarOnMain(final int color, final boolean light) {
        final UiSession s = session;
        runOnUiThread(() -> {
            if (s == null || !s.isCurrent()) {
                return;
            }
            statusBarCustom = true;
            statusBarColor = color;
            statusBarLight = light;
            applySystemBarAppearance(getWindow());
        });
    }

    void showAlert(final String message) {
        final UiSession s = session;
        runOnUiThread(() -> {
            if (s == null || !s.isCurrent() || isFinishing()) {
                return;
            }
            new AlertDialog.Builder(this)
                    .setMessage(message)
                    .setPositiveButton(android.R.string.ok, null)
                    .show();
        });
    }

    void showConfirm(final String message, final LuaValue callback) {
        final UiSession s = session;
        runOnUiThread(() -> {
            if (s == null || !s.isCurrent() || isFinishing()) {
                return;
            }
            new AlertDialog.Builder(this)
                    .setMessage(message)
                    .setPositiveButton(android.R.string.ok, (d, w) -> replyConfirm(s, callback, true))
                    .setNegativeButton(android.R.string.cancel, (d, w) -> replyConfirm(s, callback, false))
                    .setOnCancelListener(d -> replyConfirm(s, callback, false))
                    .show();
        });
    }

    private void replyConfirm(final UiSession s, final LuaValue callback, final boolean value) {
        final LuaRuntime runtime = lua;
        if (callback == null || runtime == null || s == null || !s.isCurrent()) {
            return;
        }
        runtime.post(() -> {
            if (!s.isCurrent()) {
                return;
            }
            try {
                callback.invoke(value ? LuaValue.TRUE : LuaValue.FALSE);
            } catch (Throwable t) {
                Log.e(TAG, "ui.confirm 回调错误", t);
            }
        });
    }

    void showJsAlert(final String message, final JsResult result) {
        final UiSession s = session;
        runOnUiThread(() -> {
            if (s == null || !s.isCurrent() || isFinishing() || isDestroyed()) {
                result.cancel();
                return;
            }
            new AlertDialog.Builder(this)
                    .setMessage(message)
                    .setOnCancelListener(d -> result.cancel())
                    .setPositiveButton(android.R.string.ok, (d, w) -> result.confirm())
                    .show();
        });
    }

    void showJsConfirm(final String message, final JsResult result) {
        final UiSession s = session;
        runOnUiThread(() -> {
            if (s == null || !s.isCurrent() || isFinishing() || isDestroyed()) {
                result.cancel();
                return;
            }
            new AlertDialog.Builder(this)
                    .setMessage(message)
                    .setOnCancelListener(d -> result.cancel())
                    .setPositiveButton(android.R.string.ok, (d, w) -> result.confirm())
                    .setNegativeButton(android.R.string.cancel, (d, w) -> result.cancel())
                    .show();
        });
    }

    void showJsPrompt(final String message, final String defaultValue, final JsPromptResult result) {
        final UiSession s = session;
        runOnUiThread(() -> {
            if (s == null || !s.isCurrent() || isFinishing() || isDestroyed()) {
                result.cancel();
                return;
            }
            final EditText input = new EditText(this);
            input.setText(defaultValue == null ? "" : defaultValue);
            new AlertDialog.Builder(this)
                    .setMessage(message)
                    .setView(input)
                    .setOnCancelListener(d -> result.cancel())
                    .setPositiveButton(android.R.string.ok,
                            (d, w) -> result.confirm(input.getText().toString()))
                    .setNegativeButton(android.R.string.cancel, (d, w) -> result.cancel())
                    .show();
        });
    }

    void onRendererGone(final String reason) {
        runOnUiThread(() -> showFatalError(new RuntimeException("WebView 渲染进程退出: " + reason)));
    }

    void finishOnMain() {
        final UiSession s = session;
        runOnUiThread(() -> {
            if (s != null && s.isCurrent()) {
                finish();
            }
        });
    }

    void setFullscreenOnMain(final boolean on) {
        final UiSession s = session;
        runOnUiThread(() -> {
            if (s == null || !s.isCurrent()) {
                return;
            }
            fullscreen = on;
            applyFullscreen(on);
        });
    }

    @SuppressWarnings("deprecation")
    private void applyFullscreen(boolean on) {
        Window window = getWindow();
        if (Build.VERSION.SDK_INT >= 30) {
            WindowInsetsController controller = insetsController(window);
            if (controller == null) {
                return;
            }
            if (on) {
                controller.hide(WindowInsets.Type.systemBars());
                controller.setSystemBarsBehavior(
                        WindowInsetsController.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE);
            } else {
                controller.show(WindowInsets.Type.systemBars());
            }
            // 立即让 insets 监听器按新的全屏状态刷新留白。
            window.getDecorView().requestApplyInsets();
            return;
        }
        View decor = window.getDecorView();
        if (on) {
            decor.setSystemUiVisibility(
                    View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
                            | View.SYSTEM_UI_FLAG_FULLSCREEN
                            | View.SYSTEM_UI_FLAG_HIDE_NAVIGATION
                            | View.SYSTEM_UI_FLAG_LAYOUT_STABLE
                            | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                            | View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION);
        } else {
            decor.setSystemUiVisibility(View.SYSTEM_UI_FLAG_LAYOUT_STABLE);
        }
    }

    void setKeepScreenOnOnMain(final boolean on) {
        final UiSession s = session;
        runOnUiThread(() -> {
            if (s == null || !s.isCurrent()) {
                return;
            }
            if (on) {
                getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
            } else {
                getWindow().clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
            }
        });
    }

    void setOrientationOnMain(final String mode) {
        final int orientation;
        switch (mode == null ? "" : mode) {
            case "portrait":
                orientation = ActivityInfo.SCREEN_ORIENTATION_PORTRAIT;
                break;
            case "landscape":
                orientation = ActivityInfo.SCREEN_ORIENTATION_LANDSCAPE;
                break;
            case "sensor":
                orientation = ActivityInfo.SCREEN_ORIENTATION_SENSOR;
                break;
            case "auto":
                orientation = ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED;
                break;
            default:
                throw new IllegalArgumentException("未知方向: " + mode);
        }
        final UiSession s = session;
        runOnUiThread(() -> {
            if (s != null && s.isCurrent()) {
                setRequestedOrientation(orientation);
            }
        });
    }

    // ---- 权限 ----

    void requestPermissionsFromLua(final List<String> names, final LuaValue callback) {
        final int code;
        synchronized (pendingPermissions) {
            code = nextRequestCode++;
            pendingPermissions.put(code, new PermRequest(names, callback));
        }
        final UiSession s = session;
        runOnUiThread(() -> {
            if (s != null && s.isCurrent()) {
                requestPermissions(names.toArray(new String[0]), code);
            }
        });
    }

    @Override
    public void onRequestPermissionsResult(int requestCode, String[] permissions, int[] grantResults) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults);
        final PermRequest req;
        synchronized (pendingPermissions) {
            req = pendingPermissions.remove(requestCode);
        }
        if (req == null) {
            return;
        }
        final LuaTable result = new LuaTable();
        for (int i = 0; i < permissions.length && i < grantResults.length; i++) {
            result.set(permissions[i], grantResults[i] == android.content.pm.PackageManager.PERMISSION_GRANTED
                    ? LuaValue.TRUE : LuaValue.FALSE);
        }
        final LuaRuntime runtime = lua;
        final UiSession s = session;
        if (req.callback != null && runtime != null && s != null && s.isCurrent()) {
            runtime.post(() -> {
                if (!s.isCurrent()) {
                    return;
                }
                try {
                    req.callback.invoke(result);
                } catch (Throwable t) {
                    Log.e(TAG, "permission 回调错误", t);
                }
            });
        }
    }

    void openFileChooser(final ValueCallback<Uri[]> callback, final Intent intent) {
        final UiSession s = session;
        runOnUiThread(() -> {
            if (s == null || !s.isCurrent() || isFinishing() || isDestroyed()) {
                callback.onReceiveValue(null);
                return;
            }
            if (pendingFileChooser != null) {
                pendingFileChooser.onReceiveValue(null);
            }
            pendingFileChooser = callback;
            try {
                startActivityForResult(intent, FILE_CHOOSER_REQUEST);
            } catch (Exception e) {
                Log.w(TAG, "无法打开文件选择器", e);
                pendingFileChooser = null;
                callback.onReceiveValue(null);
                Toast.makeText(this, "没有可用的文件选择器", Toast.LENGTH_SHORT).show();
            }
        });
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        if (requestCode == FILE_CHOOSER_REQUEST) {
            ValueCallback<Uri[]> callback = pendingFileChooser;
            pendingFileChooser = null;
            if (callback == null) {
                return;
            }
            if (!uiActive()) {
                callback.onReceiveValue(null);
                return;
            }
            Uri[] result = null;
            if (resultCode == RESULT_OK && data != null) {
                ClipData clip = data.getClipData();
                if (clip != null && clip.getItemCount() > 0) {
                    result = new Uri[clip.getItemCount()];
                    for (int i = 0; i < clip.getItemCount(); i++) {
                        result[i] = clip.getItemAt(i).getUri();
                    }
                } else if (data.getData() != null) {
                    result = new Uri[]{data.getData()};
                }
            }
            callback.onReceiveValue(result);
            return;
        }
        super.onActivityResult(requestCode, resultCode, data);
    }

    // ---- 其他 ----

    String clipboardText() {
        ClipboardManager cm = (ClipboardManager) getSystemService(Context.CLIPBOARD_SERVICE);
        if (cm == null || !cm.hasPrimaryClip()) {
            return "";
        }
        ClipData clip = cm.getPrimaryClip();
        if (clip == null || clip.getItemCount() == 0) {
            return "";
        }
        CharSequence text = clip.getItemAt(0).coerceToText(this);
        return text == null ? "" : text.toString();
    }

    boolean setClipboardTextOnMain(final String text) {
        final UiSession s = session;
        if (text == null || s == null || !s.isCurrent()) {
            return false;
        }
        runOnUiThread(() -> {
            if (!s.isCurrent()) {
                return;
            }
            ClipboardManager cm = (ClipboardManager) getSystemService(Context.CLIPBOARD_SERVICE);
            if (cm != null) {
                cm.setPrimaryClip(ClipData.newPlainText("berba", text));
            }
        });
        return true;
    }

    void shareOnMain(final String text, final String url) {
        StringBuilder content = new StringBuilder();
        if (text != null && !text.isEmpty()) {
            content.append(text);
        }
        if (url != null && !url.isEmpty()) {
            if (content.length() > 0) {
                content.append('\n');
            }
            content.append(url);
        }
        final String payload = content.toString();
        final UiSession s = session;
        runOnUiThread(() -> {
            if (s == null || !s.isCurrent()) {
                return;
            }
            Intent send = new Intent(Intent.ACTION_SEND);
            send.setType("text/plain");
            send.putExtra(Intent.EXTRA_TEXT, payload);
            try {
                startActivity(Intent.createChooser(send, null));
            } catch (Exception e) {
                Log.w(TAG, "无法打开分享面板", e);
                Toast.makeText(this, "没有可用的分享应用", Toast.LENGTH_SHORT).show();
            }
        });
    }

    void handleDownload(final String url) {
        if (!isHttpUrl(url)) {
            Log.w(TAG, "忽略非 http(s) 下载: " + url);
            return;
        }
        openUrlOnMain(url);
    }

    void openUrlOnMain(final String url) {
        if (!isHttpUrl(url)) {
            throw new IllegalArgumentException("只支持 http/https");
        }
        final Uri uri = Uri.parse(url);
        final UiSession s = session;
        runOnUiThread(() -> {
            if (s == null || !s.isCurrent()) {
                return;
            }
            Intent view = new Intent(Intent.ACTION_VIEW, uri);
            if (view.resolveActivity(getPackageManager()) == null) {
                Toast.makeText(this, "没有可打开该链接的应用", Toast.LENGTH_SHORT).show();
                return;
            }
            try {
                startActivity(view);
            } catch (Exception e) {
                Log.w(TAG, "无法打开链接", e);
                Toast.makeText(this, "无法打开链接", Toast.LENGTH_SHORT).show();
            }
        });
    }

    static boolean isHttpUrl(String url) {
        if (url == null) {
            return false;
        }
        String lower = url.toLowerCase(Locale.ROOT);
        return lower.startsWith("http://") || lower.startsWith("https://");
    }
}
