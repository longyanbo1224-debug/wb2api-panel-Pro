// taskexpiry.go 任务有效期（valid_start / valid_end）解析与到期预警口径。
//
// 背景：上游成长任务带限时窗口（实测 `Hp_Appearance` 至 2026-11-02、
// `Expert_lighthouse` 至 2026-11-13、四个 `expert_use_*` 至 2026-11-20），
// 过期后**不可再领**，而任务列表里只是安静地不再下发——不主动读 valid_end
// 就只能等「发现积分没涨」才回头查，那时已经晚了。
//
// 本文件只做纯计算（不发起任何上游调用），由 panel 的补领清扫在每轮巡检时调用。
package upstream

import (
	"math"
	"strings"
	"time"
)

// ValidEndTime 解析 valid_end。上游实测为 RFC3339 带时区
// （`2026-11-13T23:59:00+08:00`）；另兼容无时区的 `2006-01-02 15:04:05`
// 与纯日期 `2006-01-02` 两种历史形态。
//
// 返回 (零值, false) 表示「没有有效期或格式无法识别」——调用方据此跳过，
// **绝不猜日期**：猜错会漏报（预警形同不存在）或误报（天天喊到期）。
func (t Task) ValidEndTime() (time.Time, bool) {
	s := strings.TrimSpace(t.ValidEnd)
	if s == "" {
		return time.Time{}, false
	}
	if ts, err := time.Parse(time.RFC3339, s); err == nil {
		return ts, true
	}
	if ts, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local); err == nil {
		return ts, true
	}
	if ts, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		// 纯日期按当天结束算，避免「当天 00:00 就报已过期」。
		return ts.Add(24*time.Hour - time.Second), true
	}
	return time.Time{}, false
}

// ExpiringTask 即将到期（或已过期）的未完成任务。
type ExpiringTask struct {
	Task
	Deadline time.Time // valid_end 解析结果
	Expired  bool      // 已经过期（Deadline 早于 now）
	Days     int       // 距到期整天数，向上取整；已过期时为 0（看 Expired）
}

// ExpiringTasks 选出「未完成 + 窗口内到期」的任务。
//
// 口径（与 WorkBuddy-Daily 的到期预警一致）：
//   - 未领取（Claimed=false）；
//   - 进度可度量且未达标（Target>0 且 Current<Target）——Target<=0 的任务没有
//     进度语义，报「未完成」是噪音，直接跳过；
//   - valid_end 可解析且不晚于 now+window。
//
// 已过期的一并返回（Expired=true）：「昨天刚过期」比「7 天后过期」更该被看见，
// 静默跳过就等于把已经丢掉的分藏起来。
//
// 纯函数、无 I/O、不依赖时钟（now 由调用方传入），便于表驱动测试。
func ExpiringTasks(tasks []Task, now time.Time, window time.Duration) []ExpiringTask {
	out := make([]ExpiringTask, 0, len(tasks))
	limit := now.Add(window)
	for _, t := range tasks {
		if t.Claimed || t.Target <= 0 || t.Current >= t.Target {
			continue
		}
		deadline, ok := t.ValidEndTime()
		if !ok || deadline.After(limit) {
			continue
		}
		expired := deadline.Before(now)
		days := 0
		if !expired {
			days = int(math.Ceil(deadline.Sub(now).Hours() / 24))
		}
		out = append(out, ExpiringTask{Task: t, Deadline: deadline, Expired: expired, Days: days})
	}
	return out
}
