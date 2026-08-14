# 14 · 部署 runbook（阶段 1a-1c 首上线）

> 目标读者：**首次把 bus-pooling 从本机搬到公网机器**的人（可能只有 1 个）。
> 前提：`archive/sprint-1a-backend.md` DoD 已打勾（已归档 · 阶段 1 feature-complete），真 vendor happy path 至少跑过一次（`#117`）。

## 0. 结构一览

```
公网                                       内部
─────────────────────                       ─────────────────────
乘客浏览器  ─── https ────►  Caddy / nginx  ─── http ────► bus-pooling (Go, :8080)
                                                                │
                                                                ├─ 404bus-payment-gateway (:18099)
                                                                │
                                                                └─ kiro.rs (kiro.aibbq.xyz, https)
```

**约定**：反代 + 单进程 Go 后端 + SQLite 单文件 + 外部 kiro.rs + 外部 payment-gateway。
不引入 k8s / redis / 消息队列 —— 阶段 1 的规模用不上。

## 1. 机器要求

| 项 | 最低 | 推荐 | 备注 |
|---|---|---|---|
| CPU | 1 vCPU | 2 vCPU | SQLite 写不好扛并发，不为它加核 |
| 内存 | 512 MB | 1 GB | Go runtime + SQLite page cache |
| 磁盘 | 10 GB | 20 GB SSD | DB + logs + backups |
| 网络 | | 稳定出网 | 要能到 kiro.rs、vendor 官方接口、payment-gateway |
| OS | Linux amd64 | Debian 12 / Ubuntu 22.04 | 别选 alpine musl · CGO SQLite 编不过要多做一步 |

## 2. 生成主密钥（第一次·唯一一次）

```bash
BP_MASTER_KEY=$(./bp genkey)
echo "$BP_MASTER_KEY"    # 记下·丢了 vendor 凭证 / 号池 token 全解不出
```

**放哪**：`/etc/bus-pooling/env`（`chmod 600` · 只有服务用户可读）。**别**放 git、别放 systemd unit file、别 echo 到日志。

## 3. 环境文件 · `/etc/bus-pooling/env`

```bash
# ── 必填 ──
BP_MASTER_KEY=<32 hex 主密钥>
BP_DB_PATH=/var/lib/bus-pooling/data.db
BP_ADDR=127.0.0.1:8080

# ── 号池（kiro.rs 是外部服务·由 kiro.rs 侧提供 URL + admin key）──
BP_HOUSEPOOL_URL=https://kiro.aibbq.xyz
BP_HOUSEPOOL_ADMIN_KEY=<kiro.rs 侧签发>
BP_HOUSEPOOL_EXPECTED_VERSION=<语义版本 · 例 2.3.1>

# ── vendor（有几家配几家 · 生产 live 前 DRY_RUN=1 先跑一遍）──
BP_VENDOR_KIRO91_ENABLED=1
BP_VENDOR_KIRO91_API_KEY=<vendor 侧签发>
BP_VENDOR_KIRO91_WEBHOOK_SECRET=<vendor 侧签发>
# 同理其他 5 家（KIROCEO / KIROOOO / KIROAPPIO / KIROAPPCC / KIRODROP）

# ── 服务费率（万分位 · 500 = 5%）──
BP_RATE_SERVICE_BP=500
# 其他加价链层阶段 1 全 0 · 不设

# ── payment-gateway（另起一个进程 · 见 §5）──
BP_GW_BASE=http://127.0.0.1:18099
BP_GW_TOKEN=<gateway -add-client 生成>
BP_GW_SETTLEMENT_SECRET=<gateway -add-client 生成>
BP_GW_SUCCESS_URL=https://<你的域名>/wallet   # 用户付完回跳位置

# ── 上线双锁（默认 DRY_RUN=1 · 都要显式开才拉真号）──
DRY_RUN=0
BP_ALLOW_LIVE_PULL=1

# ── 安全开关 ──
BP_STRICT_HANDOFF=1                 # 拒占位明文·防降级
# BP_INSECURE_COOKIE 生产必空
# BP_ENABLE_DEV_TOPUP 生产必空
```

**checklist**（上线前最后一遍）：
- [ ] `DRY_RUN=0` **且** `BP_ALLOW_LIVE_PULL=1` 都设
- [ ] `BP_STRICT_HANDOFF=1`
- [ ] `BP_INSECURE_COOKIE` **不**设（生产要 https + Secure cookie）
- [ ] `BP_ENABLE_DEV_TOPUP` **不**设（暴露 dev 接口 = 白嫖入口）
- [ ] `BP_RATE_SERVICE_BP` 非零（零费率启动会拒）
- [ ] `BP_HOUSEPOOL_EXPECTED_VERSION` 跟 kiro.rs 现装的版本一致

## 4. 首次装 · 步骤

```bash
# ── ① 建服务用户 + 目录 ──
sudo useradd --system --shell /usr/sbin/nologin bp
sudo install -d -o bp -g bp -m 750 /var/lib/bus-pooling /var/log/bus-pooling
sudo install -d -o root -g bp -m 750 /etc/bus-pooling
sudo install -m 640 -o root -g bp env /etc/bus-pooling/env

# ── ② 编译（在开发机或 CI 上都行 · 生成 linux amd64 二进制传上去）──
GOOS=linux GOARCH=amd64 CGO_ENABLED=1 go build -o /tmp/bp ./cmd/bus-pooling
scp /tmp/bp <host>:/usr/local/bin/bp
sudo chmod 755 /usr/local/bin/bp

# ── ③ DB 迁移（一次性）──
sudo -u bp env $(cat /etc/bus-pooling/env | xargs) /usr/local/bin/bp migrate

# ── ④ 前端 build → 传上去 → 反代 root ──
cd web && npm ci && npm run build
scp -r dist/* <host>:/var/www/bus-pooling/
```

## 5. systemd unit

`/etc/systemd/system/bus-pooling.service`：

```ini
[Unit]
Description=bus-pooling backend
After=network.target
Requires=network.target

[Service]
Type=simple
User=bp
Group=bp
EnvironmentFile=/etc/bus-pooling/env
ExecStart=/usr/local/bin/bp serve
Restart=on-failure
RestartSec=5s
# 不 kill · 让 janitor 有机会推进卡单
KillSignal=SIGTERM
TimeoutStopSec=30
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/bus-pooling /var/log/bus-pooling
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now bus-pooling
sudo systemctl status bus-pooling
sudo journalctl -u bus-pooling -f     # 看日志
```

## 6. payment-gateway（另起）

见 `404bus-payment-gateway` 项目自己的 README。**关键**：

```bash
# 用 CLI 建 client · 拿到 bearer + settlement_secret + settlement_url 三样
./gateway -add-client \
  --name bus-pooling \
  --settlement-url https://<你的域名>/api/hooks/paymentgw/settlement
```

回填到 `/etc/bus-pooling/env` 的 `BP_GW_TOKEN` / `BP_GW_SETTLEMENT_SECRET`（`BP_GW_BASE` 是 gateway 自己的 base URL）。

`settlement_url` 填的路径要跟 bus-pooling 里挂的 handler 对上 —— 现在是 `POST /api/hooks/paymentgw/settlement`（server.go 装配时自动挂）。

### 6.1 ⚠️ settlement URL 检查清单（**上生产前必核** · P7 · 2026-08-14）

`404bus-payment-gateway` 的 README 里的示例路径可能写的是 `/api/hooks/404bus/settlement`
（旧命名 · 那时候还没定我方项目名叫 kirobus）。**bus-pooling 里没这条路由 · 按 README
示例直配 · 乘客付了钱回调打到 404 · 到不了账**（这是唯一要盯死的坑）。

**部署时逐条对**：

- [ ] gateway CLI 的 `--settlement-url` **完整拷** 到浏览器打开 · 域名对 · 路径是 `/api/hooks/paymentgw/settlement`
- [ ] 回一个正式请求打上面这个 URL · 应返 `401 unauthorized`（签名不对）· 而不是 `404 not_found`
      （404 = 路径写错 · 401 = 路径对但签名验证挂 —— 正确的"没配 secret"表现）
- [ ] `BP_GW_SETTLEMENT_SECRET` 跟 gateway 侧的 client 匹配 —— 上一步能过 401 就说明匹配
- [ ] 打一笔 dry-run 充值 · 看 `outbound_webhook_delivery` 里 settlement 到帐记录存在
- [ ] `wallet_ledger` 里能看到 `reason=recharge` + `reason=channel_fee` 两条（`§1.4` 口径）

## 7. 反代 · Caddy 例

`/etc/caddy/Caddyfile`：

```caddy
<你的域名> {
    encode gzip zstd

    # ── 前端静态资源 ──
    root * /var/www/bus-pooling
    file_server

    # ── 后端 API ──
    handle /api/* {
        reverse_proxy 127.0.0.1:8080
    }

    # ── SPA fallback ──
    try_files {path} /index.html
}
```

Caddy 自动办 Let's Encrypt 证书。nginx 版本类似 —— 静态 root + `location /api { proxy_pass http://127.0.0.1:8080; }` + `try_files $uri /index.html`。

## 8. 备份 · 恢复

**SQLite WAL 模式**下不能直接 `cp data.db`（会拿到不一致快照）。用 `sqlite3` 的 `.backup`：

```bash
# 备份（每天 cron）
sudo -u bp sqlite3 /var/lib/bus-pooling/data.db ".backup /var/lib/bus-pooling/backup-$(date +%F).db"

# 定期上传 off-site（rsync / rclone / aws s3）
```

**恢复**：停服务 → 覆盖 `data.db` → 起服务 → 用 `bp` 起来的第一次 startup 会自动应用 pending migration。

## 9. 首次真流量前 · smoke test

```bash
# 起服务后 · 用真 API key 打
BASE=https://<你的域名>
KEY=<某 passenger 的 api key>

# ① 健康
curl -f -sS "$BASE/api/health"

# ② 账号
curl -f -sS -H "X-API-Key: $KEY" "$BASE/api/me"

# ③ 建 1 人 bus + 拉 1 号（真金白银·钱包要够）
BUS=$(curl -sS -H "X-API-Key: $KEY" -H "content-type: application/json" \
  -X POST "$BASE/api/me/buses" \
  -d '{"name":"smoke","kind":"single"}' | jq -r .bus.id)

curl -sS -H "X-API-Key: $KEY" -H "content-type: application/json" \
  -H "X-Idempotency-Key: $(openssl rand -hex 16)" \
  -X POST "$BASE/api/me/buses/$BUS/pull" \
  -d '{"count":1}' | jq
```

跑成功·从 kiro.rs admin UI 看得到 `bus-$BUS` group 里多了 1 个 credential · smoke 通过。

## 10. 上线后 · 手工监控 checklist（阶段 1 · 阶段 3+ 才做真监控）

**每天一次**：
- `journalctl -u bus-pooling --since yesterday | grep -E "level=ERROR|panic"`
- `sqlite3 data.db 'SELECT COUNT(*), status FROM pending_purchase WHERE status NOT IN ("completed","need_manual") GROUP BY status'` —— 卡单看看
- `sqlite3 data.db 'SELECT COUNT(*) FROM pending_topup WHERE status="pending_manual"'` —— 转人工的充值单
- kiro.rs `GET /admin/health` —— 号池版本 / 存活数量对不对

**每周一次**：
- 磁盘剩余 > 20%
- 备份文件真的有生成 · 挑一个恢复到测试环境跑一遍

## 11. 下线 / 迁机

```bash
sudo systemctl stop bus-pooling
sudo -u bp sqlite3 /var/lib/bus-pooling/data.db ".backup /tmp/final.db"
# 传 final.db 到新机 · 走 §4 步骤重装 · 起服务
```

`data.db` + `/etc/bus-pooling/env` + 前端 dist 就是全部状态。**没有其他外部依赖需要迁移**（kiro.rs / payment-gateway 独立运维）。

## 12. 已知踩坑

| 症状 | 原因 | 修法 |
|---|---|---|
| 启动即退出 · `master key must be 32 bytes` | `BP_MASTER_KEY` 少了 / 多了字符 | `bp genkey` 重新生成 |
| 启动即退出 · `service_rate must be > 0` | `BP_RATE_SERVICE_BP` 没设或设成 0 | 设个非零值（万分位·500=5%） |
| 启动即退出 · `housepool version mismatch` | kiro.rs 侧升级了 · 本地 EXPECTED_VERSION 没跟上 | 核对 kiro.rs 的 `GET /admin/system/update/check` · 更新 env |
| 拉号一直 500 · log 无 error | `DRY_RUN=0` 忘设 · 或 `BP_ALLOW_LIVE_PULL` 忘设 | 补上·重启 |
| 充值 QR 打不开 | payment-gateway 没起 / `BP_GW_BASE` 打错 | `curl $BP_GW_BASE/health` 试 |
| webhook 收不到 | 反代没把 `/api/hooks/*` 转发到后端 | Caddyfile 的 `handle /api/*` 该盖住 |
| Cookie 不生效（登录后 GET /me 返 401） | 反代没转发 `Cookie` header 或 `Set-Cookie` 被 `Secure` 拒（http） | 用 https · 或本地调试临时设 `BP_INSECURE_COOKIE=1` |
