// msg2agent Dashboard — vanilla JS, no build step
//
// OAuth 2.1 PKCE client (public, token_endpoint_auth_method=none) against the
// msg2agent relay AS at the same origin. Dynamic client registration on first
// load; client_id persisted in localStorage. Access/refresh tokens kept in
// sessionStorage so a closed tab requires a fresh sign-in.

const OAuth = {
  AS: location.origin,
  REDIRECT_URI: location.origin + '/app/',

  get token()    { return sessionStorage.getItem('m2a_access_token'); },
  set token(v)   { v ? sessionStorage.setItem('m2a_access_token', v) : sessionStorage.removeItem('m2a_access_token'); },
  get refresh()  { return sessionStorage.getItem('m2a_refresh_token'); },
  set refresh(v) { v ? sessionStorage.setItem('m2a_refresh_token', v) : sessionStorage.removeItem('m2a_refresh_token'); },
  get clientId() { return localStorage.getItem('m2a_client_id'); },
  set clientId(v){ v ? localStorage.setItem('m2a_client_id', v) : localStorage.removeItem('m2a_client_id'); },
  get verifier() { return sessionStorage.getItem('m2a_pkce_verifier'); },
  set verifier(v){ v ? sessionStorage.setItem('m2a_pkce_verifier', v) : sessionStorage.removeItem('m2a_pkce_verifier'); },
  get state()    { return sessionStorage.getItem('m2a_oauth_state'); },
  set state(v)   { v ? sessionStorage.setItem('m2a_oauth_state', v) : sessionStorage.removeItem('m2a_oauth_state'); },

  b64url(buf) {
    return btoa(String.fromCharCode(...new Uint8Array(buf)))
      .replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  },
  random(len = 32) {
    const buf = new Uint8Array(len);
    crypto.getRandomValues(buf);
    return this.b64url(buf);
  },
  async sha256(s) {
    return new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(s)));
  },

  async ensureClient() {
    if (this.clientId) return this.clientId;
    const r = await fetch(this.AS + '/oauth/register', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        client_name: 'msg2agent dashboard',
        redirect_uris: [this.REDIRECT_URI],
        grant_types: ['authorization_code', 'refresh_token'],
        token_endpoint_auth_method: 'none',
      }),
    });
    if (!r.ok) {
      const err = await r.json().catch(() => ({}));
      throw new Error('client registration failed: ' + (err.error_description || r.status));
    }
    const data = await r.json();
    this.clientId = data.client_id;
    return data.client_id;
  },

  async signIn() {
    const clientId = await this.ensureClient();
    const verifier = this.random(32);
    const challenge = this.b64url(await this.sha256(verifier));
    const state = this.random(16);
    this.verifier = verifier;
    this.state = state;
    const params = new URLSearchParams({
      response_type: 'code',
      client_id: clientId,
      redirect_uri: this.REDIRECT_URI,
      code_challenge: challenge,
      code_challenge_method: 'S256',
      state,
    });
    location.href = this.AS + '/oauth/authorize?' + params.toString();
  },

  async handleCallback() {
    const url = new URL(location.href);
    const errCode = url.searchParams.get('error');
    if (errCode) {
      this.verifier = null; this.state = null;
      history.replaceState(null, '', this.REDIRECT_URI);
      throw new Error(url.searchParams.get('error_description') || errCode);
    }
    const code = url.searchParams.get('code');
    const state = url.searchParams.get('state');
    if (!code || !state) return false;
    if (state !== this.state) {
      this.verifier = null; this.state = null;
      throw new Error('OAuth state mismatch');
    }
    const verifier = this.verifier;
    const clientId = this.clientId;
    if (!verifier || !clientId) throw new Error('OAuth session lost; sign in again');

    const body = new URLSearchParams({
      grant_type: 'authorization_code',
      code,
      redirect_uri: this.REDIRECT_URI,
      client_id: clientId,
      code_verifier: verifier,
    });
    const r = await fetch(this.AS + '/oauth/token', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body,
    });
    this.verifier = null; this.state = null;
    if (!r.ok) {
      const err = await r.json().catch(() => ({}));
      history.replaceState(null, '', this.REDIRECT_URI);
      throw new Error('token exchange failed: ' + (err.error_description || r.status));
    }
    const data = await r.json();
    this.token = data.access_token;
    if (data.refresh_token) this.refresh = data.refresh_token;
    history.replaceState(null, '', this.REDIRECT_URI);
    return true;
  },

  async tryRefresh() {
    if (!this.refresh || !this.clientId) return false;
    const body = new URLSearchParams({
      grant_type: 'refresh_token',
      refresh_token: this.refresh,
      client_id: this.clientId,
    });
    const r = await fetch(this.AS + '/oauth/token', {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body,
    });
    if (!r.ok) {
      this.token = null; this.refresh = null;
      return false;
    }
    const data = await r.json();
    this.token = data.access_token;
    if (data.refresh_token) this.refresh = data.refresh_token;
    return true;
  },

  signOut() {
    this.token = null;
    this.refresh = null;
    location.href = this.REDIRECT_URI;
  },
};

async function api(path, opts = {}) {
  const headers = { 'Content-Type': 'application/json', ...(opts.headers || {}) };
  if (OAuth.token) headers.Authorization = 'Bearer ' + OAuth.token;
  let r = await fetch(path, { ...opts, headers });
  if (r.status === 401 && await OAuth.tryRefresh()) {
    headers.Authorization = 'Bearer ' + OAuth.token;
    r = await fetch(path, { ...opts, headers });
  }
  if (!r.ok) {
    const err = await r.json().catch(() => ({ error: r.statusText }));
    return Promise.reject(err);
  }
  return r.status === 204 ? null : r.json();
}

function showKey(key) {
  const banner = document.createElement('div');
  banner.className = 'key-reveal';
  banner.innerHTML = `<strong>Copy your key — it will not be shown again:</strong><br><code>${key}</code>
    <button id="dismiss-key">Dismiss</button>`;
  document.getElementById('section-keys').prepend(banner);
  banner.querySelector('#dismiss-key').addEventListener('click', () => banner.remove());
}

function renderKeys(payload) {
  const keys = Array.isArray(payload) ? payload : (payload.items || []);
  const el = document.getElementById('keys-list');
  if (!keys.length) { el.innerHTML = '<p>No API keys yet.</p>'; return; }
  el.innerHTML = `<table><thead><tr>
    <th>Label</th><th>Prefix</th><th>Created</th><th>Status</th><th></th>
  </tr></thead><tbody>${keys.map(k => `<tr>
    <td>${k.label}</td>
    <td><code>${k.key_prefix}…</code></td>
    <td>${new Date(k.created_at).toLocaleDateString()}</td>
    <td>${k.revoked_at ? 'Revoked' : 'Active'}</td>
    <td>${k.revoked_at ? '' : `<button class="danger" data-id="${k.id}">Revoke</button>`}</td>
  </tr>`).join('')}</tbody></table>`;
  el.querySelectorAll('.danger[data-id]').forEach(btn => {
    btn.addEventListener('click', () => {
      if (!confirm('Revoke this key?')) return;
      api(`/api/dashboard/keys/${btn.dataset.id}`, { method: 'DELETE' })
        .then(() => loadKeys()).catch(e => alert('Revoke failed: ' + JSON.stringify(e)));
    });
  });
}

function renderUsage(payload) {
  const rows = Array.isArray(payload) ? payload : (payload.items || []);
  const el = document.getElementById('usage-chart');
  if (!rows.length) { el.innerHTML = '<p>No usage data yet.</p>'; return; }
  el.innerHTML = `<table><thead><tr><th>Period</th><th>Event</th><th>Count</th></tr></thead>
    <tbody>${rows.map(r => `<tr><td>${r.period}</td><td>${r.event}</td><td>${r.count.toLocaleString()}</td></tr>`).join('')}</tbody></table>`;
}

function loadKeys() {
  return api('/api/dashboard/keys').then(renderKeys).catch(() => {
    document.getElementById('keys-list').textContent = 'Failed to load keys.';
  });
}

function showAuthGate(message) {
  document.getElementById('auth-gate').hidden = false;
  document.getElementById('main-content').style.display = 'none';
  document.getElementById('btn-signout').hidden = true;
  if (message) {
    const el = document.getElementById('auth-error');
    el.textContent = message;
    el.hidden = false;
  }
}

async function init() {
  document.getElementById('btn-signin').addEventListener('click', () => {
    OAuth.signIn().catch(e => showAuthGate(e.message));
  });
  document.getElementById('btn-signout').addEventListener('click', () => OAuth.signOut());

  // Step 1: handle OAuth callback if URL carries ?code=&state=
  try {
    await OAuth.handleCallback();
  } catch (e) {
    showAuthGate(e.message);
    return;
  }

  // Step 2: load /me with the token (if any)
  const me = await api('/api/dashboard/me').catch(() => null);
  if (!me) {
    showAuthGate();
    return;
  }

  document.getElementById('btn-signout').hidden = false;
  document.getElementById('nav-plan').textContent = me.plan;
  document.getElementById('account-info').innerHTML =
    `<p><strong>${me.name}</strong> &lt;${me.email}&gt;</p>
     <p>Plan: <strong>${me.plan}</strong> &nbsp; Billing: ${me.billing_status}</p>
     <p>Messages/mo: ${me.quota.max_messages_per_month.toLocaleString()} &nbsp;
        Tool calls/mo: ${me.quota.max_tool_calls_per_month.toLocaleString()}</p>`;

  loadKeys();

  api('/api/dashboard/usage').then(renderUsage).catch(() => {
    document.getElementById('usage-chart').textContent = 'Failed to load usage.';
  });

  document.getElementById('btn-create-key').addEventListener('click', () => {
    const label = prompt('Key label:', 'My Key');
    if (!label) return;
    api('/api/dashboard/keys', { method: 'POST', body: JSON.stringify({ label }) })
      .then(res => { showKey(res.key); return loadKeys(); })
      .catch(e => alert('Failed to create key: ' + JSON.stringify(e)));
  });

  const checkout = (plan) => {
    api('/api/dashboard/checkout', {
      method: 'POST',
      body: JSON.stringify({ plan, success_url: location.href, cancel_url: location.href })
    }).then(res => { if (res.url) location.href = res.url; })
      .catch(e => alert('Checkout failed: ' + JSON.stringify(e)));
  };
  document.getElementById('btn-upgrade-starter').addEventListener('click', () => checkout('starter'));
  document.getElementById('btn-upgrade-team').addEventListener('click', () => checkout('team'));

  document.getElementById('btn-portal').addEventListener('click', () => {
    api('/api/dashboard/portal', {
      method: 'POST',
      body: JSON.stringify({ return_url: location.href })
    }).then(res => { if (res.url) location.href = res.url; })
      .catch(e => alert('Portal failed: ' + JSON.stringify(e)));
  });
}

document.addEventListener('DOMContentLoaded', init);
