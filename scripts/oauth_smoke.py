#!/usr/bin/env python3
"""
oauth_smoke.py — End-to-end OAuth 2.1 PKCE smoke test for msg2agent.

Usage:
    # Against production (requires MSG2AGENT_TEST_TENANT_ID set in .env for headless flow):
    python3 scripts/oauth_smoke.py --base-url https://msg2agent.home.gianlucamazza.it

    # Against local dev relay:
    python3 scripts/oauth_smoke.py --base-url http://localhost:8080 --tenant-id <id>

The script performs the full PKCE round-trip:
  1. POST /oauth/register   → client_id
  2. Generate code_verifier + code_challenge (S256)
  3. POST /oauth/authorize/confirm (headless, using MSG2AGENT_TEST_TENANT_ID to skip Google)
  4. POST /oauth/token      → access_token + refresh_token
  5. POST /mcp tools/list   → assert 14 tools
  6. POST /oauth/token (refresh_token grant) → new access_token
  7. POST /mcp tools/list   → assert still works after refresh
  8. POST /mcp tools/list with API key (sk_live_) → assert backwards compat

Note: step 3 requires the relay to be running with MSG2AGENT_OAUTH_TEST_TENANT_ID set.
This env var is ONLY read when MSG2AGENT_OAUTH_AS_BASE_URL is set, and it enables a
headless consent bypass for testing. Never set it in production.
"""

import argparse
import base64
import hashlib
import json
import os
import secrets
import sys
import urllib.error
import urllib.parse
import urllib.request

BASE_URL = "https://msg2agent.home.gianlucamazza.it"
MCP_URL = BASE_URL + "/mcp"


def b64url(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode()


def pkce_pair() -> tuple[str, str]:
    verifier = b64url(secrets.token_bytes(32))
    challenge = b64url(hashlib.sha256(verifier.encode()).digest())
    return verifier, challenge


def http_post_json(url: str, payload: dict, headers: dict | None = None) -> dict:
    body = json.dumps(payload).encode()
    req = urllib.request.Request(url, data=body, method="POST")
    req.add_header("Content-Type", "application/json")
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    try:
        with urllib.request.urlopen(req) as r:
            return json.loads(r.read())
    except urllib.error.HTTPError as e:
        body = e.read().decode()
        print(f"  ERROR {e.code}: {body}", file=sys.stderr)
        raise


def http_post_form(url: str, fields: dict, headers: dict | None = None) -> dict:
    body = urllib.parse.urlencode(fields).encode()
    req = urllib.request.Request(url, data=body, method="POST")
    req.add_header("Content-Type", "application/x-www-form-urlencoded")
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    try:
        with urllib.request.urlopen(req) as r:
            return json.loads(r.read())
    except urllib.error.HTTPError as e:
        body = e.read().decode()
        print(f"  ERROR {e.code}: {body}", file=sys.stderr)
        raise


def mcp_tools_list(base_url: str, token: str) -> list:
    payload = {"jsonrpc": "2.0", "id": 1, "method": "tools/list"}
    result = http_post_json(
        base_url + "/mcp",
        payload,
        headers={
            "Authorization": f"Bearer {token}",
            "Accept": "application/json, text/event-stream",
        },
    )
    return result.get("result", {}).get("tools", [])


def step(n: int, desc: str):
    print(f"\n[{n}] {desc}")


def ok(msg: str):
    print(f"    ✓ {msg}")


def fail(msg: str):
    print(f"    ✗ {msg}", file=sys.stderr)
    sys.exit(1)


def main():
    parser = argparse.ArgumentParser(description="msg2agent OAuth 2.1 smoke test")
    parser.add_argument(
        "--base-url", default=os.environ.get("MSG2AGENT_BASE_URL", BASE_URL)
    )
    parser.add_argument(
        "--tenant-id", default=os.environ.get("MSG2AGENT_TEST_TENANT_ID", "")
    )
    parser.add_argument(
        "--api-key",
        default=os.environ.get("MSG2AGENT_API_KEY", ""),
        help="Existing API key for backwards-compat test",
    )
    args = parser.parse_args()

    base = args.base_url.rstrip("/")
    tenant_id = args.tenant_id

    print(f"smoke test: {base}")

    # ── 1. DCR ─────────────────────────────────────────────────────────────────
    step(1, "DCR — POST /oauth/register")
    reg = http_post_json(
        base + "/oauth/register",
        {
            "client_name": "smoke-test-client",
            "redirect_uris": ["http://localhost:9999/callback"],
            "grant_types": ["authorization_code", "refresh_token"],
            "token_endpoint_auth_method": "none",
        },
    )
    client_id = reg.get("client_id", "")
    if not client_id.startswith("cli_"):
        fail(f"unexpected client_id: {client_id!r}")
    ok(f"client_id = {client_id}")

    # ── 2. PKCE pair ────────────────────────────────────────────────────────────
    step(2, "Generate PKCE pair")
    verifier, challenge = pkce_pair()
    ok(f"code_verifier={verifier[:16]}… code_challenge={challenge[:16]}…")

    # ── 3. Headless consent (requires MSG2AGENT_TEST_TENANT_ID on server) ───────
    step(3, "POST /oauth/authorize/confirm (headless)")
    if not tenant_id:
        print(
            "    SKIP — MSG2AGENT_TEST_TENANT_ID not set; cannot perform headless consent."
        )
        print("           Set --tenant-id or MSG2AGENT_TEST_TENANT_ID and re-run.")
        code = None
    else:
        # The relay honours a test bypass when MSG2AGENT_OAUTH_TEST_TENANT_ID matches.
        confirm = http_post_form(
            base + "/oauth/authorize/confirm",
            {
                "action": "allow",
                "session": f"__test__{tenant_id}",  # relay decodes test sessions when env is set
                "client_id": client_id,
                "redirect_uri": "http://localhost:9999/callback",
                "code_challenge": challenge,
                "code_challenge_method": "S256",
                "scope": "mcp:tools:read mcp:tools:write",
                "state": "smoke",
            },
        )
        code = confirm.get("code")
        if not code:
            fail(f"no code in response: {confirm}")
        ok(f"code = {code[:16]}…")

    if code:
        # ── 4. Token exchange ─────────────────────────────────────────────────
        step(4, "POST /oauth/token (authorization_code)")
        tok = http_post_form(
            base + "/oauth/token",
            {
                "grant_type": "authorization_code",
                "client_id": client_id,
                "redirect_uri": "http://localhost:9999/callback",
                "code": code,
                "code_verifier": verifier,
            },
        )
        access_token = tok.get("access_token", "")
        refresh_token = tok.get("refresh_token", "")
        if not access_token.startswith("eyJ"):
            fail(f"access_token does not look like a JWT: {access_token[:40]!r}")
        ok(f"access_token={access_token[:24]}… refresh_token={refresh_token[:16]}…")

        # ── 5. tools/list with JWT ────────────────────────────────────────────
        step(5, "MCP tools/list with JWT access token")
        tools = mcp_tools_list(base, access_token)
        if len(tools) != 14:
            fail(f"expected 14 tools, got {len(tools)}: {[t['name'] for t in tools]}")
        ok(f"{len(tools)} tools returned")

        # ── 6. Refresh ────────────────────────────────────────────────────────
        step(6, "POST /oauth/token (refresh_token)")
        tok2 = http_post_form(
            base + "/oauth/token",
            {
                "grant_type": "refresh_token",
                "client_id": client_id,
                "refresh_token": refresh_token,
            },
        )
        access_token2 = tok2.get("access_token", "")
        refresh_token2 = tok2.get("refresh_token", "")
        if not access_token2.startswith("eyJ"):
            fail(f"refreshed access_token invalid: {access_token2[:40]!r}")
        if refresh_token2 == refresh_token:
            fail("refresh token was not rotated")
        ok(f"new access_token={access_token2[:24]}… (rotated refresh)")

        # ── 7. tools/list with refreshed JWT ─────────────────────────────────
        step(7, "MCP tools/list with refreshed JWT")
        tools2 = mcp_tools_list(base, access_token2)
        if len(tools2) != 14:
            fail(f"expected 14 tools after refresh, got {len(tools2)}")
        ok(f"{len(tools2)} tools returned")

        # Old refresh must be revoked (rotation).
        step(7.1, "Old refresh token must be revoked")
        try:
            http_post_form(
                base + "/oauth/token",
                {
                    "grant_type": "refresh_token",
                    "client_id": client_id,
                    "refresh_token": refresh_token,
                },
            )
            fail("old refresh token was still accepted after rotation")
        except urllib.error.HTTPError as e:
            if e.code == 400:
                ok("old refresh token correctly rejected")
            else:
                raise
    else:
        print("\n    Skipping steps 4-7 (no authorization code).")

    # ── 8. API key backwards compat ───────────────────────────────────────────
    step(8, "MCP tools/list with API key (backwards compat)")
    api_key = args.api_key
    if not api_key:
        print("    SKIP — --api-key / MSG2AGENT_API_KEY not set.")
    else:
        tools_ak = mcp_tools_list(base, api_key)
        if len(tools_ak) != 14:
            fail(f"expected 14 tools via API key, got {len(tools_ak)}")
        ok(f"{len(tools_ak)} tools returned via API key")

    # ── AS metadata ───────────────────────────────────────────────────────────
    step(9, "AS metadata + JWKS sanity")
    req = urllib.request.urlopen(base + "/.well-known/oauth-authorization-server")
    meta = json.loads(req.read())
    if "S256" not in meta.get("code_challenge_methods_supported", []):
        fail("S256 missing from code_challenge_methods_supported")
    ok(f"issuer={meta.get('issuer')}")

    req2 = urllib.request.urlopen(base + "/.well-known/jwks.json")
    jwks = json.loads(req2.read())
    keys = jwks.get("keys", [])
    if not keys or keys[0].get("kty") != "OKP" or keys[0].get("crv") != "Ed25519":
        fail(f"unexpected JWKS: {jwks}")
    ok(f"JWKS: kty=OKP crv=Ed25519 kid={keys[0].get('kid')}")

    print("\n✓ smoke test passed\n")


if __name__ == "__main__":
    main()
