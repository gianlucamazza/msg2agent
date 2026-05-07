// msg2agent Dashboard — vanilla JS, no build step

const api = (path, opts = {}) =>
  fetch(path, { headers: { 'Content-Type': 'application/json' }, ...opts })
    .then(r => r.ok ? r.json() : r.json().then(e => Promise.reject(e)));

function showKey(key) {
  const banner = document.createElement('div');
  banner.className = 'key-reveal';
  banner.innerHTML = `<strong>Copy your key — it will not be shown again:</strong><br><code>${key}</code>
    <button id="dismiss-key">Dismiss</button>`;
  document.getElementById('section-keys').prepend(banner);
  banner.querySelector('#dismiss-key').addEventListener('click', () => banner.remove());
}

function renderKeys(keys) {
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

function renderUsage(rows) {
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

function showAuthGate() {
  document.getElementById('auth-gate').hidden = false;
  document.getElementById('main-content').style.display = 'none';
}

async function init() {
  const me = await api('/api/dashboard/me').catch(() => null);
  if (!me) {
    showAuthGate();
    return;
  }

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
