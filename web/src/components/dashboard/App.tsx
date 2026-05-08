import { useEffect, useState } from 'preact/hooks';
import { Router, Route, Redirect } from 'wouter-preact';
import { me, addToast } from './state.js';
import { handleCallback, signIn, signOut } from '@/lib/oauth.js';
import { api, type MeResponse } from '@/lib/api.js';
import { pollForActivation } from '@/lib/stripe-return.js';
import Modal from './Modal.js';
import Toast from './Toast.js';
import AccountSection from './AccountSection.js';
import KeysSection, { loadKeys } from './KeysSection.js';
import UsageSection, { loadUsage } from './UsageSection.js';
import BillingSection from './BillingSection.js';

type AppState = 'loading' | 'gate' | 'ready';

export default function App() {
  const [state, setState] = useState<AppState>('loading');
  const [gateMsg, setGateMsg] = useState('');

  useEffect(() => {
    async function init() {
      const url = new URL(location.href);
      const checkoutResult = url.searchParams.get('checkout');
      if (checkoutResult) {
        history.replaceState(null, '', location.origin + '/app/');
        if (checkoutResult === 'success') {
          await pollForActivation(addToast);
        } else if (checkoutResult === 'cancelled') {
          addToast('Checkout cancelled.', 'info');
        }
      }

      try {
        await handleCallback();
      } catch (e: unknown) {
        const err = e as Error;
        setGateMsg(err?.message ?? 'Authentication error');
        setState('gate');
        return;
      }

      const meData = await api<MeResponse>('/api/dashboard/me').catch(() => null);
      if (!meData) {
        setState('gate');
        return;
      }
      me.value = meData;
      setState('ready');
      await Promise.all([loadKeys(), loadUsage()]);
    }
    init();
  }, []);

  if (state === 'loading') {
    return (
      <>
        <Modal />
        <Toast />
        <nav>
          <span class="logo">msg2agent</span>
        </nav>
        <main>
          <div class="skeleton skeleton-sm-narrow" style="margin: 2rem auto; width: 200px; height: 1.5rem;" />
        </main>
      </>
    );
  }

  if (state === 'gate') {
    return (
      <>
        <Modal />
        <Toast />
        <nav>
          <a href="/" class="logo">msg2agent</a>
        </nav>
        <div class="auth-gate-container">
          <h1 class="auth-gate-title">Sign in to msg2agent</h1>
          <p class="auth-gate-subtitle">Use your API key to access the dashboard.</p>
          {gateMsg && <p class="error-text">{gateMsg}</p>}
          <button class="btn-primary" onClick={() => signIn().catch(e => setGateMsg(e.message))}>
            Sign in with msg2agent
          </button>
        </div>
      </>
    );
  }

  const meVal = me.value!;

  return (
    <>
      <Modal />
      <Toast />
      <nav>
        <a href="/" class="logo">msg2agent</a>
        <span id="nav-plan">{meVal.plan}</span>
        <button id="btn-signout" onClick={signOut}>Sign out</button>
      </nav>
      <main>
        <Router base="/app">
          <Route path="/" component={() => <Redirect to="/account" />} />
          <Route path="/account" component={AccountSection} />
          <Route path="/keys" component={KeysSection} />
          <Route path="/usage" component={UsageSection} />
          <Route path="/billing" component={BillingSection} />
        </Router>
      </main>
    </>
  );
}
