// blackcat.go 夜猫子任务（black_cat）+ 新手礼包/补偿 API。
//
// 判据（WorkBuddy-Daily 项目实测口径 + 本网关验证）：black_cat 要求在
// **23:00–08:00（本地时区）窗口内**完成 3 次 glm-5.2 对话并上报 chat 事件链；
// 窗口外行为不计分。真实对话走网关既有 ChatStream（glm-5.2），事件链用
// ReportChatActivityModel（chat_5 同款上报形状）。
package upstream

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/longyanbo1224-debug/wb2api-panel-Pro/internal/auth"
)

// InNightWindow 当前是否处于夜猫子计数窗口（23:00–08:00 本地时区）。
func InNightWindow(now time.Time) bool {
	h := now.Hour()
	return h >= 23 || h < 8
}

// BlackcatNeed 查 black_cat 任务剩余差额（需要再完成几次对话）。
// 任务不存在返回 0（无可做）；拉取失败返回错误。
func (c *Client) BlackcatNeed(a *auth.Auth) (int64, error) {
	tasks, err := c.ListTasks(a)
	if err != nil {
		return 0, err
	}
	for _, t := range tasks {
		if t.TaskCode == "black_cat" {
			if t.Claimed || t.Current >= t.Target {
				return 0, nil
			}
			return t.Target - t.Current, nil
		}
	}
	return 0, nil
}

// RealChatOnce 发一次极短真实对话（glm-5.2，1+1）并上报 chat_request_send 事件。
//
// 用途：`/v2/report` 返回 200 但热力墙当天 score 仍为 0 时，作为「补计分」动作
// （真实对话比再补一条事件更可靠——上游对真实会话的计数不吃风控丢弃）。
// 也是 RunNightChats 的单次形态，两者共用同一请求形状，避免口径漂移。
func (c *Client) RealChatOnce(a *auth.Auth) error {
	body, _ := json.Marshal(map[string]any{
		"model":    "glm-5.2",
		"messages": []map[string]any{{"role": "user", "content": "1+1等于几？直接回答。"}},
		"stream":   true,
	})
	rc, status, respBody, err := c.ChatStream(a, body, "", ChatMeta{})
	if err != nil || status >= 400 {
		if rc != nil {
			rc.Close()
		}
		return fmt.Errorf("对话失败: http=%d err=%v body=%.120s", status, err, respBody)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(rc, 1<<20))
	rc.Close()
	return c.ReportChatActivityModel(a, fmt.Sprintf("wb2api-night-%d", time.Now().UnixMilli()), "", "glm-5.2", "GLM-5.2")
}

// RunNightChats 夜猫子：发 need 次 glm-5.2 真实对话（读干流）并上报事件链。
// 返回成功次数。对话内容极短（1+1），消耗可忽略。
func (c *Client) RunNightChats(a *auth.Auth, need int) (int64, error) {
	var ok int64
	for i := 0; i < need; i++ {
		if err := c.RealChatOnce(a); err != nil {
			return ok, fmt.Errorf("第 %d 次: %w", i+1, err)
		}
		ok++
		time.Sleep(4 * time.Second)
	}
	return ok, nil
}

// ClaimGift 领取新手礼包（每号一次，已领返回业务错误）。
func (c *Client) ClaimGift(a *auth.Auth) (int64, error) {
	data, err := c.billingJSON(a, http.MethodPost, "/billing/meter/claim-gift", map[string]any{})
	if err != nil {
		return 0, err
	}
	var resp struct {
		Credit int64 `json:"credit"`
	}
	_ = json.Unmarshal(data, &resp)
	return resp.Credit, nil
}

// ClaimCompensation 领取活动补偿（有则领，无则业务错误）。
func (c *Client) ClaimCompensation(a *auth.Auth) (int64, error) {
	data, err := c.billingJSON(a, http.MethodPost, "/billing/meter/claim-compensation", map[string]any{})
	if err != nil {
		return 0, err
	}
	var resp struct {
		Credit int64 `json:"credit"`
	}
	_ = json.Unmarshal(data, &resp)
	return resp.Credit, nil
}

// HeatmapYesterdayMissed / HeatmapSnapshot / UseMakeupCard 等热力墙与补签接口见
// heatmap.go（补签断点选择需要整张热力墙，不再只用「昨日是否漏签」这一个判据）。

// UseMakeupCard 对指定日期使用补签卡（保住连登连续天数；无卡返回业务错误）。
func (c *Client) UseMakeupCard(a *auth.Auth, date string) error {
	_, err := c.growthJSON(a, http.MethodPost, "/activity/growth/makeup-cards/use",
		map[string]any{"target_date": date})
	return err
}
