// OperationDialog — модальное окно, которое показывает статус выполнения Operation.
// Поллит /v1/operations/{id} каждые 1 сек. При done=true закрывается сам (с колбэком).
// При ошибке — показывает сообщение и кнопку закрытия.

import { useEffect } from "react";
import { Loader2, CheckCircle2, XCircle } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@shared/components/atoms/ui/Dialog";
import { Button } from "@shared/components/atoms/ui/Button";
import { useOperation } from "@shared/lib/use-operation";
import type { Operation } from "@shared/api/types";

interface Props {
  /** ID операции для слежения, null = диалог закрыт */
  opId: string | null;
  /** Заголовок операции, например "Creating Instance" */
  title: string;
  /** Вызывается при успешном завершении (done=true, error не задан) */
  onSuccess: () => void;
  /** Вызывается при закрытии (в т.ч. по ошибке) */
  onClose: () => void;
}

export function OperationDialog({ opId, title, onSuccess, onClose }: Props) {
  const { data: op, isError, error } = useOperation(opId);

  // Автозакрытие при успешном завершении
  useEffect(() => {
    if (op?.done && !op.error) {
      onSuccess();
    }
  }, [op, onSuccess]);

  const open = !!opId;
  const done = op?.done ?? false;
  const opError = op?.error;
  const fetchError = isError ? (error as Error) : null;

  const shortId = opId ? opId.slice(0, 16) + "…" : "";

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        // Разрешаем закрыть только если операция завершена или произошла ошибка
        if (!o && (done || fetchError)) onClose();
      }}
    >
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            <span className="font-mono text-xs text-muted-foreground">{shortId}</span>
          </DialogDescription>
        </DialogHeader>

        <div className="py-4 flex flex-col items-center gap-3">
          {!done && !fetchError && !opError && (
            <>
              <Loader2 className="h-8 w-8 animate-spin text-primary" />
              <p className="text-sm text-muted-foreground">Выполнение операции…</p>
            </>
          )}
          {done && !opError && !fetchError && (
            <>
              <CheckCircle2 className="h-8 w-8 text-emerald-500" />
              <p className="text-sm font-medium text-emerald-700">Успешно завершено</p>
            </>
          )}
          {(opError || fetchError) && (
            <>
              <XCircle className="h-8 w-8 text-rose-500" />
              <p className="text-sm font-medium text-rose-700">Операция завершилась с ошибкой</p>
              <p className="text-xs text-rose-600 text-center max-w-xs">{opError?.message ?? fetchError?.message}</p>
            </>
          )}
        </div>

        {(opError || fetchError || done) && (
          <DialogFooter>
            <Button variant="outline" onClick={onClose}>
              Закрыть
            </Button>
          </DialogFooter>
        )}
      </DialogContent>
    </Dialog>
  );
}

/**
 * Хелпер: достаёт Operation.id из ответа на Create/Update/Delete/action.
 *
 * Backend (через grpc-gateway) возвращает Operation как top-level JSON:
 * `{id, description, done, metadata, ...}` — БЕЗ обёртки `{operation: ...}`.
 * (api.create/update/delete TS-типы исторически указывали обёртку — она ошибочна.)
 */
export function extractOperationId(
  resp: Partial<Operation> | { operation?: Operation } | null | undefined,
): string | null {
  if (!resp) return null;
  // Top-level Operation: имеет id + done. Если done нет — это sync resource
  // (Region/Zone/AddressPool — admin-only RPC возвращают объект напрямую).
  if (
    "id" in resp &&
    typeof resp.id === "string" &&
    "done" in resp &&
    typeof (resp as Record<string, unknown>).done === "boolean"
  ) {
    return resp.id;
  }
  // Legacy обёртка — на всякий случай.
  if ("operation" in resp && resp.operation) return resp.operation.id ?? null;
  return null;
}
