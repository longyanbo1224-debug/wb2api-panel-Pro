package com.wb2api.tasks;

import android.content.Context;
import android.content.pm.ApplicationInfo;
import android.util.Log;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.File;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.lang.reflect.Method;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.text.SimpleDateFormat;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Date;
import java.util.List;
import java.util.Locale;
import java.util.Map;

/**
 * 引擎进程管理：把 APK 内嵌的 Go 二进制以 {@code -mode=tasks} 跑起来，并维护日志尾巴。
 *
 * <h3>为什么是「执行一个 .so」</h3>
 * jniLibs 里的文件只有在名字形如 lib*.so 时才会被解压到 {@code nativeLibraryDir}，
 * 而该目录是 Android 上少数**可执行**的目录：API 29+ 的 W^X 限制禁止从应用可写
 * 目录（filesDir/cacheDir）exec 任何文件。所以 Go 可执行文件被打包成
 * {@code libwb2api.so}，安装时落到 nativeLibraryDir，再按普通可执行文件启动。
 *
 * <h3>职责边界</h3>
 * 只负责「起进程 / 探活 / 挂掉重启 / 收集日志」。签到任务、账号池、面板全部在 Go 侧，
 * 这里不做任何业务判断——两边唯一约定是：{@code -config <filesDir>/engine/config.json}
 * 与 {@code /healthz} 探活端点。
 */
public final class Engine {
    private static final String TAG = "wb2api";
    /** 必须与 jniLibs 里的文件名一致（构建脚本负责生成）。 */
    static final String BIN_NAME = "libwb2api.so";

    private static final int LOG_RING_CAP = 600;          // 内存日志环
    private static final long LOG_FILE_MAX = 512 * 1024;  // 落盘日志上限，超了轮转一次

    private static Engine sInstance;

    private final Context ctx;
    private final ArrayDeque<String> ring = new ArrayDeque<>();
    private final Object ringLock = new Object();

    private volatile boolean want = false;   // 期望状态（true=应该跑着）
    private volatile Process proc;
    private Thread supervisor;

    private Engine(Context c) {
        this.ctx = c.getApplicationContext();
    }

    public static synchronized Engine get(Context c) {
        if (sInstance == null) sInstance = new Engine(c);
        return sInstance;
    }

    /* ── 路径 ────────────────────────────────────────────────────────── */

    static File engineDir(Context c) {
        return new File(c.getFilesDir(), "engine");
    }

    static File configFile(Context c) {
        return new File(engineDir(c), "config.json");
    }

    /** Go 侧工作目录（相对路径 ./auths、./data 都相对它）。 */
    private File dir() {
        return engineDir(ctx);
    }

    /** 内嵌二进制的真实路径。 */
    private File binary() {
        ApplicationInfo ai = ctx.getApplicationInfo();
        return new File(ai.nativeLibraryDir, BIN_NAME);
    }

    int port() {
        return Prefs.get(ctx).port();
    }

    String baseUrl() {
        return "http://127.0.0.1:" + port();
    }

    /* ── 首次运行：目录与 config.json ────────────────────────────────── */

    /**
     * 准备好引擎工作目录并落一份最小 config.json。
     *
     * 只写「Android 上没法自动拿到」的几项（listen / auth_dir / state_file / DNS），
     * 其余键交给 Go 侧的 Default() 补齐——App 不复刻默认值，避免以后加配置项时两边漂移。
     */
    synchronized void ensureLayout() {
        File d = dir();
        mkdirs(d);
        mkdirs(new File(d, "auths"));
        mkdirs(new File(d, "data"));
        mkdirs(new File(d, "tmp"));

        File cfg = configFile(ctx);
        if (!cfg.exists()) {
            writeBootstrapConfig(cfg);
            return;
        }
        // 端口在 App 设置里被改过：把 config.json 的 listen 同步过来（只改这一键，
        // 不动用户在面板里调过的其它配置）。
        try {
            JSONObject o = new JSONObject(readAll(cfg));
            String wantListen = "127.0.0.1:" + port();
            if (!wantListen.equals(o.optString("listen", ""))) {
                o.put("listen", wantListen);
                writeAll(cfg, o.toString(2));
                log("已同步面板端口到配置：" + wantListen);
            }
        } catch (Exception e) {
            log("同步端口失败（配置保持原样）：" + e.getMessage());
        }
    }

    private void writeBootstrapConfig(File cfg) {
        try {
            JSONObject o = new JSONObject();
            // 只监听回环：面板页面本身不校验口令，绑到 0.0.0.0 等于同一 Wi-Fi 下
            // 谁都能打开面板看到账号与积分。任务模式也不再注册 /v1 反代路由。
            o.put("listen", "127.0.0.1:" + port());
            o.put("api_key", "");
            o.put("auth_dir", "./auths");
            o.put("state_file", "./data/state.json");
            // 手机上没有 /etc/resolv.conf：Go 的纯 Go 解析器会退回 127.0.0.1:53，
            // 结果查任何域名都失败（"connection refused"）。必须显式给 DNS。
            JSONArray dns = new JSONArray();
            dns.put("223.5.5.5");
            dns.put("119.29.29.29");
            o.put("dns_servers", dns);
            writeAll(cfg, o.toString(2));
            log("首次运行：已生成 " + cfg.getAbsolutePath());
        } catch (Exception e) {
            log("生成配置失败：" + e.getMessage());
        }
    }

    /* ── 生命周期 ────────────────────────────────────────────────────── */

    /** 幂等启动：已在跑则什么都不做。 */
    public synchronized void start() {
        ensureLayout();
        if (want && supervisor != null && supervisor.isAlive()) return;
        if (!binary().canExecute()) {
            // 常见原因：APK 里没打进去（构建脚本没跑）或 ABI 不匹配。
            log("内嵌引擎不可执行：" + binary().getAbsolutePath()
                    + "（检查构建脚本是否已生成 libwb2api.so，以及手机是否为 arm64）");
        }
        want = true;
        supervisor = new Thread(this::superviseLoop, "engine-supervisor");
        supervisor.setDaemon(true);
        supervisor.start();
    }

    /** 停止：先 SIGTERM 让 Go 优雅落盘，超时再强杀。 */
    public synchronized void stop() {
        want = false;
        Process p = proc;
        proc = null;
        if (p != null) terminate(p);
        Thread t = supervisor;
        supervisor = null;
        if (t != null) t.interrupt();
    }

    /** 重启（改端口 / 改时区后调用）。 */
    public void restart() {
        stop();
        start();
    }

    private void superviseLoop() {
        long backoff = 2000;
        while (want) {
            Process p;
            long launchedAt = 0;
            try {
                p = launch();
                launchedAt = System.currentTimeMillis();
            } catch (Exception e) {
                log("启动引擎失败：" + e);
                sleep(backoff);
                backoff = Math.min(backoff * 2, 60000);
                continue;
            }
            int code = -1;
            try {
                code = p.waitFor();
            } catch (InterruptedException ie) {
                Thread.currentThread().interrupt();
            }
            proc = null;
            if (!want) break;
            // 长跑过（>2 分钟）说明这次启动是好的，重置退避；否则指数退避避免疯狂重启。
            if (System.currentTimeMillis() - launchedAt > 120000) backoff = 2000;
            log("引擎进程退出 code=" + code + "，" + (backoff / 1000) + " 秒后重启");
            sleep(backoff);
            backoff = Math.min(backoff * 2, 60000);
        }
        log("引擎已停止");
    }

    private Process launch() throws IOException {
        File d = dir();
        ProcessBuilder pb = new ProcessBuilder(
                binary().getAbsolutePath(),
                "-mode=tasks",
                "-config", configFile(ctx).getAbsolutePath());
        pb.directory(d);
        pb.redirectErrorStream(true);

        Map<String, String> env = pb.environment();
        // TZ 必须显式给：二进制内嵌了 IANA 时区库（cmd/server 里 import time/tzdata），
        // 有了 TZ 就能在没有任何系统 zoneinfo 的 Android 上取到正确的本地时间。
        env.put("TZ", Prefs.get(ctx).tz());
        env.put("HOME", d.getAbsolutePath());
        env.put("TMPDIR", new File(d, "tmp").getAbsolutePath());

        Process p = pb.start();
        proc = p;
        log("引擎已启动（pid=" + pidOf(p) + ", TZ=" + Prefs.get(ctx).tz()
                + ", port=" + port() + "）");
        Thread reader = new Thread(() -> pump(p), "engine-log");
        reader.setDaemon(true);
        reader.start();
        return p;
    }

    /** 读子进程 stdout/stderr（已合并），逐行进日志环 + 日志文件。 */
    private void pump(Process p) {
        try (InputStream in = p.getInputStream();
             BufferedReader r = new BufferedReader(new InputStreamReader(in, StandardCharsets.UTF_8))) {
            String line;
            while ((line = r.readLine()) != null) log(line);
        } catch (Exception e) {
            log("读取引擎输出失败：" + e.getMessage());
        }
    }

    /**
     * 尽力优雅终止：Go 侧监听 SIGTERM 做 pool.Flush（state 落盘）。
     * Java 的 Process.destroy() 在 Android 上发的是 SIGKILL —— 直接把进程打死会丢状态，
     * 所以先反射取 pid 发 SIGTERM，等 1.5 秒仍在才 destroy()。
     */
    private void terminate(Process p) {
        int pid = pidOf(p);
        if (pid > 0) {
            try {
                android.os.Process.sendSignal(pid, 15); // 15 = SIGTERM
            } catch (Throwable ignore) {
                // 权限/内核限制：退回 destroy()
            }
        }
        try {
            if (!p.waitFor(1500, java.util.concurrent.TimeUnit.MILLISECONDS)) p.destroy();
        } catch (Exception e) {
            p.destroy();
        }
    }

    private static int pidOf(Process p) {
        try {
            Method m = Process.class.getMethod("pid");
            Object v = m.invoke(p);
            return v instanceof Integer ? (Integer) v : -1;
        } catch (Throwable t) {
            return -1;
        }
    }

    /* ── 探活 ────────────────────────────────────────────────────────── */

    /**
     * 探 /healthz。返回 true 表示端口已经在监听——注意 503 也算「在」（空账号池时
     * healthz 的语义就是 503），App 只关心引擎起来没有，不关心池健康。
     */
    boolean probe(int timeoutMs) {
        HttpURLConnection c = null;
        try {
            c = (HttpURLConnection) new URL(baseUrl() + "/healthz").openConnection();
            c.setConnectTimeout(timeoutMs);
            c.setReadTimeout(timeoutMs);
            c.setRequestMethod("GET");
            int code = c.getResponseCode();
            return code > 0;
        } catch (Exception e) {
            return false;
        } finally {
            if (c != null) c.disconnect();
        }
    }

    /** 轮询等待引擎就绪（首次启动要加载账号、建池，通常 1-3 秒）。 */
    boolean awaitReady(long totalMs) {
        long end = System.currentTimeMillis() + totalMs;
        while (System.currentTimeMillis() < end) {
            if (probe(700)) return true;
            sleep(350);
        }
        return false;
    }

    boolean isRunning() {
        Process p = proc;
        return p != null && p.isAlive();
    }

    /* ── 日志 ────────────────────────────────────────────────────────── */

    /** 追加一行日志：内存环（面板外的排障用）+ 落盘（重启后仍在）。 */
    void log(String line) {
        String stamped = new SimpleDateFormat("MM-dd HH:mm:ss", Locale.US).format(new Date()) + "  " + line;
        Log.i(TAG, line);
        synchronized (ringLock) {
            ring.addLast(stamped);
            while (ring.size() > LOG_RING_CAP) ring.removeFirst();
        }
        appendFile(stamped);
    }

    /** 最近 n 行日志（时间升序）。 */
    List<String> tail(int n) {
        synchronized (ringLock) {
            int skip = Math.max(0, ring.size() - n);
            List<String> out = new ArrayList<>(ring.size());
            int i = 0;
            for (String s : ring) {
                if (i++ >= skip) out.add(s);
            }
            return out;
        }
    }

    File logFile() {
        return new File(dir(), "engine.log");
    }

    private void appendFile(String line) {
        File f = logFile();
        try {
            if (f.length() > LOG_FILE_MAX) {
                File old = new File(dir(), "engine.log.1");
                //noinspection ResultOfMethodCallIgnored
                old.delete();
                //noinspection ResultOfMethodCallIgnored
                f.renameTo(old);
            }
            try (OutputStream os = new FileOutputStream(f, true)) {
                os.write((line + "\n").getBytes(StandardCharsets.UTF_8));
            }
        } catch (Exception ignore) {
            // 日志写失败不影响引擎本身
        }
    }

    /* ── 小工具 ──────────────────────────────────────────────────────── */

    private static void mkdirs(File f) {
        if (!f.exists()) {
            //noinspection ResultOfMethodCallIgnored
            f.mkdirs();
        }
    }

    private static String readAll(File f) throws IOException {
        StringBuilder sb = new StringBuilder();
        try (InputStream in = new java.io.FileInputStream(f);
             BufferedReader r = new BufferedReader(new InputStreamReader(in, StandardCharsets.UTF_8))) {
            String line;
            while ((line = r.readLine()) != null) sb.append(line).append('\n');
        }
        return sb.toString();
    }

    private static void writeAll(File f, String content) throws IOException {
        // 先写临时文件再 rename：引擎进程可能正在读 config.json，直接覆盖会读到半截内容。
        File tmp = new File(f.getParentFile(), f.getName() + ".tmp");
        try (OutputStream os = new FileOutputStream(tmp, false)) {
            os.write(content.getBytes(StandardCharsets.UTF_8));
        }
        if (!tmp.renameTo(f)) {
            // rename 失败（极少见）时退回直接写，至少保证配置可用。
            try (OutputStream os = new FileOutputStream(f, false)) {
                os.write(content.getBytes(StandardCharsets.UTF_8));
            }
        }
    }

    private static void sleep(long ms) {
        try {
            Thread.sleep(ms);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
        }
    }
}
