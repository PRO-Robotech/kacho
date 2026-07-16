// OperationBanner — sticky-плашка под Header для async ops feedback.
// Подписана на operationStore (см. lib/use-operation-store.ts).
// Поллит /operations/{id} каждые 1сек пока pending — на done переключает status.
// Заменяет блокирующий OperationDialog modal для Create-flow.

import { useEffect } from "react";
import { Loader2, X } from "lucide-react";
import { theme } from "antd";
import { useInvalidateResourceList, useOperation } from "@/lib/use-operation";
import { operationStore, useOperationEntry } from "@/lib/use-operation-store";
import { toast } from "@/lib/toast";

export function OperationBanner() {
  const entry = useOperationEntry();
  const { token } = theme.useToken();
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

  const palette = {
    bg: token.colorBgElevated,
    border: token.colorBorderSecondary,
    text: token.colorText,
    icon: <Loader2 size={16} className="animate-spin" color={token.colorPrimary} />,
  };

  return (
    <div
      role="status"
      aria-live="polite"
      style={{
        position: "sticky",
        top: 48,
        zIndex: 19,
        display: "flex",
        alignItems: "center",
        gap: 12,
        padding: "10px 16px",
        background: palette.bg,
        borderBottom: `1px solid ${palette.border}`,
        color: palette.text,
        fontSize: 13,
      }}
    >
      {palette.icon}
      <div style={{ flex: 1, minWidth: 0 }}>
        <span style={{ fontWeight: 500 }}>{entry.title}</span>
        <span style={{ marginLeft: 8, color: token.colorTextSecondary, fontSize: 12 }}>операция выполняется…</span>
      </div>
      <button
        type="button"
        onClick={() => operationStore.dismiss()}
        aria-label="Скрыть"
        style={{
          display: "inline-flex",
          alignItems: "center",
          justifyContent: "center",
          width: 24,
          height: 24,
          borderRadius: 4,
          border: "none",
          background: "transparent",
          color: token.colorTextSecondary,
          cursor: "pointer",
        }}
      >
        <X size={14} />
      </button>
    </div>
  );
}
