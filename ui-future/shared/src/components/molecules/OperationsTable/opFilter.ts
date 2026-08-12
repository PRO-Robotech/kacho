// opFilter — чистая (без antd/react) логика фильтрации LRO-операций. Вынесена из
// OperationsTable.tsx, чтобы предикаты были юнит-тестируемы без импорта тяжёлого
// UI-графа (antd/es/table и т.п.).

/** Минимальная форма операции, нужная предикатам фильтрации. */
export interface OpLike {
  done?: boolean;
  error?: { code?: number | string; message?: string };
}

export type OperationStatus = "running" | "done" | "error" | "cancelled";

export function statusOf(op: OpLike): OperationStatus {
  if (!op.done) return "running";
  if (op.error) {
    return Number(op.error.code) === 1 ? "cancelled" : "error";
  }
  return "done";
}

export function statusLabel(s: OperationStatus): string {
  switch (s) {
    case "running":
      return "Выполняется";
    case "done":
      return "Выполнена";
    case "error":
      return "Ошибка";
    case "cancelled":
      return "Отменена";
  }
}

