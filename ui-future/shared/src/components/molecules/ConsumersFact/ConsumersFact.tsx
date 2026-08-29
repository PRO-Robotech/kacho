// ConsumersFact — единый вид поля «кем используется» (`used_by`).
//
// ОДИН ПРЕДМЕТ — ОДИН ВИД. Потребители адреса и потребители группы правил — это
// одно и то же поле контракта (`repeated kacho.cloud.reference.Reference`), и
// рисовать их двумя разными кусками разметки значило бы показать один предмет
// двумя. Отличия ресурса задаются параметрами, а не вторым компонентом.
//
// ПОЧЕМУ ЕСТЬ ПОТОЛОК И ПОЧЕМУ ПОДПИСЬ ИМЕННО ТАКАЯ. Число потребителей у
// группы правил ничем не ограничено (интерфейс может взять группу, а потолков на
// число интерфейсов у платформы нет), поэтому сервер отдаёт не больше предела
// плюс ОДНУ строку. Лишняя строка и есть признак «есть ещё»: отдельного поля под
// него в контракте нет.
//
// Отсюда правило подписи, и оно не косметическое: компонент утверждает ровно то,
// что знает.
//
//   получено <= предела      список полон — показываем всё, ничего не дописываем;
//   получено  > предела      потребителей БОЛЬШЕ, чем показано, — показываем
//                            первые `limit` и говорим об этом прямо.
//
// Написать «показаны первые N» на полном списке значило бы соврать в другую
// сторону, поэтому подпись появляется только во второй ветке.

import { type ReactNode } from "react";
import { Typography } from "antd";

import { ReferrerLink } from "@shared/lib/spec-columns";
import type { ResourceReference } from "@shared/api/types";

export interface ConsumersFactProps {
  /** `used_by` как он приехал с сервера. Тип — ОБЩИЙ контрактный
   *  (`kacho.cloud.reference.Reference`), а не своя выписка его полей: выписка
   *  здесь уже стоила `owned`, который объявляла, но до разметки не доносила. */
  usedBy: ResourceReference[] | undefined;
  projectId: string | null | undefined;
  /**
   * Сколько записей показывать. Не задан — показываются все полученные: у
   * ресурсов, где число потребителей ограничено по построению, потолка нет и
   * придумывать его не надо.
   */
  limit?: number;
}

export function ConsumersFact({ usedBy, projectId, limit }: ConsumersFactProps): ReactNode {
  const all = usedBy ?? [];
  if (all.length === 0) {
    return <Typography.Text type="secondary">—</Typography.Text>;
  }
  const truncated = typeof limit === "number" && all.length > limit;
  const shown = truncated ? all.slice(0, limit) : all;
  return (
    <span style={{ display: "inline-flex", flexDirection: "column", gap: 4, alignItems: "flex-start" }}>
      {shown.map((u, i) => (
        <ReferrerLink key={`${u.referrer?.id ?? i}`} projectId={projectId} referrer={u.referrer} owned={u.owned} />
      ))}
      {truncated && (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          Показаны первые {limit} — потребителей больше
        </Typography.Text>
      )}
    </span>
  );
}
