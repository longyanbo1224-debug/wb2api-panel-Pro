package panel

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/longyanbo1224-debug/wb2api-panel-Pro/internal/auth"
)

// importProgress 一次导入的实时进度。导入是串行的（每号签到 + 余额查询，
// 15 个号大约 20-60 秒），前端拿它把"正在导入"变成"正在导入 7/15"。
//
// id 由前端生成并带在 POST 上：这样同一个请求还在跑的时候，前端就能用同一个 id
// 轮询进度（不需要先拿 job id 再二次请求）。条目按 TTL 回收，容量有界。
type importProgress struct {
	Total    int       `json:"total"`
	Done     int       `json:"done"`
	Imported int       `json:"imported"`
	Skipped  int       `json:"skipped"`
	Errors   []string  `json:"errors,omitempty"`
	Finished bool      `json:"finished"`
	Updated  time.Time `json:"-"`
}

// importProgressTTL 进度条目保留时长：够用户看完成结果，又不会常驻。
const importProgressTTL = 10 * time.Minute

// importStart 登记一次导入，返回前端轮询用的 job id。
// 前端自带 id（同一次 POST 期间就能轮询）；没带时由服务端生成并随响应返回。
func (p *Panel) importStart(id string, total int) string {
	if id == "" {
		id = fmt.Sprintf("imp-%d", time.Now().UnixNano())
	}
	p.importMu.Lock()
	if p.imports == nil {
		p.imports = make(map[string]*importProgress)
	}
	for k, v := range p.imports {
		if time.Since(v.Updated) > importProgressTTL {
			delete(p.imports, k)
		}
	}
	p.imports[id] = &importProgress{Total: total, Updated: time.Now()}
	p.importMu.Unlock()
	return id
}

func (p *Panel) importSetTotal(id string, total int) {
	p.importMu.Lock()
	if v := p.imports[id]; v != nil {
		v.Total = total
		v.Updated = time.Now()
	}
	p.importMu.Unlock()
}

// importStep 单个账号处理完（成功或跳过）推进一格。
func (p *Panel) importStep(id string, imported bool) {
	p.importMu.Lock()
	if v := p.imports[id]; v != nil {
		v.Done++
		if imported {
			v.Imported++
		} else {
			v.Skipped++
		}
		v.Updated = time.Now()
	}
	p.importMu.Unlock()
}

func (p *Panel) importFinish(id string) {
	p.importMu.Lock()
	if v := p.imports[id]; v != nil {
		v.Finished = true
		v.Updated = time.Now()
	}
	p.importMu.Unlock()
}

func (p *Panel) importDrop(id string) {
	p.importMu.Lock()
	delete(p.imports, id)
	p.importMu.Unlock()
}

// importSnapshot 返回进度副本（不把内部指针暴露给 handler 之外的代码）。
func (p *Panel) importSnapshot(id string) (importProgress, bool) {
	p.importMu.Lock()
	defer p.importMu.Unlock()
	v := p.imports[id]
	if v == nil {
		return importProgress{}, false
	}
	cp := *v
	cp.Errors = append([]string(nil), v.Errors...)
	return cp, true
}

// importStatus 导入进度查询：GET /panel/api/import/status?id=...
// 与 POST /import/cockpit 并行：POST 还在跑时前端就能用同一个 id 看 7/15。
func (p *Panel) importStatus(w http.ResponseWriter, r *http.Request) {
	snap, ok := p.importSnapshot(r.URL.Query().Get("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown import job（导入已结束或 id 不正确）")
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// cockpitAccount 映射 cockpit tools 导出格式的单个账号。
type cockpitAccount struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	UID      string `json:"uid"`
	Nickname string `json:"nickname"`
	// Note 备注：本仓库导出（export_cockpit）会带上；cockpit tools 导出没有这个键，
	// 此时保留池中已有备注，避免"重新导入一次把备注洗掉"。
	Note           string `json:"note"`
	AccessToken    string `json:"access_token"`
	RefreshToken   string `json:"refresh_token"`
	TokenType      string `json:"token_type"`
	ExpiresAt      int64  `json:"expires_at"`
	Domain         string `json:"domain"`
	DosageNotify   string `json:"dosage_notify_code"`
	PaymentType    string `json:"payment_type"`
	Status         string `json:"status"`
	UsageUpdatedAt int64  `json:"usage_updated_at"`
	LastCheckin    int64  `json:"last_checkin_time"`
	CheckinStreak  int    `json:"checkin_streak"`
	CreatedAt      int64  `json:"created_at"`
	LastUsed       int64  `json:"last_used"`
}

// importCockpit 接收 cockpit tools 导出的 JSON 文件，批量导入账号到池中。
//
//	POST /panel/api/import/cockpit
//	Content-Type: multipart/form-data
//	Body: file=<json>
//
// 返回 {ok, total, imported, skipped, errors}。
func (p *Panel) importCockpit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "parse form: "+err.Error())
		return
	}
	jobID := p.importStart(r.URL.Query().Get("id"), 0) // Total 解析出账号数后再补
	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "missing file field: "+err.Error())
		return
	}
	defer file.Close()

	raw, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read file: "+err.Error())
		return
	}

	var accounts []cockpitAccount
	if err := json.Unmarshal(raw, &accounts); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if len(accounts) == 0 {
		p.importDrop(jobID)
		writeErr(w, http.StatusBadRequest, "empty accounts array")
		return
	}
	p.importSetTotal(jobID, len(accounts))

	var total, imported, skipped int
	var errs []string

	for _, acc := range accounts {
		uid := strings.TrimSpace(acc.UID)
		at := strings.TrimSpace(acc.AccessToken)
		rt := strings.TrimSpace(acc.RefreshToken)
		if uid == "" || at == "" || rt == "" {
			skipped++
			errs = append(errs, fmt.Sprintf("missing required fields (id=%s)", acc.ID))
			p.importStep(jobID, false)
			continue
		}
		if !validImportUID(uid) {
			skipped++
			errs = append(errs, fmt.Sprintf("invalid uid (id=%s)", acc.ID))
			p.importStep(jobID, false)
			continue
		}

		// 按 domain 推断 realm：workbuddy.ai 家族 → global，否则 cn。
		realm := auth.ResolveRealm("", acc.Domain)

		// cockpit tools 的 expires_at 为毫秒时间戳，转为秒。
		expiresAt := acc.ExpiresAt / 1000
		if expiresAt <= 0 {
			expiresAt = time.Now().Add(365 * 24 * time.Hour).Unix()
		}

		nickname := acc.Nickname
		if strings.TrimSpace(nickname) == "" {
			nickname = acc.Email
		}

		// 备注：导入文件没带 note 时沿用池中已有备注（重复导入不洗备注）。
		note := strings.TrimSpace(acc.Note)
		if note == "" {
			if old := p.cfg.Pool.AuthByUID(uid); old != nil {
				old.Lock()
				note = old.Note
				old.Unlock()
			}
		}

		a := &auth.Auth{
			AccessToken:  at,
			RefreshToken: rt,
			ExpiresAt:    expiresAt,
			Domain:       acc.Domain,
			UID:          uid,
			Nickname:     nickname,
			Note:         note,
			FilePath:     filepath.Join(p.cfg.AuthDir, fmt.Sprintf("workbuddy-%s.json", uid)),
		}

		if realm == "global" {
			if _, err := auth.BackfillRealmFor(a, "global"); err != nil {
				skipped++
				errs = append(errs, fmt.Sprintf("uid=%s: set realm failed: %v", uid, err))
				p.importStep(jobID, false)
				continue
			}
		} else {
			_, _ = a.BackfillRealm()
		}

		if err := a.SaveAtomic(); err != nil {
			skipped++
			errs = append(errs, fmt.Sprintf("uid=%s: save auth failed: %v", uid, err))
			p.importStep(jobID, false)
			continue
		}

		p.cfg.Pool.Add(a)
		p.cfg.Pool.Revive(uid)

		// 顺带签到/激活（幂等；失败仅记日志，不阻断导入）。
		if realm == "global" {
			if activated, err := p.cfg.Upstream.GlobalCompleteRegistration(a); err != nil {
				log.Printf("panel: import global 注册激活 uid=%s: %v", uid, err)
			} else if activated {
				log.Printf("panel: import global 注册激活 uid=%s 完成", uid)
			}
			if claimed, err := p.cfg.Upstream.ClaimTrial(a); err != nil {
				log.Printf("panel: import global trial uid=%s: %v", uid, err)
			} else if claimed {
				log.Printf("panel: import global trial uid=%s 已领", uid)
			}
		} else {
			if err := p.cfg.Upstream.DailyCheckin(a); err != nil {
				log.Printf("panel: import checkin uid=%s: %v", uid, err)
			}
		}
		if rm, tt, err := p.cfg.Upstream.UserResource(a); err == nil {
			p.cfg.Pool.ReenableIfCredits(uid, rm, tt)
		}

		imported++
		p.importStep(jobID, true)
	}

	p.importFinish(jobID)
	total = len(accounts)
	log.Printf("panel: cockpit import finished total=%d imported=%d skipped=%d", total, imported, skipped)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"job":      jobID,
		"total":    total,
		"imported": imported,
		"skipped":  skipped,
		"errors":   errs,
	})
}

// validImportUID 校验导入 uid 是否可用于拼文件名（同 login.go validUID 口径）。
func validImportUID(uid string) bool {
	if uid == "" || len(uid) > 64 {
		return false
	}
	for _, c := range uid {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}
