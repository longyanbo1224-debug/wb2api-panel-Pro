package scheduler

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/longyanbo1224-debug/wb2api-panel-Pro/internal/auth"
	"github.com/longyanbo1224-debug/wb2api-panel-Pro/internal/pool"
	"github.com/longyanbo1224-debug/wb2api-panel-Pro/internal/upstream"
)

// fastHeatmap 关掉「计分落库等待」，避免测试白等 3s。
func fastHeatmap(t *testing.T) {
	t.Helper()
	old := heatmapSettleDelay
	heatmapSettleDelay = 0
	t.Cleanup(func() { heatmapSettleDelay = old })
}

// heatmapStub 假热力墙：按调用次数返回不同 score，并统计「补对话」次数。
type heatmapStub struct {
	scores []int // 第 i 次 GET 返回 scores[min(i,len-1)]
	gets   atomic.Int32
	chats  atomic.Int32
}

func (s *heatmapStub) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/activity/growth/heatmap":
			i := int(s.gets.Add(1)) - 1
			if i >= len(s.scores) {
				i = len(s.scores) - 1
			}
			score := s.scores[i]
			w.Write([]byte(`{"code":0,"data":{"today":{"date":"2026-10-28","score":` +
				strconv.Itoa(score) + `},"cells":[]}}`))
		case "/v2/chat/completions":
			s.chats.Add(1)
			w.Write([]byte("data: [DONE]\n\n"))
		case "/v2/report":
			w.Write([]byte(`{"code":0,"msg":"OK"}`))
		default:
			w.Write([]byte(`{"code":0,"data":{}}`))
		}
	})
}

func itoa(v int) string { return strconv.Itoa(v) }

// newHeatmapScheduler 用假上游搭一个可直接调 verifyHeatmapScored 的 Scheduler。
func newHeatmapScheduler(t *testing.T, stub *heatmapStub) (*Scheduler, *auth.Auth) {
	t.Helper()
	srv := httptest.NewServer(stub.handler())
	t.Cleanup(srv.Close)
	p := pool.New("")
	a := &auth.Auth{UID: "u1", AccessToken: "at", RefreshToken: "rt", ExpiresAt: 9999999999}
	p.Add(a)
	up := &upstream.Client{HTTP: srv.Client(), ChatBaseCN: srv.URL, BillingBaseCN: srv.URL}
	return New(Config{Pool: p, Upstream: up}), a
}

// TestVerifyHeatmapScoredAlreadyScored 当天已计分：不补对话、不告警。
func TestVerifyHeatmapScoredAlreadyScored(t *testing.T) {
	fastHeatmap(t)
	stub := &heatmapStub{scores: []int{2}}
	s, a := newHeatmapScheduler(t, stub)

	if suspicious := s.verifyHeatmapScored(a); suspicious {
		t.Error("score>0 时不应判为可疑")
	}
	if n := stub.chats.Load(); n != 0 {
		t.Errorf("补对话次数=%d want 0（已计分不该多打一次模型）", n)
	}
}

// TestVerifyHeatmapScoredRecoversByChat 上报 200 但 score=0 → 补一次真实对话 → 计分。
func TestVerifyHeatmapScoredRecoversByChat(t *testing.T) {
	fastHeatmap(t)
	stub := &heatmapStub{scores: []int{0, 2}}
	s, a := newHeatmapScheduler(t, stub)

	if suspicious := s.verifyHeatmapScored(a); suspicious {
		t.Error("补对话后已计分，不应判为可疑")
	}
	if n := stub.chats.Load(); n != 1 {
		t.Errorf("补对话次数=%d want 1", n)
	}
	if n := stub.gets.Load(); n != 2 {
		t.Errorf("热力墙回读次数=%d want 2（补对话前后各一次）", n)
	}
}

// TestVerifyHeatmapScoredStaysZero 补对话后仍为 0 → 判可疑（只告警，不伪造计分）。
func TestVerifyHeatmapScoredStaysZero(t *testing.T) {
	fastHeatmap(t)
	stub := &heatmapStub{scores: []int{0}}
	s, a := newHeatmapScheduler(t, stub)

	if suspicious := s.verifyHeatmapScored(a); !suspicious {
		t.Error("补对话后仍 score=0 应判为可疑")
	}
	if n := stub.chats.Load(); n != 1 {
		t.Errorf("补对话次数=%d want 1（只补一次，不重试风暴）", n)
	}
}

// TestVerifyHeatmapScoredReadError 回读失败 → 判可疑但不补对话（状态未知时不乱动）。
func TestVerifyHeatmapScoredReadError(t *testing.T) {
	fastHeatmap(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`boom`))
	}))
	defer srv.Close()
	p := pool.New("")
	a := &auth.Auth{UID: "u1", AccessToken: "at", RefreshToken: "rt", ExpiresAt: 9999999999}
	p.Add(a)
	s := New(Config{Pool: p, Upstream: &upstream.Client{HTTP: srv.Client(), ChatBaseCN: srv.URL, BillingBaseCN: srv.URL}})

	if suspicious := s.verifyHeatmapScored(a); !suspicious {
		t.Error("回读失败应判为可疑")
	}
}
