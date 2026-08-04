// ErrorResult — обёртка antd Result: статус и текст берутся из ответа, а не
// придумываются. Разбор ответа — в lib/error-presentation.
//
// Используется единым образом для:
//   - "ресурс не найден" (NotFound → 404)
//   - "не хватает прав" (Forbidden → 403)
//   - "сервер упал" (5xx → 500)
//   - "нереализовано" (статически status="404" + custom subTitle)
//
// Отдельно про 404. Шлюз прячет существование: недоступный вызывающему ресурс
// отвечает NOT_FOUND, дословно совпадающим с настоящим промахом, — ровно чтобы
// эти два случая нельзя было различить. Поэтому под сообщением сервера идёт
// строка, честно называющая обе возможности и не выбирающая между ними:
// «не существует» — выдумка, «нет доступа» — выдумка наоборот. 403 такой
// неоднозначности не несёт и остаётся 403.

import type { ReactNode } from "react";
import { Result, Typography } from "antd";
import type { ResultStatusType } from "antd/es/result";
import { presentError } from "@shared/lib/error-presentation";

interface Props {
  /** Если передан error — статус и subTitle вычисляются автоматически. */
  error?: unknown;
  /** Явный override статуса. Имеет приоритет над auto-detect из error. */
  status?: ResultStatusType;
  title?: ReactNode;
  subTitle?: ReactNode;
  extra?: ReactNode;
  /** При false — без flex-центрирования (полезно если уже в центрированном контейнере). */
  centered?: boolean;
}

export function ErrorResult({ error, status: statusOverride, title, subTitle, extra, centered = true }: Props) {
  const p = presentError(error);
  const status = statusOverride ?? p.status;
  const finalTitle = title ?? p.title;
  // Оговорку про неоднозначность 404 показываем только когда именно её и
  // получили: под подставленным сверху статусом или чужим текстом она была бы
  // утверждением не о том ответе.
  const showNote = p.note !== null && statusOverride === undefined && subTitle === undefined;
  const finalSubTitle =
    subTitle ??
    (p.subTitle === null ? null : (
      <span>
        {p.subTitle}
        {showNote && (
          <>
            <br />
            <Typography.Text type="secondary">{p.note}</Typography.Text>
          </>
        )}
      </span>
    ));

  const result = <Result status={status} title={finalTitle} subTitle={finalSubTitle} extra={extra} />;

  if (!centered) return result;

  return (
    <div
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        minHeight: "60vh",
        width: "100%",
      }}
    >
      <div style={{ width: "100%", maxWidth: 560 }}>{result}</div>
    </div>
  );
}
