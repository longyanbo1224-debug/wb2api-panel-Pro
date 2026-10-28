package upstream

import (
	"testing"
	"time"
)

// TestTaskValidEndTime 钉住 valid_end 的三种上游形态与两种「不可用」形态。
// 关键语义：解析不出来一律返回 false——预警宁可漏报一天，也不能猜错日期
// 造成天天喊到期（那会让整个预警被运维无视）。
func TestTaskValidEndTime(t *testing.T) {
	// 期望值必须写成「时刻」而不是「本地钟点」：带时区的输入解析出的是绝对时刻，
	// 用 time.Local 拼钟点只在本地时区恰好是 +08 时相等（CI runner 是 UTC）。
	cst := time.FixedZone("CST", 8*3600)
	cases := []struct {
		name string
		in   string
		want time.Time // 零值 = 期望 ok=false
	}{
		{"RFC3339 带时区（实测口径）", "2026-11-13T23:59:00+08:00", time.Date(2026, 11, 13, 23, 59, 0, 0, cst)},
		{"无时区空格形态", "2026-11-13 23:59:00", time.Date(2026, 11, 13, 23, 59, 0, 0, time.Local)},
		{"纯日期按当天结束", "2026-11-13", time.Date(2026, 11, 13, 23, 59, 59, 0, time.Local)},
		{"空值", "", time.Time{}},
		{"无法识别", "下个月吧", time.Time{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := Task{ValidEnd: c.in}.ValidEndTime()
			if c.want.IsZero() {
				if ok {
					t.Fatalf("ValidEndTime(%q) ok=true want false", c.in)
				}
				return
			}
			if !ok {
				t.Fatalf("ValidEndTime(%q) ok=false want true", c.in)
			}
			if !got.Equal(c.want) {
				t.Errorf("ValidEndTime(%q)=%s want %s", c.in, got.Format(time.RFC3339), c.want.Format(time.RFC3339))
			}
		})
	}
}

// TestExpiringTasks 到期预警口径：未领取 + 进度可度量且未达标 + valid_end 落在窗口内。
// 已过期的一并返回（Expired=true）——静默跳过等于把已经丢掉的分藏起来。
func TestExpiringTasks(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.Local)
	window := 7 * 24 * time.Hour
	day := func(d int) string {
		return now.AddDate(0, 0, d).Format(time.RFC3339)
	}
	tasks := []Task{
		{TaskCode: "soon", Target: 1, Current: 0, ValidEnd: day(2)},                   // 2 天后到期 → 报
		{TaskCode: "expired", Target: 1, Current: 0, ValidEnd: day(-1)},               // 已过期 → 报（Expired）
		{TaskCode: "far", Target: 1, Current: 0, ValidEnd: day(30)},                   // 窗口外 → 不报
		{TaskCode: "claimed", Target: 1, Current: 1, Claimed: true, ValidEnd: day(1)}, // 已领 → 不报
		{TaskCode: "done", Target: 1, Current: 1, ValidEnd: day(1)},                   // 已达标未领（由补领负责）→ 不报
		{TaskCode: "no_target", Target: 0, Current: 0, ValidEnd: day(1)},              // 无进度语义 → 不报
		{TaskCode: "no_valid", Target: 1, Current: 0},                                 // 无有效期 → 不报
	}

	got := ExpiringTasks(tasks, now, window)
	byCode := map[string]ExpiringTask{}
	for _, g := range got {
		byCode[g.TaskCode] = g
	}
	if len(got) != 2 {
		t.Fatalf("ExpiringTasks 命中 %d 条 want 2: %+v", len(got), got)
	}
	if e, ok := byCode["soon"]; !ok || e.Expired || e.Days != 2 {
		t.Errorf("soon=%+v want Expired=false Days=2", e)
	}
	if e, ok := byCode["expired"]; !ok || !e.Expired || e.Days != 0 {
		t.Errorf("expired=%+v want Expired=true Days=0", e)
	}
}

// TestExpiringTasksEmptyWindow 空列表/空窗口不 panic、不误报。
func TestExpiringTasksEmptyWindow(t *testing.T) {
	now := time.Now()
	if got := ExpiringTasks(nil, now, 7*24*time.Hour); len(got) != 0 {
		t.Errorf("nil 输入返回 %d 条 want 0", len(got))
	}
	tasks := []Task{{TaskCode: "x", Target: 1, Current: 0, ValidEnd: now.Add(time.Hour).Format(time.RFC3339)}}
	if got := ExpiringTasks(tasks, now, 0); len(got) != 0 {
		t.Errorf("窗口 0 返回 %d 条 want 0（窗口外不报）", len(got))
	}
}
