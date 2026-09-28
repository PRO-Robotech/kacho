// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { isOpenBeforeAddressConfirmation, verificationAddress } from "./ceremony-addresses";
import { tabExiting } from "./tab-exit";

/**
 * Увести вкладку на экран подтверждения по отказу края `EMAIL_NOT_VERIFIED`
 * (приёмка F6b, F6b-25) — документом и с адресом возврата: страницей, чьё
 * обращение получило отказ.
 *
 * Один уход на всех читателей поверхности платформы — клиент каркаса, клиент
 * панели и клиент модулей. Каждый решает по ОДНОМУ решению на отказ
 * (`refusalActionOf` → `confirm-address`) и зовёт этот уход; своего перехода ни у
 * кого нет, и адрес собирается одной функцией (`verificationAddress`).
 *
 * Не уводит с экранов, открытых до подтверждения (вход, регистрация, выход,
 * сам экран подтверждения): они и есть разрешённое, и переход с них был бы
 * кругом. Не уводит, пока вкладка уходит выходом (`tab-exit.ts`): переход,
 * начатый после перехода выхода, отменил бы его.
 *
 * `go` — переход документа; пробы подставляют свой.
 */
export function leaveToAddressConfirmation(go: (to: string) => void = (to) => window.location.assign(to)): void {
  const { pathname, search, hash } = window.location;
  if (isOpenBeforeAddressConfirmation(pathname) || tabExiting()) return;
  go(verificationAddress(`${pathname}${search}${hash}`));
}
