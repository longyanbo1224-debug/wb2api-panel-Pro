package com.wb2api.tasks;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.graphics.drawable.Icon;
import android.os.Build;
import android.os.IBinder;

/**
 * 引擎宿主服务：前台常驻，把 {@link Engine} 拉起来并显示不可划掉的常驻通知。
 *
 * <h3>为什么必须前台服务</h3>
 * Android 8 之后后台进程会被系统随时回收；前台服务 + 常驻通知是唯一被允许「一直在跑」
 * 的形态。此外 {@code dataSync} 类型是 Android 14 起对「后台数据同步类」任务的官方归类。
 *
 * <h3>targetSdk 停在 34 的原因</h3>
 * Android 15 对 dataSync 前台服务有「24 小时内累计 6 小时」的上限，只对 targetSdk>=35
 * 生效。本应用需要整天待命做定时任务，因此停在 34（见 app/build.gradle）。
 */
public class EngineService extends Service {
    public static final String ACTION_START = "com.wb2api.tasks.action.START";
    public static final String ACTION_STOP = "com.wb2api.tasks.action.STOP";

    private static final String CHANNEL_ID = "engine";
    private static final int NOTIF_ID = 1001;

    /** 幂等启动（Activity / 开机广播 / 保活心跳都走这里）。 */
    public static void start(Context c) {
        Intent i = new Intent(c, EngineService.class).setAction(ACTION_START);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            c.startForegroundService(i);
        } else {
            c.startService(i);
        }
    }

    /** 停止引擎并退出前台（通知随即消失）。 */
    public static void stop(Context c) {
        Intent i = new Intent(c, EngineService.class).setAction(ACTION_STOP);
        c.startService(i);
    }

    @Override
    public void onCreate() {
        super.onCreate();
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            NotificationManager nm = getSystemService(NotificationManager.class);
            if (nm != null && nm.getNotificationChannel(CHANNEL_ID) == null) {
                NotificationChannel ch = new NotificationChannel(
                        CHANNEL_ID, "任务引擎", NotificationManager.IMPORTANCE_LOW);
                ch.setDescription("签到 / 做任务引擎的后台常驻状态");
                ch.setShowBadge(false);
                nm.createNotificationChannel(ch);
            }
        }
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        String action = (intent == null) ? ACTION_START : intent.getAction();
        if (ACTION_STOP.equals(action)) {
            Engine.get(this).stop();
            stopForeground(true);
            stopSelf();
            return START_NOT_STICKY;
        }

        // Android 14 起 startForeground 必须在 5 秒内调用，否则 ANR/崩溃。
        startForeground(NOTIF_ID, buildNotification());
        Engine.get(this).start();
        // 续订保活心跳：Doze 下进程会被冻结，Go 的定时器不前进，靠闹钟定期唤醒。
        PulseReceiver.schedule(this);
        return START_STICKY;
    }

    @Override
    public void onDestroy() {
        // 服务被系统或用户结束时必须一并收掉子进程，否则会留下孤儿 Go 进程占着端口。
        Engine.get(this).stop();
        super.onDestroy();
    }

    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }

    private Notification buildNotification() {
        PendingIntent open = PendingIntent.getActivity(
                this, 0,
                new Intent(this, MainActivity.class),
                PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);

        PendingIntent stop = PendingIntent.getService(
                this, 1,
                new Intent(this, EngineService.class).setAction(ACTION_STOP),
                PendingIntent.FLAG_UPDATE_CURRENT | PendingIntent.FLAG_IMMUTABLE);

        Notification.Builder b;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            b = new Notification.Builder(this, CHANNEL_ID);
        } else {
            b = new Notification.Builder(this);
        }
        b.setContentTitle("wb2api-panel-Pro 运行中")
                .setContentText("签到 / 成长任务 / 活跃 / 旅行按设定时间自动执行")
                .setSmallIcon(android.R.drawable.stat_notify_sync)
                .setContentIntent(open)
                .setOngoing(true)
                .setShowWhen(false);

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            Notification.Action act = new Notification.Action.Builder(
                    Icon.createWithResource(this, android.R.drawable.ic_media_pause),
                    "停止", stop).build();
            b.addAction(act);
        }
        return b.build();
    }
}
