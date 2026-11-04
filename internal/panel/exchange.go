// exchange.go 账号在「端与端」之间迁移用的一对端点：
//
//	POST /panel/api/accounts/{uid}/note   设置备注（写进 auth 文件，随账号一起走）
//	GET  /panel/api/export/cockpit        导出全部账号为 cockpit 数组（与导入同格式）
//
// 设计取舍：备注不是「本机设置」，而是账号元数据。早期版本把备注存在浏览器
// localStorage 里，结果复制 auths 目录到另一台机器、或用 cockpit JSON 迁移时备注全丢。
// 现在它落在 auth 文件的顶层 note 键，导出/导入两端都带着走。
package panel

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

// noteMaxRunes 备注长度上限（面板输入框是 24，这里留余量兜住手写 API 调用）。
const noteMaxRunes = 40

// cockpitExport 导出条目。字段与 importCockpit 接受的格式一致（外加 note），
// 所以「导出 → 另一台机器导入」是无损的；expires_at 用毫秒，与导入端的 /1000 对齐。
type cockpitExport struct {
	ID           string `json:"id"`
	UID          string `json:"uid"`
	Nickname     string `json:"nickname,omitempty"`
	Note         string `json:"note,omitempty"`
	Email        string `json:"email"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresAt    int64  `json:"expires_at"`
	Domain       string `json:"domain"`
	Realm        string `json:"realm,omitempty"`
	Status       string `json:"status"`
}

// accountNote 设置备注。空串 = 清除备注（文件里的 note 键随之消失）。
func (p *Panel) accountNote(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	a := p.cfg.Pool.AuthByUID(uid)
	if a == nil {
		writeErr(w, http.StatusNotFound, "unknown uid")
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<12)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	note := strings.TrimSpace(req.Note)
	if utf8.RuneCountInString(note) > noteMaxRunes {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("note too long (max %d chars)", noteMaxRunes))
		return
	}
	if err := a.SetNote(note); err != nil {
		writeErr(w, http.StatusInternalServerError, "save note: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "uid": uid, "note": note})
}

// exportCockpit 导出全部账号（含备注），供另一台机器/手机端导入。
func (p *Panel) exportCockpit(w http.ResponseWriter, r *http.Request) {
	out := make([]cockpitExport, 0, 16)
	for _, st := range p.cfg.Pool.List() {
		a := p.cfg.Pool.AuthByUID(st.UID)
		if a == nil {
			continue
		}
		// token 会被 RefreshToken 并发改写，快照必须在锁内取（与出站请求头同口径）。
		// 注意：不能在这个锁里调 a.Realm()——它自己也要拿同一把不可重入的锁。
		// 昵称 / 备注 / realm 都来自 Status 快照（pool.List 已按锁内状态生成）。
		a.Lock()
		at, rt, domain, expires := a.AccessToken, a.RefreshToken, a.Domain, a.ExpiresAt
		a.Unlock()

		status := "active"
		if st.Disabled {
			status = "disabled"
		}
		out = append(out, cockpitExport{
			ID:           st.UID,
			UID:          st.UID,
			Nickname:     st.Nickname,
			Note:         st.Note,
			AccessToken:  at,
			RefreshToken: rt,
			TokenType:    "Bearer",
			ExpiresAt:    expires * 1000, // 秒 → 毫秒，与导入端口径一致
			Domain:       domain,
			Realm:        st.Realm,
			Status:       status,
		})
	}

	w.Header().Set("Content-Disposition", `attachment; filename="cockpit-accounts.json"`)
	writeJSON(w, http.StatusOK, out)
}
