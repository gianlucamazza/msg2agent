import { toasts } from './state.js';

export default function Toast() {
  return (
    <div id="toast-container">
      {toasts.value.map(t => (
        <div
          key={t.id}
          class={`toast${t.kind !== 'info' ? ' ' + t.kind : ''}`}
          role={t.kind === 'error' ? 'alert' : 'status'}
          aria-live={t.kind === 'error' ? 'assertive' : 'polite'}
        >
          {t.msg}
        </div>
      ))}
    </div>
  );
}
