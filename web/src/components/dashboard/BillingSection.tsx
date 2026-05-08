import { useState } from 'preact/hooks';
import { me, addToast } from './state.js';
import { api } from '@/lib/api.js';

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
          <button id="btn-upgrade-starter" class="btn-primary" onClick={() => checkout('starter')} disabled={loadingCheckout === 'starter'}>
            {loadingCheckout === 'starter' ? '…' : 'Upgrade to Starter ($19/mo)'}
          </button>
          <button id="btn-upgrade-team" class="btn-primary" onClick={() => checkout('team')} disabled={loadingCheckout === 'team'}>
            {loadingCheckout === 'team' ? '…' : 'Upgrade to Team ($99/mo)'}
          </button>
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
