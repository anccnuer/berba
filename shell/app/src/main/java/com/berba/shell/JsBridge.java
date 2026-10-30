package com.berba.shell;

import android.util.Log;
import android.webkit.JavascriptInterface;

import org.json.JSONObject;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.SecureRandom;

/**
 * JS -> Lua 桥。每次启动生成随机 token，仅允许白名单 origin 调用。
 */
public final class JsBridge {
    static final String NAME = "__berbaNative";
    static final String SHIM_PATH = "__berba.js";
    private static final String TAG = "berba";
    private static final int MAX_ARGS = 1024 * 1024;

    private final UiSession session;
    private final String token;

    JsBridge(UiSession session) {
        this.session = session;
        byte[] random = new byte[16];
        new SecureRandom().nextBytes(random);
        this.token = hex(random);
    }

    private static String hex(byte[] bytes) {
        StringBuilder sb = new StringBuilder(bytes.length * 2);
        for (byte b : bytes) {
            sb.append(Character.forDigit((b >> 4) & 0xF, 16));
            sb.append(Character.forDigit(b & 0xF, 16));
        }
        return sb.toString();
    }

    @JavascriptInterface
    public void dispatch(String reqId, String reqToken, String name, String argsJson) {
        if (reqId == null || name == null) {
            return;
        }
        if (!session.isCurrent()) {
            Log.w(TAG, "桥调用来自已失效的界面，已拒绝");
            return;
        }
        if (!constantTimeEquals(reqToken, token)) {
            Log.w(TAG, "桥调用 token 不匹配，已拒绝");
            return;
        }
        MainActivity activity = session.activity();
        // origin 由原生侧按当前文档记录，不信任页面自报。
        String origin = activity.pageOrigin();
        if (!activity.config().originAllowed(origin)) {
            Log.w(TAG, "桥调用 origin 不在白名单: " + origin);
            return;
        }
        if (argsJson != null && argsJson.length() > MAX_ARGS) {
            reply(reqId, false, "参数过大");
            return;
        }
        session.runtime().callBridge(name, argsJson, (ok, payload) -> reply(reqId, ok, payload));
    }

    private void reply(String reqId, boolean ok, String payload) {
        if (session.isCurrent()) {
            session.activity().deliverBridgeResult(reqId, ok, payload);
        }
    }

    private static boolean constantTimeEquals(String a, String b) {
        if (a == null || b == null) {
            return false;
        }
        return MessageDigest.isEqual(a.getBytes(StandardCharsets.UTF_8), b.getBytes(StandardCharsets.UTF_8));
    }

    String shim() {
        String t = JSONObject.quote(token);
        return "(function(){"
                + "if(window.berba&&window.berba.__berba){return;}"
                + "var callbacks={},events={},seq=0,native=window.__berbaNative,"
                + "epoch=Math.random().toString(36).slice(2)+'.';"
                + "window.__berbaResolve=function(id,ok,payload){"
                + "var e=callbacks[id];if(!e){return;}delete callbacks[id];"
                + "var v;try{v=JSON.parse(payload);}catch(x){v=payload;}"
                + "if(ok){e.resolve(v);}else{e.reject(new Error(typeof v==='string'?v:(v&&v.error)||'调用失败'));}};"
                + "window.__berbaEvent=function(name,payload){"
                + "var list=events[name];if(!list){return;}"
                + "var v;try{v=JSON.parse(payload);}catch(x){v=payload;}"
                + "for(var i=0;i<list.length;i++){try{list[i](v);}catch(e){console.error(e);}}};"
                + "window.berba={__berba:true,"
                + "call:function(name,args){return new Promise(function(resolve,reject){"
                + "var id=epoch+(++seq);callbacks[id]={resolve:resolve,reject:reject};"
                + "try{native.dispatch(id," + t + ",String(name),JSON.stringify(args===undefined?null:args));}"
                + "catch(err){delete callbacks[id];reject(err);}});},"
                + "on:function(name,fn){(events[name]=events[name]||[]).push(fn);return fn;},"
                + "off:function(name,fn){var l=events[name];if(!l){return;}var i=l.indexOf(fn);if(i>=0){l.splice(i,1);}}};"
                + "})();";
    }

    String shimTag() {
        return "<script src=\"https://" + AppConfig.LOCAL_HOST + "/" + SHIM_PATH + "\"></script>";
    }
}
