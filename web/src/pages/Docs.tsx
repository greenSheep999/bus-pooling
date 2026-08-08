import { useState } from "react";
import type { ReactNode } from "react";
import { KeyRound, ShieldCheck, Terminal, Webhook } from "lucide-react";
import { Link } from "react-router-dom";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { CodeBlock } from "@/components/ui/code-block";
import {
  BareHead, BareList, BareRow, Card, Chip, Em, SectionHead,
} from "@/components/ui/primitives";
import { cn } from "@/lib/utils";

/* 这是技术页 · 允许出现内部术语（credential / housepool 之类）
   —— 跟设置里的号池页同性质（CLAUDE.md §12.6 技术页例外） */

type Section = "start" | "pull" | "assign" | "webhook" | "errors";

const NAV: { id: Section; label: string }[] = [
  { id: "start", label: "开始" },
  { id: "pull", label: "拉号" },
  { id: "assign", label: "派去向" },
  { id: "webhook", label: "Webhook" },
  { id: "errors", label: "错误码" },
];

export default function Docs() {
  const [sec, setSec] = useState<Section>("start");

  return (
    <div className="space-y-section">
      <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
        <div className="min-w-0 space-y-2">
          <h1 className="text-hero font-semibold">对接文档</h1>
          <p className="text-fg-tertiary">
            用 API key 调我方接口 · 拉号、派去向、收 webhook 都能脚本化
          </p>
        </div>
        <Button variant="ghost" asChild>
          <Link to="/settings/api-keys">
            <KeyRound />
            管理 API key
          </Link>
        </Button>
      </div>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-[180px_minmax(0,1fr)]">
        {/* 侧边导航 · 移动端横向滚动 */}
        <nav className="-mx-1 flex gap-1 overflow-x-auto px-1 lg:sticky lg:top-24 lg:mx-0 lg:h-fit lg:flex-col lg:px-0">
          {NAV.map((n) => (
            <button
              key={n.id}
              type="button"
              onClick={() => setSec(n.id)}
              className={cn(
                "shrink-0 rounded-xl px-3 py-2 text-left text-label font-medium transition-colors",
                sec === n.id
                  ? "bg-brand-subtle font-semibold text-brand-strong"
                  : "text-fg-tertiary hover:bg-bg-elevated hover:text-fg-secondary",
              )}
            >
              {n.label}
            </button>
          ))}
        </nav>

        <div className="min-w-0 space-y-6">
          {sec === "start" && <StartSection />}
          {sec === "pull" && <PullSection />}
          {sec === "assign" && <AssignSection />}
          {sec === "webhook" && <WebhookSection />}
          {sec === "errors" && <ErrorsSection />}
        </div>
      </div>
    </div>
  );
}

/* ─────────────── 开始 ─────────────── */

function StartSection() {
  return (
    <>
      <Card className="p-7">
        <SectionHead title="鉴权" sub="脚本用 API key · 浏览器登录后自动带 cookie" />
        <div className="mt-4 space-y-3">
          <CodeBlock
            lang="bash"
            code={`curl https://<base-url>/api/me/profile \\
  -H "X-API-Key: usr-<你的 key>"

# 或者用 Bearer
curl https://<base-url>/api/me/profile \\
  -H "Authorization: Bearer usr-<你的 key>"`}
          />
          <Alert tone="warn" icon={ShieldCheck} title="API key 的权限是收窄的">
            只能调 <code className="font-mono">/api/me/*</code> ·
            不能改登录密码，也不能创建新的 API key —— 免得 key 泄露后被人反锁你自己
          </Alert>
        </div>
      </Card>

      <Card className="p-7">
        <SectionHead title="通用约定" sub="所有接口都遵守这几条" />
        <div className="mt-4 space-y-3">
          <ConventionRow label="金额单位" value={<>整数 microunit · 1 积分 = <Em>1_000_000</Em></>} />
          <ConventionRow label="时间" value="ISO-8601 UTC 字符串" />
          <ConventionRow
            label="分页"
            value={<code className="font-mono text-label">?page=1&page_size=50</code>}
          />
          <ConventionRow
            label="错误"
            value={<>按 <code className="font-mono">code</code> 分派，别按 message（message 会改）</>}
          />
          <ConventionRow
            label="幂等"
            value={
              <>
                拉号 / 派去向 / 充值起单<Em plain>必须</Em>带{" "}
                <code className="font-mono text-label">X-Idempotency-Key</code>（32 hex）
              </>
            }
          />
        </div>
      </Card>

      <Card className="p-7">
        <SectionHead title="幂等键怎么用" sub="同一个 key 重放，拿到字节一致的原响应，不会重复扣钱" />
        <div className="mt-4">
          <CodeBlock
            lang="bash"
            code={`KEY=$(openssl rand -hex 16)

curl -X POST https://<base-url>/api/me/buses/<bus_id>/pull \\
  -H "X-API-Key: usr-<你的 key>" \\
  -H "X-Idempotency-Key: $KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"count": 3}'

# 网络断了？拿同一个 $KEY 重发，不会拉第二批`}
          />
        </div>
        <p className="mt-3 text-label text-fg-tertiary">幂等窗口 30 天 · 之后同 key 视为新请求</p>
      </Card>
    </>
  );
}

function ConventionRow({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex flex-col gap-1 border-b border-hairline pb-3 last:border-0 last:pb-0 sm:flex-row sm:items-baseline sm:gap-4">
      <span className="w-24 shrink-0 text-label font-semibold text-fg-secondary">{label}</span>
      <span className="min-w-0 text-label text-fg-secondary">{value}</span>
    </div>
  );
}

/* ─────────────── 拉号 ─────────────── */

function PullSection() {
  return (
    <>
      <Card className="p-7">
        <SectionHead
          title="给车拉号"
          sub={<><code className="font-mono">POST /api/me/buses/{"{bus_id}"}/pull</code></>}
        />
        <div className="mt-4 space-y-3">
          <CodeBlock
            lang="json"
            code={`// 请求
{
  "count": 5,
  "vendor_id": "kiro91",        // 不传 = 让系统比价选
  "constraints": {
    "max_unit_price": 30000000  // 30 积分
  }
}`}
          />
          <CodeBlock
            lang="json"
            code={`// 响应
{
  "pull_round_id": "01H8...",
  "vendor_id": "kiro91",
  "purchased": 5,
  "key_cost": 100000000,        // 号价 · 原样转给上游
  "single_pull_fee": 0,          // 只拉 1 个时才有
  "service_fee_total": 5000000,  // 每人每次 · 社群 1 / 零售 7 积分
  "total_debit": 105000000,
  "balance_remaining": 895000000
}`}
          />
          <Alert tone="neutral" icon={Terminal}>
            <code className="font-mono">count == 1</code> 会触发单次议价（号价 × 20%）·
            多拉几个更划算 · 服务费按注册时有没有邀请码分两档 ·
            拉号不涉及通道费，那个只在充值时收
          </Alert>
        </div>
      </Card>

      <Card className="p-7">
        <SectionHead
          title="不进车 · 单独拉"
          sub={<><code className="font-mono">POST /api/me/pull</code> · 号进「待派」等你决定去向</>}
        />
        <div className="mt-4">
          <CodeBlock lang="bash" code={`curl -X POST https://<base-url>/api/me/pull \\
  -H "X-API-Key: usr-<你的 key>" \\
  -H "X-Idempotency-Key: $(openssl rand -hex 16)" \\
  -d '{"count": 2, "zone": "us"}'`} />
        </div>
      </Card>
    </>
  );
}

/* ─────────────── 派去向 ─────────────── */

function AssignSection() {
  return (
    <>
      <Card className="p-7">
        <SectionHead
          title="派去向"
          sub={<><code className="font-mono">POST /api/me/pull-records/assign</code> · 3 种去向</>}
        />
        <div className="mt-4 space-y-3">
          <div className="divide-y divide-hairline">
            <DestRow
              code="into_bus"
              title="进车"
              desc="号归车管理 · 车内成员共享 · 我方持续监控存活"
            />
            <DestRow
              code="push_pool"
              title="推我的号池"
              desc="双写到你配的 kiro.rs · 我方保留副本继续监控"
            />
            <DestRow
              code="handoff"
              title="拿走"
              desc="拿明文后号离开系统 · 我方不再监控，也给不回明文"
            />
          </div>
          <CodeBlock
            lang="json"
            code={`// 请求
{
  "credential_ids": ["01H8...", "01H9..."],
  "destination": "into_bus",
  "bus_id": "01HA..."          // destination=into_bus 时必填
}`}
          />
        </div>
      </Card>

      <Card className="p-7">
        <SectionHead title="拿走是两阶段的" sub="防止「我方已删号、你却没收到明文」" />
        <div className="mt-4 space-y-3">
          <CodeBlock
            lang="bash"
            code={`# 1. 起单 → 拿 token
POST /api/me/pull-records/<id>/handoff-init

# 2. 用 token 取明文（TTL 内可以重试）
GET /api/me/handoff/<token>

# 3. 确认收到 → 我方才真正删号
POST /api/me/handoff/<token>/confirm`}
          />
          <Alert tone="warn" icon={ShieldCheck} title="第 3 步之前号还在">
            没 confirm 的话号不会被删 —— 所以第 2 步断线了可以重来 ·
            confirm 之后明文就再也拿不到了
          </Alert>
        </div>
      </Card>
    </>
  );
}

function DestRow({ code, title, desc }: { code: string; title: string; desc: string }) {
  return (
    <div className="flex flex-col gap-1 py-3 sm:flex-row sm:items-baseline sm:gap-4">
      <code className="w-28 shrink-0 font-mono text-label font-semibold text-brand-strong">
        {code}
      </code>
      <div className="min-w-0">
        <div className="text-label font-semibold">{title}</div>
        <p className="text-label text-fg-tertiary">{desc}</p>
      </div>
    </div>
  );
}

/* ─────────────── Webhook ─────────────── */

function WebhookSection() {
  return (
    <>
      <Card className="p-7">
        <SectionHead title="收事件" sub="我方 POST 到你配的地址 · 请求带 HMAC 签名" />
        <div className="mt-4 space-y-3">
          <CodeBlock
            lang="json"
            code={`// 我方发给你的
{
  "event": "round.completed",
  "created_at": "2026-08-08T12:34:56.789Z",
  "data": {
    "pull_round_id": "01H8...",
    "bus_id": "01HA...",
    "purchased": 3,
    "total_debit": 63000000
  }
}`}
          />
          <p className="text-label text-fg-tertiary">
            去 <Link to="/settings/webhook" className="font-semibold text-brand-strong hover:underline">机器人通知</Link> 配地址和订阅哪些事件
          </p>
        </div>
      </Card>

      <Card className="p-7">
        <SectionHead title="验签" sub="用你那边的 secret 算 HMAC-SHA256，跟 header 比" />
        <div className="mt-4 space-y-3">
          <CodeBlock
            lang="js"
            code={`import crypto from "node:crypto";

// 用**原始 body 字节**算，不要先 JSON.parse 再 stringify
function verify(rawBody, signature, secret) {
  const mac = crypto.createHmac("sha256", secret).update(rawBody).digest("hex");
  // 定长比较，避免时序侧信道
  return crypto.timingSafeEqual(
    Buffer.from(mac),
    Buffer.from(signature),
  );
}`}
          />
          <Alert tone="danger" icon={Webhook} title="别用 == 比签名">
            字符串短路比较会泄露信息 · 用 <code className="font-mono">timingSafeEqual</code> ·
            也别拿 parse 过又 stringify 的 body 算，字节序会变
          </Alert>
        </div>
      </Card>
    </>
  );
}

/* ─────────────── 错误码 ─────────────── */

const ERRORS: { http: number; code: string; meaning: string }[] = [
  { http: 400, code: "bad_json", meaning: "请求体不是合法 JSON，或带了未知字段" },
  { http: 400, code: "bad_count", meaning: "count 超范围" },
  { http: 400, code: "bad_vendor", meaning: "vendor_id 不认识" },
  { http: 400, code: "bad_bus_id", meaning: "车不存在，或不是你的车" },
  { http: 401, code: "invalid_api_key", meaning: "key 无效或已吊销" },
  { http: 402, code: "insufficient_balance", meaning: "余额不够，先充值" },
  { http: 403, code: "session_required", meaning: "这个操作只能浏览器登录做（改密码 / 建 key）" },
  { http: 404, code: "not_found", meaning: "资源不存在" },
  { http: 409, code: "no_stock", meaning: "上游缺货，过会儿再试" },
  { http: 409, code: "idempotency_conflict", meaning: "同一个幂等键，但请求体不一样" },
  { http: 429, code: "rate_limited", meaning: "限流了，看 retry_after 秒数" },
  { http: 502, code: "vendor_error", meaning: "上游挂了或网络不通" },
  { http: 503, code: "housepool_unavailable", meaning: "号池暂时不可用" },
  { http: 500, code: "internal", meaning: "我方的问题，可以报给我们" },
];

function ErrorsSection() {
  return (
    <Card className="p-7">
      <SectionHead
        title="错误码"
        sub={<>按 <code className="font-mono">code</code> 分派逻辑 · message 是给人看的，会改</>}
      />

      <div className="mt-4 overflow-x-auto">
        <div className="min-w-[520px]">
          <BareHead>
            <span className="w-14 shrink-0">HTTP</span>
            <span className="w-[200px] shrink-0">code</span>
            <span className="min-w-0 flex-1">含义</span>
          </BareHead>
          <BareList>
            {ERRORS.map((e) => (
              <BareRow key={e.code}>
                <span className="w-14 shrink-0">
                  <Chip tone={e.http >= 500 ? "danger" : e.http >= 400 ? "warn" : "neutral"}>
                    {e.http}
                  </Chip>
                </span>
                <code className="w-[200px] shrink-0 truncate font-mono text-label font-semibold text-fg-secondary">
                  {e.code}
                </code>
                <span className="min-w-0 flex-1 text-label text-fg-tertiary">{e.meaning}</span>
              </BareRow>
            ))}
          </BareList>
        </div>
      </div>

      <Alert tone="neutral" icon={Terminal} className="mt-4">
        <code className="font-mono">429</code> 和 <code className="font-mono">502/503</code> 都值得自动重试 ·
        <code className="font-mono">402</code> 和 <code className="font-mono">4xx</code> 参数类的重试也没用
      </Alert>
    </Card>
  );
}
