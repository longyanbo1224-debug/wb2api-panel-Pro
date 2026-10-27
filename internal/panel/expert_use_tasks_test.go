package panel

import "testing"

// TestExpertUseTaskActionsWired 守住「任务码 ↔ autoAction ↔ 专家 id」三处接线：
// growthPending 只认 autoActions 白名单，任一环节拼错该任务都会从任务中心静默消失。
func TestExpertUseTaskActionsWired(t *testing.T) {
	for _, code := range []string{
		"expert_use_maidehao", "expert_use_douyinyin",
		"expert_use_shangjia_xuanchuan", "expert_use_bowangwang", "WorkBuddy_Valorant",
	} {
		if e, ok := expertUseTargets[code]; !ok || e.ExpertID == "" {
			t.Errorf("%s 缺专家目标", code)
		}
		if autoActionFor(code) == nil {
			t.Errorf("%s 未登记 autoAction", code)
		}
	}
}
