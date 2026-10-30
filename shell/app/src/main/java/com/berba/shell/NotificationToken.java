package com.berba.shell;

import android.content.Context;
import android.content.SharedPreferences;
import android.util.Log;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.SecureRandom;

/** 通知点击校验 token：首次使用时生成 128 位随机值并持久化。 */
final class NotificationToken {
    private static final String TAG = "berba";
    private static final String PREFS = "berba_notify";
    private static final String KEY = "click_token";
    private static final int HEX_LENGTH = 32;
    private static final SecureRandom RANDOM = new SecureRandom();

    private NotificationToken() {
    }

    static synchronized String get(Context context) {
        SharedPreferences prefs =
                context.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        String token = prefs.getString(KEY, null);
        if (token != null && token.length() == HEX_LENGTH) {
            return token;
        }
        byte[] bytes = new byte[16];
        RANDOM.nextBytes(bytes);
        StringBuilder sb = new StringBuilder(HEX_LENGTH);
        for (byte b : bytes) {
            sb.append(Character.forDigit((b >> 4) & 0xF, 16));
            sb.append(Character.forDigit(b & 0xF, 16));
        }
        token = sb.toString();
        prefs.edit().putString(KEY, token).commit();
        Log.i(TAG, "已生成通知点击 token");
        return token;
    }

    static boolean matches(Context context, String candidate) {
        if (candidate == null || candidate.length() != HEX_LENGTH) {
            return false;
        }
        String expected = get(context);
        return MessageDigest.isEqual(expected.getBytes(StandardCharsets.US_ASCII),
                candidate.getBytes(StandardCharsets.US_ASCII));
    }
}
