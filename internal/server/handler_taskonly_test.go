// handler_taskonly_test.go 任务模式（TaskOnly）的路由口径。
//
// 任务模式是手机端 App / 单机自用的运行形态：同一份二进制，只做签到与各类任务，
// 不对外提供 OpenAI 兼容反代。这里锁住三件事：
//
//  1. 反代端点全部 404（不是 401/503——没注册就是不存在，不暴露任何行为差异）；
//  2. /panel/ 与 /healthz 仍可用（App 靠 healthz 判断引擎就绪，用户靠面板做任务）；
//  3. 缺省（TaskOnly=false）仍是完整网关，反代端点不得被误删。
package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/longyanbo1224-debug/wb2api-panel-Pro/internal/pool"
	"github.com/longyanbo1224-debug/wb2api-panel-Pro/internal/upstream"
)

// proxyPaths 全部反代端点（方法 + 路径）。任务模式下必须逐个 404。
var proxyPaths = []struct {
	method string
	path   string
}{
	{http.MethodPost, "/v1/chat/completions"},
	{http.MethodPost, "/v1/search/messages"},
	{http.MethodGet, "/v1/models"},
	{http.MethodGet, "/status"},
}

// doReq 发一次请求并返回状态码（调用方负责断言，body 无需保留）。
func doReq(t *testing.T, client *http.Client, method, url, body string) int {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, url, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// TestTaskOnlyModeDisablesProxyRoutes 任务模式只留面板与 healthz。
func TestTaskOnlyModeDisablesProxyRoutes(t *testing.T) {
	panelHit := 0
	h := NewHandler(Config{
		Pool:     pool.New(""),
		Upstream: upstream.New(),
		TaskOnly: true,
		Panel: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panelHit++
			w.WriteHeader(http.StatusOK)
		}),
	})
	srv := httptest.NewServer(h)
	defer srv.Close()

	for _, c := range proxyPaths {
		// body 用 "{}"：完整形态下这些端点会先过鉴权/解析，用合法 JSON 才能区分
		// 「没注册（404）」和「注册了但业务报错」。
		if code := doReq(t, srv.Client(), c.method, srv.URL+c.path, "{}"); code != http.StatusNotFound {
			t.Errorf("任务模式 %s %s: 期望 404（路由未注册），实得 %d", c.method, c.path, code)
		}
		if panelHit != 0 {
			t.Fatalf("任务模式 %s %s 竟命中面板 handler", c.method, c.path)
		}
	}

	if code := doReq(t, srv.Client(), http.MethodGet, srv.URL+"/panel/api/overview", ""); code != http.StatusOK {
		t.Errorf("任务模式面板应可用：期望 200，实得 %d", code)
	}
	if panelHit == 0 {
		t.Error("任务模式面板 handler 未被调用")
	}

	// /healthz 保留：空池时语义是 503（无可用账号），关键是**没有 404**。
	if code := doReq(t, srv.Client(), http.MethodGet, srv.URL+"/healthz", ""); code == http.StatusNotFound {
		t.Error("任务模式 /healthz 不应 404：App 依赖它判断引擎就绪")
	}
}

// TestFullModeKeepsProxyRoutes 缺省完整模式不得丢反代端点（零回归护栏）。
func TestFullModeKeepsProxyRoutes(t *testing.T) {
	h := NewHandler(Config{Pool: pool.New(""), Upstream: upstream.New()})
	srv := httptest.NewServer(h)
	defer srv.Close()

	for _, c := range proxyPaths {
		if code := doReq(t, srv.Client(), c.method, srv.URL+c.path, "{}"); code == http.StatusNotFound {
			t.Errorf("完整模式 %s %s: 不应 404（反代端点必须在），实得 %d", c.method, c.path, code)
		}
	}
}
