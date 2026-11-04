package com.wb2api.tasks;

import android.app.AlarmManager;
import android.app.PendingIntent;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.os.Build;
import android.os.PowerManager;

/**
 * 保活心跳：每 15 分钟把应用叫醒一次，让 Go 侧的定时器追上真实时间。
 *
 * <h3>为什么需要这一层</h3>
 * 前台服务只保证「进程不被杀」，不保证「CPU 不睡」。进入 Doze 后整个进程被冻结，
 * Go 的 time.Ticker 不前进，于是「9:00 签到」这类按钟点触发的排程会被推迟到用户
 * 下次点亮屏幕。用 {@code setAndAllowWhileIdle} 的闹钟定期唤醒进程几秒，Go 的 ticker
 * 就能在下一拍发现「已经过了 9 点」并补做任务（Go 侧还有补签 / 补偿逻辑兜底）。
 *
 * <h3>为什么不用精确闹钟</h3>
 * {@code setExactAndAllowWhileIdle} 在 Android 12+ 需要 SCHEDULE_EXACT_ALARM 权限，
 * 而任务窗口是按小时计的，±几分钟的偏差无关紧要。用 setAndAllowWhileIdle 既不需要
 * 额外权限，也能穿透 Doze（系统对它的限流是 9 分钟一次，15 分钟间隔安全）。
 */
public class PulseReceiver extends BroadcastReceiver {
    private static final long INTERVAL_MS = 15 * 60 * 1000L;
    /** 每次唤醒持有 wakelock 的时长：够引擎跑完一轮任务与一次网络往返。 */
    private static final long HOLD_MS = 90 * 1000L;

    @Override
    public void onReceive(Context context, Intent intent) {
        Context ctx = context.getApplicationContext();

        PowerManager pm = (PowerManager) ctx.getSystemService(Context.POWER_SERVICE);
        if (pm != null) {
            // 带超时的 acquire：不需要成对 release，也不会因为异常漏放导致永久耗电。
            PowerManager.WakeLock wl =
                    pm.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "wb2api:pulse");
            wl.acquire(HOLD_MS);
        }

        // 进程可能已被系统回收：重新拉起（幂等）。
        EngineService.start(ctx);
        // 链式续订下一拍（闹钟是一次性的，自己续自己才能长期有效）。
        schedule(ctx);
    }

    /** 安排下一次心跳。调用点在：服务启动、每次心跳、开机广播。 */
    public static void schedule(Context context) {
        AlarmManager am = (AlarmManager) context.getSystemService(Context.ALARM_SERVICE);
        if (am == null) return;
        long at = System.currentTimeMillis() + INTERVAL_MS;
        try {
            am.setAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, at, pending(context));
        } catch (Throwable t) {
            // 部分 ROM 会限制该 API；失败不影响前台服务本身的运行。
        }
    }

    /** 取消心跳（用户彻底停用引擎时调用，避免白白唤醒）。 */
    public static void cancel(Context context) {
        AlarmManager am = (AlarmManager) context.getSystemService(Context.ALARM_SERVICE);
        if (am != null) am.cancel(pending(context));
    }

    private static PendingIntent pending(Context context) {
        Intent i = new Intent(context, PulseReceiver.class);
        int flags = PendingIntent.FLAG_UPDATE_CURRENT;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) flags |= PendingIntent.FLAG_IMMUTABLE;
        return PendingIntent.getBroadcast(context, 0, i, flags);
    }
}
