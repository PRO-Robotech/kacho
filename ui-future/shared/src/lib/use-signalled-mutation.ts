// Мутация, которая ОБЯЗАНА сообщить о своём исходе — единая точка на всю консоль.
//
// Предмет, довод про три исхода и форму сообщений — `mutation-signal.ts`. Здесь
// проводка: запрос → разбор ответа → опрос операции → сигнал.
//
// # Почему это хук, а не обёртка над `useMutation`
//
// Исход асинхронной мутации становится известен НЕ в колбэке запроса, а позже —
// когда опрос операции дойдёт до `done`. Значит между отправкой и вердиктом живёт
// состояние (идентификатор операции), а состояние в React живёт в хуке. Обёртка
// над `useMutation` вернула бы управление раньше, чем появился предмет сообщения.
//
// # Порядок, который здесь важен
//
// Успех объявляется ПОСЛЕ `onSucceeded` (сброс кэшей, переход), а не до: сначала
// экран должен показывать то, о чём сообщает подпись. Обратный порядок даёт
// «создана» поверх списка, в котором созданного ещё нет.

import { useCallback, useEffect, useRef, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { errorText } from "@shared/lib/error-presentation";
import { operationOutcome, operationWarnings, resolveMutationResponse } from "@shared/lib/operation-outcome";
import { toast } from "@shared/lib/toast";
import { useOperation } from "@shared/lib/use-operation";
import {
  mutationFailureText,
  mutationSuccessText,
  type MutationSubject,
  type MutationVerb,
} from "@shared/lib/mutation-signal";

export interface SignalledMutationInput<TVars> {
  verb: MutationVerb;
  /**
   * О чём сообщать. Функция — когда имя экземпляра известно только из аргументов
   * вызова (создание: имя набирают в форме).
   */
  subject: MutationSubject | ((vars: TVars) => MutationSubject);
  /** Запрос к краю. Ответ разбирается здесь, вызывающему разбирать нечего. */
  mutationFn: (vars: TVars) => Promise<unknown>;
  /**
   * Ресурс объявил, что мутации отвечают `Operation`
   * (`spec.mutationsReturnOperation`). Тогда ответ БЕЗ операции — не «выполнено
   * синхронно», а нарушение контракта: подтверждать нечем.
   */
  expectOperation: boolean;
  /** Вызывается на подтверждённом успехе — сброс кэшей, переход, закрытие модалки. */
  onSucceeded?: () => void;
  /** Вызывается на подтверждённом отказе — снять блокировку формы и т.п. */
  onFailed?: (message: string) => void;
}

export interface SignalledMutation<TVars> {
  run: (vars: TVars) => void;
  /** Запрос идёт ЛИБО операция ещё не завершилась — кнопка остаётся занятой. */
  pending: boolean;
}

export function useSignalledMutation<TVars = void>(
  input: SignalledMutationInput<TVars>,
): SignalledMutation<TVars> {
  const { verb, subject, mutationFn, expectOperation, onSucceeded, onFailed } = input;

  const [pendingOpId, setPendingOpId] = useState<string | null>(null);
  // Подлежащее фиксируется в момент отправки: к моменту вердикта форма может быть
  // уже очищена, а сообщать надо про то, что отправляли.
  const subjectRef = useRef<MutationSubject | null>(null);

  const { data: op, error: opFetchError } = useOperation(pendingOpId);
  const outcome = operationOutcome({ opId: pendingOpId, op, fetchError: opFetchError });

  const fail = useCallback(
    (message: string) => {
      const s = subjectRef.current;
      if (s) toast.error(mutationFailureText(verb, s, message));
      onFailed?.(message);
    },
    [verb, onFailed],
  );

  const mutation = useMutation({
    mutationFn,
    onSuccess: (resp) => {
      const resolved = resolveMutationResponse(resp, expectOperation);
      if (resolved.kind === "operation") {
        setPendingOpId(resolved.opId);
        return;
      }
      if (resolved.kind === "violation") {
        fail(resolved.message);
        return;
      }
      // Синхронный ответ самим ресурсом — исход известен сразу.
      const s = subjectRef.current;
      onSucceeded?.();
      if (s) toast.success(mutationSuccessText(verb, s));
    },
    onError: (err) => fail(errorText(err)),
  });

  useEffect(() => {
    if (outcome.kind === "failed") {
      setPendingOpId(null);
      fail(outcome.message);
      return;
    }
    if (outcome.kind !== "succeeded") return;
    const s = subjectRef.current;
    setPendingOpId(null);
    onSucceeded?.();
    // Громкий no-op операции: geo так сообщает, что запись каталога создана
    // ЗАКРЫТОЙ для размещения. Операция успешна, поэтому без отдельного показа
    // оператор уйдёт уверенным, что она пригодна к работе.
    for (const w of operationWarnings(op)) toast.error(s ? `${s.label}: ${w}` : w);
    if (s) toast.success(mutationSuccessText(verb, s));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [outcome.kind, outcome.kind === "failed" ? outcome.message : null]);

  const run = useCallback(
    (vars: TVars) => {
      subjectRef.current = typeof subject === "function" ? subject(vars) : subject;
      mutation.mutate(vars);
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [subject, mutation.mutate],
  );

  return { run, pending: mutation.isPending || pendingOpId !== null };
}
