import { useState } from 'preact/hooks';
import { usage } from './state.js';
import { api, type UsageRow, type ToolUsageRow, type Paginated } from '@/lib/api.js';
import { accessToken } from '@/lib/tokens.js';
import { Skeleton } from '@/components/ui/index.js';

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

function downloadCSV(period?: string) {
  const tok = accessToken.get();
  const url = '/api/dashboard/usage.csv' + (period ? `?period=${period}` : '');
  const a = document.createElement('a');
  a.href = url;
  // Token can't be injected into a plain <a> download — open in a new tab so
  // the browser sends the session cookie / auth header via fetch instead.
  // For a true authenticated download we use a temporary fetch-blob approach.
  fetch(url, { headers: tok ? { Authorization: 'Bearer ' + tok } : {} })
    .then(r => r.blob())
    .then(blob => {
      const objUrl = URL.createObjectURL(blob);
      a.href = objUrl;
      a.download = period ? `usage-${period}.csv` : 'usage.csv';
      a.click();
      URL.revokeObjectURL(objUrl);
    });
}

export default function UsageSection() {
  const usageVal = usage.value;
  const [offset, setOffset] = useState(0);
  const [showByTool, setShowByTool] = useState(false);
  const [toolData, setToolData] = useState<ToolUsageRow[] | null>(null);

  async function loadToolBreakdown(p?: string) {
    try {
      const url = `/api/dashboard/usage/by-tool` + (p ? `?period=${p}` : '');
      const data = await api<Paginated<ToolUsageRow>>(url);
      setToolData(data.items);
    } catch {
      setToolData([]);
    }
  }

  async function handleToggleByTool() {
    const next = !showByTool;
    setShowByTool(next);
    if (next && toolData === null) {
      await loadToolBreakdown();
    }
  }

  async function changePage(newOffset: number) {
    setOffset(newOffset);
    await loadUsage(newOffset);
  }

  const items = usageVal?.items ?? [];
  const total = usageVal?.total ?? 0;

  return (
    <section id="section-usage">
      <div class="section-header">
        <h2>Usage</h2>
        <div class="row-gap-sm">
          <button class="btn-ghost btn-sm" onClick={() => downloadCSV()}>Export CSV</button>
          <button class="btn-ghost btn-sm" onClick={handleToggleByTool}>
            {showByTool ? 'By period' : 'By tool'}
          </button>
        </div>
      </div>
      <div id="usage-chart">
        {showByTool ? (
          toolData === null ? (
            <Skeleton class="skeleton-banner" />
          ) : toolData.length === 0 ? (
            <p>No tool usage data yet.</p>
          ) : (
            <div class="table-wrap">
              <table>
                <thead><tr><th>Tool</th><th>Calls</th></tr></thead>
                <tbody>
                  {toolData.map((r, i) => (
                    <tr key={i}>
                      <td>{r.tool_name}</td>
                      <td>{r.count.toLocaleString()}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )
        ) : (
          <>
            {usageVal === null ? (
              <Skeleton class="skeleton-banner" />
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
          </>
        )}
      </div>
    </section>
  );
}
