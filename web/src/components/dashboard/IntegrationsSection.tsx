import { useEffect, useState } from 'preact/hooks';
import { api, type OAuthClientSummary } from '@/lib/api.js';
import { addToast, showModal } from './state.js';
import { Skeleton } from '@/components/ui/index.js';

export default function IntegrationsSection() {
  const [clients, setClients] = useState<OAuthClientSummary[] | null>(null);

  async function loadClients() {
    try {
      const data = await api<OAuthClientSummary[]>('/api/dashboard/oauth-clients');
      setClients(data);
    } catch {
      setClients([]);
    }
  }

  useEffect(() => { loadClients(); }, []);

  async function revokeClient(clientId: string, clientName: string) {
    const ok = await showModal({
      title: 'Disconnect app?',
      message: `"${clientName}" will no longer be able to access your account.`,
      confirmLabel: 'Disconnect',
    });
    if (!ok) return;
    try {
      await api(`/api/dashboard/oauth-clients/${clientId}`, { method: 'DELETE' });
      addToast(`${clientName} disconnected.`, 'success');
      await loadClients();
    } catch {
      addToast('Failed to disconnect app.', 'error');
    }
  }

  return (
    <section id="section-integrations">
      <h2>Connected Apps</h2>
      <p class="muted-small">Apps authorized to access msg2agent on your behalf via OAuth.</p>

      {clients === null ? (
        <Skeleton class="skeleton-banner" />
      ) : clients.length === 0 ? (
        <p class="muted-small">No connected apps.</p>
      ) : (
        <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>App</th>
                <th>Client ID</th>
                <th>Authorized</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {clients.map(c => (
                <tr key={c.client_id}>
                  <td><strong>{c.client_name}</strong></td>
                  <td><code class="muted-small">{c.client_id.slice(0, 16)}…</code></td>
                  <td class="muted-small">{new Date(c.created_at).toLocaleDateString()}</td>
                  <td>
                    <button
                      class="btn-danger btn-sm"
                      onClick={() => revokeClient(c.client_id, c.client_name)}
                    >
                      Disconnect
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
