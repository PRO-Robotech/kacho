// ErrorResult — обёртка antd Result со статусом, авто-определённым по
// HTTP-коду ApiError, и центрированием по доступной площади.
//
// Используется единым образом для:
//   - "ресурс не найден" (NotFound → 404)
//   - "не хватает прав" (Forbidden → 403)
//   - "сервер упал" (5xx → 500)
//   - "нереализовано" (статически status="404" + custom subTitle)

import type { ReactNode } from "react";
import { Result } from "antd";
import type { ResultStatusType } from "antd/es/result";
import { ApiError } from "@/api/client";

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

const STATUS_FALLBACK_TITLE: Record<string, string> = {
  "404": "404",
  "403": "403",
  "500": "500",
  error: "Ошибка",
  warning: "Внимание",
  info: "Информация",
  success: "Готово",
};

function statusFromHttp(status: number): ResultStatusType {
  if (status === 404) return "404";
  if (status === 403) return "403";
  if (status >= 500) return "500";
  if (status >= 400) return "warning";
  return "error";
}

function isNetworkFailure(err: unknown): boolean {
  if (!(err instanceof Error)) return false;
  const m = err.message.toLowerCase();
  return (
    err.name === "TypeError" &&
    (m.includes("failed to fetch") || m.includes("networkerror") || m.includes("load failed"))
  );
}

function statusFromError(err: unknown): ResultStatusType {
  if (err instanceof ApiError) return statusFromHttp(err.status);
  if (isNetworkFailure(err)) return "500";
  return "error";
}

function defaultTitle(err: unknown, status: ResultStatusType): ReactNode {
  if (isNetworkFailure(err)) return "Сеть недоступна";
  return STATUS_FALLBACK_TITLE[String(status)] ?? "Ошибка";
}

function defaultSubTitle(err: unknown): ReactNode {
  if (!err) return null;
  if (isNetworkFailure(err)) return "Не удалось связаться с сервером. Проверьте подключение или повторите позже.";
  if (err instanceof ApiError) return `${err.code}: ${err.message}`;
  if (err instanceof Error) return err.message;
  return String(err);
}

export function ErrorResult({ error, status: statusOverride, title, subTitle, extra, centered = true }: Props) {
  const status = statusOverride ?? statusFromError(error);
  const finalTitle = title ?? defaultTitle(error, status);
  const finalSubTitle = subTitle ?? defaultSubTitle(error);

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
