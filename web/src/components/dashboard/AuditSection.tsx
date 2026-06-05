import { useEffect, useState } from 'preact/hooks';
import { Check } from 'lucide-preact';
import { api } from '@/lib/api.js';
import { addToast } from './state.js';
import { Skeleton } from '@/components/ui/index.js';

interface AuditVerifyResult {
  tenant_id: string;
  verified: number;
  tampered: boolean;
  first_bad_id?: string;
}

interface AuditEvent {
  id: string;
  event: string;
  tool_name?: string;
  request_id?: string;
  timestamp: string;
}

interface Paginated<T> {
  items: T[];
  total: number;
  limit: number;
  offset: number;
}

function currentPeriod(): string {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}`;
}

export default function AuditSection() {
  const [verify, setVerify] = useState<AuditVerifyResult | null>(null);
  const [events, setEvents] = useState<Paginated<AuditEvent> | null>(null);
  const [period, setPeriod] = useState(currentPeriod());
  const [loading, setLoading] = useState(true);
  const [evtLoading, setEvtLoading] = useState(false);

  async function runVerify() {
    setLoading(true);
    try {
      const r = await api<AuditVerifyResult>('/api/dashboard/audit/verify');
      setVerify(r);
    } catch {
      addToast('Failed to verify audit chain.', 'error');
    } finally {
      setLoading(false);
    }
  }

  async function loadEvents(p: string) {
    setEvtLoading(true);
    try {
      const r = await api<Paginated<AuditEvent>>(
        `/api/dashboard/audit/events?period=${p}&limit=50`,
      );
      setEvents(r);
    } catch {
      addToast('Failed to load audit events.', 'error');
    } finally {
      setEvtLoading(false);
    }
  }

  useEffect(() => {
    runVerify();
    loadEvents(period);
  }, []);

  function handlePeriodChange(e: Event) {
    const val = (e.target as HTMLInputElement).value;
    setPeriod(val);
    loadEvents(val);
  }

  return (
    <section id="section-audit">
      <h2>Audit Chain</h2>

      <div class="audit-verify-box">
        {loading ? (
          <Skeleton class="skeleton-banner" />
        ) : verify ? (
          <div class={`audit-status ${verify.tampered ? 'audit-tampered' : 'audit-ok'}`}>
            {verify.tampered ? (
              <>
                <span class="audit-badge audit-badge-fail">⚠ Tampered</span>
                <span>First bad event: <code>{verify.first_bad_id}</code></span>
              </>
            ) : (
              <>
                <span class="audit-badge audit-badge-ok"><Check size={12} aria-hidden="true" /> Verified</span>
                <span>{verify.verified.toLocaleString()} events verified</span>
              </>
            )}
          </div>
        ) : null}
        <button class="btn-ghost btn-sm" onClick={runVerify} disabled={loading}>
          Re-verify
        </button>
      </div>

      <div class="audit-events">
        <div class="audit-events-header">
          <h3>Events</h3>
          <label class="audit-period-label">
            Period
            <input
              type="month"
              value={period}
              onInput={handlePeriodChange}
              class="audit-period-input"
            />
          </label>
        </div>

        {evtLoading ? (
          <Skeleton class="skeleton-banner" />
        ) : !events || events.items.length === 0 ? (
          <p class="muted-small">No events for this period.</p>
        ) : (
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Time</th>
                  <th>Event</th>
                  <th>Tool</th>
                  <th>Request ID</th>
                </tr>
              </thead>
              <tbody>
                {events.items.map(ev => (
                  <tr key={ev.id}>
                    <td class="audit-ts">{new Date(ev.timestamp).toLocaleString()}</td>
                    <td><code>{ev.event}</code></td>
                    <td>{ev.tool_name || '—'}</td>
                    <td class="audit-reqid">{ev.request_id ? <code>{ev.request_id.slice(0, 12)}…</code> : '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            {events.total > events.limit && (
              <p class="muted-small">Showing {events.items.length} of {events.total} events.</p>
            )}
          </div>
        )}
      </div>
    </section>
  );
}
