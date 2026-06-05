import { useEffect, useRef } from 'preact/hooks';
import { modalState } from './state.js';

export default function Modal() {
  const dlgRef = useRef<HTMLDialogElement>(null);
  const inpRef = useRef<HTMLInputElement>(null);
  const state = modalState.value;

  useEffect(() => {
    const dlg = dlgRef.current;
    if (!dlg) return;
    if (state) {
      dlg.showModal();
      if (state.input && inpRef.current) {
        inpRef.current.value = '';
        inpRef.current.focus();
      }
    } else {
      if (dlg.open) dlg.close();
    }
  }, [state]);

  function confirm() {
    const current = modalState.value;
    if (!current) return;
    const result = current.input ? (inpRef.current?.value.trim() || null) : true;
    modalState.value = null;
    current.resolve(result);
  }

  function cancel() {
    const current = modalState.value;
    if (!current) return;
    modalState.value = null;
    current.resolve(null);
  }

  return (
    <dialog ref={dlgRef} onClose={cancel} aria-labelledby="modal-title">
      <p id="modal-title" class="modal-title">{state?.title ?? ''}</p>
      {state?.message && <p class="modal-message">{state.message}</p>}
      {state?.input && (
        <input
          ref={inpRef}
          class="modal-input"
          type="text"
          onKeyDown={(e) => { if (e.key === 'Enter') confirm(); if (e.key === 'Escape') cancel(); }}
        />
      )}
      <div class="modal-actions">
        <button class="btn-ghost" onClick={cancel}>Cancel</button>
        <button class="btn-primary" onClick={confirm}>{state?.confirmLabel ?? 'OK'}</button>
      </div>
    </dialog>
  );
}
