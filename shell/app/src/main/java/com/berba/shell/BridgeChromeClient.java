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

    private final UiSession session;

    BridgeChromeClient(UiSession session) {
        this.session = session;
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
        if (!session.isCurrent()) {
            result.cancel();
            return true;
        }
        session.activity().showJsAlert(message, result);
        return true;
    }

    @Override
    public boolean onJsConfirm(WebView view, String url, String message, JsResult result) {
        if (!session.isCurrent()) {
            result.cancel();
            return true;
        }
        session.activity().showJsConfirm(message, result);
        return true;
    }

    @Override
    public boolean onJsPrompt(WebView view, String url, String message, String defaultValue,
                              JsPromptResult result) {
        if (!session.isCurrent()) {
            result.cancel();
            return true;
        }
        session.activity().showJsPrompt(message, defaultValue, result);
        return true;
    }

    @Override
    public boolean onShowFileChooser(WebView view, ValueCallback<Uri[]> callback,
                                     FileChooserParams params) {
        if (params == null || !session.isCurrent()) {
            callback.onReceiveValue(null);
            return true;
        }
        session.activity().openFileChooser(callback, params.createIntent());
        return true;
    }
}
