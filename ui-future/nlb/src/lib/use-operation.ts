// Hook для polling Operation до завершения.
// После Create/Update/Delete/action backend возвращает {operation: Operation}.
// Этот hook поллит GET /operations/{id} каждые 1 сек до done=true.
// URL verbatim из proto: operation/operation_service.proto → GET /operations/{operation_id}

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/api/client";
import type { Operation } from "@/api/types";

/**
 * useOperation — поллит /operations/{opId} каждые 1 сек.
 * Останавливается когда done=true.
 *
 * Передайте null чтобы деактивировать hook.
 */
export function useOperation(opId: string | null) {
  return useQuery({
    queryKey: ["operation", opId],
    queryFn: () => api.get<Operation>(`/operations/${opId}`),
    refetchInterval: (query) => (query.state.data?.done ? false : 1_000),
    enabled: !!opId,
    staleTime: 0,
  });
}

/**
 * invalidateResourceList — хелпер: инвалидирует кэш list-query после завершения операции.
 * Вызывать после done=true. Также инвалидирует breadcrumb-pill queries и
 * dashboard-queries — чтобы новые account/project/network/etc. сразу
 * появлялись в pills и в счётчиках без ручного refresh.
 *
 * useResourceList queryKey = [spec.id, "list", filterField, filterValue].
 * invalidateQueries с queryKey работает по prefix match — ["networks", "list"]
 * матчит все ["networks", "list", *, *] независимо от parent-фильтра.
 */
// Все query-ключи, которые нужно сбросить после завершения мутации над
// ресурсом resourceId. Вынесено отдельно, чтобы переиспользовать в немедленной
// и отложенной (safety) инвалидации.
//
// refetchType:"all" — КЛЮЧЕВОЕ: по умолчанию invalidateQueries рефетчит только
// АКТИВНЫЕ (примонтированные) запросы, а неактивные лишь помечает stale. При
// create-then-navigate список, на который мы переходим, ещё не примонтирован в
// момент инвалидации → он бы не рефетчился и показывал старое. "all" заставляет
// рефетчить и неактивные → новая строка видна сразу после перехода.
function invalidateResourceKeys(qc: ReturnType<typeof useQueryClient>, resourceId: string) {
  const opts = { refetchType: "all" as const };
  // Все list-варианты этого ресурса (любой parent-фильтр).
  qc.invalidateQueries({ queryKey: [resourceId, "list"], ...opts });
  // Detail-ключи resourceId-first:
  //   ResourceDetailPage/EditPage → [spec.id, "detail", uid]
  //   ResourceShell (Обзор + overviewBelow, где живут доменные панели) → [spec.id, "shell-detail", uid]
  qc.invalidateQueries({ queryKey: [resourceId, "detail"], ...opts });
  qc.invalidateQueries({ queryKey: [resourceId, "shell-detail"], ...opts });
  // RefNameLink резолвит имя по списку владельца — сбрасываем все ref-name кэши.
  qc.invalidateQueries({ queryKey: ["ref-name"], ...opts });
  // Network.Create side-effect'ом создаёт default Security Group.
  if (resourceId === "networks") {
    qc.invalidateQueries({ queryKey: ["security-groups", "list"], ...opts });
    qc.invalidateQueries({ queryKey: ["security-groups", "detail"], ...opts });
    qc.invalidateQueries({ queryKey: ["security-groups", "shell-detail"], ...opts });
  }
  // Breadcrumb pills + dashboard counts.
  qc.invalidateQueries({ queryKey: ["accounts-crumb"], ...opts });
  qc.invalidateQueries({ queryKey: ["projects-crumb"], ...opts });
  qc.invalidateQueries({ queryKey: ["dash"], ...opts });
}

export function useInvalidateResourceList() {
  const qc = useQueryClient();
  return (resourceId: string, _projectId?: string | null) => {
    void _projectId;
    // Немедленная инвалидация (рефетч активных + неактивных списков/detail).
    invalidateResourceKeys(qc, resourceId);
    // Poll-retry по всему окну FGA-пропагации. Мутации асинхронные (Operation);
    // после commit'а строки owner/creator-tuple пишется в FGA и появляется в
    // authz-filtered List НЕ мгновенно (пропагация ~0.6–2с, редко до ~3–4с).
    // Единичный refetch «сразу» отдаёт старый список → пользователь «не видит
    // созданное». Поэтому доинвалидируем несколько раз, покрывая всё окно:
    // ресурс подтянется, как только tuple пропагируется. Идемпотентно и дёшево.
    [700, 1500, 2800, 4200].forEach((ms) => setTimeout(() => invalidateResourceKeys(qc, resourceId), ms));
  };
}
