// Turning a failed request into something to show, without adding meaning the
// server did not send.
//
// The gateway hides existence: when a caller may not see a resource it answers
// NOT_FOUND with a message byte-identical to a real miss, precisely so that the
// two cases cannot be told apart. A 404 in this UI therefore means "absent or
// invisible" and nothing narrower. Rendering it as "this resource does not
// exist" invents a fact; rendering it as "you have no access" invents the
// opposite one. We state both and settle neither.
//
// A 403 carries no such ambiguity and is left alone.

import { ApiError } from "@shared/api/client";

export type ErrorStatus = "403" | "404" | "500" | "warning" | "error";

export interface ErrorPresentation {
  status: ErrorStatus;
  title: string;
  /** The backend's own text, kept verbatim — the message tone is contract. */
  subTitle: string | null;
  /** Extra line shown under the message; null when there is nothing to add. */
  note: string | null;
  ambiguousNotFound: boolean;
}

/** The only thing a 404 lets us claim. */
export const NOT_FOUND_IS_AMBIGUOUS =
  "Сервер отвечает одинаково, когда ресурса не существует и когда он недоступен вашей учётной записи, — по этому ответу различить эти случаи нельзя.";

const TITLES: Record<ErrorStatus, string> = {
  "403": "403",
  "404": "404",
  "500": "500",
  warning: "Внимание",
  error: "Ошибка",
};

function statusFromHttp(status: number): ErrorStatus {
  if (status === 404) return "404";
  if (status === 403) return "403";
  if (status >= 500) return "500";
  if (status >= 400) return "warning";
  return "error";
}

/** A fetch that never reached the server — distinct from anything it answered. */
export function isNetworkFailure(err: unknown): boolean {
  if (!(err instanceof Error)) return false;
  const m = err.message.toLowerCase();
  return (
    err.name === "TypeError" &&
    (m.includes("failed to fetch") || m.includes("networkerror") || m.includes("load failed"))
  );
}

export function presentError(err: unknown): ErrorPresentation {
  if (err === null || err === undefined) {
    return { status: "error", title: TITLES.error, subTitle: null, note: null, ambiguousNotFound: false };
  }

  if (isNetworkFailure(err)) {
    return {
      status: "500",
      title: "Сеть недоступна",
      subTitle: "Не удалось связаться с сервером. Проверьте подключение или повторите позже.",
      note: null,
      ambiguousNotFound: false,
    };
  }

  if (err instanceof ApiError) {
    const status = statusFromHttp(err.status);
    const ambiguousNotFound = status === "404";
    return {
      status,
      title: TITLES[status],
      subTitle: `${err.code}: ${err.message}`,
      note: ambiguousNotFound ? NOT_FOUND_IS_AMBIGUOUS : null,
      ambiguousNotFound,
    };
  }

  return {
    status: "error",
    title: TITLES.error,
    subTitle: err instanceof Error ? err.message : String(err),
    note: null,
    ambiguousNotFound: false,
  };
}
