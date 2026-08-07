import { http, HttpResponse, delay } from "msw";
import * as fx from "./fixtures";

const ok = async (data: any, ms = 120) => {
  await delay(ms);
  return HttpResponse.json(data);
};

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
    const days = range === "today" ? 1 : range === "7d" ? 7 : range === "90d" ? 90 : 30;
    return ok(fx.trend(metric, days));
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
  http.put("/api/me/buses/:id/strategy", () => ok({ ok: true }, 300)),
  http.post("/api/me/buses/:id/pull", () => ok({ round_id: "rd_new", status: "initiated" }, 600)),
  http.delete("/api/me/buses/:id", () => ok({ ok: true }, 400)),

  // ── 提取 key
  http.get("/api/me/extract/records", () => ok({ items: fx.extractRecords, total: fx.extractRecords.length, page: 1, page_size: 20 })),
  http.post("/api/me/extract/estimate", async ({ request }) => {
    const b = (await request.json()) as { count: number };
    const unit = 20_000_000;
    const keyCost = unit * b.count;
    const single = b.count === 1 ? keyCost * 0.2 : 0;
    const service = 1_000_000;
    return ok({ key_cost: keyCost, single_pull_fee: single, service_fee: service, total: keyCost + single + service }, 80);
  }),
  http.post("/api/me/extract", () => ok({ round_id: "rd_new", status: "initiated" }, 800)),

  // ── 配置
  http.get("/api/me/downstream", () => ok(fx.downstream)),
  http.put("/api/me/downstream/passengerpool", () => ok({ ok: true }, 300)),
  http.post("/api/me/downstream/passengerpool/test", () => ok({ ok: true, latency_ms: 87 }, 700)),
  http.get("/api/me/downstream/webhook", () => ok(fx.webhook)),
  http.put("/api/me/downstream/webhook", () => ok({ ok: true }, 300)),
  http.post("/api/me/downstream/webhook/test", () => ok({ ok: true, status_code: 200, latency_ms: 132 }, 700)),
  http.get("/api/me/downstream/webhook/deliveries", () => ok(fx.webhookDeliveries)),

  // ── API key
  http.get("/api/me/api-keys", () => ok(fx.apiKeys)),
  http.post("/api/me/api-keys", () => ok({ id: "k_new", plaintext: "sk_live_" + crypto.randomUUID().replace(/-/g, "") }, 400)),
  http.delete("/api/me/api-keys/:id", () => ok({ ok: true }, 300)),

  // ── auth
  http.post("/api/login", () => ok({ ok: true }, 500)),
  http.post("/api/register", () => ok({ ok: true }, 500)),
  http.post("/api/logout", () => ok({ ok: true }, 200)),
];
