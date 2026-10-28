// streak.go 连登管家：签到排程后自动检查连登兑换档位 → 可兑换即兑换 → 按抽奖次数抽奖。
//
// 背景（2026-09-12）：成长中心连登档位（7d/14d/28d）按连续登录天数解锁，兑换发
// credit/energy/补签卡/抽奖次数；抽奖次数只能从兑换获得。兑换按钮在 UI 上恒可点，
// 但未解锁时服务端 403「连续登录天数不足」——所以放在每日签到后跑一遍（幂等），
// 到天数那天自动完成「兑换 → 抽奖」闭环，无需人工盯。
package scheduler

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/longyanbo1224-debug/wb2api-panel-Pro/internal/auth"
	"github.com/longyanbo1224-debug/wb2api-panel-Pro/internal/logfmt"
	"github.com/longyanbo1224-debug/wb2api-panel-Pro/internal/upstream"
)

// RunStreakBonusNow 对所有可用账号执行连登兑换 + 抽奖（幂等：locked/无次数自动跳过）。
// 由签到排程（RunCheckinNow）末尾调用；也可面板手动触发。
func (s *Scheduler) RunStreakBonusNow() {
	for _, st := range s.cfg.Pool.List() {
		if st.Disabled {
			continue
		}
		a := s.cfg.Pool.AuthByUID(st.UID)
		if a == nil || a.AccessTokenValue() == "" {
			continue
		}
		if a.IsGlobal() {
			continue // D4 门控：global 无 CN 任务体系，不发起任何上游调用
		}
		if a.IsEnterpriseSession() {
			continue // 企业版 / 企业切换会话门控：无成长体系（/activity/growth/streak 企业号 403；切换会话 200 但不计分）
		}
		s.streakBonusAccount(a)
	}
}

// streakBonusAccount 单账号：补签保连登 → 礼包/补偿 → 兑换所有已解锁档位 → 抽完所有 chances。
func (s *Scheduler) streakBonusAccount(a *auth.Auth) {
	// 0. 补签保连登：连登段一旦断掉就要从 0 重攒，28d 档值 +150c/号。
	s.makeupBoundary(a)
	// 0.5 礼包/补偿（每号一次，无则业务错误静默跳过）。
	if credit, err := s.cfg.Upstream.ClaimGift(a); err == nil {
		log.Printf("streak-bonus %s: 🎊 新手礼包 +%dc", logfmt.Label(a.UID, a.Nickname), credit)
	}
	// 补偿改为「先查状态 → 待领才领」（issue #189）：旧写法 `if credit, err :=
	// ClaimCompensation(...); err == nil` 把「没有补偿」「已经领过」与「领取真的
	// 失败」压成同一形态，补偿带截止日（实测那次为 10/22）时签到一旦停摆就静默
	// 过期。现在无待领不记日志，但状态查询失败与领取失败一律显式告警。
	s.claimCompensationQuiet(a)

	full, err := s.cfg.Upstream.GrowthStreakFull(a)
	if err != nil {
		log.Printf("streak-bonus %s: %v", logfmt.Label(a.UID, a.Nickname), err)
		return
	}
	s.remindNextTier(a, full)
	statuses := map[string]string{
		"7d":  full.RedemptionStatus.Tier7dStatus,
		"14d": full.RedemptionStatus.Tier14dStatus,
		"28d": full.RedemptionStatus.Tier28dStatus,
	}
	for _, tier := range full.RedemptionStatus.Tiers {
		status := statuses[tier.Tier]
		if status == "locked" || status == "claimed" {
			continue
		}
		if err := s.cfg.Upstream.GrowthRedeemTier(a, tier.Tier); err != nil {
			// 未解锁（403）属预期，静默；其余记日志。
			log.Printf("streak-bonus %s: redeem %s: %v", logfmt.Label(a.UID, a.Nickname), tier.Tier, err)
			continue
		}
		log.Printf("streak-bonus %s: ★ 兑换 %s 档（+%dc +%de 卡×%d 抽奖×%d）",
			a.UID, tier.Tier, tier.Credit, tier.Energy, tier.Cards, tier.Chances)
	}
	// 抽奖：按当前 chances 全抽完（兑换刚发的次数已在服务端累加）。
	chances, err := s.cfg.Upstream.LotteryChances(a)
	if err != nil {
		log.Printf("streak-bonus %s: lottery summary: %v", logfmt.Label(a.UID, a.Nickname), err)
		return
	}
	for i := 0; i < chances; i++ {
		raw, err := s.cfg.Upstream.LotteryDraw(a)
		if err != nil {
			log.Printf("streak-bonus %s: draw: %v", logfmt.Label(a.UID, a.Nickname), err)
			return
		}
		log.Printf("streak-bonus %s: 🎲 第%d抽 %s", logfmt.Label(a.UID, a.Nickname), i+1, compactJSON(raw))
	}
	if chances > 0 {
		log.Printf("streak-bonus %s: 抽奖完成 %d 次", logfmt.Label(a.UID, a.Nickname), chances)
	}
}

// compactJSON 裁剪奖品载荷（日志单行可读）。
func compactJSON(raw json.RawMessage) string {
	s := string(raw)
	if len(s) > 220 {
		return s[:220] + "…"
	}
	return s
}

// makeupBoundary 用补签卡补「当前连登段的直接断点」。
//
// 旧实现（makeupYesterday）有两处会静默丢分：
//  1. 只看「昨天」：停摆 2 天以上就救不回来——连登一断，7d/14d/28d 档位全部从 0
//     重攒（28d 档值 +150c/号，等于白丢一个月）；
//  2. 用本地 time.Now() 算日期：本地时区/夏令时与账号时区不一致时会补错日期，
//     上游按「非漏签日」直接拒绝，看起来像「补签功能坏了」。
//
// 现在按服务端 today（热力墙 today.date）+ streak.days 定位断点，每次只补一张，
// 边界取 max(当月 1 号, launch_date)——跨月/上线前的日期补了不计分。
// 无卡、无漏签、查询失败均静默（不影响主流程）。
func (s *Scheduler) makeupBoundary(a *auth.Auth) {
	full, err := s.cfg.Upstream.GrowthStreakFull(a)
	if err != nil || full.MakeupCards.Balance <= 0 {
		return
	}
	snap, err := s.cfg.Upstream.HeatmapSnapshot(a)
	if err != nil || !snap.TodayFound {
		return
	}
	// 锚点：今日已计分则今天，否则昨日（连登段此时止于昨天）。
	anchor := snap.TodayDate
	if snap.TodayScore <= 0 {
		if t, err := time.Parse("2006-01-02", snap.TodayDate); err == nil {
			anchor = t.AddDate(0, 0, -1).Format("2006-01-02")
		}
	}
	notBefore := monthStart(anchor)
	if launch := strings.TrimSpace(full.LaunchDate); launch != "" {
		if len(launch) > 10 {
			launch = launch[:10]
		}
		if launch > notBefore {
			notBefore = launch
		}
	}
	target := upstream.MakeupTarget(anchor, full.Streak.Days, snap, full.Streak.MakeupDates, notBefore)
	if target == "" {
		return
	}
	if err := s.cfg.Upstream.UseMakeupCard(a, target); err != nil {
		log.Printf("streak-bonus %s: 补签 %s 失败: %v", logfmt.Label(a.UID, a.Nickname), target, err)
		return
	}
	log.Printf("streak-bonus %s: ★ 已用补签卡补签 %s（连登 %d→%d 天）",
		logfmt.Label(a.UID, a.Nickname), target, full.Streak.Days, full.Streak.Days+1)
}

// monthStart 返回 date 所在月 1 号（"YYYY-MM-DD"）；解析失败时原样返回
// （MakeupTarget 拿不到合法边界就不补，宁可不补也不乱补）。
func monthStart(date string) string {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(date))
	if err != nil {
		return strings.TrimSpace(date)
	}
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
}

// streakTierReminderDays 距离下一档 ≤ 这么多天时提醒一次（默认 3 天）。
var streakTierReminderDays = 3

// remindNextTier 下一档快到时报一句（运维提醒，不改变任何行为）。
//
// 动机：28d 档值 +150c，而「差 2 天」和「差 20 天」在面板上长得一样；不提醒的话
// 某天停摆清零，事后完全看不出「本来再撑两天就到手」。
func (s *Scheduler) remindNextTier(a *auth.Auth, full *upstream.StreakFull) {
	if full == nil {
		return
	}
	remain := full.Streak.NextTierRemaining
	if remain <= 0 || remain > streakTierReminderDays {
		return
	}
	credit := int64(0)
	for _, t := range full.RedemptionStatus.Tiers {
		if t.Tier == full.Streak.NextTier {
			credit = int64(t.Credit)
			break
		}
	}
	log.Printf("streak-bonus %s: ⏳ 距 %s 档还差 %d 天（+%dc）",
		logfmt.Label(a.UID, a.Nickname), full.Streak.NextTier, remain, credit)
}
