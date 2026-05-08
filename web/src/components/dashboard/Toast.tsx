import { toasts } from './state.js';

export default function Toast() {
  return (
    <div id="toast-container">
      {toasts.value.map(t => (
        <div key={t.id} class={`toast${t.kind !== 'info' ? ' ' + t.kind : ''}`}>
          {t.msg}
        </div>
      ))}
    </div>
  );
}
