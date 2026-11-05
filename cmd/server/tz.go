package main

import (
	"log"
	"os"
	"runtime"
	"strings"
	"time"
)

// applyTZ 把 TZ 环境变量落到 time.Local。
//
// 必须手动做：GOOS=android 上 Go 的 time 包把 Local 固定为 UTC 且**不读 TZ**
// （time/zoneinfo_android.go: initLocal 直接 localLoc = *UTC）。手机端 App 虽在
// 环境变量里传了 TZ，进程内 time.Now() 仍是 UTC —— 于是「今日已签到」要到 UTC
// 零点（北京 08:00）才重置、按钟点触发的排程也整体偏 8 小时。
// 时区库由 main.go 的 time/tzdata 内嵌，Android 没有 zoneinfo 文件也能 LoadLocation。
func applyTZ(tz string) error {
	tz = strings.TrimPrefix(strings.TrimSpace(tz), ":") // POSIX 允许 ":" 前缀
	if tz == "" {
		return nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return err
	}
	time.Local = loc
	return nil
}

func init() {
	// 只有 Android 需要这一层：其它平台的 time 包本来就会读 TZ（Windows 走注册表，
	// 不跟随 TZ 是既有行为，这里不改）。
	if runtime.GOOS != "android" {
		return
	}
	if tz := os.Getenv("TZ"); tz != "" {
		if err := applyTZ(tz); err != nil {
			log.Printf("WARN: TZ=%q 不是有效 IANA 时区，沿用系统时区: %v", tz, err)
		}
	}
}
