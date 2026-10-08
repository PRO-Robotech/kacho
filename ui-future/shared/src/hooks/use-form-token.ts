// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { useEffect, useMemo } from "react";
import { FormTokenHolder, type FormKind } from "@shared/api/login-lane";

/**
 * Держатель признака формы на время жизни экрана.
 *
 * Признак добывается ПРИ ОТКРЫТИИ экрана: отправка не ждёт лишнего обращения, а
 * отказ выдачи признака виден до того, как человек заполнил форму. Отказ
 * заблаговременной выдачи здесь не показывается — его покажет отправка, которая
 * спросит признак снова (держатель неудачу не хранит).
 *
 * Уход экрана завершает решатель вызова края, если отправка его ждёт
 * (замысел `issue-2917` З10, CX2-30): `Worker` принадлежит отправке, а отправка —
 * экрану; его поздний ответ доказательства не выпускает.
 */
export function useFormToken(kind: FormKind): FormTokenHolder {
  const holder = useMemo(() => new FormTokenHolder(kind), [kind]);
  useEffect(() => {
    holder.get().catch(() => undefined);
    return () => holder.end();
  }, [holder]);
  return holder;
}
