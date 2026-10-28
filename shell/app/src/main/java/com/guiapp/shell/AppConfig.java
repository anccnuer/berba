package com.guiapp.shell;

import android.content.Context;
import android.content.pm.PackageInfo;
import android.content.pm.PackageManager;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.ByteArrayOutputStream;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Locale;
import java.util.Set;

/** 读取 assets/app_config.json，由 guiapp 构建时写入。 */
final class AppConfig {
    private static final String TAG = "guiapp";
    static final String LOCAL_HOST = "appassets.androidhost";
    static final String LOCAL_ORIGIN = "https://" + LOCAL_HOST;

    final String name;
    final String packageName;
    final String versionName;
    final int versionCode;
    final String startUrl;
    final String luaEntry;
    final boolean spa;
    final boolean cleartext;
    final boolean debug;
    final String theme;
    final List<String> origins = new ArrayList<>();
    private final Set<String> originSet = new HashSet<>();

    private AppConfig(String name, String packageName, String versionName, int versionCode,
                      String startUrl, String luaEntry, boolean spa, boolean cleartext,
                      boolean debug, String theme, List<String> origins) {
        this.name = name;
        this.packageName = packageName;
        this.versionName = versionName;
        this.versionCode = versionCode;
        this.startUrl = startUrl;
        this.luaEntry = luaEntry;
        this.spa = spa;
        this.cleartext = cleartext;
        this.debug = debug;
        this.theme = theme;
        this.origins.addAll(origins);
        for (String o : origins) {
            originSet.add(o.toLowerCase(Locale.ROOT));
        }
    }

    static AppConfig load(Context ctx) {
        String name = null;
        String packageName = ctx.getPackageName();
        String versionName = "1.0";
        int versionCode = 1;
        String startUrl = LOCAL_ORIGIN + "/index.html";
        String luaEntry = null;
        boolean spa = false;
        boolean cleartext = false;
        boolean debug = false;
        String theme = "light";
        List<String> origins = new ArrayList<>();

        try {
            PackageInfo info = ctx.getPackageManager().getPackageInfo(packageName, 0);
            versionName = info.versionName;
            versionCode = info.versionCode;
            CharSequence label = ctx.getApplicationInfo().loadLabel(ctx.getPackageManager());
            name = label == null ? packageName : label.toString();
        } catch (PackageManager.NameNotFoundException ignored) {
        }

        try {
            String text = readAsset(ctx, "app_config.json");
            JSONObject json = new JSONObject(text);
            name = json.optString("name", name);
            packageName = json.optString("package", packageName);
            versionName = json.optString("versionName", versionName);
            versionCode = json.optInt("versionCode", versionCode);
            startUrl = json.optString("startUrl", startUrl);
            luaEntry = json.optString("luaEntry", null);
            if (luaEntry != null && luaEntry.isEmpty()) {
                luaEntry = null;
            }
            spa = json.optBoolean("spa", false);
            cleartext = json.optBoolean("cleartext", false);
            debug = json.optBoolean("debug", false);
            theme = json.optString("theme", "light");
            JSONArray arr = json.optJSONArray("allowedOrigins");
            if (arr != null) {
                for (int i = 0; i < arr.length(); i++) {
                    String o = normalizeOrigin(arr.optString(i, ""));
                    if (!o.isEmpty()) {
                        origins.add(o);
                    }
                }
            }
        } catch (Exception e) {
            Log.w(TAG, "app_config.json 读取失败，使用默认配置: " + e);
        }
        return new AppConfig(name == null ? packageName : name, packageName, versionName,
                versionCode, startUrl, luaEntry, spa, cleartext, debug, theme, origins);
    }

    static String readAsset(Context ctx, String path) throws Exception {
        try (InputStream in = ctx.getAssets().open(path)) {
            ByteArrayOutputStream out = new ByteArrayOutputStream();
            byte[] buf = new byte[8192];
            int n;
            while ((n = in.read(buf)) > 0) {
                out.write(buf, 0, n);
            }
            return out.toString(StandardCharsets.UTF_8.name());
        }
    }

    boolean isLocalOrigin(String origin) {
        return LOCAL_ORIGIN.equalsIgnoreCase(normalizeOrigin(origin));
    }

    boolean originAllowed(String origin) {
        String o = normalizeOrigin(origin);
        return !o.isEmpty() && (isLocalOrigin(o) || originSet.contains(o));
    }

    static String normalizeOrigin(String raw) {
        if (raw == null) {
            return "";
        }
        String s = raw.trim().toLowerCase(Locale.ROOT);
        if (s.endsWith("/")) {
            s = s.substring(0, s.length() - 1);
        }
        return s;
    }
}
