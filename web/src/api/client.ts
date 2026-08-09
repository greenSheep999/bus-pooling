export class ApiError extends Error {
  status: number;
  code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

/**
 * 全局 401 处理：session 过期 / 后端重启 · 自动踢到 /login。
 * - 例外：`/api/login` 和 `/api/register` 本身返 401 是"密码错"·**不算 session 失效**·别踢
 * - 已经在 /login 或 /register 页时不重复跳
 * - 用 window.location 而不是 router.navigate · client.ts 在 React 树外·拿不到 router
 */
function redirectToLoginOnAuthLoss(path: string) {
  // /api/login / /api/register 的 401 是登录密码错误 · 让上层 form 展示
  if (path.includes("/login") || path.includes("/register")) return;
  const loc = window.location;
  if (loc.pathname === "/login" || loc.pathname === "/register") return;
  // 记录被踢前的路径 · 登录后可选跳回
  const target = loc.pathname + loc.search;
  window.location.href = `/login?next=${encodeURIComponent(target)}`;
}

export async function api<T>(
  path: string,
  init?: RequestInit & { params?: Record<string, string | number | undefined> },
): Promise<T> {
  const { params, ...rest } = init ?? {};
  let url = path.startsWith("/api") ? path : `/api${path}`;
  if (params) {
    const q = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) {
      if (v !== undefined) q.set(k, String(v));
    }
    const s = q.toString();
    if (s) url += `?${s}`;
  }

  const res = await fetch(url, {
    ...rest,
    headers: {
      "Content-Type": "application/json",
      ...(rest.headers ?? {}),
    },
    credentials: "include",
  });

  if (!res.ok) {
    let code = "unknown";
    let msg = res.statusText;
    try {
      const body = await res.json();
      // 后端错误形状 { code, message, retry_after? }（05-api-contract §错误响应）
      code = body.code ?? body.error ?? code;
      msg = body.message ?? msg;
    } catch {
      /* ignore */
    }
    // 401 = session 失效 · 全局踢到登录页（登录 / 注册页的 401 让上层 form 处理）
    if (res.status === 401) {
      redirectToLoginOnAuthLoss(url);
    }
    throw new ApiError(res.status, code, msg);
  }

  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

export const post = <T>(path: string, body?: unknown) =>
  api<T>(path, { method: "POST", body: body ? JSON.stringify(body) : undefined });

export const put = <T>(path: string, body?: unknown) =>
  api<T>(path, { method: "PUT", body: body ? JSON.stringify(body) : undefined });

export const del = <T>(path: string) => api<T>(path, { method: "DELETE" });

/** 32 位十六进制 idempotency key · 契约要求这个格式（api §幂等键） */
export function newIdempotencyKey(): string {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

/** 需要幂等键的写操作 · 拉号 / 派去向 / handoff / 建单 · 自动生成 32 位 hex key */
export const postIdempotent = <T>(path: string, body?: unknown, key?: string) =>
  api<T>(path, {
    method: "POST",
    body: body ? JSON.stringify(body) : undefined,
    headers: { "X-Idempotency-Key": key ?? newIdempotencyKey() },
  });
