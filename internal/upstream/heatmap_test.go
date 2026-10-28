package upstream

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/longyanbo1224-debug/wb2api-panel-Pro/internal/auth"
)

// TestHeatmapSnapshotObjectToday 实测口径：today 是对象（date/score/is_active...）。
func TestHeatmapSnapshotObjectToday(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/activity/growth/heatmap" {
			t.Errorf("path=%s want /activity/growth/heatmap", r.URL.Path)
		}
		w.Write([]byte(`{"code":0,"msg":"OK","data":{
			"today":{"date":"2026-10-28","score":2,"is_active":true,"status_text":"今日已活跃"},
			"cells":[{"date":"2026-10-27","score":52},{"date":"2026-10-28","score":2}]}}`))
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), ChatBaseCN: srv.URL, BillingBaseCN: srv.URL}
	snap, err := c.HeatmapSnapshot(&auth.Auth{AccessToken: "at", UID: "u1"})
	if err != nil {
		t.Fatalf("HeatmapSnapshot: %v", err)
	}
	if !snap.TodayFound || snap.TodayDate != "2026-10-28" || snap.TodayScore != 2 {
		t.Errorf("today=%q score=%d found=%v want 2026-10-28/2/true", snap.TodayDate, snap.TodayScore, snap.TodayFound)
	}
	if score, found := snap.DayScore("2026-10-27"); !found || score != 52 {
		t.Errorf("DayScore(2026-10-27)=%d,%v want 52,true", score, found)
	}
	if _, found := snap.DayScore("2026-01-01"); found {
		t.Error("DayScore 对不存在的日期应返回 found=false（不猜 0）")
	}
}

// TestHeatmapSnapshotStringToday 早期口径：today 是纯日期串（无 score）。
func TestHeatmapSnapshotStringToday(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":0,"data":{"today":"2026-10-28","cells":[]}}`))
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), ChatBaseCN: srv.URL, BillingBaseCN: srv.URL}
	snap, err := c.HeatmapSnapshot(&auth.Auth{AccessToken: "at", UID: "u1"})
	if err != nil {
		t.Fatalf("HeatmapSnapshot: %v", err)
	}
	if !snap.TodayFound || snap.TodayDate != "2026-10-28" || snap.TodayScore != 0 {
		t.Errorf("today=%q score=%d found=%v want 2026-10-28/0/true", snap.TodayDate, snap.TodayScore, snap.TodayFound)
	}
}

// TestHeatmapSnapshotNoToday 缺 today 字段 → TodayFound=false（调用方据此不做判断）。
func TestHeatmapSnapshotNoToday(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":0,"data":{"cells":[{"date":"2026-10-28","score":0}]}}`))
	}))
	defer srv.Close()

	c := &Client{HTTP: srv.Client(), ChatBaseCN: srv.URL, BillingBaseCN: srv.URL}
	snap, err := c.HeatmapSnapshot(&auth.Auth{AccessToken: "at", UID: "u1"})
	if err != nil {
		t.Fatalf("HeatmapSnapshot: %v", err)
	}
	if snap.TodayFound {
		t.Error("缺 today 字段时 TodayFound 应为 false")
	}
}

// TestMakeupTarget 补签断点选择：只补「当前连登段的直接断点」。
func TestMakeupTarget(t *testing.T) {
	cells := []HeatCell{
		{"2026-10-09", 0},
		{"2026-10-10", 8},
		{"2026-10-11", 0}, // 断点
		{"2026-10-12", 4},
		{"2026-10-13", 195},
	}
	snap := &HeatmapSnapshot{TodayDate: "2026-10-13", TodayScore: 195, TodayFound: true, Cells: cells}

	cases := []struct {
		name       string
		anchor     string
		streakDays int
		snap       *HeatmapSnapshot
		madeUp     []string
		notBefore  string
		want       string
	}{
		{"连登 2 天 → 断点是 10-11", "2026-10-13", 2, snap, nil, "2026-10-01", "2026-10-11"},
		{"已补过该日 → 不重复补", "2026-10-13", 2, snap, []string{"2026-10-11"}, "2026-10-01", ""},
		{"断点早于当月 1 号 → 不补（跨月补签上游不计分）", "2026-10-13", 2, snap, nil, "2026-10-12", ""},
		{"上线日远早于断点 → 照补", "2026-10-13", 2, snap, nil, "2026-06-17", "2026-10-11"},
		{"无连登（days=0）→ 不补", "2026-10-13", 0, snap, nil, "2026-10-01", ""},
		{"断点当天本已计分 → 不补", "2026-10-13", 3, snap, nil, "2026-10-01", ""}, // 断点=10-10 score=8
		{"热力墙无该日记录 → 不补（不猜）", "2026-10-13", 6, snap, nil, "2026-10-01", ""},
		{"anchor 非法 → 不补", "not-a-date", 2, snap, nil, "2026-10-01", ""},
		{"snap 为 nil → 不补", "2026-10-13", 2, nil, nil, "2026-10-01", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MakeupTarget(c.anchor, c.streakDays, c.snap, c.madeUp, c.notBefore); got != c.want {
				t.Errorf("MakeupTarget=%q want %q", got, c.want)
			}
		})
	}
}
