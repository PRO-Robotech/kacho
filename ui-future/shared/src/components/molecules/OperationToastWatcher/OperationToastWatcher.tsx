// OperationToastWatcher — невидимый watcher: при заданном opId поллит
// /operations/{id} и через toast() отображает прогресс / финальный результат.
//
// Используется DeleteButton, SubnetCidrManager, SubnetRelocateDialog: действие
// завершается сразу после получения Operation, а пользователь видит
// progress-toast в углу.

import { useEffect, useRef } from "react";
import { useOperation } from "@shared/lib/use-operation";
import { toast } from "@shared/lib/toast";

interface Props {
  opId: string | null;
  /** Базовый заголовок: "Creating Cloud", "Deleting Network smoke-net". */
  title: string;
  /** Callback при завершении (done=true). success=true если без error. */
  onDone?: (success: boolean) => void;
}

export function OperationToastWatcher({ opId, title, onDone }: Props) {
  const { data: op, isError } = useOperation(opId);
  const loadingToastIdRef = useRef<string | null>(null);

  // Loading-toast при appearance opId
  useEffect(() => {
    if (!opId) return;
    loadingToastIdRef.current = toast.loading(`${title}…`);
    return () => {
      if (loadingToastIdRef.current) {
        toast.dismiss(loadingToastIdRef.current);
        loadingToastIdRef.current = null;
      }
    };
  }, [opId, title]);

  // Финальный toast при done
  const handledRef = useRef(false);
  useEffect(() => {
    if (!opId || handledRef.current) return;
    if (isError) {
      handledRef.current = true;
      toast.error(`${title}: ошибка опроса операции`);
      onDone?.(false);
      return;
    }
    if (op?.done) {
      handledRef.current = true;
      if (op.error) {
        toast.error(`${title}: ${op.error.message}`);
        onDone?.(false);
      } else {
        toast.success(`${title}: готово`);
        onDone?.(true);
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [op?.done, op?.error?.code, isError, opId]);

  // Reset handled flag при смене opId
  useEffect(() => {
    handledRef.current = false;
  }, [opId]);

  return null;
}
