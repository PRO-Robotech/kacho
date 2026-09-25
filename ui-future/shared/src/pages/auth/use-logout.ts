// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useState } from "react";
import { type LaneRefusal, laneRefusalOf, loginLane } from "@shared/api/login-lane";
import { useFormToken } from "@shared/hooks/use-form-token";
import { forgetPrincipalState } from "@shared/lib/principal-state";
import { loginAddress } from "./ceremony-addresses";

/**
 * Выход — ОДНО действие на консоль: кнопка каркаса и экран `/logout` зовут его
 * отсюда (приёмка F8, S1, группа D).
 *
 * Экран НЕ показывает выхода, пока служба его не подтвердила (F8-19): «вышли»
 * на экране при живом носителе — человек уходит от чужого монитора уверенным,
 * что вышел. Поэтому на отказе адрес не меняется, и отказ назван; носитель
 * консоль не трогает — его гасит служба, и только она.
 *
 * Выход в консоли ОДИН (условие C13): контекст личности своего выхода не
 * держит. После подтверждённого выхода снимается состояние браузера,
 * привязанное к человеку (`forgetPrincipalState`, условие C14), — и только
 * после него: отказ выхода не снимает ничего.
 */
export function useLogout(leave: (to: string) => void = (to) => window.location.replace(to)) {
  const holder = useFormToken("logout");
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<LaneRefusal | null>(null);
  const logout = async () => {
    if (busy) return;
    setBusy(true);
    setRefusal(null);
    try {
      await loginLane.logout(holder);
      forgetPrincipalState();
      leave(loginAddress());
      return;
    } catch (err) {
      setRefusal(laneRefusalOf(err));
    }
    setBusy(false);
  };
  return { logout, busy, refusal };
}
