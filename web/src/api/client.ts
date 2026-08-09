export class ApiError extends Error {
  status: number;
  code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
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
