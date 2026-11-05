package com.wb2api.tasks;

import android.app.Activity;
import android.content.ActivityNotFoundException;
import android.content.Context;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.os.Message;
import android.os.PowerManager;
import android.provider.Settings;
import android.text.InputType;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.webkit.WebChromeClient;
import android.webkit.WebResourceError;
import android.webkit.WebResourceRequest;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.EditText;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.ProgressBar;
import android.widget.TextView;
import android.widget.Toast;

import java.util.TimeZone;

/**
 * 唯一界面：把本机引擎的面板装进 WebView。
 *
 * <h3>为什么不重写界面</h3>
 * 面板（含手机适配层 mobile.css/mobile.js）已经把账号池、任务中心、积分、日志、
 * 配置全都做完了。WebView 直接加载 127.0.0.1 的面板，功能与桌面端一比一，
 * 也不用维护第二套 UI。
 *
 * <h3>这个 Activity 自己只处理四件事</h3>
 * 1. 启动引擎并等待 /healthz 就绪（就绪前显示加载页，失败显示错误 + 日志尾巴）；
 * 2. 把「非本机地址」的跳转交给系统浏览器 —— OAuth 添加账号必须走真实浏览器
 *    （腾讯授权页要求完整浏览器环境，WebView 里登录容易触发风控）；
 * 3. 右下角的齿轮菜单：重启引擎 / 浏览器打开 / 引擎日志 / 电池白名单 / 设置；
 * 4. 返回键在面板内回退而不是直接退出。
 */
public class MainActivity extends Activity {
    /** 就绪等待上限：首次启动要读账号、建池、预热模型表，给足 25 秒。 */
    private static final long READY_TIMEOUT_MS = 25000;

    private final Handler ui = new Handler(Looper.getMainLooper());

    private WebView web;
    /** 右下角悬浮齿轮（引擎菜单）。弹层打开时由页面经 JS 桥隐藏，避免盖住按钮。 */
    private TextView gear;
    /** 引擎就绪前的整屏启动层：品牌区 + 状态行 + 进度条 + 失败卡片。 */
    private View bootLayer;
    private TextView bootStatus;
    private ProgressBar bootBar;
    private LinearLayout bootError;
    private TextView bootErrorText;
    private boolean loadedOnce = false;

    /** 面板里 &lt;input type=file&gt;（添加账号 → 导入）用的回调用；null = 没有待处理的选择。 */
    private android.webkit.ValueCallback<Uri[]> pendingFileCallback;
    private static final int REQ_PICK_FILE = 2001;

    /* ── 生命周期 ────────────────────────────────────────────────────── */

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        buildUi();
        askNotificationPermission();
        boot();
    }

    @Override
    protected void onResume() {
        super.onResume();
        // 从浏览器授权页返回：引擎若已就绪而页面还是空的，补一次加载。
        if (loadedOnce && web != null && web.getUrl() == null && Engine.get(this).probe(400)) {
            loadPanel();
        }
        // 回到前台时确保引擎在跑（可能被 ROM 清掉了）。
        EngineService.start(this);
    }

    @Override
    public void onBackPressed() {
        if (web != null && web.canGoBack()) {
            web.goBack();
            return;
        }
        super.onBackPressed();
    }

    /** 文件选择器的结果必须回传给 WebView，否则页面里的 file input 会一直等。 */
    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        if (requestCode != REQ_PICK_FILE) {
            super.onActivityResult(requestCode, resultCode, data);
            return;
        }
        Uri[] result = null;
        if (resultCode == RESULT_OK && data != null) {
            if (data.getClipData() != null) {
                int n = data.getClipData().getItemCount();
                result = new Uri[n];
                for (int i = 0; i < n; i++) {
                    result[i] = data.getClipData().getItemAt(i).getUri();
                }
            } else if (data.getData() != null) {
                result = new Uri[]{data.getData()};
            }
        }
        if (pendingFileCallback != null) {
            pendingFileCallback.onReceiveValue(result);
            pendingFileCallback = null;
        }
    }

    /* ── 界面 ────────────────────────────────────────────────────────── */

    private void buildUi() {
        FrameLayout root = new FrameLayout(this);

        web = new WebView(this);
        FrameLayout.LayoutParams webLp = new FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT);
        root.addView(web, webLp);
        setupWebView(web);

        root.addView(buildBootLayer(), new FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT));

        // 齿轮：面板是全屏网页，引擎相关的入口只能浮在上面。做成右下角圆形小按钮
        // （40dp、半透明底 + 发丝描边），比原来那块不透明方块安静得多。
        gear = Ui.text(this, "⚙", 18, Ui.INK_2, false);
        gear.setGravity(Gravity.CENTER);
        gear.setBackground(Ui.cardBg(0xCC14171F, 20, this));
        gear.setOnClickListener(v -> showMenu());
        FrameLayout.LayoutParams gearLp = new FrameLayout.LayoutParams(
                Ui.dp(this, 40), Ui.dp(this, 40));
        gearLp.gravity = Gravity.BOTTOM | Gravity.END;
        gearLp.setMargins(0, 0, Ui.dp(this, 14), Ui.dp(this, 48));
        root.addView(gear, gearLp);

        setContentView(root);
    }

    /**
     * 启动层：App 唯一一处"自己设计"的画面。信息优先级 —— 现在在做什么（状态行） >
     * 出错后怎么办（错误卡片里的重试 / 日志） > 装饰。色板与 WebView 里的面板同源
     * （{@link Ui}），所以从启动层切到面板不会有观感断层。
     */
    private View buildBootLayer() {
        // 外层用 ScrollView：失败卡片带日志尾巴时，小屏（含横屏）不会被裁掉半截；
        // fillViewport 保证内容少时仍然垂直居中。
        android.widget.ScrollView scroller = new android.widget.ScrollView(this);
        scroller.setFillViewport(true);
        scroller.setBackgroundColor(Ui.BG);

        LinearLayout content = new LinearLayout(this);
        content.setOrientation(LinearLayout.VERTICAL);
        content.setGravity(Gravity.CENTER);
        content.setPadding(Ui.dp(this, 28), Ui.dp(this, 24), Ui.dp(this, 28), Ui.dp(this, 24));
        scroller.addView(content, new FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));

        TextView logo = Ui.text(this, "✓", 30, Ui.OK, true);
        logo.setGravity(Gravity.CENTER);
        logo.setBackground(Ui.fill(Ui.ACCENT_SOFT, 18, this));
        LinearLayout.LayoutParams logoLp = new LinearLayout.LayoutParams(
                Ui.dp(this, 64), Ui.dp(this, 64));
        logoLp.gravity = Gravity.CENTER_HORIZONTAL;
        content.addView(logo, logoLp);

        TextView name = Ui.text(this, "wb2api-panel-Pro", 17, Ui.INK, true);
        name.setGravity(Gravity.CENTER);
        LinearLayout.LayoutParams nameLp = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        nameLp.gravity = Gravity.CENTER_HORIZONTAL;
        nameLp.topMargin = Ui.dp(this, 16);
        content.addView(name, nameLp);

        TextView sub = Ui.text(this, "签到 · 任务 · 积分", 12, Ui.INK_3, false);
        sub.setGravity(Gravity.CENTER);
        LinearLayout.LayoutParams subLp = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        subLp.gravity = Gravity.CENTER_HORIZONTAL;
        subLp.topMargin = Ui.dp(this, 4);
        content.addView(sub, subLp);

        bootStatus = Ui.mono(this, "正在启动引擎…", 12, Ui.INK_2);
        bootStatus.setGravity(Gravity.CENTER);
        LinearLayout.LayoutParams stLp = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        stLp.gravity = Gravity.CENTER_HORIZONTAL;
        stLp.topMargin = Ui.dp(this, 32);
        content.addView(bootStatus, stLp);

        bootBar = new ProgressBar(this, null, android.R.attr.progressBarStyleHorizontal);
        bootBar.setIndeterminate(true);
        bootBar.setIndeterminateTintList(android.content.res.ColorStateList.valueOf(Ui.ACCENT));
        LinearLayout.LayoutParams barLp = new LinearLayout.LayoutParams(
                Ui.dp(this, 132), Ui.dp(this, 3));
        barLp.gravity = Gravity.CENTER_HORIZONTAL;
        barLp.topMargin = Ui.dp(this, 14);
        content.addView(bootBar, barLp);

        // 失败卡片（默认隐藏）：不弹系统对话框，就地显示"发生了什么 + 最近日志 + 重试"。
        bootError = new LinearLayout(this);
        bootError.setOrientation(LinearLayout.VERTICAL);
        bootError.setBackground(Ui.cardBg(Ui.SURFACE, 14, this));
        bootError.setPadding(Ui.dp(this, 14), Ui.dp(this, 12), Ui.dp(this, 14), Ui.dp(this, 14));
        bootError.setVisibility(View.GONE);
        LinearLayout.LayoutParams errLp = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        errLp.topMargin = Ui.dp(this, 24);
        content.addView(bootError, errLp);

        bootError.addView(Ui.text(this, "引擎没有启动起来", 13, Ui.BAD, true));

        bootErrorText = Ui.mono(this, "", 12, Ui.INK_2);
        bootErrorText.setTextIsSelectable(true);
        bootErrorText.setMaxLines(12);
        bootErrorText.setPadding(0, Ui.dp(this, 6), 0, 0);
        bootError.addView(bootErrorText);

        LinearLayout acts = new LinearLayout(this);
        acts.setOrientation(LinearLayout.HORIZONTAL);
        LinearLayout.LayoutParams actsLp = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        actsLp.topMargin = Ui.dp(this, 12);
        bootError.addView(acts, actsLp);

        TextView retry = Ui.button(this, "重试", true);
        retry.setOnClickListener(v -> boot());
        acts.addView(retry);

        TextView logs = Ui.button(this, "引擎日志", false);
        LinearLayout.LayoutParams logsLp = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        logsLp.leftMargin = Ui.dp(this, 8);
        logs.setOnClickListener(v -> showLogs());
        acts.addView(logs, logsLp);

        bootLayer = scroller;
        return bootLayer;
    }

    private void setupWebView(WebView w) {
        WebSettings s = w.getSettings();
        s.setJavaScriptEnabled(true);
        s.setDomStorageEnabled(true);          // 面板用 localStorage 存主题与 api_key
        s.setSupportMultipleWindows(true);      // window.open 由 onCreateWindow 接管
        s.setJavaScriptCanOpenWindowsAutomatically(true);
        s.setAllowFileAccess(false);
        s.setAllowContentAccess(false);
        s.setDatabaseEnabled(true);
        s.setCacheMode(WebSettings.LOAD_DEFAULT);
        s.setTextZoom(100);
        // 关闭长按选择/缩放带来的误触（面板本身是响应式布局，不依赖双击缩放）。
        s.setUseWideViewPort(true);
        s.setLoadWithOverviewMode(false);
        s.setBuiltInZoomControls(false);

        // 原生视图永远画在 WebView 之上：任务抽屉 / 更多菜单 / 导入弹层打开时，
        // 不隐藏齿轮就会盖住抽屉底部的按钮。面板 app.js 观察 .veil.on 并经此桥通知；
        // 桌面浏览器没有这个对象，页面侧的调用是空操作。
        w.addJavascriptInterface(new Object() {
            @android.webkit.JavascriptInterface
            public void setOverlay(final boolean on) {
                runOnUiThread(() -> {
                    if (gear != null) gear.setVisibility(on ? View.GONE : View.VISIBLE);
                });
            }
        }, "Wb2ApiApp");

        w.setWebViewClient(new WebViewClient() {
            @Override
            public boolean shouldOverrideUrlLoading(WebView view, WebResourceRequest request) {
                return handleUrl(request.getUrl().toString());
            }

            @Override
            public boolean shouldOverrideUrlLoading(WebView view, String url) {
                return handleUrl(url);
            }

            @Override
            public void onPageFinished(WebView view, String url) {
                loadedOnce = true;
                hideBusy();
            }

            @Override
            public void onReceivedError(WebView view, WebResourceRequest request, WebResourceError error) {
                // 只处理主文档错误；子资源失败（比如 favicon）不该盖住整个页面。
                if (request != null && request.isForMainFrame()) {
                    showError("面板加载失败",
                            String.valueOf(error.getDescription())
                                    + "\n\n可点右下角齿轮 → 引擎日志 查看原因。");
                }
            }
        });

        w.setWebChromeClient(new WebChromeClient() {
            @Override
            public boolean onCreateWindow(WebView view, boolean isDialog,
                                          boolean isUserGesture, Message resultMsg) {
                // 面板里的「打开授权页」走 window.open。WebView 弹窗没有地址栏、没有
                // 完整浏览器环境，OAuth 容易失败或被风控，因此一律转系统浏览器：
                // 用一个临时 WebView 接住目标地址，再交给 Intent。
                WebView tmp = new WebView(MainActivity.this);
                tmp.setWebViewClient(new WebViewClient() {
                    @Override
                    public boolean shouldOverrideUrlLoading(WebView v, WebResourceRequest req) {
                        openExternal(req.getUrl().toString());
                        v.post(v::destroy); // 不在自身回调里 destroy
                        return true;
                    }

                    @Override
                    public boolean shouldOverrideUrlLoading(WebView v, String url) {
                        openExternal(url);
                        v.post(v::destroy);
                        return true;
                    }
                });
                WebView.WebViewTransport transport = (WebView.WebViewTransport) resultMsg.obj;
                transport.setWebView(tmp);
                resultMsg.sendToTarget();
                return true;
            }

            @Override
            public boolean onShowFileChooser(WebView view,
                                             android.webkit.ValueCallback<Uri[]> callback,
                                             android.webkit.WebChromeClient.FileChooserParams params) {
                // 不实现这个回调，面板「添加账号 → 导入」的 <input type=file> 在 WebView
                // 里点了没反应（WebView 不会自己拉起系统的文件选择器）。
                if (pendingFileCallback != null) {
                    pendingFileCallback.onReceiveValue(null);
                }
                pendingFileCallback = callback;
                try {
                    startActivityForResult(params.createIntent(), REQ_PICK_FILE);
                } catch (ActivityNotFoundException e) {
                    pendingFileCallback = null;
                    toast("没有可用的文件选择器");
                    return false;
                }
                return true;
            }
        });
    }

    /** 本机回环的地址在 WebView 内加载，其余一律交给系统浏览器。 */
    private boolean handleUrl(String url) {
        Uri u = Uri.parse(url);
        String host = u.getHost();
        if (host == null
                || "127.0.0.1".equals(host)
                || "localhost".equals(host)
                || "::1".equals(host)) {
            return false; // 交给 WebView 自己加载
        }
        openExternal(url);
        return true;
    }

    private void openExternal(String url) {
        try {
            startActivity(new Intent(Intent.ACTION_VIEW, Uri.parse(url)));
        } catch (ActivityNotFoundException e) {
            toast("没有可用的浏览器打开该链接");
        }
    }

    /* ── 启动流程 ────────────────────────────────────────────────────── */

    private void boot() {
        showBusy("正在启动引擎…");
        EngineService.start(this);
        // 状态分两段：等端口 → 载入账号。只在仍停在启动层时更新第二段，
        // 避免面板已经出来又把状态文案改回去。
        ui.postDelayed(() -> {
            if (bootLayer.getVisibility() == View.VISIBLE
                    && bootError.getVisibility() != View.VISIBLE) {
                bootStatus.setText("正在载入账号…");
            }
        }, 1200);

        Thread t = new Thread(() -> {
            Engine eng = Engine.get(this);
            // 目录与 config.json 由 Engine.start()（进程内、串行）准备好，这里只等端口就绪。
            boolean ok = eng.awaitReady(READY_TIMEOUT_MS);
            ui.post(() -> {
                if (ok) {
                    loadPanel();
                } else {
                    showError("引擎启动超时（" + (READY_TIMEOUT_MS / 1000) + " 秒）",
                            "· APK 里没有内嵌引擎？\n"
                                    + "· 手机不是 arm64？\n"
                                    + "· 端口被占用？（齿轮 → 设置 可改端口）\n\n"
                                    + "最近日志：\n" + join(eng.tail(12)));
                }
            });
        }, "engine-boot");
        t.setDaemon(true);
        t.start();
    }

    private void loadPanel() {
        loadedOnce = true;
        web.loadUrl(Engine.get(this).baseUrl() + "/panel/");
    }

    /** 启动中：状态行 + 进度条；失败卡片收起。 */
    private void showBusy(String status) {
        bootLayer.setVisibility(View.VISIBLE);
        bootError.setVisibility(View.GONE);
        bootStatus.setVisibility(View.VISIBLE);
        bootBar.setVisibility(View.VISIBLE);
        bootStatus.setText(status);
    }

    /** 失败：收起状态行与进度条，换成可读的错误卡片（摘要 + 日志尾巴 + 重试）。 */
    private void showError(String summary, String detail) {
        bootLayer.setVisibility(View.VISIBLE);
        bootStatus.setVisibility(View.GONE);
        bootBar.setVisibility(View.GONE);
        bootError.setVisibility(View.VISIBLE);
        bootErrorText.setText(detail == null || detail.isEmpty() ? summary : summary + "\n\n" + detail);
    }

    private void hideBusy() {
        bootLayer.setVisibility(View.GONE);
    }

    /* ── 齿轮菜单 ────────────────────────────────────────────────────── */

    private void showMenu() {
        final String[] items = {
                "重启引擎",
                "在浏览器中打开面板",
                "引擎日志",
                "加入电池优化白名单",
                "设置（端口 / 时区）",
                "停止引擎并退出",
        };
        // 底部抽屉：拇指可达、行高更大，比系统 AlertDialog 的列表更少误触。
        Ui.sheet(this, "引擎", items, 5, which -> {
            switch (which) {
                case 0:
                    restartEngine();
                    break;
                case 1:
                    openExternal(Engine.get(this).baseUrl() + "/panel/");
                    break;
                case 2:
                    showLogs();
                    break;
                case 3:
                    ignoreBattery();
                    break;
                case 4:
                    showSettings();
                    break;
                case 5:
                    PulseReceiver.cancel(this);
                    EngineService.stop(this);
                    finish();
                    break;
                default:
                    break;
            }
        });
    }

    private void restartEngine() {
        showBusy("正在重启引擎…");
        Engine eng = Engine.get(this);
        eng.restart();
        Thread t = new Thread(() -> {
            boolean ok = eng.awaitReady(READY_TIMEOUT_MS);
            ui.post(() -> {
                if (ok) {
                    loadPanel();
                } else {
                    showError("重启失败", "最近日志：\n" + join(eng.tail(12)));
                }
            });
        }, "engine-restart");
        t.setDaemon(true);
        t.start();
    }

    private void showLogs() {
        Engine eng = Engine.get(this);
        final String body = join(eng.tail(300)).isEmpty() ? "（还没有日志）" : join(eng.tail(300));

        TextView tv = Ui.mono(this, body, 12, Ui.INK_2);
        tv.setTextIsSelectable(true);
        tv.setPadding(Ui.dp(this, 12), Ui.dp(this, 10), Ui.dp(this, 12), Ui.dp(this, 10));

        android.widget.ScrollView sv = new android.widget.ScrollView(this);
        sv.setBackground(Ui.fill(Ui.BG, 12, this));
        sv.addView(tv);

        // ScrollView 需要确定高度才能滚动：给一个"约半屏"的固定值。
        LinearLayout holder = new LinearLayout(this);
        holder.setOrientation(LinearLayout.VERTICAL);
        holder.addView(sv, new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, Ui.dp(this, 320)));

        Ui.card(this, "引擎日志 · 最近 300 行", holder, "复制日志", () -> {
            android.content.ClipboardManager cm =
                    (android.content.ClipboardManager) getSystemService(Context.CLIPBOARD_SERVICE);
            if (cm != null) {
                cm.setPrimaryClip(android.content.ClipData.newPlainText("wb2api-log", body));
                toast("日志已复制");
            }
        });
    }

    private void showSettings() {
        final Prefs p = Prefs.get(this);

        LinearLayout box = new LinearLayout(this);
        box.setOrientation(LinearLayout.VERTICAL);

        box.addView(caption("端口"));
        final EditText portIn = Ui.field(this, "1024-65535", InputType.TYPE_CLASS_NUMBER);
        portIn.setText(String.valueOf(p.port()));
        LinearLayout.LayoutParams plp = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        plp.bottomMargin = Ui.dp(this, 12);
        box.addView(portIn, plp);

        box.addView(caption("时区"));
        final EditText tzIn = Ui.field(this, "Asia/Shanghai", InputType.TYPE_CLASS_TEXT);
        tzIn.setText(p.tz());
        box.addView(tzIn);

        TextView tip = Ui.text(this,
                "时区必须是 IANA 名称（Asia/Shanghai、Asia/Tokyo…）。填错会让签到按 UTC 钟点触发。"
                        + "保存后引擎会自动重启，账号与任务数据不受影响。",
                11, Ui.INK_3, false);
        tip.setLineSpacing(0, 1.2f);
        LinearLayout.LayoutParams tlp = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        tlp.topMargin = Ui.dp(this, 10);
        box.addView(tip, tlp);

        Ui.card(this, "引擎设置", box, "保存", () -> {
            int port;
            try {
                port = Integer.parseInt(portIn.getText().toString().trim());
            } catch (Exception e) {
                toast("端口必须是数字");
                return;
            }
            if (port < 1024 || port > 65535) {
                toast("端口需在 1024-65535 之间");
                return;
            }
            String tz = tzIn.getText().toString().trim();
            if (tz.isEmpty()) tz = "Asia/Shanghai";
            // 未知 ID 会被 java.util.TimeZone 兜底成 GMT。提前拦住：引擎侧
            // LoadLocation 失败只会 WARN 后沿用 UTC，「今日」和排程会整体偏 8 小时。
            if (!TimeZone.getTimeZone(tz).getID().equals(tz)) {
                toast("时区无效，请填 IANA 名称（如 Asia/Shanghai）");
                return;
            }
            p.setPort(port);
            p.setTz(tz);
            // 端口要写回 config.json（引擎按配置监听），时区靠 TZ 环境变量。
            Engine.get(this).ensureLayout();
            restartEngine();
        });
    }

    /** 表单小标题：11px、灰、略放松字距（与面板 label.fld > .lb 同口径）。 */
    private TextView caption(String s) {
        TextView t = Ui.text(this, s, 12, Ui.INK_3, false);
        t.setLetterSpacing(0.02f);
        t.setPadding(0, 0, 0, Ui.dp(this, 5));
        return t;
    }

    private void ignoreBattery() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            PowerManager pm = (PowerManager) getSystemService(Context.POWER_SERVICE);
            if (pm != null && pm.isIgnoringBatteryOptimizations(getPackageName())) {
                toast("已在电池优化白名单中");
                return;
            }
            try {
                Intent i = new Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS);
                i.setData(Uri.parse("package:" + getPackageName()));
                startActivity(i);
            } catch (Exception e) {
                // 部分 ROM 没有这个入口：退回电池优化列表页，让用户手动找。
                try {
                    startActivity(new Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS));
                } catch (Exception e2) {
                    toast("该机型不支持自动跳转，请到系统设置里手动允许后台运行");
                }
            }
        }
    }

    private void askNotificationPermission() {
        if (Build.VERSION.SDK_INT >= 33) {
            if (checkSelfPermission("android.permission.POST_NOTIFICATIONS")
                    != PackageManager.PERMISSION_GRANTED) {
                requestPermissions(new String[]{"android.permission.POST_NOTIFICATIONS"}, 1);
            }
        }
    }

    /* ── 小工具 ──────────────────────────────────────────────────────── */

    private void toast(String msg) {
        Toast.makeText(this, msg, Toast.LENGTH_SHORT).show();
    }

    private static String join(java.util.List<String> lines) {
        StringBuilder sb = new StringBuilder();
        for (String l : lines) sb.append(l).append('\n');
        return sb.toString();
    }
}
