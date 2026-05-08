import { useState, useEffect } from 'preact/hooks';

type Plan = 'free' | 'starter' | 'team';

export default function SignupForm() {
  const [paidEnabled, setPaidEnabled] = useState(false);
  const [selectedPlan, setSelectedPlan] = useState<Plan>('free');
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [apiKey, setApiKey] = useState('');
  const [checkoutUrl, setCheckoutUrl] = useState('');
  const [copied, setCopied] = useState(false);
  const [bannerEmail, setBannerEmail] = useState('');

  useEffect(() => {
    const params = new URLSearchParams(location.search);
    const plan = params.get('plan') as Plan | null;
    if (plan && ['free', 'starter', 'team'].includes(plan)) {
      setSelectedPlan(plan);
    }
    if (params.get('reason') === 'no-account') {
      const e = params.get('email') || '';
      setBannerEmail(e);
      if (e) setEmail(e);
    }
    fetch('/api/public/config')
      .then(r => r.json())
      .then(({ paid_enabled }) => setPaidEnabled(!!paid_enabled))
      .catch(() => {});
  }, []);

  async function submit() {
    setError('');
    if (!name.trim()) { setError('Name is required.'); return; }
    if (!email.trim()) { setError('Email is required.'); return; }
    setLoading(true);
    try {
      const res = await fetch('/api/tenants', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: name.trim(), email: email.trim(), plan: selectedPlan }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || 'Signup failed');
      if (data.checkout_url) {
        setCheckoutUrl(data.checkout_url);
        setTimeout(() => { location.href = data.checkout_url; }, 800);
      } else if (data.api_key) {
        setApiKey(data.api_key);
      }
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Something went wrong.');
    } finally {
      setLoading(false);
    }
  }

  function copy() {
    navigator.clipboard.writeText(apiKey).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }

  const tabs: { plan: Plan; label: string; price: string; disabled?: boolean }[] = [
    { plan: 'free', label: 'Free', price: '$0/mo' },
    { plan: 'starter', label: 'Starter', price: paidEnabled ? '$19/mo' : 'Coming soon', disabled: !paidEnabled },
    { plan: 'team', label: 'Team', price: paidEnabled ? '$99/mo' : 'Coming soon', disabled: !paidEnabled },
  ];

  return (
    <div class="form-section">
      <div class="plan-tabs">
        {tabs.map(t => (
          <div
            key={t.plan}
            class={`plan-tab${selectedPlan === t.plan ? ' selected' : ''}`}
            data-plan={t.plan}
            data-disabled={t.disabled ? '1' : undefined}
            onClick={() => { if (!t.disabled) setSelectedPlan(t.plan); }}
            title={t.disabled ? 'Available soon' : undefined}
          >
            {t.label}<span class="price">{t.price}</span>
          </div>
        ))}
      </div>

      {bannerEmail && (
        <div class="no-account-banner">
          <p>No account found for <strong>{bannerEmail}</strong>. Create one below.</p>
        </div>
      )}

      {!apiKey && !checkoutUrl && (
        <>
          <div>
            <label for="name">Name or organization</label>
            <input
              type="text"
              id="name"
              placeholder="Acme Corp"
              maxLength={100}
              value={name}
              onInput={(e) => setName((e.target as HTMLInputElement).value)}
            />
          </div>
          <div>
            <label for="email">Email address</label>
            <input
              type="email"
              id="email"
              placeholder="you@example.com"
              value={email}
              onInput={(e) => setEmail((e.target as HTMLInputElement).value)}
            />
          </div>
          <button class="btn-primary btn-block" onClick={submit} disabled={loading}>
            {loading ? 'Creating…' : 'Create account'}
          </button>
          {error && <div class="error-msg">{error}</div>}
        </>
      )}

      {apiKey && (
        <div class="key-reveal">
          <p><strong>Your API key — copy it now, it won't be shown again:</strong></p>
          <code>{apiKey}</code>
          <button class="copy-btn" onClick={copy}>{copied ? 'Copied!' : 'Copy'}</button>
        </div>
      )}

      {checkoutUrl && (
        <div class="checkout-msg" style="margin-top: 1.5rem">
          <p>Redirecting you to Stripe Checkout…<br />
          If you aren't redirected, <a href={checkoutUrl}>click here</a>.</p>
        </div>
      )}
    </div>
  );
}
