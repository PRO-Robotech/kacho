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

// OutcomeFilter — quick-filter по исходу операции (ортогонален statusOf-фильтру):
// «all» без фильтра, «error» — только с ошибкой, «ok» — завершённые без ошибки.
export type OutcomeFilter = "all" | "error" | "ok";

/** matchesOutcome — предикат OutcomeFilter-фильтра над одной операцией. */
export function matchesOutcome(op: OpLike, outcome: OutcomeFilter): boolean {
  if (outcome === "error") return !!op.error;
  if (outcome === "ok") return !!op.done && !op.error;
  return true;
}
