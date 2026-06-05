import { useState } from "preact/hooks";
import { keys, addToast, showModal } from "./state.js";
import { api, type ApiKey, type Paginated } from "@/lib/api.js";
import { Skeleton } from "@/components/ui/index.js";

const KEYS_LIMIT = 20;

async function loadKeys(offset = 0): Promise<void> {
  try {
    const data = await api<Paginated<ApiKey>>(
      `/api/dashboard/keys?limit=${KEYS_LIMIT}&offset=${offset}`,
    );
    keys.value = data;
  } catch {
    keys.value = { items: [], total: 0 };
  }
}

export { loadKeys };

export default function KeysSection() {
  const keysVal = keys.value;
  const [offset, setOffset] = useState(0);
  const [revealedKey, setRevealedKey] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [creating, setCreating] = useState(false);

  async function createKey() {
    const label = await showModal({
      title: "New API Key",
      message: "Enter a label for the key:",
      input: true,
      confirmLabel: "Create",
    });
    if (!label || typeof label !== "string") return;
    setCreating(true);
    try {
      const res = await api<{ key: string }>("/api/dashboard/keys", {
        method: "POST",
        body: JSON.stringify({ label }),
      });
      setRevealedKey(res.key);
      await loadKeys(offset);
    } catch (e: unknown) {
      const err = e as { error?: string };
      addToast(
        "Failed to create key: " + (err?.error ?? "unknown error"),
        "error",
      );
    } finally {
      setCreating(false);
    }
  }

  async function renameKey(id: string, currentLabel: string) {
    const newLabel = await showModal({
      title: 'Rename API Key',
      message: `Current name: "${currentLabel}". Enter new name:`,
      input: true,
      confirmLabel: 'Rename',
    });
    if (!newLabel || typeof newLabel !== 'string') return;
    try {
      await api(`/api/dashboard/keys/${id}`, {
        method: 'PATCH',
        body: JSON.stringify({ label: newLabel }),
      });
      addToast('Key renamed.', 'success');
      await loadKeys(offset);
    } catch {
      addToast('Failed to rename key.', 'error');
    }
  }

  async function revokeKey(id: string) {
    const ok = await showModal({
      title: "Revoke key?",
      message: "The key will stop working immediately.",
      confirmLabel: "Revoke",
    });
    if (!ok) return;
    try {
      await api(`/api/dashboard/keys/${id}`, { method: "DELETE" });
      await loadKeys(offset);
    } catch (e: unknown) {
      const err = e as { error?: string };
      addToast("Revoke failed: " + (err?.error ?? "unknown error"), "error");
    }
  }

  async function changePage(newOffset: number) {
    setOffset(newOffset);
    await loadKeys(newOffset);
  }

  const items = keysVal?.items ?? [];
  const total = keysVal?.total ?? 0;

  return (
    <section id="section-keys">
      <h2>API Keys</h2>

      {revealedKey && (
        <div class="key-reveal">
          <p>
            <strong>Copy your key — it will not be shown again:</strong>
          </p>
          <code>{revealedKey}</code>
          <button
            class="copy-btn"
            onClick={() => {
              navigator.clipboard.writeText(revealedKey!).then(() => {
                setCopied(true);
                setTimeout(() => setCopied(false), 2000);
              });
            }}
          >
            {copied ? "Copied!" : "Copy"}
          </button>
          <button
            class="btn-ghost btn-sm"
            onClick={() => setRevealedKey(null)}
          >
            Dismiss
          </button>
        </div>
      )}

      <button
        class="btn-primary"
        id="btn-create-key"
        onClick={createKey}
        disabled={creating}
      >
        {creating ? "…" : "New key"}
      </button>

      <div id="keys-list">
        {keysVal === null ? (
          <Skeleton class="skeleton-banner" />
        ) : items.length === 0 && offset === 0 ? (
          <p>No API keys yet. Create one to get started.</p>
        ) : (
          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Label</th>
                  <th>Prefix</th>
                  <th>Created</th>
                  <th>Status</th>
                  <th>Last used</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {items.map((k) => (
                  <tr key={k.id}>
                    <td>{k.label}</td>
                    <td>
                      <code>{k.key_prefix}…</code>
                    </td>
                    <td>{new Date(k.created_at).toLocaleDateString()}</td>
                    <td>{k.revoked_at ? "Revoked" : "Active"}</td>
                    <td>{k.last_used ? new Date(k.last_used).toLocaleDateString() : '—'}</td>
                    <td>
                      {!k.revoked_at && (
                        <>
                          <button
                            class="btn-ghost btn-sm"
                            onClick={() => renameKey(k.id, k.label)}
                            aria-label={`Rename key ${k.label}`}
                          >
                            Rename
                          </button>
                          {' '}
                          <button
                            class="btn-danger"
                            onClick={() => revokeKey(k.id)}
                            aria-label={`Revoke key ${k.label}`}
                          >
                            Revoke
                          </button>
                        </>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {total > KEYS_LIMIT && (
          <div class="pager">
            <button
              onClick={() => changePage(Math.max(0, offset - KEYS_LIMIT))}
              disabled={offset === 0}
            >
              ‹ Prev
            </button>
            <span>
              {offset + 1}–{Math.min(offset + items.length, total)} / {total}
            </span>
            <button
              onClick={() => changePage(offset + KEYS_LIMIT)}
              disabled={offset + items.length >= total}
            >
              Next ›
            </button>
          </div>
        )}
      </div>
    </section>
  );
}
