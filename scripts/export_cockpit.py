#!/usr/bin/env python3
"""把桌面端的 auths/workbuddy-*.json 导出成「cockpit tools」数组格式。

用途：手机端 App 的账号在它自己的私有目录里，与电脑端互不相通。想把手上的账号
搬到手机，最省事的路径是：本脚本导出一个 JSON → 传到手机 → 面板「添加账号 →
导入」里选这个文件。

字段映射（桌面内部格式 → 面板导入格式）：

    account.uid          -> uid
    account.nickname     -> nickname
    auth.accessToken     -> access_token
    auth.refreshToken    -> refresh_token
    auth.expiresAt (秒)  -> expires_at (毫秒；服务端导入时会 /1000)
    auth.domain          -> domain（cn/global 由它判定）
    auth.realm           -> realm=global 且 domain 为空时补 www.workbuddy.ai
    顶层 note            -> note（面板里的账号备注，随账号一起迁移）

用法：
    python scripts/export_cockpit.py              # 读 ./auths，写 cockpit-accounts.json
    python scripts/export_cockpit.py -o out.json auths/

注意：导出文件里是**明文** accessToken / refreshToken，等同于账号本身。
     传完即删，别留在聊天记录、网盘或仓库里（文件名已在 .gitignore 之外，自行判断）。
"""

import argparse
import glob
import json
import os
import sys


def convert_one(path):
    """读一个 workbuddy-*.json；字段不全返回 None（跳过并提示）。"""
    try:
        with open(path, encoding="utf-8") as f:
            raw = json.load(f)
    except (OSError, ValueError) as e:
        print(f"  跳过 {os.path.basename(path)}: {e}", file=sys.stderr)
        return None

    acct = raw.get("account") or {}
    auth = raw.get("auth") or {}

    uid = str(acct.get("uid") or "").strip()
    access = str(auth.get("accessToken") or "").strip()
    refresh = str(auth.get("refreshToken") or "").strip()
    if not (uid and access and refresh):
        print(f"  跳过 {os.path.basename(path)}: 缺 uid/accessToken/refreshToken", file=sys.stderr)
        return None

    try:
        expires = int(auth.get("expiresAt") or 0)
    except (TypeError, ValueError):
        expires = 0

    note = str(raw.get("note") or "").strip()
    domain = str(auth.get("domain") or "").strip()
    if str(auth.get("realm") or "").strip() == "global" and not domain:
        domain = "www.workbuddy.ai"

    return {
        "id": uid,
        "uid": uid,
        "nickname": str(acct.get("nickname") or ""),
        "note": note,
        "email": "",
        "access_token": access,
        "refresh_token": refresh,
        "token_type": "Bearer",
        "expires_at": expires * 1000,  # 面板导入按毫秒解析
        "domain": domain,
        "status": "active",
    }


def main():
    ap = argparse.ArgumentParser(description="导出桌面端账号为面板可导入的 JSON")
    ap.add_argument("auths", nargs="?", default="auths", help="auths 目录（默认 ./auths）")
    ap.add_argument("-o", "--out", default="cockpit-accounts.json", help="输出文件")
    args = ap.parse_args()

    files = sorted(glob.glob(os.path.join(args.auths, "workbuddy-*.json")))
    if not files:
        print(f"错误：{args.auths} 下没有 workbuddy-*.json", file=sys.stderr)
        return 1

    accounts = [a for a in (convert_one(f) for f in files) if a]
    if not accounts:
        print("错误：没有可导出的账号（全部字段不全或无法解析）", file=sys.stderr)
        return 1

    with open(args.out, "w", encoding="utf-8") as f:
        json.dump(accounts, f, ensure_ascii=False, indent=2)

    try:
        os.chmod(args.out, 0o600)
    except OSError:
        pass

    print(f"已导出 {len(accounts)} 个账号（共扫描 {len(files)} 个文件）-> {args.out}")
    print("该文件含明文 token：传到手机并在面板「添加账号 → 导入」里选中后，请删除本地副本。")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
