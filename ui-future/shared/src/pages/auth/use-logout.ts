// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useState } from "react";
import { LaneRefusal, loginLane } from "@shared/api/login-lane";
import { useFormToken } from "@shared/hooks/use-form-token";
import { loginAddress } from "./ceremony-addresses";

/**
 * Выход — ОДНО действие на консоль: кнопка каркаса и экран `/logout` зовут его
 * отсюда (приёмка F8, S1, группа D).
 *
 * Экран НЕ показывает выхода, пока служба его не подтвердила (F8-19): «вышли»
 * на экране при живом носителе — человек уходит от чужого монитора уверенным,
 * что вышел. Поэтому на отказе адрес не меняется, и отказ назван; носитель
 * консоль не трогает — его гасит служба, и только она.
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
      leave(loginAddress());
      return;
    } catch (err) {
      setRefusal(err instanceof LaneRefusal ? err : new LaneRefusal(0, null, String(err), null, null, null));
    }
    setBusy(false);
  };
  return { logout, busy, refusal };
}
