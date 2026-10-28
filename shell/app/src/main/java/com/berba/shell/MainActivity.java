package com.berba.shell;

import android.annotation.SuppressLint;
import android.app.Activity;
import android.app.AlertDialog;
import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Context;
import android.content.SharedPreferences;
import android.content.pm.ActivityInfo;
import android.content.res.Configuration;
import android.graphics.Color;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.util.Log;
import android.view.View;
import android.view.Window;
import android.view.WindowInsets;
import android.view.WindowInsetsController;
import android.view.WindowManager;
import android.webkit.WebSettings;
import android.webkit.WebView;
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
import java.util.concurrent.Callable;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

/**
 * APK 宿主 Activity：代码构建 WebView，加载 assets/www，启动 Lua 运行时。
 */
public class MainActivity extends Activity {
    private static final String TAG = "berba";
    private static final String PREFS = "berba";

    private AppConfig config;
    private WebView webView;
    private JsBridge bridge;
    private LuaRuntime lua;
    private SharedPreferences prefs;
    private final Handler mainHandler = new Handler(Looper.getMainLooper());
    private final Map<Integer, PermRequest> pendingPermissions = new HashMap<>();
    private int nextRequestCode = 1001;
    private volatile boolean luaLoadRequested;
    private boolean fullscreen;

    /** 把 API 33 类型隔离到独立类，避免旧系统验证 MainActivity 时解析新类。 */
    private static final class BackApi33 {
        private static OnBackInvokedCallback callback;

        static void register(MainActivity activity) {
            callback = activity::handleBack;
            activity.getOnBackInvokedDispatcher().registerOnBackInvokedCallback(
                    OnBackInvokedDispatcher.PRIORITY_DEFAULT, callback);
        }

        static void unregister(MainActivity activity) {
            if (callback != null) {
                activity.getOnBackInvokedDispatcher().unregisterOnBackInvokedCallback(callback);
                callback = null;
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
        config = AppConfig.load(this);
        prefs = getSharedPreferences(PREFS, Context.MODE_PRIVATE);

        FrameLayout root = new FrameLayout(this);
        webView = WebViewGuard.create(this);
        webView.setLayoutParams(new FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.MATCH_PARENT, FrameLayout.LayoutParams.MATCH_PARENT));
        configureWebView();
        lua = new LuaRuntime(this, config);
        bridge = new JsBridge(this, lua);
        webView.addJavascriptInterface(bridge, JsBridge.NAME);
        webView.setWebViewClient(new AssetServer(this, config, bridge));
        root.addView(webView);
        applyInsets(root);
        setContentView(root);
        // 必须在 decor 创建后调用：Android 16 的 PhoneWindow.getInsetsController()
        // 在 mDecor 为 null 时会直接 NPE。
        setupWindow();
        registerBackCallback();

        lua.start(this::onLuaReady);
    }

    /** 启动期异常没有 WebView/Lua 可用，用纯文本页展示堆栈，便于无 adb 时截图反馈。 */
    private void showFatalError(Throwable error) {
        try {
            if (lua != null) {
                lua.shutdown();
                lua = null;
            }
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
            BackApi33.register(this);
        }
    }

    private void setupWindow() {
        Window window = getWindow();
        if (Build.VERSION.SDK_INT >= 30) {
            window.setDecorFitsSystemWindows(false);
            window.setStatusBarColor(Color.TRANSPARENT);
            window.setNavigationBarColor(Color.TRANSPARENT);
            applySystemBarAppearance(window);
        }
    }

    private void applySystemBarAppearance(Window window) {
        WindowInsetsController controller = insetsController(window);
        if (controller == null) {
            return;
        }
        boolean dark = isDarkTheme();
        int mask = WindowInsetsController.APPEARANCE_LIGHT_STATUS_BARS
                | WindowInsetsController.APPEARANCE_LIGHT_NAVIGATION_BARS;
        controller.setSystemBarsAppearance(dark ? 0 : mask, mask);
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
            android.graphics.Insets bars = insets.getInsets(
                    WindowInsets.Type.systemBars() | WindowInsets.Type.displayCutout());
            android.graphics.Insets ime = insets.getInsets(WindowInsets.Type.ime());
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

    private void onLuaReady() {
        if (lua == null) {
            return;
        }
        lua.onAppEvent("create", () -> {
            if (!luaLoadRequested && webView != null) {
                webView.loadUrl(config.startUrl);
            }
        });
    }

    @Override
    protected void onPause() {
        super.onPause();
        if (webView != null) {
            webView.onPause();
        }
        if (lua != null) {
            lua.onAppEvent("pause");
        }
    }

    @Override
    protected void onResume() {
        super.onResume();
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
        if (lua != null) {
            lua.onAppEvent("resume");
        }
    }

    @Override
    protected void onDestroy() {
        if (Build.VERSION.SDK_INT >= 33) {
            BackApi33.unregister(this);
        }
        if (webView != null) {
            webView.removeJavascriptInterface(JsBridge.NAME);
            webView.destroy();
            webView = null;
        }
        if (lua != null) {
            lua.shutdown();
            lua = null;
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
        if (lua != null && lua.hasHandlers("back")) {
            lua.onBack(this::defaultBack);
        } else {
            defaultBack();
        }
    }

    private void defaultBack() {
        if (webView != null && webView.canGoBack()) {
            webView.goBack();
        } else {
            finish();
        }
    }

    AppConfig config() {
        return config;
    }

    SharedPreferences prefs() {
        return prefs;
    }

    // ---- WebView 控制（可在任意线程调用） ----

    void loadFromLua(final String url) {
        if (!isHttpUrl(url)) {
            throw new IllegalArgumentException("web.load 只支持 http/https");
        }
        luaLoadRequested = true;
        runOnUiThread(() -> {
            if (webView != null) {
                webView.loadUrl(url);
            }
        });
    }

    void reloadOnMain() {
        runOnUiThread(() -> {
            if (webView != null) {
                webView.reload();
            }
        });
    }

    void goBackOnMain() {
        runOnUiThread(() -> {
            if (webView != null && webView.canGoBack()) {
                webView.goBack();
            }
        });
    }

    boolean canGoBackOnMain() {
        Boolean result = callOnMain(() -> webView != null && webView.canGoBack());
        return result != null && result;
    }

    void evaluateJs(final String script, final LuaValue callback) {
        runOnUiThread(() -> {
            if (webView == null) {
                return;
            }
            webView.evaluateJavascript(script, value -> {
                if (callback != null && lua != null) {
                    lua.post(() -> {
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
        runOnUiThread(() -> {
            if (webView == null) {
                return;
            }
            String js = "window.__berbaResolve(" + JSONObject.quote(reqId) + ","
                    + (ok ? "true" : "false") + ","
                    + JSONObject.quote(payload == null ? "null" : payload) + ");";
            webView.evaluateJavascript(js, null);
        });
    }

    void onPageStartedEvent(final String url) {
        if (lua != null) {
            lua.onAppEvent("pageStarted", LuaValue.valueOf(url));
        }
    }

    void onPageFinishedEvent(final String url) {
        if (lua != null) {
            lua.onAppEvent("pageFinished", LuaValue.valueOf(url));
        }
    }

    void onWebErrorEvent(final String description, final String url) {
        if (lua != null) {
            lua.onAppEvent("error", LuaValue.valueOf(description), LuaValue.valueOf(url));
        }
    }

    // ---- UI ----

    void toastOnMain(final String message) {
        runOnUiThread(() -> Toast.makeText(this, message, Toast.LENGTH_SHORT).show());
    }

    void showAlert(final String message) {
        runOnUiThread(() -> {
            if (isFinishing()) {
                return;
            }
            new AlertDialog.Builder(this)
                    .setMessage(message)
                    .setPositiveButton(android.R.string.ok, null)
                    .show();
        });
    }

    void showConfirm(final String message, final LuaValue callback) {
        runOnUiThread(() -> {
            if (isFinishing()) {
                return;
            }
            new AlertDialog.Builder(this)
                    .setMessage(message)
                    .setPositiveButton(android.R.string.ok, (d, w) -> replyConfirm(callback, true))
                    .setNegativeButton(android.R.string.cancel, (d, w) -> replyConfirm(callback, false))
                    .setOnCancelListener(d -> replyConfirm(callback, false))
                    .show();
        });
    }

    private void replyConfirm(final LuaValue callback, final boolean value) {
        if (callback == null || lua == null) {
            return;
        }
        lua.post(() -> {
            try {
                callback.invoke(value ? LuaValue.TRUE : LuaValue.FALSE);
            } catch (Throwable t) {
                Log.e(TAG, "ui.confirm 回调错误", t);
            }
        });
    }

    void finishOnMain() {
        runOnUiThread(this::finish);
    }

    void setFullscreenOnMain(final boolean on) {
        runOnUiThread(() -> {
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
        runOnUiThread(() -> {
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
        runOnUiThread(() -> setRequestedOrientation(orientation));
    }

    // ---- 权限 ----

    boolean hasPermission(String name) {
        return checkSelfPermission(name) == android.content.pm.PackageManager.PERMISSION_GRANTED;
    }

    void requestPermissionsFromLua(final List<String> names, final LuaValue callback) {
        final int code;
        synchronized (pendingPermissions) {
            code = nextRequestCode++;
            pendingPermissions.put(code, new PermRequest(names, callback));
        }
        runOnUiThread(() -> requestPermissions(names.toArray(new String[0]), code));
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
        if (req.callback != null && lua != null) {
            lua.post(() -> {
                try {
                    req.callback.invoke(result);
                } catch (Throwable t) {
                    Log.e(TAG, "permission 回调错误", t);
                }
            });
        }
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

    <T> T callOnMain(Callable<T> task) {
        if (Looper.myLooper() == Looper.getMainLooper()) {
            try {
                return task.call();
            } catch (Exception e) {
                return null;
            }
        }
        final AtomicReference<T> ref = new AtomicReference<>();
        final CountDownLatch latch = new CountDownLatch(1);
        mainHandler.post(() -> {
            try {
                ref.set(task.call());
            } catch (Exception e) {
                Log.w(TAG, "主线程任务失败: " + e);
            } finally {
                latch.countDown();
            }
        });
        try {
            if (!latch.await(5, TimeUnit.SECONDS)) {
                Log.w(TAG, "主线程任务超时");
                return null;
            }
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            return null;
        }
        return ref.get();
    }

    static boolean isHttpUrl(String url) {
        if (url == null) {
            return false;
        }
        String lower = url.toLowerCase(Locale.ROOT);
        return lower.startsWith("http://") || lower.startsWith("https://");
    }
}
