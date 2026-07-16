// use-operation-store — глобальный стор для async-feedback (sticky banner).
//
// Use-case: после Create/Update мутация возвращает Operation.id. Вместо modal'а
// (OperationDialog) или toast'а — пушим состояние сюда; OperationBanner подписан
// и рендерит sticky-плашку под Header'ом, поллит до done.
//
// Один banner за раз — последний инициированный замещает предыдущий "pending"
// (queue из 1). Завершённые success-баннеры авто-dismiss'аются за 5 сек.

import { useSyncExternalStore } from "react";

export type OpStatus = "pending" | "success" | "error" | "cancelled";

export interface OpEntry {
  id: string; // operation_id
  title: string; // "Создание сети my-net"
  status: OpStatus;
  errorMessage?: string;
  resourceId?: string; // resource id для invalidate (например spec.id)
  projectId?: string | null;
  startedAt: number;
}

let current: OpEntry | null = null;
const listeners = new Set<() => void>();

function emit() {
  listeners.forEach((l) => l());
}

export const operationStore = {
  /** Запустить новую операцию. Перезаписывает текущую (queue=1). */
  start(entry: { id: string; title: string; resourceId?: string; projectId?: string | null }) {
    current = {
      id: entry.id,
      title: entry.title,
      status: "pending",
      resourceId: entry.resourceId,
      projectId: entry.projectId ?? null,
      startedAt: Date.now(),
    };
    emit();
  },
  /** Перевести в success. Auto-dismiss через 5 сек. */
  markDone() {
    if (!current) return;
    current = { ...current, status: "success" };
    emit();
    const op = current;
    setTimeout(() => {
      // только если состояние не было перезаписано новой операцией
      if (current && current.id === op.id && current.status === "success") {
        current = null;
        emit();
      }
    }, 5000);
  },
  /** Перевести в error. Не авто-dismiss — ждём ручного. */
  markError(message: string) {
    if (!current) return;
    current = { ...current, status: "error", errorMessage: message };
    emit();
  },
  /** Перевести в cancelled. Auto-dismiss через 5 сек (как success). */
  markCancelled(message?: string) {
    if (!current) return;
    current = { ...current, status: "cancelled", errorMessage: message };
    emit();
    const op = current;
    setTimeout(() => {
      if (current && current.id === op.id && current.status === "cancelled") {
        current = null;
        emit();
      }
    }, 5000);
  },
  dismiss() {
    current = null;
    emit();
  },
};

export function useOperationEntry(): OpEntry | null {
  return useSyncExternalStore(
    (cb) => {
      listeners.add(cb);
      return () => listeners.delete(cb);
    },
    () => current,
    () => null,
  );
}
