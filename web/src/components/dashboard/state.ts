import { signal } from '@preact/signals';
import type { MeResponse, ApiKey, Paginated, UsageRow } from '@/lib/api.js';

export const me = signal<MeResponse | null>(null);
export const keys = signal<Paginated<ApiKey> | null>(null);
export const usage = signal<Paginated<UsageRow> | null>(null);

export interface ToastItem { id: number; msg: string; kind: string; }
export const toasts = signal<ToastItem[]>([]);
let nextToastId = 0;

export function addToast(msg: string, kind = 'info'): void {
  const id = ++nextToastId;
  toasts.value = [...toasts.value, { id, msg, kind }];
  setTimeout(() => {
    toasts.value = toasts.value.filter(t => t.id !== id);
  }, 4000);
}

export interface ModalState {
  title: string;
  message?: string;
  input?: boolean;
  confirmLabel?: string;
  resolve: (result: string | boolean | null) => void;
}
export const modalState = signal<ModalState | null>(null);

export function showModal(opts: Omit<ModalState, 'resolve'>): Promise<string | boolean | null> {
  return new Promise(resolve => {
    modalState.value = { ...opts, resolve };
  });
}
