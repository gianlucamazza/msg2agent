import { api, type MeResponse } from './api.js';

export async function pollForActivation(
  onToast: (msg: string, kind: string) => void,
): Promise<void> {
  onToast('Activating your plan…', 'info');
  const maxAttempts = 24;
  let attempts = 0;
  await new Promise<void>(resolve => {
    const poll = setInterval(async () => {
      attempts++;
      try {
        const me = await api<MeResponse>('/api/dashboard/me');
        if (me && me.billing_status === 'active') {
          clearInterval(poll);
          onToast('Plan activated!', 'success');
          resolve();
        } else if (attempts >= maxAttempts) {
          clearInterval(poll);
          onToast('Plan activation is taking longer than expected — check back in a minute.', 'info');
          resolve();
        }
      } catch {
        if (attempts >= maxAttempts) { clearInterval(poll); resolve(); }
      }
    }, 5000);
  });
}
