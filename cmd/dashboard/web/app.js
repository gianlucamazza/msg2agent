// msg2agent Dashboard — vanilla JS, no build step
//
// OAuth 2.1 PKCE client (public, token_endpoint_auth_method=none) against the
// msg2agent relay AS at the same origin. Dynamic client registration on first
// load; client_id persisted in localStorage. Access/refresh tokens kept in
// sessionStorage so a closed tab requires a fresh sign-in.

const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));

// ── Toast notifications ──────────────────────────────────────────────────────

function toast(msg, kind = 'info') {
  const el = document.createElement('div');
  el.className = 'toast' + (kind !== 'info' ? ' ' + kind : '');
  el.textContent = msg;
  document.getElementById('toast-container').appendChild(el);
  setTimeout(() => el.remove(), 4000);
}

// ── Modal (replaces prompt / confirm) ────────────────────────────────────────

function showModal({ title, message = '', input = false, confirmLabel = 'OK' }) {
  return new Promise(resolve => {
    const dlg = document.getElementById('modal');
    document.getElementById('modal-title').textContent = title;
    document.getElementById('modal-message').textContent = message;
    document.getElementById('modal-confirm').textContent = confirmLabel;
    const inp = document.getElementById('modal-input');
    inp.style.display = input ? 'block' : 'none';
    inp.value = '';

    const cleanup = result => {
      dlg.removeEventListener('close', onClose);
      document.getElementById('modal-cancel').removeEventListener('click', onCancel);
      resolve(result);
    };
    const onClose = () => cleanup(input ? inp.value.trim() || null : true);
    const onCancel = () => { dlg.close(); cleanup(null); };

    dlg.addEventListener('close', onClose, { once: true });
    document.getElementById('modal-cancel').addEventListener('click', onCancel, { once: true });
    dlg.showModal();
    if (input) inp.focus();
  });
}

// ── Loading state for action buttons ─────────────────────────────────────────

async function withLoading(btn, fn) {
  const orig = btn.textContent;
  btn.disabled = true;
  btn.textContent = '…';
  try {
    await fn();
  } finally {
    btn.disabled = false;
    btn.textContent = orig;
  }
}

// ── OAuth 2.1 PKCE ───────────────────────────────────────────────────────────

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
    return this._registerClient();
  },

  async _registerClient() {
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
      // invalid_client means the AS has forgotten our registration; re-register next time
      if (err.error === 'invalid_client') this.clientId = null;
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
      const err = await r.json().catch(() => ({}));
      if (err.error === 'invalid_client') this.clientId = null;
      this.token = null; this.refresh = null;
      return false;
    }
    const data = await r.json();
    this.token = data.access_token;
    if (data.refresh_token) this.refresh = data.refresh_token;
    return true;
  },

  async signOut() {
    // Best-effort RFC 7009 revocation; errors are ignored
    if (this.refresh && this.clientId) {
      fetch(this.AS + '/oauth/revoke', {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: new URLSearchParams({ token: this.refresh, token_type_hint: 'refresh_token', client_id: this.clientId }),
      }).catch(() => {});
    }
    this.token = null;
    this.refresh = null;
    location.href = this.REDIRECT_URI;
  },
};

// ── API helper ───────────────────────────────────────────────────────────────

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

// ── Key reveal banner ────────────────────────────────────────────────────────

function showKey(key) {
  const banner = document.createElement('div');
  banner.className = 'key-reveal';
  banner.innerHTML = `<strong>Copy your key — it will not be shown again:</strong><br><code>${esc(key)}</code>
    <button id="dismiss-key">Dismiss</button>`;
  document.getElementById('section-keys').prepend(banner);
  banner.querySelector('#dismiss-key').addEventListener('click', () => banner.remove());
}

// ── Keys list with pagination ────────────────────────────────────────────────

let keysOffset = 0;
const KEYS_LIMIT = 20;

function renderKeys(payload) {
  const keys = Array.isArray(payload) ? payload : (payload.items || []);
  const total = payload.total ?? keys.length;
  const el = document.getElementById('keys-list');
  if (!keys.length && keysOffset === 0) {
    el.innerHTML = '<p>No API keys yet. Create one to get started.</p>';
    return;
  }
  el.innerHTML = `<div class="table-wrap"><table><thead><tr>
    <th>Label</th><th>Prefix</th><th>Created</th><th>Status</th><th></th>
  </tr></thead><tbody>${keys.map(k => `<tr>
    <td>${esc(k.label)}</td>
    <td><code>${esc(k.key_prefix)}…</code></td>
    <td>${new Date(k.created_at).toLocaleDateString()}</td>
    <td>${k.revoked_at ? 'Revoked' : 'Active'}</td>
    <td>${k.revoked_at ? '' : `<button class="danger" data-id="${esc(k.id)}" aria-label="Revoke key ${esc(k.label)}">Revoke</button>`}</td>
  </tr>`).join('')}</tbody></table></div>`;

  if (total > KEYS_LIMIT) {
    const start = keysOffset + 1;
    const end = Math.min(keysOffset + keys.length, total);
    el.innerHTML += `<div class="pager">
      <button id="keys-prev" ${keysOffset === 0 ? 'disabled' : ''}>‹ Prev</button>
      <span>${start}–${end} / ${total}</span>
      <button id="keys-next" ${end >= total ? 'disabled' : ''}>Next ›</button>
    </div>`;
    el.querySelector('#keys-prev')?.addEventListener('click', () => { keysOffset = Math.max(0, keysOffset - KEYS_LIMIT); loadKeys(); });
    el.querySelector('#keys-next')?.addEventListener('click', () => { keysOffset += KEYS_LIMIT; loadKeys(); });
  }

  el.querySelectorAll('.danger[data-id]').forEach(btn => {
    btn.addEventListener('click', async () => {
      const ok = await showModal({ title: 'Revoke key?', message: 'The key will stop working immediately.', confirmLabel: 'Revoke' });
      if (!ok) return;
      await withLoading(btn, () =>
        api(`/api/dashboard/keys/${btn.dataset.id}`, { method: 'DELETE' })
          .then(() => loadKeys())
          .catch(e => toast('Revoke failed: ' + (e.error || JSON.stringify(e)), 'error'))
      );
    });
  });
}

function loadKeys() {
  return api(`/api/dashboard/keys?limit=${KEYS_LIMIT}&offset=${keysOffset}`)
    .then(renderKeys)
    .catch(() => { document.getElementById('keys-list').textContent = 'Failed to load keys.'; });
}

// ── Usage table with pagination ───────────────────────────────────────────────

let usageOffset = 0;
const USAGE_LIMIT = 50;

function renderUsage(payload) {
  const rows = Array.isArray(payload) ? payload : (payload.items || []);
  const total = payload.total ?? rows.length;
  const el = document.getElementById('usage-chart');
  if (!rows.length && usageOffset === 0) { el.innerHTML = '<p>No usage data yet.</p>'; return; }
  el.innerHTML = `<div class="table-wrap"><table><thead><tr><th>Period</th><th>Event</th><th>Count</th></tr></thead>
    <tbody>${rows.map(r => `<tr><td>${esc(r.period)}</td><td>${esc(r.event)}</td><td>${r.count.toLocaleString()}</td></tr>`).join('')}</tbody></table></div>`;
  if (total > USAGE_LIMIT) {
    const start = usageOffset + 1;
    const end = Math.min(usageOffset + rows.length, total);
    el.innerHTML += `<div class="pager">
      <button id="usage-prev" ${usageOffset === 0 ? 'disabled' : ''}>‹ Prev</button>
      <span>${start}–${end} / ${total}</span>
      <button id="usage-next" ${end >= total ? 'disabled' : ''}>Next ›</button>
    </div>`;
    el.querySelector('#usage-prev')?.addEventListener('click', () => { usageOffset = Math.max(0, usageOffset - USAGE_LIMIT); loadUsage(); });
    el.querySelector('#usage-next')?.addEventListener('click', () => { usageOffset += USAGE_LIMIT; loadUsage(); });
  }
}

function loadUsage() {
  return api(`/api/dashboard/usage?limit=${USAGE_LIMIT}&offset=${usageOffset}`)
    .then(renderUsage)
    .catch(() => { document.getElementById('usage-chart').textContent = 'Failed to load usage.'; });
}

// ── Quota progress bars ───────────────────────────────────────────────────────

function renderQuota(quota, usagePayload) {
  const rows = Array.isArray(usagePayload) ? usagePayload : (usagePayload?.items || []);
  const now = new Date();
  const period = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}`;
  let messages = 0, toolCalls = 0;
  for (const r of rows) {
    if (r.period !== period) continue;
    if (r.event === 'messages') messages += r.count;
    if (r.event === 'tool_calls') toolCalls += r.count;
  }

  const bars = [
    { label: 'Messages', used: messages, max: quota.max_messages_per_month },
    { label: 'Tool calls', used: toolCalls, max: quota.max_tool_calls_per_month },
  ];

  const html = bars.map(b => {
    const pct = b.max > 0 ? Math.min(100, (b.used / b.max) * 100) : 0;
    const cls = pct >= 100 ? 'over' : pct >= 80 ? 'warn' : '';
    return `<div class="quota-item">
      <label><span>${esc(b.label)}/mo</span><span>${b.used.toLocaleString()} / ${b.max.toLocaleString()}</span></label>
      <div class="quota-bar"><div class="quota-bar-fill ${cls}" style="width:${pct.toFixed(1)}%"></div></div>
    </div>`;
  }).join('');
  document.getElementById('account-info').insertAdjacentHTML('beforeend', `<div class="quota-bar-wrap">${html}</div>`);
}

// ── Auth gate ─────────────────────────────────────────────────────────────────

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

// ── Main init ─────────────────────────────────────────────────────────────────

async function init() {
  document.getElementById('btn-signin').addEventListener('click', () => {
    OAuth.signIn().catch(e => showAuthGate(e.message));
  });
  document.getElementById('btn-signout').addEventListener('click', () => OAuth.signOut());

  // Handle ?checkout= return from Stripe before anything else
  const url = new URL(location.href);
  const checkoutResult = url.searchParams.get('checkout');
  if (checkoutResult) {
    history.replaceState(null, '', OAuth.REDIRECT_URI);
    if (checkoutResult === 'success') toast('Subscription updated successfully.', 'success');
    else if (checkoutResult === 'cancelled') toast('Checkout cancelled.', 'info');
  }

  // Step 1: handle OAuth callback if URL carries ?code=&state=
  try {
    await OAuth.handleCallback();
  } catch (e) {
    showAuthGate(e.message);
    return;
  }

  // Step 2: load /me — gate everything behind this
  const me = await api('/api/dashboard/me').catch(() => null);
  if (!me) {
    showAuthGate();
    return;
  }

  document.getElementById('btn-signout').hidden = false;
  document.getElementById('nav-plan').textContent = me.plan;
  document.getElementById('account-info').innerHTML =
    `<p><strong>${esc(me.name)}</strong> &lt;${esc(me.email)}&gt;</p>
     <p>Plan: <strong>${esc(me.plan)}</strong> &nbsp; Billing: ${esc(me.billing_status)}</p>`;

  // Load keys and usage in parallel; render quota progress after both
  const [, usagePayload] = await Promise.allSettled([
    loadKeys(),
    api('/api/dashboard/usage').then(p => { renderUsage(p); return p; }).catch(() => null),
  ]).then(results => results.map(r => r.status === 'fulfilled' ? r.value : null));

  if (usagePayload) renderQuota(me.quota, usagePayload);

  // Create key
  document.getElementById('btn-create-key').addEventListener('click', async () => {
    const btn = document.getElementById('btn-create-key');
    const label = await showModal({ title: 'New API Key', message: 'Enter a label for the key:', input: true, confirmLabel: 'Create' });
    if (!label) return;
    await withLoading(btn, () =>
      api('/api/dashboard/keys', { method: 'POST', body: JSON.stringify({ label }) })
        .then(res => { showKey(res.key); return loadKeys(); })
        .catch(e => toast('Failed to create key: ' + (e.error || JSON.stringify(e)), 'error'))
    );
  });

  // Checkout / upgrade
  const checkout = (plan) => async (e) => {
    await withLoading(e.currentTarget, () =>
      api('/api/dashboard/checkout', {
        method: 'POST',
        body: JSON.stringify({
          plan,
          success_url: location.origin + '/app/?checkout=success',
          cancel_url:  location.origin + '/app/?checkout=cancelled',
        }),
      }).then(res => { if (res?.url) location.href = res.url; })
        .catch(e => toast('Checkout failed: ' + (e.error || JSON.stringify(e)), 'error'))
    );
  };
  document.getElementById('btn-upgrade-starter').addEventListener('click', checkout('starter'));
  document.getElementById('btn-upgrade-team').addEventListener('click', checkout('team'));

  // Billing portal
  document.getElementById('btn-portal').addEventListener('click', async (e) => {
    await withLoading(e.currentTarget, () =>
      api('/api/dashboard/portal', {
        method: 'POST',
        body: JSON.stringify({ return_url: location.href }),
      }).then(res => { if (res?.url) location.href = res.url; })
        .catch(e => toast('Portal failed: ' + (e.error || JSON.stringify(e)), 'error'))
    );
  });
}

document.addEventListener('DOMContentLoaded', init);
