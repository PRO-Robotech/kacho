// OperationBanner — sticky-плашка под Header для async ops feedback.
// Подписана на operationStore (см. lib/use-operation-store.ts).
// Поллит /operations/{id} каждые 1сек пока pending — на done переключает status.
// Заменяет блокирующий OperationDialog modal для Create-flow.

import { useEffect } from "react";
import { Loader2, X } from "lucide-react";
import { useInvalidateResourceList, useOperation } from "@shared/lib/use-operation";
import { operationStore, useOperationEntry } from "@shared/lib/use-operation-store";
import { toast } from "@shared/lib/toast";

export function OperationBanner() {
  const entry = useOperationEntry();
  const invalidate = useInvalidateResourceList();

  // Поллим Operation пока pending. При done — обновляем стор.
  const opId = entry?.status === "pending" ? entry.id : null;
  const { data: op } = useOperation(opId);

  useEffect(() => {
    if (!entry || entry.status !== "pending" || !op) return;
    if (!op.done) return;
    // done=true: финальные нотификации идут как toast снизу-справа
    // (consistency со всеми остальными уведомлениями), банер dismiss'им.
    if (op.error) {
      const isCancelled = Number(op.error.code) === 1;
      const msg = op.error.message ?? (isCancelled ? "отменена" : "ошибка");
      if (isCancelled) {
        toast.info(`${entry.title}: ${msg}`);
      } else {
        toast.error(`${entry.title}: ${msg}`);
      }
    } else {
      if (entry.resourceId) {
        invalidate(entry.resourceId, entry.projectId ?? null);
      }
      toast.success(`${entry.title} — готово`);
    }
    operationStore.dismiss();
  }, [op, entry, invalidate]);

  // Банер показываем ТОЛЬКО для pending — финальные состояния уезжают в toast.
  if (!entry || entry.status !== "pending") return null;

  return (
    <div
      role="status"
      aria-live="polite"
      style={{
        position: "sticky",
        // Плашка липнет ПОД шапкой, поэтому её отступ равен высоте шапки.
        // Высоту объявляет `SHAPE.headerHeight` в `lib/theme.ts` — там же, где
        // её читает AntD Layout; здесь стоит то же число, и разойтись они могут
        // только вместе с правкой шапки.
        top: 54,
        zIndex: 19,
        display: "flex",
        alignItems: "center",
        gap: 12,
        padding: "10px 16px",
        background: "var(--kc-elevated)",
        borderBottom: "1px solid var(--kc-border)",
        color: "var(--kc-text)",
        fontSize: 13,
      }}
    >
      <Loader2 size={15} className="animate-spin" color="var(--kc-primary)" />
      <div style={{ flex: 1, minWidth: 0 }}>
        <span style={{ fontWeight: 550 }}>{entry.title}</span>
        <span style={{ marginLeft: 8, color: "var(--kc-text-secondary)", fontSize: 12 }}>операция выполняется…</span>
      </div>
      <button
        type="button"
        onClick={() => operationStore.dismiss()}
        aria-label="Скрыть"
        // Иконочная кнопка — форма целевого оформления: 30×30, радиус 6,
        // тусклый тон. Заливка при наведении даётся классом, чтобы состояние
        // не пришлось держать в React ради одного цвета.
        className="hover:bg-[var(--kc-hover-fill)] hover:text-[var(--kc-text)] transition-colors"
        style={{
          display: "inline-flex",
          alignItems: "center",
          justifyContent: "center",
          width: 30,
          height: 30,
          borderRadius: 6,
          border: "none",
          background: "transparent",
          color: "var(--kc-text-tertiary)",
          cursor: "pointer",
        }}
      >
        <X size={14} />
      </button>
    </div>
  );
}
