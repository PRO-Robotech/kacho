// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useState, useSyncExternalStore } from "react";
import { type LaneRefusal, laneRefusalOf, loginLane } from "@shared/api/login-lane";
import { useFormToken } from "@shared/hooks/use-form-token";
import { forgetPrincipalState } from "@shared/lib/principal-state";
import { loginAddress } from "./ceremony-addresses";
import { beginTabExit, subscribeTabExit, tabExiting } from "./tab-exit";

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
 *
 * С начала выхода и до ухода документа вкладку на вход уводит ТОЛЬКО выход
 * (`tab-exit.ts`): переход по отказу `401` чтения, выпущенного прежней
 * страницей, нёс бы её адрес возврата и отменял бы переход выхода. Отказ выхода
 * это снимает — экран остаётся.
 *
 * Выход ВКЛАДКИ один, а хуков выхода в ней больше одного: панель учётной записи,
 * закрытая и открытая заново в полёте выхода, несёт новый. Поэтому «выход идёт»
 * — метка вкладки, а не состояние хука: пока она стоит, второй выход не
 * начинается, и каждая кнопка выхода показывает, что выход идёт. Снимает метку
 * только отказ ТОГО выхода, что её поставил.
 */
export function useLogout(leave: (to: string) => void = (to) => window.location.replace(to)) {
  const holder = useFormToken("logout");
  const busy = useSyncExternalStore(subscribeTabExit, tabExiting, tabExiting);
  const [refusal, setRefusal] = useState<LaneRefusal | null>(null);
  const logout = async () => {
    const exit = beginTabExit();
    if (exit === null) return;
    setRefusal(null);
    try {
      await loginLane.logout(holder);
    } catch (err) {
      exit.abandon();
      setRefusal(laneRefusalOf(err));
      return;
    }
    forgetPrincipalState();
    leave(loginAddress());
  };
  return { logout, busy, refusal };
}
