// heatmap.go 成长中心热力墙（GET /activity/growth/heatmap）+ 补签断点选择。
//
// 热力墙是「当天到底有没有计分」的唯一权威口径：`score > 0` 才算当天活跃，
// 连登天数与 7d/14d/28d 兑换档位（28d 值 +150c/号）全挂在这条链上。
// `/v2/report` 返回 200 不等于计分（缺 userId、上游风控丢弃都是 200 静默丢），
// 所以必须回读热力墙确认。
//
// 同时，热力墙的 `today` 字段给出**服务端当天日期**（账号时区口径）。补签断点
// 必须用它，不能用本地 `time.Now()`：本地时区/夏令时与账号时区不一致时会补错日期。
package upstream

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/longyanbo1224-debug/wb2api-panel-Pro/internal/auth"
)

// heatmapPath 热力墙路径（growth 域，chatBase，无 /v2 前缀）。
const heatmapPath = "/activity/growth/heatmap"

// heatDateLayout 热力墙日期口径（服务端一律 YYYY-MM-DD）。
const heatDateLayout = "2006-01-02"

// HeatCell 热力墙单日格。
type HeatCell struct {
	Date  string `json:"date"`
	Score int    `json:"score"`
}

// HeatmapSnapshot 热力墙快照。
type HeatmapSnapshot struct {
	TodayDate  string     // 服务端当天日期（YYYY-MM-DD）；解析失败为 ""
	TodayScore int        // 当天得分（>0 = 已计分）
	TodayFound bool       // 是否拿到 today 字段
	Cells      []HeatCell // 全年格子（服务端固定下发 365 天）
}

// HeatmapSnapshot 拉取热力墙快照。today 兼容两种形状：
//   - {"date":"2026-10-28","score":2,"is_active":true,...}（实测）
//   - "2026-10-28"（早期口径）
func (c *Client) HeatmapSnapshot(a *auth.Auth) (*HeatmapSnapshot, error) {
	data, err := c.growthJSON(a, http.MethodGet, heatmapPath, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Today json.RawMessage `json:"today"`
		Cells []HeatCell      `json:"cells"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	snap := &HeatmapSnapshot{Cells: resp.Cells}
	raw := strings.TrimSpace(string(resp.Today))
	if raw == "" || raw == "null" {
		return snap, nil
	}
	var obj struct {
		Date  string `json:"date"`
		Score int    `json:"score"`
	}
	if err := json.Unmarshal(resp.Today, &obj); err == nil && obj.Date != "" {
		snap.TodayDate, snap.TodayScore, snap.TodayFound = obj.Date, obj.Score, true
		return snap, nil
	}
	var date string
	if err := json.Unmarshal(resp.Today, &date); err == nil && date != "" {
		snap.TodayDate, snap.TodayFound = date, true
	}
	return snap, nil
}

// DayScore 返回指定日期的得分。found=false 表示热力墙里没有这一天（不猜 0）。
func (s *HeatmapSnapshot) DayScore(date string) (score int, found bool) {
	if s == nil {
		return 0, false
	}
	for _, c := range s.Cells {
		if c.Date == date {
			return c.Score, true
		}
	}
	return 0, false
}

// MakeupTarget 选「补签卡该补哪一天」：只补**当前连登段的直接断点**。
//
// 为什么不是「当月所有漏签日」：补签卡上限 4 张，补一段与今天不连续的旧空档
// 不会延长连登（连登只认「连续到今天」），纯属浪费卡。当前连登段 = 以 anchor
// 结尾、长 streakDays 天的连续区间；段前那一天才是唯一能 +1 的位置。补上之后
// 下一次运行会把断点自然往前推一天，逐日恢复。
//
// 参数：
//   - anchor：服务端 today（今日已计分）或昨日（今日尚未计分）；
//   - streakDays：上游 `streak.days`（连续活跃天数）；
//   - snap：热力墙快照（提供各日 score）；
//   - madeUp：上游 `streak.makeup_dates`（已补过的日期，不重复提交）；
//   - notBefore：允许补的最早日期 = max(当月 1 号, launch_date)。跨月补签上游不计分。
//
// 返回 "" 表示不需要/不能补（无连登、已补过、当天本已计分、热力墙无该日记录、
// 早于 notBefore、日期非法）。
func MakeupTarget(anchor string, streakDays int, snap *HeatmapSnapshot, madeUp []string, notBefore string) string {
	if snap == nil || streakDays <= 0 {
		return ""
	}
	day, err := time.Parse(heatDateLayout, strings.TrimSpace(anchor))
	if err != nil {
		return ""
	}
	target := day.AddDate(0, 0, -streakDays).Format(heatDateLayout)
	// 同为 YYYY-MM-DD 定长串，字典序即时间序。
	if nb := strings.TrimSpace(notBefore); nb != "" && target < nb {
		return ""
	}
	for _, d := range madeUp {
		if strings.TrimSpace(d) == target {
			return ""
		}
	}
	score, found := snap.DayScore(target)
	if !found || score > 0 {
		return ""
	}
	return target
}
