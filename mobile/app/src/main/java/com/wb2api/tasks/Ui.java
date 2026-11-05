package com.wb2api.tasks;

import android.app.Activity;
import android.app.Dialog;
import android.content.Context;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.ColorDrawable;
import android.graphics.drawable.GradientDrawable;
import android.graphics.drawable.StateListDrawable;
import android.text.TextUtils;
import android.util.TypedValue;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.view.Window;
import android.view.WindowManager;
import android.widget.EditText;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.TextView;

/**
 * 原生壳的视觉令牌与控件工厂。
 *
 * <p>色值直接取自 {@code internal/panel/index.html} 的 {@code :root}：原生界面与
 * WebView 里的面板必须是同一套观感——冷石墨底、单一靛蓝强调、状态色只给状态。
 *
 * <p>两条手机上的取舍：
 * <ul>
 *   <li>不画阴影。小屏上阴影糊成脏边，层级改用 1px 描边 + 圆角表达。</li>
 *   <li>所有数字/日志用等宽字体，像一本台账；标题与正文用系统字体。</li>
 * </ul>
 */
final class Ui {
    static final int BG = 0xFF0C0E14;
    static final int SURFACE = 0xFF14171F;
    static final int SURFACE_2 = 0xFF1A1E28;
    static final int LINE = 0xFF262C3A;
    static final int LINE_SOFT = 0xFF1E232E;
    static final int INK = 0xFFE8EBF2;
    static final int INK_2 = 0xFFA8B0C2;
    static final int INK_3 = 0xFF6B7488;
    static final int ACCENT = 0xFF5B7CFA;
    static final int ACCENT_SOFT = 0x225B7CFA;
    static final int OK = 0xFF3DDC97;
    static final int WARN = 0xFFF5B544;
    static final int BAD = 0xFFF0655F;

    private Ui() {
    }

    static int dp(Context c, float v) {
        return Math.round(TypedValue.applyDimension(
                TypedValue.COMPLEX_UNIT_DIP, v, c.getResources().getDisplayMetrics()));
    }

    /* ── 形状 ────────────────────────────────────────────────────────── */

    static GradientDrawable fill(int color, float radiusDp, Context c) {
        GradientDrawable d = new GradientDrawable();
        d.setColor(color);
        d.setCornerRadius(dp(c, radiusDp));
        return d;
    }

    /** 描边卡片：1px 发丝线，替代阴影表达层级。 */
    static GradientDrawable cardBg(int color, float radiusDp, Context c) {
        GradientDrawable d = fill(color, radiusDp, c);
        d.setStroke(Math.max(1, dp(c, 1) / 2), LINE);
        return d;
    }

    /** 只有上方两个角是圆的（底部抽屉）。 */
    static GradientDrawable topRounded(int color, float radiusDp, Context c) {
        float r = dp(c, radiusDp);
        GradientDrawable d = new GradientDrawable();
        d.setColor(color);
        d.setCornerRadii(new float[]{r, r, r, r, 0, 0, 0, 0});
        return d;
    }

    /** 无描边的按压反馈（抽屉菜单行）。 */
    static StateListDrawable pressable(Context c, float radiusDp) {
        GradientDrawable normal = new GradientDrawable();
        normal.setColor(Color.TRANSPARENT);
        normal.setCornerRadius(dp(c, radiusDp));
        GradientDrawable pressed = new GradientDrawable();
        pressed.setColor(SURFACE_2);
        pressed.setCornerRadius(dp(c, radiusDp));
        StateListDrawable s = new StateListDrawable();
        s.addState(new int[]{android.R.attr.state_pressed}, pressed);
        s.addState(new int[]{}, normal);
        return s;
    }

    /* ── 文本与控件 ──────────────────────────────────────────────────── */

    static TextView text(Context c, CharSequence s, float sizeSp, int color, boolean bold) {
        TextView t = new TextView(c);
        t.setText(s);
        t.setTextSize(TypedValue.COMPLEX_UNIT_SP, sizeSp);
        t.setTextColor(color);
        if (bold) t.setTypeface(t.getTypeface(), Typeface.BOLD);
        return t;
    }

    /** 等宽文本：数字、日志、端口这类"要对齐/要分辨字符"的内容。 */
    static TextView mono(Context c, CharSequence s, float sizeSp, int color) {
        TextView t = text(c, s, sizeSp, color, false);
        t.setTypeface(Typeface.MONOSPACE);
        t.setLetterSpacing(0.01f);
        return t;
    }

    static TextView button(Context c, CharSequence s, boolean primary) {
        TextView t = text(c, s, 14, primary ? Color.WHITE : INK, primary);
        t.setGravity(Gravity.CENTER);
        t.setPadding(dp(c, 16), dp(c, 11), dp(c, 16), dp(c, 11));
        t.setMinHeight(dp(c, 42));
        t.setClickable(true);
        t.setFocusable(true);
        t.setBackground(primary ? fill(ACCENT, 10, c) : cardBg(SURFACE_2, 10, c));
        return t;
    }

    static EditText field(Context c, String hint, int inputType) {
        EditText e = new EditText(c);
        e.setHint(hint);
        e.setHintTextColor(INK_3);
        e.setTextColor(INK);
        e.setTextSize(TypedValue.COMPLEX_UNIT_SP, 15);
        e.setInputType(inputType);
        e.setSingleLine(true);
        e.setBackground(cardBg(SURFACE_2, 10, c));
        e.setPadding(dp(c, 12), dp(c, 10), dp(c, 12), dp(c, 10));
        return e;
    }

    static View hairline(Context c) {
        View v = new View(c);
        v.setBackgroundColor(LINE_SOFT);
        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, Math.max(1, dp(c, 0.5f)));
        v.setLayoutParams(lp);
        return v;
    }

    /* ── 底部抽屉 ────────────────────────────────────────────────────── */

    interface OnPick {
        void onPick(int index);
    }

    /**
     * 底部抽屉菜单。相比系统 AlertDialog 的列表：从底部弹出（拇指可达）、
     * 行高更大、和面板同一套底色，长按误触也少。
     *
     * @param dangerIndex 需要标红的行（如"停止引擎并退出"），-1 表示没有
     */
    static void sheet(Activity a, String title, String[] items, int dangerIndex, final OnPick onPick) {
        final Dialog d = new Dialog(a, android.R.style.Theme_DeviceDefault_NoActionBar);
        d.requestWindowFeature(Window.FEATURE_NO_TITLE);

        LinearLayout root = new LinearLayout(a);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setBackground(topRounded(SURFACE, 20, a));
        root.setPadding(0, dp(a, 6), 0, dp(a, 14));

        View handle = new View(a);
        handle.setBackground(fill(LINE, 2, a));
        LinearLayout.LayoutParams hp = new LinearLayout.LayoutParams(dp(a, 34), dp(a, 4));
        hp.gravity = Gravity.CENTER_HORIZONTAL;
        hp.topMargin = dp(a, 4);
        hp.bottomMargin = dp(a, 10);
        root.addView(handle, hp);

        if (!TextUtils.isEmpty(title)) {
            TextView t = text(a, title, 12, INK_3, false);
            t.setPadding(dp(a, 20), 0, dp(a, 20), dp(a, 8));
            root.addView(t);
        }

        for (int i = 0; i < items.length; i++) {
            final int idx = i;
            TextView row = text(a, items[i], 15, i == dangerIndex ? BAD : INK, false);
            row.setGravity(Gravity.CENTER_VERTICAL);
            row.setPadding(dp(a, 20), dp(a, 14), dp(a, 20), dp(a, 14));
            row.setBackground(pressable(a, 12));
            row.setOnClickListener(v -> {
                d.dismiss();
                onPick.onPick(idx);
            });
            LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(
                    ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT);
            lp.setMargins(dp(a, 8), dp(a, 1), dp(a, 8), dp(a, 1));
            root.addView(row, lp);
        }

        TextView cancel = text(a, "取消", 15, INK_2, false);
        cancel.setGravity(Gravity.CENTER);
        cancel.setPadding(0, dp(a, 14), 0, dp(a, 6));
        cancel.setBackground(pressable(a, 12));
        cancel.setOnClickListener(v -> d.dismiss());
        LinearLayout.LayoutParams clp = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        clp.setMargins(dp(a, 8), dp(a, 6), dp(a, 8), 0);
        root.addView(cancel, clp);

        d.setContentView(root);
        // 点抽屉外的空白（遮罩区）直接收回：底部抽屉的标准手势，
        // 不该逼用户去点「取消」。窗口只包住抽屉本体，空白区在窗口外，
        // setCanceledOnTouchOutside 靠 WATCH_OUTSIDE_TOUCH 接住这个点击。
        d.setCanceledOnTouchOutside(true);
        Window w = d.getWindow();
        if (w != null) {
            w.setBackgroundDrawable(new ColorDrawable(Color.TRANSPARENT));
            w.addFlags(WindowManager.LayoutParams.FLAG_DIM_BEHIND);
            w.setDimAmount(0.55f);
            w.setLayout(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT);
            w.setGravity(Gravity.BOTTOM);
        }
        d.show();
    }

    /* ── 居中卡片对话框 ──────────────────────────────────────────────── */

    /**
     * 居中卡片对话框。{@code okText} 为 null 时只显示"关闭"。
     * 返回 Dialog，调用方可自行继续装配（例如需要"记住输入值"时）。
     */
    static Dialog card(Activity a, String title, View body, String okText, final Runnable onOk) {
        final Dialog d = new Dialog(a, android.R.style.Theme_DeviceDefault_NoActionBar);
        d.requestWindowFeature(Window.FEATURE_NO_TITLE);

        FrameLayout wrap = new FrameLayout(a);
        int edge = dp(a, 18);
        wrap.setPadding(edge, edge, edge, edge);

        LinearLayout root = new LinearLayout(a);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setBackground(fill(SURFACE, 18, a));
        root.setPadding(dp(a, 16), dp(a, 16), dp(a, 16), dp(a, 14));

        if (!TextUtils.isEmpty(title)) {
            root.addView(text(a, title, 16, INK, true));
        }
        if (body != null) {
            LinearLayout.LayoutParams bp = new LinearLayout.LayoutParams(
                    ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT);
            bp.topMargin = dp(a, 12);
            root.addView(body, bp);
        }

        LinearLayout actions = new LinearLayout(a);
        actions.setOrientation(LinearLayout.HORIZONTAL);
        actions.setGravity(Gravity.END);
        LinearLayout.LayoutParams ap = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        ap.topMargin = dp(a, 16);
        root.addView(actions, ap);

        TextView cancel = button(a, "关闭", false);
        cancel.setOnClickListener(v -> d.dismiss());
        LinearLayout.LayoutParams clp = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        clp.rightMargin = dp(a, 8);
        actions.addView(cancel, clp);

        if (okText != null) {
            TextView ok = button(a, okText, true);
            ok.setOnClickListener(v -> {
                d.dismiss();
                if (onOk != null) onOk.run();
            });
            actions.addView(ok);
        }

        wrap.addView(root, new FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));
        d.setContentView(wrap);
        Window w = d.getWindow();
        if (w != null) {
            w.setBackgroundDrawable(new ColorDrawable(Color.TRANSPARENT));
            w.addFlags(WindowManager.LayoutParams.FLAG_DIM_BEHIND);
            w.setDimAmount(0.55f);
            w.setLayout(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        }
        d.show();
        return d;
    }
}
