// Посадка admin-плоскости: обслуживает ли край, к которому подключена ЭТА
// консоль, административную поверхность платформы (#2692).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ СПРАШИВАЕТСЯ У КРАЯ, А НЕ ОБЪЯВЛЯЕТСЯ ПРОФИЛЕМ
//
// Административные глаголы (`Internal*`) живут только на внутреннем слушателе
// края (запрет #6); консоль же одна на обе посадки — арендаторскую, чей край
// внешний, и операторскую, чей край внутренний. Объявление «плоскость есть» в
// профиле консоли было бы вторым местом об одном предмете: оно разошлось бы с
// тем, куда на самом деле смотрит `upstreams.apiGateway`, и разошлось бы молча.
// Ответ края расходиться не с чем: он и есть посадка.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО СПРАШИВАЕТСЯ И ПОЧЕМУ 404 ЗДЕСЬ — ОТВЕТ, А НЕ ПРОМАХ
//
// Проба — чтение КОЛЛЕКЦИИ admin-плоскости, у которой нет публичного близнеца
// по тому же пути. Внешний слушатель отвечает на скрытую поверхность ровно так,
// как на несуществующий маршрут (`restmux`: «byte-identical to a route that
// does not exist»), — то есть 404. На чтении коллекции 404 не может значить
// «ресурса нет»: коллекция есть всегда. Значит 404 здесь читается однозначно —
// на этом слушателе плоскости нет.
//
// Настоящий отказ в правах приходит 403 и остаётся отказом в правах: раздел
// на него не закрывается, и смотрящий видит то же «нет прав», что и на любом
// другом действии. Всё остальное (5xx, сеть) посадки не называет — и не
// выдаётся ни за наличие, ни за отсутствие: неизвестное остаётся неизвестным.

import { useQuery } from "@tanstack/react-query";
import { api, ApiError } from "@shared/api/client";

/**
 * `pending` — проба ещё не ответила; `unknown` — ответила, но посадки не назвала
 * (сбой, сеть). Разводятся намеренно: до ответа раздел действий не предлагает,
 * а после неназванного ответа — предлагает, чтобы смотрящий увидел настоящий
 * отказ края, а не молчаливо снятые кнопки.
 */
export type AdminPlanePosture = "pending" | "unknown" | "present" | "absent" | "denied";

/**
 * Путь пробы — список пулов адресов: `InternalAddressPoolService/List`,
 * обслуживается только внутренним слушателем, публичного GET по этому пути нет.
 */
export const ADMIN_PLANE_PROBE_PATH = "/vpc/v1/addressPools";

/** Классификация исхода пробы. `null` — проба ответила успехом. */
export function classifyAdminPlaneProbe(failure: unknown): AdminPlanePosture {
  if (failure === null || failure === undefined) return "present";
  if (!(failure instanceof ApiError)) return "unknown";
  switch (failure.status) {
    case 404:
      return "absent";
    case 403:
      return "denied";
    default:
      return "unknown";
  }
}

async function probeAdminPlane(): Promise<AdminPlanePosture> {
  try {
    await api.list(ADMIN_PLANE_PROBE_PATH, { pageSize: "1" });
    return classifyAdminPlaneProbe(null);
  } catch (e) {
    return classifyAdminPlaneProbe(e);
  }
}

/**
 * Посадка admin-плоскости для этой консоли. Спрашивается один раз на сессию:
 * посадка — свойство края, а не запроса, и меняется она перекатом, а не
 * временем.
 */
export function useAdminPlanePosture(): AdminPlanePosture {
  const { data } = useQuery({
    queryKey: ["admin-plane-posture"],
    queryFn: probeAdminPlane,
    staleTime: Infinity,
    retry: false,
  });
  return data ?? "pending";
}
