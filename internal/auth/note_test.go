package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestNotePersistsInAuthFile 备注必须落在 auth 文件里（顶层 note 键）：
// 这是"复制 auths 目录 / 导出 cockpit JSON 换机器后备注不丢"的基础。
func TestNotePersistsInAuthFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workbuddy-u1.json")
	a := &Auth{
		UID: "u1", Nickname: "小明",
		AccessToken: "at", RefreshToken: "rt",
		Domain: "www.codebuddy.cn", FilePath: path,
	}
	if err := a.SaveAtomic(); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := a.SetNote("主力号"); err != nil {
		t.Fatalf("set note: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got, _ := doc["note"].(string); got != "主力号" {
		t.Fatalf("note in file = %q, want 主力号", got)
	}

	b, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if b.Note != "主力号" {
		t.Fatalf("parsed note = %q", b.Note)
	}

	// 清空备注应把键从文件里移除，而不是留一个空串（不打扰别处的读取方）。
	if err := a.SetNote(""); err != nil {
		t.Fatalf("clear note: %v", err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc = map[string]any{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["note"]; ok {
		t.Fatal("empty note should not be written to the file")
	}
}
