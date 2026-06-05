import { useState } from 'preact/hooks';
import { me, addToast } from './state.js';
import { api } from '@/lib/api.js';
import { PLANS } from '@/lib/plans.js';

export default function BillingSection() {
  const meVal = me.value;
  const [loadingCheckout, setLoadingCheckout] = useState<string | null>(null);
  const [loadingPortal, setLoadingPortal] = useState(false);

  async function checkout(plan: string) {
    setLoadingCheckout(plan);
    try {
      const res = await api<{ url?: string }>('/api/dashboard/checkout', {
        method: 'POST',
        body: JSON.stringify({
          plan,
          success_url: location.origin + '/app/?checkout=success',
          cancel_url: location.origin + '/app/?checkout=cancelled',
        }),
      });
      if (res?.url) location.href = res.url;
    } catch (e: unknown) {
      const err = e as { error?: string };
      addToast('Checkout failed: ' + (err?.error ?? 'unknown error'), 'error');
    } finally {
      setLoadingCheckout(null);
    }
  }

  async function openPortal() {
    setLoadingPortal(true);
    try {
      const res = await api<{ url?: string }>('/api/dashboard/portal', {
        method: 'POST',
        body: JSON.stringify({ return_url: location.href }),
      });
      if (res?.url) location.href = res.url;
    } catch (e: unknown) {
      const err = e as { error?: string };
      addToast('Portal failed: ' + (err?.error ?? 'unknown error'), 'error');
    } finally {
      setLoadingPortal(false);
    }
  }

  const plan = meVal?.plan ?? '';
  const billingStatus = meVal?.billing_status ?? '';
  const isPaid = plan === 'starter' || plan === 'team';

  return (
    <section id="section-billing">
      <h2>Billing</h2>
      <p>Current plan: <strong>{plan}</strong> — {billingStatus}</p>

      {!isPaid && (
        <>
          {PLANS.map(p => (
            <button
              key={p.id}
              id={`btn-upgrade-${p.id}`}
              class="btn-primary"
              onClick={() => checkout(p.id)}
              disabled={loadingCheckout === p.id}
            >
              {loadingCheckout === p.id
                ? '…'
                : `Upgrade to ${p.name} (${p.price}/${p.interval})`}
            </button>
          ))}
        </>
      )}

      {isPaid && (
        <button id="btn-portal" class="btn-ghost" onClick={openPortal} disabled={loadingPortal}>
          {loadingPortal ? '…' : 'Manage subscription'}
        </button>
      )}
    </section>
  );
}
