import { http, HttpResponse, delay } from "msw";
import { MICRO, topupBreakdown, vendorLabel } from "@/lib/utils";
import * as fx from "./fixtures";

const ok = async (data: any, ms = 120) => {
  await delay(ms);
  return HttpResponse.json(data);
};

/** 是否享优惠价 · decisions §8.20
    有注册邀请码 → 永久免 · 或本次请求带了优惠码 → 单次免 */
const isWaived = (request: Request): boolean => {
  if (fx.passenger.invited) return true;
  const code = new URL(request.url).searchParams.get("coupon_code");
  return !!code && code.trim().length > 0;
};

/* handoff 三段式的进程内状态（真实后端是 pending_handoff 表 · 09-transactions §4）
   token → credential_ids · confirm 后进 handedOff 并从待派列表消失 */
const handoffTokens = new Map<string, string[]>();
const handedOff = new Set<string>();

export const handlers = [
  // ── 账号 / 钱包
  http.get("/api/me", () => ok(fx.passenger)),
  http.get("/api/me/wallet", () => ok(fx.wallet)),
  http.get("/api/me/ledger", () => ok({ items: fx.ledger, total: fx.ledger.length, page: 1, page_size: 20 })),

  // ── 上游库存（header badge）
  http.get("/api/vendors/stock", () => ok(fx.stock)),

  // ── 概览
  http.get("/api/me/overview", () => ok(fx.overview)),
  http.get("/api/me/trend", ({ request }) => {
    const u = new URL(request.url);
    const metric = u.searchParams.get("metric") ?? "credits";
    const range = u.searchParams.get("range") ?? "30d";
    const busId = u.searchParams.get("bus_id") ?? undefined;
    const vendor = u.searchParams.get("vendor") ?? undefined;
    const days = range === "today" ? 1 : range === "7d" ? 7 : range === "90d" ? 90 : 30;
    return ok(fx.trend(metric, days, { busId, vendor }));
  }),
  http.get("/api/me/activities", () => ok({ items: fx.activities, total: fx.activities.length, page: 1, page_size: 20 })),

  // ── Vendor
  http.get("/api/vendors/stats", () => ok({ stats: fx.vendorStats, share: fx.vendorShare })),

  // ── Bus
  http.get("/api/me/buses", () => ok({ items: fx.buses, total: fx.buses.length, page: 1, page_size: 20 })),
  http.get("/api/me/buses/:id", ({ params }) => {
    const bus = fx.buses.find((b) => b.id === params.id);
    return bus ? ok(bus) : HttpResponse.json({ error: "not_found" }, { status: 404 });
  }),
  http.get("/api/me/buses/:id/credentials", ({ params }) =>
    ok(fx.credentials.filter((c) => c.owner_bus_id === params.id)),
  ),
  http.get("/api/me/buses/:id/pulls", ({ params }) =>
    ok(fx.pullRounds.filter((p) => p.bus_id === params.id)),
  ),
  http.post("/api/me/buses", async ({ request }) => {
    const body = (await request.json()) as { name?: string };
    await delay(400);
    return HttpResponse.json({ ...fx.buses[0], id: `bus_${Date.now()}`, name: body.name ?? "新车" }, { status: 201 });
  }),
  /* PUT /me/buses/{id}/strategy · 1f-B 支持 null 值(跟随全局)· 直接落到 fx.buses 里那份对象
     所有覆盖字段(auto_refill_enabled / refill_watermark / refill_min_count / per_round_count /
     preferred_vendor / max_unit_price) 都接受 null · null = 跟随全局默认 */
  http.put("/api/me/buses/:id/strategy", async ({ params, request }) => {
    const bus = fx.buses.find((b) => b.id === params.id);
    if (!bus) return HttpResponse.json({ error: "not_found" }, { status: 404 });
    const body = (await request.json()) as Record<string, unknown>;
    for (const k of [
      "auto_refill_enabled", "refill_watermark", "refill_min_count",
      "per_round_count", "max_unit_price",
      "daily_round_limit", "daily_spend_limit", "preferred_vendor",
    ] as const) {
      if (k in body) (bus.strategy as any)[k] = body[k];
    }
    return ok({ ok: true }, 300);
  }),
  http.put("/api/me/buses/:id", () => ok({ ok: true }, 300)),
  http.post("/api/me/buses/:id/pull", () => ok({ round_id: "rd_new", status: "initiated" }, 600)),
  http.delete("/api/me/buses/:id", () => ok({ ok: true }, 400)),

  /* 成员管理 · decisions §8.26
     mock 直接改 fixtures 里那份对象 · refetch 后 UI 就反映出来了 */
  http.put("/api/me/buses/:id/members/:memberId", async ({ params, request }) => {
    const body = (await request.json()) as { suspended?: boolean };
    const bus = fx.buses.find((b) => b.id === params.id);
    const m = bus?.members.find((x) => x.passenger_id === params.memberId);
    if (m) {
      m.status = body.suspended ? "suspended" : "active";
      // 解挂 = 认为他已充值 · 跳过计数归零，重新开始数
      if (!body.suspended) { m.skipped_count = 0; m.last_skipped_at = null; }
    }
    return ok({ ok: true }, 300);
  }),
  http.delete("/api/me/buses/:id/members/:memberId", ({ params }) => {
    const bus = fx.buses.find((b) => b.id === params.id);
    if (bus) {
      bus.members = bus.members.filter((x) => x.passenger_id !== params.memberId);
      bus.member_count = bus.members.length;
    }
    return ok({ ok: true }, 400);
  }),
  http.post("/api/me/buses/:id/invite-code", ({ params }) => {
    const bus = fx.buses.find((b) => b.id === params.id);
    const code = `${Math.random().toString(36).slice(2, 5).toUpperCase()}-${Math.random().toString(36).slice(2, 5).toUpperCase()}`;
    if (bus) bus.invite_code = code;
    return ok({ invite_code: code }, 300);
  }),

  // ── 拉号记录（record group · 未派去向号）· 派去向
  http.get("/api/me/pull-records", () => {
    // 拿走过的号不再出现在「待派」（台账行仍在 fx.credentials 里，供追溯）
    const items = fx.credentials.filter((c) => c.owner_bus_id === null && !handedOff.has(c.id));
    return ok({ items, total: items.length, page: 1, page_size: 20 });
  }),
  http.post("/api/me/pull-records/assign", () => ok({ ok: true, assigned: 0 }, 400)),

  /* 拿走 · 三段式（09-transactions §4）· 号在 confirm 之前都还在池里 */
  http.post("/api/me/handoff", async ({ request }) => {
    const { credential_ids } = (await request.json()) as { credential_ids: string[] };
    const token = crypto.randomUUID().replace(/-/g, "");
    handoffTokens.set(token, credential_ids);
    return ok({
      download_token: token,
      expires_at: new Date(Date.now() + 5 * 60_000).toISOString(),
    }, 400);
  }),
  http.get("/api/me/handoff/:token", ({ params }) => {
    const ids = handoffTokens.get(String(params.token));
    if (!ids) return HttpResponse.json({ code: "token_expired", message: "下载链接已过期，请重新发起" }, { status: 404 });
    const keys = ids.map((id) => {
      const c = fx.credentials.find((x) => x.id === id);
      return {
        credential_id: id,
        // 真实环境是后端从号池实时读的明文；mock 造一个完整形状的假 key
        key: `ksk_live_${crypto.randomUUID().replace(/-/g, "")}`,
        vendor_id: c?.vendor_id ?? "91kiro",
        account: c?.account ?? "unknown",
      };
    });
    return ok({ keys }, 500);
  }),
  http.post("/api/me/handoff/:token/confirm", ({ params }) => {
    const token = String(params.token);
    const ids = handoffTokens.get(token);
    if (!ids) return HttpResponse.json({ code: "token_expired", message: "下载链接已过期" }, { status: 404 });
    /* 这一步才真删。台账行**不删**（§8.24 售后要能追溯），只是号离开系统、
       不再出现在「待派」列表里 —— 所以记一个 handed_off 集合去过滤，
       而不是改 owner_bus_id 塞魔法值 */
    ids.forEach((id) => handedOff.add(id));
    handoffTokens.delete(token);
    return ok({ ok: true }, 400);
  }),

  // ── 提取 key（端点在 /me/pull* 名下 · 待派列表就是上面那个 pull-records，不另开一个）
  http.get("/api/me/pull/events", () => ok({ items: fx.extractEvents, total: fx.extractEvents.length, page: 1, page_size: 20 })),
  http.get("/api/me/assign/events", () => ok({ items: fx.assignEvents, total: fx.assignEvents.length, page: 1, page_size: 20 })),
  /* 预估 · **对外只三项**(CLAUDE §0.1 · 不出内部加价链分层)
   *
   *  后端 internal/api/estimate.go:23-27 estimateResp:
   *    unit_price  · 分项算完的**最终单价**(已含 vendor/zone/tier/service 全部加价)
   *    service_fee · 服务费一项(对外唯一露出的分项)
   *    total       · = unit_price × count
   *
   *  **别再返 key_cost / single_pull_fee** —— 那是内部加价链字段 · §0.1 明令禁 ·
   *  而且前端 hooks.ts:497 + PullExtractForm.tsx:76 只认三字段 · 返旧形状会显示 NaN。 */
  http.post("/api/me/pull/estimate", async ({ request }) => {
    const b = (await request.json()) as { count: number; vendor_id?: string; zone?: string };
    // mock 最终单价 · 跟 fixtures.finalPrice 一个量级(20 积分左右)
    const unitPrice = 20_000_000;
    // 服务费按号数(mock · 真实规则在后端 decider.PriceEstimate)
    const serviceFee = b.count * MICRO;
    return ok({
      unit_price: unitPrice,
      service_fee: serviceFee,
      total: unitPrice * b.count,
    }, 80);
  }),
  http.post("/api/me/pull", () => ok({ round_id: "rd_new", status: "initiated" }, 800)),

  // ── 上游即时快照 + 我方历史（PullExtractModal）· docs/14 §4.3
  //    单价按身份返回**最终价**（已含所有分项）· 绝不下发原价 · decisions §8.20
  http.get("/api/vendors/:vendor_id/stock", ({ params, request }) => {
    const s = fx.vendorStocks[params.vendor_id as string];
    if (!s) return HttpResponse.json({ error: "not_found" }, { status: 404 });
    const waived = isWaived(request);
    return ok({
      ...s,
      zones: s.zones.map((z) => ({ ...z, unit_price: fx.finalPrice(z.unit_price, waived) })),
    });
  }),

  // ── 系统派号推荐（auto 模式 · 散客默认）· decisions §8.20
  http.get("/api/vendors/auto-pick", ({ request }) => {
    const u = new URL(request.url);
    const zone = (u.searchParams.get("zone") ?? "auto") as "us" | "eu" | "auto";
    const waived = isWaived(request);
    const pick = fx.autoPick(zone, waived);
    return ok({
      ...pick,
      /* 显示名按身份 · 有注册码看真名 · 散客看 Vendor 0N */
      vendor_label: vendorLabel(pick.vendor_id, fx.passenger.tier),
    });
  }),
  // ── vendor 价格走势（Prices 页多线图）· decisions §8.22
  //    单价按身份返回**最终价**（已含所有分项）· 显示名按身份匿名化
  http.get("/api/vendors/prices", ({ request }) => {
    const u = new URL(request.url);
    const days = Number(u.searchParams.get("days") ?? "30");
    const zone = (u.searchParams.get("zone") ?? "auto") as "us" | "eu" | "auto";
    const waived = isWaived(request);
    const trends = Object.keys(fx.vendorStocks).map((id) => {
      const t = fx.vendorPriceTrend(id, days, waived, zone);
      return { ...t, vendor_label: vendorLabel(id, fx.passenger.tier) };
    });
    return ok({ trends });
  }),
  http.get("/api/vendors/:vendor_id/history", ({ params }) => {
    const h = fx.vendorHistories[params.vendor_id as string];
    return h ? ok(h) : HttpResponse.json({ error: "not_found" }, { status: 404 });
  }),

  // ── 钱包 · 充值 / 兑换
  /* 通道费 5% pass-through（decisions §2.13 §8.21）· 只在充值这一步收
     付 200 元 → 通道费 10 → 到账 190 积分 */
  http.post("/api/me/topup", async ({ request }) => {
    const { paid } = (await request.json()) as { paid: number };
    const { credits } = topupBreakdown(paid);
    return ok({
      order_id: `to_${Date.now()}`,
      // 真实环境是 gateway.instructions.checkout_url · mock 拿个假的
      checkout_url: `https://pay.waffo.example/checkout/${Date.now()}?amount=${paid / 1_000_000}`,
      paid, credits,
      expires_at: new Date(Date.now() + 15 * 60_000).toISOString(),
      status: "pending" as const,
    }, 600);
  }),
  http.post("/api/me/redeem", async ({ request }) => {
    const { code } = (await request.json()) as { code: string };
    const c = (code ?? "").trim().toUpperCase();
    if (!c) return HttpResponse.json({ error: "invalid_code", message: "请输入兑换码" }, { status: 400 });
    // mock：以 BAD 开头的码当作无效，其他都给 50 积分 —— 让失败态也能演示
    if (c.startsWith("BAD")) {
      await delay(400);
      return HttpResponse.json({ error: "invalid_code", message: "兑换码无效或已被使用" }, { status: 400 });
    }
    const credits = 50 * 1_000_000;
    fx.wallet.balance += credits;
    fx.ledger.unshift({
      id: `l_${Date.now()}`, type: "redeem", amount: credits,
      balance_after: fx.wallet.balance, memo: `兑换码 ${c}`,
      created_at: new Date().toISOString(),
    });
    return ok({ credits, memo: `兑换码 ${c}` }, 500);
  }),

  // ── 全局策略（06-db-schema §16 · 提取 key 的限额就走这里）
  //    1f-refactor(migration 040) · default_* 三字段 = 建车 seed(不做运行时 fallback) ·
  //    auto_refill_* 三字段 = 跨车调度护栏(daily_budget / min_wallet_reserve / vendor_allowlist)
  http.get("/api/me/strategy", () => ok(fx.globalStrategy)),
  http.put("/api/me/strategy", async ({ request }) => {
    const body = (await request.json()) as Record<string, unknown>;
    for (const k of [
      "daily_round_limit", "daily_spend_limit", "per_round_count",
      "max_unit_price", "preferred_vendor", "default_zone",
      "default_auto_refill_enabled", "default_refill_watermark", "default_refill_min_count",
      "auto_refill_daily_budget", "auto_refill_min_wallet_reserve", "auto_refill_vendor_allowlist",
    ] as const) {
      if (k in body) (fx.globalStrategy as any)[k] = body[k];
    }
    return ok(fx.globalStrategy, 300);
  }),

  // ── 配置
  http.get("/api/me/downstream", () => ok(fx.downstream)),
  http.put("/api/me/downstream/passengerpool", async ({ request }) => {
    const body = (await request.json()) as any;
    if (body.passengerpool_url != null) fx.downstream.passengerpool_url = body.passengerpool_url;
    if (body.token) {
      fx.downstream.passengerpool_token_masked =
        `kiro_admin_${"•".repeat(16)}${body.token.slice(-4)}`;
    }
    if (body.rules) fx.downstream.rules = { ...fx.downstream.rules, ...body.rules };
    return ok({ ok: true }, 300);
  }),
  http.post("/api/me/downstream/passengerpool/test", () => ok({ ok: true, latency_ms: 87 }, 700)),
  http.get("/api/me/downstream/webhook", () => ok(fx.webhook)),
  http.put("/api/me/downstream/webhook", async ({ request }) => {
    const body = (await request.json()) as any;
    if (body.url != null) fx.webhook.url = body.url;
    if (body.enabled != null) fx.webhook.enabled = body.enabled;
    if (body.events) fx.webhook.events = body.events;
    return ok({ ok: true }, 300);
  }),
  http.post("/api/me/downstream/webhook/test", () => {
    fx.webhookDeliveries.unshift({
      id: `w_${Date.now()}`, event: "test", ok: true,
      status_code: 200, attempt: 1, latency_ms: 132,
      created_at: new Date().toISOString(),
    });
    return ok({ ok: true, status_code: 200, latency_ms: 132 }, 700);
  }),
  http.post("/api/me/downstream/webhook/secret", () => {
    const tail = crypto.randomUUID().replace(/-/g, "").slice(0, 4);
    // 打码格式跟后端 maskFromEncrypted 对齐:whsec_(前缀) + 16 个 • + 尾 4 hex
    fx.webhook.secret_masked = `whsec_${"•".repeat(16)}${tail}`;
    // 明文格式 = whsec_ + 64 hex(跟后端 downstream.generateSecretHex 一致)
    return ok({ secret: `whsec_${crypto.randomUUID().replace(/-/g, "")}${crypto.randomUUID().replace(/-/g, "")}` }, 400);
  }),
  http.get("/api/me/downstream/webhook/deliveries", () => ok(fx.webhookDeliveries)),

  // ── 我的邀请(好友邀请码 · 三码分离见 CLAUDE §1.2)
  //    /wallet · /me · /invite 三页都用 useMyInvite · 缺这条会 401 踢回登录
  http.get("/api/me/invite", () => ok(fx.myInvite)),

  // ── API key
  http.get("/api/me/api-keys", () => ok(fx.apiKeys)),
  http.post("/api/me/api-keys", async ({ request }) => {
    const { name } = (await request.json()) as { name: string };
    const raw = crypto.randomUUID().replace(/-/g, "");
    const id = `k_${Date.now()}`;
    const item = {
      id, name: name || "未命名", prefix: `usr-${raw.slice(0, 8)}`,
      last_used_at: null, created_at: new Date().toISOString(), revoked: false,
    };
    fx.apiKeys.unshift(item);
    return ok({ key: `usr-${raw}`, item }, 400);
  }),
  http.delete("/api/me/api-keys/:id", ({ params }) => {
    const k = fx.apiKeys.find((x) => x.id === params.id);
    if (k) k.revoked = true;   // 吊销不删行 —— 台账留痕（§8.24 同理）
    return ok({ ok: true }, 300);
  }),

  // ── 账号
  /* 阶段 1a 前端表单先做 · 后端 1b 才支持（spec 说 501）· mock 给通，好走完流程 */
  http.post("/api/me/password", async ({ request }) => {
    const { old_password } = (await request.json()) as { old_password: string };
    if (old_password !== "1234") {
      await delay(400);
      return HttpResponse.json({ error: "wrong_password", message: "旧密码不对" }, { status: 400 });
    }
    return ok({ ok: true }, 500);
  }),

  // ── auth · mock：密码 1234 通过（spec §1）
  http.post("/api/login", async ({ request }) => {
    const { password } = (await request.json()) as { password: string };
    if (password !== "1234") {
      await delay(500);
      return HttpResponse.json({ error: "bad_credentials", message: "账号或密码不对" }, { status: 401 });
    }
    return ok({ ok: true }, 500);
  }),
  http.post("/api/register", async ({ request }) => {
    const body = (await request.json()) as { invite_code?: string };
    // 填码 → tier 升到批发商（mock 简化 · 后端按 grants_tier 分社群/批发商 · docs/10-pricing §2.1）
    const hasCode = !!body.invite_code?.trim();
    fx.passenger.tier = hasCode ? "wholesale" : "retail";
    fx.passenger.invited = hasCode; // 兼容字段（下版删）
    return ok({ ok: true }, 500);
  }),
  http.post("/api/logout", () => ok({ ok: true }, 200)),
];
