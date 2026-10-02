// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useLocation } from "react-router";
import { safeInternalPath } from "@shared/lib/redirect";
import { CONSOLE_ROOT, RETURN_TO_PARAM } from "./ceremony-addresses";

/**
 * Адрес возврата экрана церемонии — из `?returnTo=`, и ТОЛЬКО на своё
 * происхождение (приёмка F8, F8-13).
 *
 * Отвергаются все четыре формы чужого адреса — абсолютный, протокол-
 * относительный `//…`, с обратной косой `/\…` и не-`http` схема: предикат уже
 * в дереве (`safeInternalPath`), и он здесь держатель, а не второй.
 *
 * Через этот адрес идёт КАЖДАЯ навигация по адресу возврата (условие C10):
 * после входа, после регистрации и отскок человека с живой сессией с экрана
 * входа. Сырого значения параметра экран не видит вовсе.
 */
export function useReturnTo(): string {
  const { search } = useLocation();
  return safeInternalPath(new URLSearchParams(search).get(RETURN_TO_PARAM), CONSOLE_ROOT);
}
