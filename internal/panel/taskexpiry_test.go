package panel

import (
	"testing"
	"time"

	"github.com/longyanbo1224-debug/wb2api-panel-Pro/internal/auth"
	"github.com/longyanbo1224-debug/wb2api-panel-Pro/internal/upstream"
)

// expiringTask 构造一条带 valid_end 的任务（默认 2 天后到期、进度 0/1）。
func expiringTask(code string, inDays int) upstream.Task {
	return upstream.Task{
		TaskCode: code,
		Title:    code + " 标题",
		Credit:   100,
		Target:   1,
		Current:  0,
		ValidEnd: time.Now().AddDate(0, 0, inDays).Format(time.RFC3339),
	}
}

// TestWarnExpiringTasksDedupPerDay 同一账号同一任务一天只告警一次
// （补领清扫 15 分钟一轮，不去重会一天刷 96 行）。
func TestWarnExpiringTasksDedupPerDay(t *testing.T) {
	resetExpiryWarned()
	defer resetExpiryWarned()
	p := &Panel{}
	a := &auth.Auth{UID: "u1", Nickname: "一号"}
	tasks := []upstream.Task{expiringTask("Hp_Appearance", 2)}

	if got := p.warnExpiringTasks(a, tasks); len(got) != 1 {
		t.Fatalf("首轮告警 %d 条 want 1", len(got))
	}
	if got := p.warnExpiringTasks(a, tasks); len(got) != 0 {
		t.Errorf("同日重复扫描仍告警 %d 条 want 0（应按天去重）", len(got))
	}
	// 另一个账号不受影响（去重粒度是 账号+任务）。
	other := &auth.Auth{UID: "u2", Nickname: "二号"}
	if got := p.warnExpiringTasks(other, tasks); len(got) != 1 {
		t.Errorf("另一账号首轮告警 %d 条 want 1", len(got))
	}
}

// TestWarnExpiringTasksSkipsFinished 已领取 / 已达标 / 无进度语义 / 无有效期都不告警。
func TestWarnExpiringTasksSkipsFinished(t *testing.T) {
	resetExpiryWarned()
	defer resetExpiryWarned()
	p := &Panel{}
	a := &auth.Auth{UID: "u1"}

	claimed := expiringTask("claimed_1", 1)
	claimed.Claimed = true
	done := expiringTask("done_1", 1)
	done.Current = 1
	noTarget := expiringTask("no_target", 1)
	noTarget.Target = 0
	noValid := upstream.Task{TaskCode: "no_valid", Target: 1, Current: 0}
	far := expiringTask("far", 30)

	got := p.warnExpiringTasks(a, []upstream.Task{claimed, done, noTarget, noValid, far})
	if len(got) != 0 {
		t.Errorf("告警 %d 条 want 0: %+v", len(got), got)
	}
}

// TestWarnExpiringTasksExpired 已过期的任务也要报（静默跳过等于把丢掉的分藏起来）。
func TestWarnExpiringTasksExpired(t *testing.T) {
	resetExpiryWarned()
	defer resetExpiryWarned()
	p := &Panel{}
	a := &auth.Auth{UID: "u1"}

	got := p.warnExpiringTasks(a, []upstream.Task{expiringTask("gone", -1)})
	if len(got) != 1 {
		t.Fatalf("过期任务告警 %d 条 want 1", len(got))
	}
	if !got[0].Expired {
		t.Error("过期任务的 Expired 应为 true")
	}
}

// TestTaskSweepSurvivesValidEnd 补领清扫遇到带 valid_end 的任务不 panic，
// 且到期预警不影响既有的 accept/claim 流程。
func TestTaskSweepSurvivesValidEnd(t *testing.T) {
	resetExpiryWarned()
	defer resetExpiryWarned()
	b := &sweepBackend{
		listFn: func(isMP bool) []map[string]any {
			if isMP {
				return nil
			}
			return []map[string]any{
				sweepTask("chat_5", 5, 5, "accepted", map[string]any{
					"valid_end": time.Now().AddDate(0, 0, 3).Format(time.RFC3339),
				}),
			}
		},
		claimFn: func(code string) (int, map[string]any) {
			return claimOK(map[string]any{"already_claimed": false, "credit": 100, "energy": 5})
		},
	}
	p := newSweepPanel(t, b, &auth.Auth{UID: "u1", AccessToken: "at", RefreshToken: "rt", ExpiresAt: 9999999999})

	stats := p.RunTaskSweepOnce()
	if stats.Claimed != 1 || stats.Credits != 100 {
		t.Errorf("stats=%+v want claimed=1 credits=100（到期预警不得干扰领奖）", stats)
	}
}
