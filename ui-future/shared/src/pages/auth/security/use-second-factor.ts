// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useCallback, useEffect, useState } from "react";
import { type LaneRefusal, laneRefusalOf, loginLane, type SecondFactorState } from "@shared/api/login-lane";

// Состояние способов второго фактора — ОДНО чтение на экран «Безопасность».
//
// Его читают сводка (способы подтверждения, «выключены на установке»), раздел
// «Способ входа» (вход ключом), таблица способов, раздел ключей (доступность) и
// раздел запасных кодов. Чтение у каждого своё разошлось бы: сводка сказала бы
// «не настроено», а таблица в тот же миг — «настроено». Первое чтение — при
// открытии экрана; дальше состояние перечитывает каждый шаг, который его
// меняет (заведение, подтверждение, снятие, перевыпуск, ключ).

export interface SecondFactorRead {
  /** `null` — ещё не прочитано либо прочитать не удалось (`unreadable`). */
  state: SecondFactorState | null;
  unreadable: LaneRefusal | null;
  reread: () => Promise<void>;
}

export function useSecondFactorState(): SecondFactorRead {
  const [state, setState] = useState<SecondFactorState | null>(null);
  const [unreadable, setUnreadable] = useState<LaneRefusal | null>(null);

  const reread = useCallback(async () => {
    try {
      setState(await loginLane.secondFactorState());
      setUnreadable(null);
    } catch (e) {
      setUnreadable(laneRefusalOf(e));
    }
  }, []);

  // Первое чтение — подпиской на ответ: исход ложится, только пока экран жив.
  useEffect(() => {
    let cancelled = false;
    loginLane.secondFactorState().then(
      (s) => {
        if (!cancelled) setState(s);
      },
      (e: unknown) => {
        if (!cancelled) setUnreadable(laneRefusalOf(e));
      },
    );
    return () => {
      cancelled = true;
    };
  }, []);

  return { state, unreadable, reread };
}

/**
 * Ответ — формы приёмки SUA (Р6): ключ `stepUpMethods` у неё есть ВСЕГДА. Только у
 * такого ответа отсутствие момента означает «не было»: у ответа прежней формы
 * моментов нет вовсе, и «ещё не использовалось» было бы утверждением, которого
 * служба не делала.
 */
export function carriesMoments(state: SecondFactorState): boolean {
  return state.stepUpMethods !== undefined;
}

/** Способы подтверждения словами — в порядке показа окна (F8-S7, Р5). */
const METHOD_WORDS: Record<string, string> = {
  webauthn: "ключ доступа",
  totp: "код из приложения",
  lookup_secret: "запасной код",
};

export function methodsInWords(methods: readonly string[]): string {
  const ordered = ["webauthn", "totp", "lookup_secret"].filter((m) => methods.includes(m));
  return ordered.length === 0 ? "не настроено" : ordered.map((m) => METHOD_WORDS[m]).join(", ");
}
