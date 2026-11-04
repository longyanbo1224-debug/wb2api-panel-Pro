package panel

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longyanbo1224-debug/wb2api-panel-Pro/internal/auth"
	"github.com/longyanbo1224-debug/wb2api-panel-Pro/internal/pool"
)

// TestAccountNoteAndExport 备注端点写入 auth 文件，导出端点把它带出去（含凭证快照）。
// 这两个端点是"端与端交换账号"的接口面：导出格式必须与导入端（importCockpit）同形。
func TestAccountNoteAndExport(t *testing.T) {
	dir := t.TempDir()
	a := &auth.Auth{
		UID: "u1", Nickname: "小明",
		AccessToken: "at", RefreshToken: "rt",
		Domain:   "www.codebuddy.cn",
		FilePath: filepath.Join(dir, "workbuddy-u1.json"),
	}
	if err := a.SaveAtomic(); err != nil {
		t.Fatalf("save auth: %v", err)
	}
	p := pool.New("")
	p.Add(a)
	pn := New(Config{Version: "test", APIKey: "test-key", Pool: p})

	// 1) 设置备注
	req := httptest.NewRequest("POST", "/panel/api/accounts/u1/note",
		strings.NewReader(`{"note":"主力号"}`))
	req.Header.Set("Authorization", "Bearer test-key")
	rr := httptest.NewRecorder()
	pn.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("note: status %d body %s", rr.Code, rr.Body.String())
	}

	// 2) 导出应带上 note / 两端 token / 毫秒级 expires_at
	req = httptest.NewRequest("GET", "/panel/api/export/cockpit", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rr = httptest.NewRecorder()
	pn.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("export: status %d body %s", rr.Code, rr.Body.String())
	}
	var out []struct {
		UID          string `json:"uid"`
		Note         string `json:"note"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresAt    int64  `json:"expires_at"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal export: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("exported %d accounts, want 1", len(out))
	}
	got := out[0]
	if got.UID != "u1" || got.Note != "主力号" || got.AccessToken != "at" || got.RefreshToken != "rt" {
		t.Fatalf("unexpected export entry: %+v", got)
	}
	if got.ExpiresAt != 0 && got.ExpiresAt < 1_000_000_000_000 {
		t.Fatalf("expires_at looks like seconds, want ms: %d", got.ExpiresAt)
	}
}

// TestImportProgressLifecycle 导入进度：登记 → 逐号推进 → 完成；过期条目在下次
// importStart 时回收（不常驻内存）。
func TestImportProgressLifecycle(t *testing.T) {
	pn := New(Config{Version: "test", APIKey: "test-key", Pool: pool.New("")})

	id := pn.importStart("job1", 3)
	pn.importStep(id, true)
	pn.importStep(id, false)
	pn.importFinish(id)

	snap, ok := pn.importSnapshot("job1")
	if !ok {
		t.Fatal("job1 not found")
	}
	if snap.Total != 3 || snap.Done != 2 || snap.Imported != 1 || snap.Skipped != 1 || !snap.Finished {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}

	// 把时间拨回过期点，再登记新任务 → 旧条目应被回收
	pn.importMu.Lock()
	pn.imports["job1"].Updated = time.Now().Add(-importProgressTTL - time.Minute)
	pn.importMu.Unlock()
	pn.importStart("job2", 1)
	if _, ok := pn.importSnapshot("job1"); ok {
		t.Fatal("expired job should be dropped on the next importStart")
	}
}
