package main

import (
	"testing"
	"time"
)

// TestApplyTZ 钉住 TZ → time.Local：Android 上 Go 不读 TZ，不手动设置的话
// 面板「今日已签到」与排程都按 UTC 走（偏离东八区 8 小时）。
func TestApplyTZ(t *testing.T) {
	old := time.Local
	t.Cleanup(func() { time.Local = old })

	if err := applyTZ("Asia/Shanghai"); err != nil {
		t.Fatalf("applyTZ(Asia/Shanghai): %v", err)
	}
	_, off := time.Now().Zone()
	if off != 8*3600 {
		t.Fatalf("Local 偏移=%d，want 28800", off)
	}
	// 中国无夏令时：本地日期恒等于 UTC+8 日期（跨零点口径）。
	want := time.Now().UTC().Add(8 * time.Hour).Format("2006-01-02")
	if got := time.Now().Format("2006-01-02"); got != want {
		t.Fatalf("Local 日期=%s，want %s（应与 UTC+8 同日）", got, want)
	}

	// 非法名称必须报错且不改动当前时区（引擎只 WARN，不能悄悄换时区）。
	if err := applyTZ("Not/AZone"); err == nil {
		t.Fatal("无效时区应返回错误")
	}
	if _, off := time.Now().Zone(); off != 8*3600 {
		t.Fatalf("无效时区改变了 Local：偏移=%d", off)
	}

	// 空值 / 前导冒号（POSIX 写法）不报错。
	if err := applyTZ(""); err != nil {
		t.Fatalf("空时区应无操作: %v", err)
	}
	if err := applyTZ(":Asia/Tokyo"); err != nil {
		t.Fatalf("冒号前缀应可用: %v", err)
	}
	if _, off := time.Now().Zone(); off != 9*3600 {
		t.Fatalf("Local 偏移=%d，want 32400", off)
	}
}
