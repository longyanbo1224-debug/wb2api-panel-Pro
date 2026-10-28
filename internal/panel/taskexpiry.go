// taskexpiry.go 任务到期预警：把「未完成 + 7 天内到期 / 已过期」的任务打进日志。
//
// 背景：上游成长任务带限时窗口（实测 `Hp_Appearance` 至 2026-11-02、
// `Expert_lighthouse` 至 2026-11-13、四个 `expert_use_*` 至 2026-11-20），
// 过期后不可再领，而任务列表只是安静地不再下发——不主动读 valid_end，
// 就只能等「发现积分没涨」再回头查，那时窗口已经关了。
//
// 挂载点选补领清扫（每 15 分钟一轮）：它是唯一「必然经过且已在拉任务列表」的
// 巡检点，零额外请求。代价是同一任务一天会被扫 96 次，故按 (账号, 任务) 每天
// 只报一次——预警要有存在感，但不能变成刷屏噪音。
package panel

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/longyanbo1224-debug/wb2api-panel-Pro/internal/auth"
	"github.com/longyanbo1224-debug/wb2api-panel-Pro/internal/logfmt"
	"github.com/longyanbo1224-debug/wb2api-panel-Pro/internal/upstream"
)

// expiringWarnWindow 提前多久预警（7 天：够人工补做一次任务并等结算）。
var expiringWarnWindow = 7 * 24 * time.Hour

// expiryWarned 当日已告警集合（key = date|uid|task_code），跨天整体重置。
// 包级状态：补领清扫是进程内单例轮转，无需按账号区分实例。
var expiryWarned = struct {
	mu   sync.Mutex
	date string
	seen map[string]bool
}{seen: map[string]bool{}}

// resetExpiryWarned 清空告警去重状态（仅供测试与跨天重置复用）。
func resetExpiryWarned() {
	expiryWarned.mu.Lock()
	expiryWarned.date = ""
	expiryWarned.seen = map[string]bool{}
	expiryWarned.mu.Unlock()
}

// warnExpiringTasks 对即将到期/已过期的未完成任务告警，返回本轮**新**告警的条目
// （去重后的子集，供测试断言；生产只关心副作用）。
func (p *Panel) warnExpiringTasks(a *auth.Auth, tasks []upstream.Task) []upstream.ExpiringTask {
	now := time.Now()
	list := upstream.ExpiringTasks(tasks, now, expiringWarnWindow)
	if len(list) == 0 {
		return nil
	}
	date := now.Format("2006-01-02")
	fresh := make([]upstream.ExpiringTask, 0, len(list))

	expiryWarned.mu.Lock()
	if expiryWarned.date != date {
		expiryWarned.date = date
		expiryWarned.seen = map[string]bool{}
	}
	for _, t := range list {
		key := date + "|" + a.UID + "|" + t.TaskCode
		if expiryWarned.seen[key] {
			continue
		}
		expiryWarned.seen[key] = true
		fresh = append(fresh, t)
	}
	expiryWarned.mu.Unlock()

	for _, t := range fresh {
		when := fmt.Sprintf("还有 %d 天到期（%s）", t.Days, t.Deadline.Format("2006-01-02 15:04"))
		if t.Expired {
			when = "已过期（" + t.Deadline.Format("2006-01-02 15:04") + "）"
		}
		name := t.TaskCode
		if t.Title != "" {
			name = t.TaskCode + " " + t.Title
		}
		log.Printf("tasksweep %s: ⏰ %s %s，进度 %d/%d，未领奖励 +%dc",
			logfmt.Label(a.UID, a.Nickname), name, when, t.Current, t.Target, t.Credit)
	}
	return fresh
}
