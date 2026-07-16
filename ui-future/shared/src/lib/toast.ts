// Simple toast system — без внешних deps.
// API: toast.success(msg), toast.error(msg), toast.info(msg), toast.dismiss(id).
// Operation-driven toasts live in the OperationToastWatcher component.

import { useSyncExternalStore } from "react";

export type ToastVariant = "success" | "error" | "info" | "loading";

export interface ToastItem {
  id: string;
  message: string;
  variant: ToastVariant;
  // ms; 0 = sticky (не закрывается автоматически)
  duration: number;
  createdAt: number;
}

let counter = 0;
let toasts: ToastItem[] = [];
const listeners = new Set<() => void>();

function emit() {
  listeners.forEach((l) => l());
}

function add(t: Omit<ToastItem, "id" | "createdAt">): string {
  const id = `t${++counter}`;
  toasts = [...toasts, { ...t, id, createdAt: Date.now() }];
  emit();
  if (t.duration > 0) {
    setTimeout(() => dismiss(id), t.duration);
  }
  return id;
}

function update(id: string, patch: Partial<Omit<ToastItem, "id" | "createdAt">>) {
  toasts = toasts.map((t) => (t.id === id ? { ...t, ...patch } : t));
  emit();
}

function dismiss(id: string) {
  toasts = toasts.filter((t) => t.id !== id);
  emit();
}

export const toast = {
  success(message: string, durationMs = 4000) {
    return add({ message, variant: "success", duration: durationMs });
  },
  error(message: string, durationMs = 7000) {
    return add({ message, variant: "error", duration: durationMs });
  },
  info(message: string, durationMs = 3000) {
    return add({ message, variant: "info", duration: durationMs });
  },
  loading(message: string) {
    return add({ message, variant: "loading", duration: 0 });
  },
  update,
  dismiss,
};

// Hook для подписки на список toast-ов (для Toaster компонента).
export function useToasts(): ToastItem[] {
  return useSyncExternalStore(
    (cb) => {
      listeners.add(cb);
      return () => listeners.delete(cb);
    },
    () => toasts,
    () => toasts,
  );
}
