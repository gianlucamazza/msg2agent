import { useState } from 'preact/hooks';
import { me, addToast } from './state.js';
import { api } from '@/lib/api.js';
import { Skeleton } from '@/components/ui/index.js';

interface ProfileResponse {
  id: string;
  name: string;
  email: string;
  email_verified: boolean;
  created_at: string;
}

export default function SettingsSection() {
  const meVal = me.value;
  const [name, setName] = useState(meVal?.name ?? '');
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);

  if (!meVal) return <section id="section-settings"><Skeleton class="skeleton-banner" /></section>;

  async function save(e: Event) {
    e.preventDefault();
    if (!dirty) return;
    setSaving(true);
    try {
      const res = await api<ProfileResponse>('/api/dashboard/profile', {
        method: 'PATCH',
        body: JSON.stringify({ name }),
      });
      // update global signal so nav + account section reflect the change
      me.value = { ...meVal!, name: res.name };
      setDirty(false);
      addToast('Profile updated.', 'success');
    } catch {
      addToast('Failed to update profile.', 'error');
    } finally {
      setSaving(false);
    }
  }

  return (
    <section id="section-settings">
      <h2>Settings</h2>

      <form class="settings-form" onSubmit={save}>
        <fieldset>
          <legend>Profile</legend>
          <label class="field-label">
            Display name
            <input
              type="text"
              value={name}
              maxLength={128}
              onInput={e => {
                setName((e.target as HTMLInputElement).value);
                setDirty(true);
              }}
              class="field-input"
              required
            />
          </label>

          <label class="field-label">
            Email
            <input type="email" value={meVal.email} class="field-input" disabled />
          </label>

          <label class="field-label">
            Member since
            <input
              type="text"
              value={new Date(meVal.created_at ?? '').toLocaleDateString()}
              class="field-input"
              disabled
            />
          </label>

          <button
            type="submit"
            class="btn-primary"
            disabled={!dirty || saving}
          >
            {saving ? 'Saving…' : 'Save changes'}
          </button>
        </fieldset>
      </form>

      <div class="settings-section-divider" />

      <div class="settings-danger-zone">
        <h3>Account</h3>
        <p class="muted-small">Account ID: <code>{meVal.id}</code></p>
        <p class="muted-small">
          Email verification: {meVal.email_verified
            ? <span class="badge-ok">Verified</span>
            : <span class="badge-warn">Not verified</span>}
        </p>
      </div>
    </section>
  );
}
