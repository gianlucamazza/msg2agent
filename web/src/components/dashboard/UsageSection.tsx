import { useState } from 'preact/hooks';
import { usage } from './state.js';
import { api, type UsageRow, type Paginated } from '@/lib/api.js';

const USAGE_LIMIT = 50;

async function loadUsage(offset = 0): Promise<Paginated<UsageRow> | null> {
  try {
    const data = await api<Paginated<UsageRow>>(`/api/dashboard/usage?limit=${USAGE_LIMIT}&offset=${offset}`);
    usage.value = data;
    return data;
  } catch {
    return null;
  }
}

export { loadUsage };

export default function UsageSection() {
  const usageVal = usage.value;
  const [offset, setOffset] = useState(0);

  async function changePage(newOffset: number) {
    setOffset(newOffset);
    await loadUsage(newOffset);
  }

  const items = usageVal?.items ?? [];
  const total = usageVal?.total ?? 0;

  return (
    <section id="section-usage">
      <h2>Usage</h2>
      <div id="usage-chart">
        {usageVal === null ? (
          <p>Loading…</p>
        ) : items.length === 0 && offset === 0 ? (
          <p>No usage data yet.</p>
        ) : (
          <div class="table-wrap">
            <table>
              <thead><tr><th>Period</th><th>Event</th><th>Count</th></tr></thead>
              <tbody>
                {items.map((r, i) => (
                  <tr key={i}>
                    <td>{r.period}</td>
                    <td>{r.event}</td>
                    <td>{r.count.toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {total > USAGE_LIMIT && (
          <div class="pager">
            <button onClick={() => changePage(Math.max(0, offset - USAGE_LIMIT))} disabled={offset === 0}>‹ Prev</button>
            <span>{offset + 1}–{Math.min(offset + items.length, total)} / {total}</span>
            <button onClick={() => changePage(offset + USAGE_LIMIT)} disabled={offset + items.length >= total}>Next ›</button>
          </div>
        )}
      </div>
    </section>
  );
}
