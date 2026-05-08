import { useState } from 'preact/hooks';
import { me, usage } from './state.js';

function CopyButton({ text }: { text: string }) {
  const [label, setLabel] = useState('Copy');
  function copy() {
    navigator.clipboard.writeText(text).then(() => {
      setLabel('Copied!');
      setTimeout(() => setLabel('Copy'), 1500);
    });
  }
  return <button class="identity-copy" onClick={copy}>{label}</button>;
}

function QuotaBars() {
  const meVal = me.value;
  const usageVal = usage.value;
  if (!meVal?.quota || !usageVal) return null;

  const now = new Date();
  const period = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}`;
  let messages = 0, toolCalls = 0;
  for (const r of usageVal.items || []) {
    if (r.period !== period) continue;
    if (r.event === 'messages') messages += r.count;
    if (r.event === 'tool_calls') toolCalls += r.count;
  }

  const bars = [
    { label: 'Messages', used: messages, max: meVal.quota.max_messages_per_month },
    { label: 'Tool calls', used: toolCalls, max: meVal.quota.max_tool_calls_per_month },
  ];

  return (
    <div class="quota-bar-wrap">
      {bars.map(b => {
        const pct = b.max > 0 ? Math.min(100, (b.used / b.max) * 100) : 0;
        const cls = pct >= 100 ? 'over' : pct >= 80 ? 'warn' : '';
        return (
          <div key={b.label} class="quota-item">
            <label>
              <span>{b.label}/mo</span>
              <span>{b.used.toLocaleString()} / {b.max.toLocaleString()}</span>
            </label>
            <div class="quota-bar">
              <div class={`quota-bar-fill${cls ? ' ' + cls : ''}`} style={{ width: pct.toFixed(1) + '%' }} />
            </div>
          </div>
        );
      })}
    </div>
  );
}

export default function AccountSection() {
  const meVal = me.value;
  if (!meVal) return <section id="section-account"><p>Loading…</p></section>;

  return (
    <section id="section-account">
      <h2>Account</h2>
      <div id="account-info">
        <p><strong>{meVal.name}</strong> &lt;{meVal.email}&gt;</p>
        <p>Plan: <strong>{meVal.plan}</strong> &nbsp; Billing: {meVal.billing_status}</p>
        <QuotaBars />
      </div>

      {meVal.did && (
        <div id="account-identity">
          <p class="identity-title">Network identity</p>
          <div class="identity-row">
            <span class="identity-label">DID</span>
            <code class="identity-code">{meVal.did}</code>
            <CopyButton text={meVal.did} />
          </div>
          <details class="identity-keys">
            <summary>Show public keys</summary>
            {meVal.signing_public_key && (
              <div class="identity-row">
                <span class="identity-label">Signing (Ed25519)</span>
                <code class="identity-code">{meVal.signing_public_key}</code>
                <CopyButton text={meVal.signing_public_key} />
              </div>
            )}
            {meVal.encryption_public_key && (
              <div class="identity-row">
                <span class="identity-label">Encryption (X25519)</span>
                <code class="identity-code">{meVal.encryption_public_key}</code>
                <CopyButton text={meVal.encryption_public_key} />
              </div>
            )}
          </details>
        </div>
      )}
    </section>
  );
}
