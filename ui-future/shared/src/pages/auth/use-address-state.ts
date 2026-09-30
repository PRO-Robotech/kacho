// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useCallback, useEffect, useState } from "react";
import { sessionIdentity } from "@shared/api/login-lane";
import { addressStateOf, type AddressState } from "./address-state";

/**
 * Положение подтверждения адреса по ответу края о сессии — ОДИН вопрос на
 * монтирование (приёмка F6b, Р7) и повтор по действию человека («Проверить
 * снова»). Читателей два — страж над каркасом и экран подтверждения, — и
 * спрашивают они одним местом, чтобы порядок вопроса и решения у них не
 * разошёлся.
 *
 * Состояние меняется только ответом края: вопрос при монтировании не трогает
 * состояние синхронно — начальное и так «спрашиваю».
 */
export function useAddressState(): { state: AddressState; retry: () => void } {
  const [state, setState] = useState<AddressState>({ kind: "asking" });

  const settle = useCallback(
    (isCancelled: () => boolean = () => false) =>
      sessionIdentity().then((answer) => {
        if (!isCancelled()) setState(addressStateOf(answer));
      }),
    [],
  );

  useEffect(() => {
    let cancelled = false;
    void settle(() => cancelled);
    return () => {
      cancelled = true;
    };
  }, [settle]);

  const retry = useCallback(() => {
    setState({ kind: "asking" });
    void settle();
  }, [settle]);

  return { state, retry };
}
