#!/usr/bin/env python3
"""kirodrop 会话 token 自动刷新 · captcha(ddddocr)→ login → 打印新 token。

kirodrop 的 /api/v1/* 富数据（reservation 时间降价）只认网页 session token（Bearer）·
登录带自建 4 位点阵图形码 · api-key 打不了。本脚本用 ddddocr 离线识别验证码 + 重试循环
（单次识别不一定准 · 每次换新码重试 · 几次内必成）· 成功把 token 打到 stdout（**只有 token**）·
失败退出码非 0。

跑法（容器内）：
    KDROP_BASE=https://drop.kiro.ss KDROP_EMAIL=... KDROP_PASSWORD=... python solve.py
只有 token 会进 stdout · 其余日志走 stderr（方便 host 脚本 `TOKEN=$(docker run ...)` 抓）。
"""
import base64
import json
import os
import re
import sys
import time
import urllib.error
import urllib.request

import io

import ddddocr
from PIL import Image, ImageFilter

BASE = os.environ.get("KDROP_BASE", "https://drop.kiro.ss").rstrip("/")
EMAIL = os.environ["KDROP_EMAIL"]
PASSWORD = os.environ["KDROP_PASSWORD"]
MAX_TRIES = int(os.environ.get("KDROP_MAX_TRIES", "25"))

# session token 的已知格式 · 从任意成功响应里递归捞它（不依赖字段名）
TOKEN_RE = re.compile(r"kd_session_[A-Za-z0-9_\-]+")

UA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124 Safari/537.36"


def log(*a):
    print(*a, file=sys.stderr, flush=True)


def http(method, path, data=None):
    url = BASE + path
    body = json.dumps(data).encode() if data is not None else None
    headers = {"User-Agent": UA, "Origin": BASE, "Referer": BASE + "/", "Accept": "application/json"}
    if body:
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(url, data=body, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=20) as r:
            return r.status, r.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, (e.read() or b"").decode("utf-8", "replace")


def build_ocr():
    """约束 ddddocr 只吐 0-9 · 消除把数字误读成字母（'E204'）· 显著提准。"""
    ocr = ddddocr.DdddOcr(show_ad=False)
    digit_mode = False
    try:
        ocr.set_ranges("0123456789")
        digit_mode = True
    except Exception as e:  # noqa: BLE001
        log(f"set_ranges 不可用 · 退回普通识别: {e}")
    return ocr, digit_mode


def denoise(img_bytes):
    """去噪：captcha 数字是暗像素(brightness<95)· 彩色噪点/浅线是亮/中调。
    只留暗像素二值化 + 中值去孤点 + MinFilter 膨胀连点 → 点阵数字变清晰实体。
    实测把重噪 captcha 从"肉眼难认"洗成清清楚楚（ddddocr 准确率大涨）。"""
    im = Image.open(io.BytesIO(img_bytes)).convert("L")
    bw = im.point(lambda p: 0 if p < 95 else 255, "1").convert("L")
    bw = bw.filter(ImageFilter.MedianFilter(3))
    bw = bw.filter(ImageFilter.MinFilter(3))
    out = io.BytesIO()
    bw.save(out, "PNG")
    return out.getvalue()


def recognize(ocr, digit_mode, img_bytes):
    """返 (识别文本, 置信度) · 先去噪再 digit_mode(set_ranges 约束只出数字)。"""
    try:
        img_bytes = denoise(img_bytes)
    except Exception as e:  # noqa: BLE001 · 去噪失败退回原图
        log(f"去噪失败 · 用原图: {e}")
    if not digit_mode:
        return ocr.classification(img_bytes), 1.0
    res = ocr.classification(img_bytes, probability=True)
    if isinstance(res, dict) and "text" in res:
        return res["text"], float(res.get("confidence") or 0.0)
    return ocr.classification(img_bytes), 1.0


def main():
    ocr, digit_mode = build_ocr()
    base_delay = float(os.environ.get("KDROP_BASE_DELAY", "2"))
    min_conf = float(os.environ.get("KDROP_MIN_CONF", "0.9"))
    max_429 = int(os.environ.get("KDROP_MAX_429", "3"))
    consec_429 = 0  # 连续限流计数 · 超阈值早退（别死磕·下次 cron 再来）
    for attempt in range(1, MAX_TRIES + 1):
        try:
            s, capraw = http("GET", "/api/v1/auth/captcha")
            if s != 200:
                log(f"[{attempt}] captcha http {s}")
                time.sleep(base_delay)
                continue
            cap = json.loads(capraw)
            img = cap["image"].split(",", 1)[1]
            code, conf = recognize(ocr, digit_mode, base64.b64decode(img))
            if not (code.isdigit() and len(code) == 4):
                log(f"[{attempt}] ocr 非 4 位数字: {code!r} · 换码")
                time.sleep(base_delay)
                continue
            if conf < min_conf:
                log(f"[{attempt}] 置信 {conf:.2f}<{min_conf} · 换码不浪费登录额度")
                time.sleep(base_delay)
                continue
            s, resraw = http("POST", "/api/v1/auth/login", {
                "email": EMAIL, "password": PASSWORD,
                "captcha_token": cap["token"], "captcha_code": code,
            })
            if s == 200:
                m = TOKEN_RE.search(resraw)
                if m:
                    print(m.group(0))  # 只有 token 进 stdout
                    log(f"[{attempt}] ✅ 登录成功 · 拿到 token")
                    return 0
                log(f"[{attempt}] 登录 200 但没找到 kd_session_ token · 响应片段: {resraw[:160]}")
                return 2  # 200 却无 token = 结构变了 · 别再重试
            try:
                code_str = json.loads(resraw).get("error", {}).get("code", "")
            except Exception:
                code_str = ""
            # 429 限流：连续超 max_429 次就早退（别死磕 · 下次 cron 再来 · 免升级封禁）
            if s == 429:
                consec_429 += 1
                if consec_429 >= max_429:
                    log(f"❌ 连续 {consec_429} 次 429 限流 · 早退（下次 cron 再试）")
                    return 3
                log(f"[{attempt}] 429 限流({consec_429}/{max_429}) · 睡 20s")
                time.sleep(20)
            else:
                consec_429 = 0  # 非限流 · 重置
                log(f"[{attempt}] 登录 http {s} {code_str} · 换码重试")
                time.sleep(base_delay)
        except Exception as e:  # noqa: BLE001
            log(f"[{attempt}] 异常: {e}")
            time.sleep(base_delay)
    log(f"❌ {MAX_TRIES} 次都没成功")
    return 1


if __name__ == "__main__":
    sys.exit(main())
