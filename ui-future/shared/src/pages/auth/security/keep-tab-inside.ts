// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import type { KeyboardEvent } from "react";

// Замкнутый обход Tab внутри окна (приёмка F8-S7, Р9: «диалог держит фокус»).
//
// Ловушка окна antd возвращает фокус по `focusin`, то есть уже ПОСЛЕ того, как
// браузер его перевёл. Окно стоит в конце документа, и Tab с его последнего
// действия переводит фокус за пределы страницы (в оболочку браузера): `focusin`
// в странице не наступает, и фокус остаётся вне диалога до следующего нажатия.
// Поэтому переход с края обхода отменяется здесь, до браузера, и фокус ставится
// на противоположный край. Внутри обхода переход браузера не трогается.

const TABBABLE = "a[href], button, input, select, textarea, [tabindex]";

function tabStops(box: HTMLElement): HTMLElement[] {
  return Array.from(box.querySelectorAll<HTMLElement>(TABBABLE)).filter(
    (el) =>
      el.tabIndex >= 0 &&
      !(el as HTMLButtonElement).disabled &&
      el.closest("[hidden]") === null &&
      el.closest('[aria-hidden="true"]') === null,
  );
}

export function keepTabInside(e: KeyboardEvent<HTMLElement>): void {
  if (e.key !== "Tab") return;
  const stops = tabStops(e.currentTarget);
  if (stops.length === 0) return;
  const first = stops[0];
  const last = stops[stops.length - 1];
  const active = document.activeElement;
  if (e.shiftKey && (active === first || !e.currentTarget.contains(active))) {
    e.preventDefault();
    last.focus();
  } else if (!e.shiftKey && (active === last || !e.currentTarget.contains(active))) {
    e.preventDefault();
    first.focus();
  }
}
