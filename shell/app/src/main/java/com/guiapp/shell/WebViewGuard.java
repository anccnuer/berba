package com.guiapp.shell;

import android.app.Activity;
import android.content.res.Resources;
import android.content.res.loader.ResourcesLoader;
import android.os.Build;
import android.util.Log;
import android.webkit.WebView;

/**
 * 规避 Android 16 首发版本（android16-release，如三星 One UI 8.0）的
 * WebView 首次初始化崩溃：ResourcesManager.redirectAllResourcesToNewImplLocked
 * 抛 "failed to redirect ResourcesImpl"。AOSP 在 QPR1 修复，未升级设备仍会崩溃。
 *
 * 崩溃发生前 WebView 资源路径已经注册，因此刷新一次 Resources 后重建即可通过。
 */
final class WebViewGuard {
    private static final String TAG = "guiapp";
    private static final String REDIRECT_ERROR = "failed to redirect ResourcesImpl";

    private WebViewGuard() {
    }

    static WebView create(Activity activity) {
        try {
            return new WebView(activity);
        } catch (RuntimeException first) {
            if (!isRedirectFailure(first)) {
                throw first;
            }
            Log.w(TAG, "命中 Android 16 WebView 资源重定向缺陷，重试", first);
        }
        refreshResources(activity);
        try {
            return new WebView(activity);
        } catch (RuntimeException second) {
            Log.e(TAG, "WebView 重建仍失败", second);
            throw second;
        }
    }

    private static boolean isRedirectFailure(Throwable t) {
        for (Throwable cause = t; cause != null; cause = cause.getCause()) {
            if (cause instanceof Resources.NotFoundException
                    && cause.getMessage() != null
                    && cause.getMessage().contains(REDIRECT_ERROR)) {
                return true;
            }
        }
        return false;
    }

    private static void refreshResources(Activity activity) {
        if (Build.VERSION.SDK_INT < 30) {
            return;
        }
        try {
            Api30.refresh(activity);
        } catch (Throwable t) {
            Log.w(TAG, "刷新 Resources 失败", t);
        }
    }

    /** 隔离 API 30 类型，避免旧系统加载 WebViewGuard 时解析新类。 */
    private static final class Api30 {
        static void refresh(Activity activity) {
            Resources resources = activity.getResources();
            ResourcesLoader loader = new ResourcesLoader();
            resources.addLoaders(loader);
            resources.removeLoaders(loader);
        }
    }
}
