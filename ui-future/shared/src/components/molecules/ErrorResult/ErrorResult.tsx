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
import { Link } from "react-router";
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
  // Код протокола в ЧИТАЕМЫЙ текст не идёт: он адресован тому, кто чинит, а не
  // тому, кто смотрит на экран. Место ему — подсказка при наведении, откуда его
  // достанет поддержка и разработчик; текст сообщения остаётся текстом сервера.
  const devDetail = statusOverride === undefined && subTitle === undefined ? p.devDetail : null;
  // Отказ по пределу оставляет клиента там, где он упёрся, если не сказать,
  // КУДА идти: сколько разрешено, сколько занято и кто задал величину, живут на
  // витрине квот. Адрес берётся из носителя, названного самим отказом, — не
  // подделывается: носителя не назвали, значит ссылки нет, а раздел всё равно
  // назван словами в оговорке ниже.
  //
  // Дополнение вызывающего сильнее: он знает про своё место больше, и подмена
  // его кнопки нашей ссылкой отняла бы у него действие.
  const quotaHref = statusOverride === undefined && subTitle === undefined ? (p.quota?.href ?? null) : null;
  const finalExtra = extra ?? (quotaHref === null ? undefined : <Link to={quotaHref}>Открыть раздел «Квоты»</Link>);
  const finalSubTitle =
    subTitle ??
    (p.subTitle === null ? null : (
      <span title={devDetail ?? undefined}>
        {p.subTitle}
        {showNote && (
          <>
            <br />
            <Typography.Text type="secondary" data-testid="note">
              {p.note}
            </Typography.Text>
          </>
        )}
      </span>
    ));

  const result = <Result status={status} title={finalTitle} subTitle={finalSubTitle} extra={finalExtra} />;

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
