package com.wb2api.tasks;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;

/**
 * 开机自启：手机重启后（以及应用更新后）自动把引擎拉起来，否则「定时做任务」就成了
 * 「开机后手动点一下才有任务」。
 *
 * 是否自启由设置项控制（默认开）；关掉后只能从 App 里手动启动。
 */
public class BootReceiver extends BroadcastReceiver {
    @Override
    public void onReceive(Context context, Intent intent) {
        String action = intent == null ? null : intent.getAction();
        if (action == null) return;
        boolean boot = Intent.ACTION_BOOT_COMPLETED.equals(action)
                || "android.intent.action.QUICKBOOT_POWERON".equals(action)
                || Intent.ACTION_MY_PACKAGE_REPLACED.equals(action);
        if (!boot) return;
        if (!Prefs.get(context).autostart()) return;

        EngineService.start(context);
        PulseReceiver.schedule(context);
    }
}
