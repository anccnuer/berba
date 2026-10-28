package com.berba.shell;

import android.net.Uri;
import android.util.Log;
import android.webkit.ConsoleMessage;
import android.webkit.JsPromptResult;
import android.webkit.JsResult;
import android.webkit.ValueCallback;
import android.webkit.WebChromeClient;
import android.webkit.WebView;

/** 把网页 console 写入 logcat，并让 alert/confirm/prompt 正常弹出。 */
final class BridgeChromeClient extends WebChromeClient {
    private static final String TAG = "berba";

    private final MainActivity activity;

    BridgeChromeClient(MainActivity activity) {
        this.activity = activity;
    }

    @Override
    public boolean onConsoleMessage(ConsoleMessage message) {
        if (message != null) {
            Log.i(TAG, "[js] " + message.message() + " (" + message.sourceId()
                    + ":" + message.lineNumber() + ")");
        }
        return true;
    }

    @Override
    public boolean onJsAlert(WebView view, String url, String message, JsResult result) {
        activity.showJsAlert(message, result);
        return true;
    }

    @Override
    public boolean onJsConfirm(WebView view, String url, String message, JsResult result) {
        activity.showJsConfirm(message, result);
        return true;
    }

    @Override
    public boolean onJsPrompt(WebView view, String url, String message, String defaultValue,
                              JsPromptResult result) {
        activity.showJsPrompt(message, defaultValue, result);
        return true;
    }

    @Override
    public boolean onShowFileChooser(WebView view, ValueCallback<Uri[]> callback,
                                     FileChooserParams params) {
        if (params == null) {
            callback.onReceiveValue(null);
            return true;
        }
        activity.openFileChooser(callback, params.createIntent());
        return true;
    }
}
