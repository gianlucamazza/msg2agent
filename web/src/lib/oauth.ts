import * as t from "./tokens.js";

const AS = location.origin;
const REDIRECT_URI = location.origin + "/app/";

function b64url(buf: ArrayBuffer): string {
  return btoa(String.fromCharCode(...new Uint8Array(buf)))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

function random(len = 32): string {
  const buf = new Uint8Array(len);
  crypto.getRandomValues(buf);
  return b64url(buf.buffer);
}

async function sha256(s: string): Promise<ArrayBuffer> {
  return crypto.subtle.digest("SHA-256", new TextEncoder().encode(s));
}

async function ensureClient(): Promise<string> {
  const id = t.clientId.get();
  if (id) return id;
  return registerClient();
}

async function registerClient(): Promise<string> {
  const r = await fetch(AS + "/oauth/register", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      client_name: "msg2agent dashboard",
      redirect_uris: [REDIRECT_URI],
      grant_types: ["authorization_code", "refresh_token"],
      token_endpoint_auth_method: "none",
    }),
  });
  if (!r.ok) {
    const err = await r.json().catch(() => ({}));
    throw new Error(
      "client registration failed: " + (err.error_description || r.status),
    );
  }
  const data = await r.json();
  t.clientId.set(data.client_id);
  return data.client_id;
}

export async function signIn(): Promise<void> {
  const cid = await ensureClient();
  const verifier = random(32);
  const challenge = b64url(await sha256(verifier));
  const state = random(16);
  t.pkceVerifier.set(verifier);
  t.oauthState.set(state);
  const params = new URLSearchParams({
    response_type: "code",
    client_id: cid,
    redirect_uri: REDIRECT_URI,
    code_challenge: challenge,
    code_challenge_method: "S256",
    state,
  });
  location.href = AS + "/oauth/authorize?" + params.toString();
}

export async function handleCallback(): Promise<boolean> {
  const url = new URL(location.href);
  const errCode = url.searchParams.get("error");
  if (errCode) {
    t.pkceVerifier.set(null);
    t.oauthState.set(null);
    history.replaceState(null, "", REDIRECT_URI);
    throw new Error(url.searchParams.get("error_description") || errCode);
  }
  const code = url.searchParams.get("code");
  const state = url.searchParams.get("state");
  if (!code || !state) return false;
  if (state !== t.oauthState.get()) {
    t.pkceVerifier.set(null);
    t.oauthState.set(null);
    throw new Error("OAuth state mismatch");
  }
  const verifier = t.pkceVerifier.get();
  const cid = t.clientId.get();
  if (!verifier || !cid) throw new Error("OAuth session lost; sign in again");

  const body = new URLSearchParams({
    grant_type: "authorization_code",
    code,
    redirect_uri: REDIRECT_URI,
    client_id: cid,
    code_verifier: verifier,
  });
  const r = await fetch(AS + "/oauth/token", {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body,
  });
  t.pkceVerifier.set(null);
  t.oauthState.set(null);
  if (!r.ok) {
    const err = await r.json().catch(() => ({}));
    history.replaceState(null, "", REDIRECT_URI);
    if (err.error === "invalid_client") t.clientId.set(null);
    throw new Error(
      "token exchange failed: " + (err.error_description || r.status),
    );
  }
  const data = await r.json();
  t.accessToken.set(data.access_token);
  if (data.refresh_token) t.refreshToken.set(data.refresh_token);
  history.replaceState(null, "", REDIRECT_URI);
  return true;
}

export async function tryRefresh(): Promise<boolean> {
  const refresh = t.refreshToken.get();
  const cid = t.clientId.get();
  if (!refresh || !cid) return false;
  const body = new URLSearchParams({
    grant_type: "refresh_token",
    refresh_token: refresh,
    client_id: cid,
  });
  const r = await fetch(AS + "/oauth/token", {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body,
  });
  if (!r.ok) {
    const err = await r.json().catch(() => ({}));
    if (err.error === "invalid_client") t.clientId.set(null);
    t.accessToken.set(null);
    t.refreshToken.set(null);
    return false;
  }
  const data = await r.json();
  t.accessToken.set(data.access_token);
  if (data.refresh_token) t.refreshToken.set(data.refresh_token);
  return true;
}

export function signOut(): void {
  const refresh = t.refreshToken.get();
  const cid = t.clientId.get();
  if (refresh && cid) {
    fetch(AS + "/oauth/revoke", {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: new URLSearchParams({
        token: refresh,
        token_type_hint: "refresh_token",
        client_id: cid,
      }),
    }).catch(() => {});
  }
  t.accessToken.set(null);
  t.refreshToken.set(null);
  location.href = REDIRECT_URI;
}
