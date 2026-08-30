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
import { operationIdOf } from "@shared/lib/operation-outcome";
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
  const fetchError = isError ? error : null;

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
            <span className="t-mono text-muted-foreground">{shortId}</span>
          </DialogDescription>
        </DialogHeader>

        <div className="py-4 flex flex-col items-center gap-3">
          {!done && !fetchError && !opError && (
            <>
              <Loader2 className="h-8 w-8 animate-spin text-primary" />
              <p className="text-sm text-muted-foreground">Выполнение операции…</p>
            </>
          )}
          {/* Исход окрашен тонами состояний продукта. Прежние `emerald-700` /
              `rose-700` брались из палитры Tailwind, не участвующей ни в одной
              теме: на тёмном фоне тёмно-зелёный текст «Успешно завершено» почти
              не читался — то есть цвет сообщал исход только на светлой. */}
          {done && !opError && !fetchError && (
            <>
              <CheckCircle2 className="h-8 w-8" style={{ color: "var(--status-ok-fg)" }} />
              <p className="text-sm font-medium" style={{ color: "var(--status-ok-fg)" }}>
                Успешно завершено
              </p>
            </>
          )}
          {(opError || fetchError) && (
            <>
              <XCircle className="h-8 w-8" style={{ color: "var(--status-error-fg)" }} />
              <p className="text-sm font-medium" style={{ color: "var(--status-error-fg)" }}>
                Операция завершилась с ошибкой
              </p>
              <p className="text-xs text-center max-w-xs" style={{ color: "var(--status-error-fg)" }}>
                {opError?.message ?? fetchError?.message}
              </p>
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
 * Реэкспорт `operationIdOf` под прежним именем — ДОЖИВАЕТ, а не является частью
 * контракта.
 *
 * Что с ним не так. Он отвечает `string | null` и потому НЕ отличает
 * «синхронный ответ ресурсом» от «операции не пришло у ресурса, который её
 * обещал». Второе — нарушение контракта, и в форме `if (id) … else …` оно
 * молча читается как успех: форма закрывается, список обновляется, оператор
 * уходит уверенным, что мутация исполнена, — при том что подтвердить её нечем.
 * Отличает эти два исхода `resolveMutationResponse` из `lib/operation-outcome`;
 * весь новый код разбирает ответ ею.
 *
 * Кто ещё зовёт — не выписано, а ВЫВОДИТСЯ: перепись гейта
 * `mutation-outcome-read-in-common` печатает и число таких мест, и их адреса
 * на каждом прогоне. Выписанный здесь перечень устарел бы молча — тем же
 * способом, каким устарела прежняя редакция этой шапки, звавшая реэкспорт
 * «для уже существующих вызывающих» при двенадцати живых вызывающих в самом
 * `shared`. Сегодня остались `nlb` и `storage`; их де-форк идёт своими
 * задачами (#408, #407), а `shared` и `compute` переведены и держатся гейтами
 * с этим именем в обоих деревьях.
 *
 * Когда вызывающих не останется, реэкспорт снимается вместе с последним из
 * них: этого требует проба того же гейта, а не чья-то память.
 */
export function extractOperationId(
  resp: Partial<Operation> | { operation?: Operation } | null | undefined,
): string | null {
  return operationIdOf(resp);
}
