import { accessToken } from "./tokens.js";
import { tryRefresh } from "./oauth.js";

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export interface MeResponse {
  id: string;
  name: string;
  email: string;
  email_verified: boolean;
  plan: string;
  billing_status: string;
  current_period_end?: string;
  created_at?: string;
  did?: string;
  signing_public_key?: string;
  encryption_public_key?: string;
  quota: {
    max_messages_per_month: number;
    max_tool_calls_per_month: number;
    max_api_keys: number;
    max_dids: number;
  };
}

export interface ApiKey {
  id: string;
  label: string;
  key_prefix: string;
  created_at: string;
  last_used?: string;
  revoked_at?: string;
}

export interface Paginated<T> {
  items: T[];
  total: number;
}

export interface UsageRow {
  period: string;
  event: string;
  count: number;
}

export interface OAuthClientSummary {
  client_id: string;
  client_name: string;
  created_at: string;
}

export interface ToolUsageRow {
  tool_name: string;
  count: number;
}

function jwtExp(token: string): number | null {
  try {
    const payload = JSON.parse(
      atob(token.split(".")[1].replace(/-/g, "+").replace(/_/g, "/")),
    );
    return typeof payload.exp === "number" ? payload.exp * 1000 : null;
  } catch {
    return null;
  }
}

export async function api<T = unknown>(
  path: string,
  opts: RequestInit = {},
): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(opts.headers as Record<string, string> | undefined),
  };
  let tok = accessToken.get();
  // proactive refresh when token expires within 60 s
  if (tok) {
    const exp = jwtExp(tok);
    if (exp !== null && exp - Date.now() < 60_000) {
      if (await tryRefresh()) tok = accessToken.get();
    }
  }
  if (tok) headers["Authorization"] = "Bearer " + tok;

  let r = await fetch(path, { ...opts, headers });
  if (r.status === 401 && (await tryRefresh())) {
    const newTok = accessToken.get();
    if (newTok) headers["Authorization"] = "Bearer " + newTok;
    r = await fetch(path, { ...opts, headers });
  }
  if (!r.ok) {
    const body = await r.json().catch(() => ({ error: r.statusText }));
    throw new ApiError(r.status, body.error ?? r.statusText);
  }
  return r.status === 204 ? (null as T) : r.json();
}

export async function fetchPublicConfig(): Promise<{ paid_enabled: boolean }> {
  for (let attempt = 0; attempt < 2; attempt++) {
    try {
      if (attempt > 0) await new Promise((res) => setTimeout(res, 1000));
      const r = await fetch("/api/public/config");
      if (r.ok) return r.json();
    } catch {
      // network error — retry once
    }
  }
  // fail-closed: hide paid CTAs if config is unreachable
  return { paid_enabled: false };
}
