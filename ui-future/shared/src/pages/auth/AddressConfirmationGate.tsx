// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import type { ReactNode } from "react";
import { Navigate, useLocation } from "react-router";
import { UNKNOWN_SESSION_TEXT } from "@shared/api/login-lane";
import { ADDRESS_STATE_NOT_NAMED_TEXT, AddressStateUnknownPage, SessionPending } from "./AddressStateUnknownPage";
import { verificationAddress } from "./ceremony-addresses";
import { useAddressState } from "./use-address-state";

// Страж над каркасом консоли — дальше входа только с подтверждённым адресом
// почты (приёмка F6b, Р7; решение владельца 2026-09-27).
//
// ─────────────────────────────────────────────────────────────────────────────
// ГДЕ СТОИТ
//
// Над каркасом и над каждым адресом церемонии, кроме экранов вне каркаса (вход,
// регистрация, выход, экран подтверждения — `OPEN_BEFORE_ADDRESS_CONFIRMATION`).
// Значит и `/settings` (в каркасе), и `/recovery` (вне его) неподтверждённой
// сессии не открываются. Маршрутизатор ставит стража по тому же объявлению
// адресов (`CEREMONY_ROUTING`), которым заводит маршруты.
//
// ЧТО РЕШАЕТ И ПО ЧЕМУ
//
// По ответу края о сессии, и только по нему; спрашивает ОДИН раз на загрузку
// документа. Каркас монтируется только на «подтверждён» — до ответа и на любом
// ином ответе он не монтируется, и его чтения (рейл, крошки, панель) не
// выпускаются. Неподтверждённая сессия уходит на экран подтверждения с адресом
// возврата; «не названо» и «неизвестно» — своя страница. Сессии нет — как до
// этой под-фазы: поведение анонимного вызова не меняется.
//
// РУБЕЖОМ СТРАЖ НЕ ЯВЛЯЕТСЯ. Рубеж — на крае и у службы: край отвергает
// неподтверждённую сессию на каждом пути платформы (Р3). Страж нужен человеку:
// без него он видел бы каркас, собранный из отказов.

export function AddressConfirmationGate({ children }: { children: ReactNode }) {
  const { pathname, search, hash } = useLocation();
  const { state, retry } = useAddressState();

  switch (state.kind) {
    case "asking":
      return <SessionPending />;
    case "confirmed":
    case "absent":
      return <>{children}</>;
    case "unconfirmed":
      return <Navigate to={verificationAddress(`${pathname}${search}${hash}`)} replace />;
    case "not-named":
      return <AddressStateUnknownPage text={ADDRESS_STATE_NOT_NAMED_TEXT} onRetry={retry} />;
    case "unknown":
      return <AddressStateUnknownPage text={UNKNOWN_SESSION_TEXT} onRetry={retry} />;
    default: {
      const unhandled: never = state;
      throw new Error(`положение подтверждения «${JSON.stringify(unhandled)}» не решено стражем`);
    }
  }
}
