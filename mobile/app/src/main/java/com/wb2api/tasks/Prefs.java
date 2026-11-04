package com.wb2api.tasks;

import android.content.Context;
import android.content.SharedPreferences;

/**
 * 应用级设置（SharedPreferences）。
 *
 * 只存三样东西：面板端口、引擎时区、是否开机自启。其余配置（DNS、排程、保活开关等）
 * 都在引擎的 config.json 里，由面板「配置」页管理——App 不再复制一份，避免两处不一致。
 */
public final class Prefs {
    private static final String FILE = "wb2api";
    private static Prefs sInstance;

    private final SharedPreferences sp;

    private Prefs(Context c) {
        sp = c.getApplicationContext().getSharedPreferences(FILE, Context.MODE_PRIVATE);
    }

    public static synchronized Prefs get(Context c) {
        if (sInstance == null) sInstance = new Prefs(c);
        return sInstance;
    }

    /** 面板监听端口（默认 7863，与桌面端一致，方便照抄习惯）。 */
    public int port() {
        return sp.getInt("port", 7863);
    }

    /**
     * 引擎时区。默认 Asia/Shanghai：Android 没有 /etc/localtime，Go 的 time.Local 会
     * 退回 UTC，若不显式给 TZ，「9:00 签到」「23:00 夜猫子」会按 UTC 钟点触发（整体偏 8 小时）。
     */
    public String tz() {
        return sp.getString("tz", "Asia/Shanghai");
    }

    /** 开机自启（默认开）。 */
    public boolean autostart() {
        return sp.getBoolean("autostart", true);
    }

    public void setPort(int port) {
        sp.edit().putInt("port", port).apply();
    }

    public void setTz(String tz) {
        sp.edit().putString("tz", tz).apply();
    }

    public void setAutostart(boolean on) {
        sp.edit().putBoolean("autostart", on).apply();
    }
}
