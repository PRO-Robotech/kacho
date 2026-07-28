// Per-resource API helpers NLB-домена. Обёртки над generic api.* (client.ts),
// который выполняет case-конверсию (snake→camel на отправку, camel→snake на
// приём) и заворачивает мутации в Operation-envelope. URL'ы — verbatim из proto
// google.api.http annotations (kacho.cloud.loadbalancer.v1).
//
// Generic ResourceListPage/Shell/Create ходят напрямую через api.* по spec.apiPath;
// здесь — доменные производные, которых в generic-конвейере нет.
//
// Действий-глаголов у балансировщика НЕТ. `:start`/`:stop` сняты с контракта
// (административное включение/выключение выражается полем `admin_state`), а
// `:attachTargetGroup`/`:detachTargetGroup` — вместе с M:N-снимком
// `attached_target_groups`: целевая группа привязывается на ЛИСТЕНЕРЕ
// (`Listener.target_group_id`). Ни один из четырёх маршрутов край не
// обслуживает; звать их — гарантированный 404.

import { api } from "./client";
import type { Operation, NetworkLoadBalancerList, ListenerList, TargetGroupList } from "./types";

const NLB_LB = "/nlb/v1/networkLoadBalancers";
const NLB_LISTENERS = "/nlb/v1/listeners";
const NLB_TG = "/nlb/v1/targetGroups";

// TargetGroupWiring — целевая группа и листенеры, которые в неё направляют
// трафик. Порядок — как в списке листенеров (первое упоминание группы).
export interface TargetGroupWiring {
  targetGroupId: string;
  listeners: { id: string; name: string }[];
}

// targetGroupWiring — какие целевые группы обслуживает балансировщик. Единственный
// источник — ЕГО ЛИСТЕНЕРЫ: привязка живёт на `Listener.target_group_id`
// (`default_target_group_id` — легаси-зеркало того же ref'а, оба сосуществуют).
// Листенер без группы (`substatus=MISCONFIGURED`) строки не даёт.
export function targetGroupWiring(listeners: Record<string, unknown>[] | undefined): TargetGroupWiring[] {
  const byID = new Map<string, TargetGroupWiring>();
  for (const row of listeners ?? []) {
    const tgID = (row.target_group_id as string) || (row.default_target_group_id as string) || "";
    if (!tgID) continue;
    const id = (row.id as string) ?? "";
    const wiring = byID.get(tgID) ?? { targetGroupId: tgID, listeners: [] };
    wiring.listeners.push({ id, name: (row.name as string) || id });
    byID.set(tgID, wiring);
  }
  return [...byID.values()];
}

export const loadBalancersApi = {
  list: (q?: Record<string, string>) => api.list<NetworkLoadBalancerList>(NLB_LB, q),
  get: (id: string) => api.get<Record<string, unknown>>(`${NLB_LB}/${id}`),
  create: (body: unknown): Promise<{ operation: Operation }> => api.create(NLB_LB, body),
  update: (id: string, body: unknown): Promise<{ operation: Operation }> => api.update(`${NLB_LB}/${id}`, body),
  delete: (id: string): Promise<{ operation: Operation }> => api.delete(`${NLB_LB}/${id}`),
};

export const listenersApi = {
  list: (q?: Record<string, string>) => api.list<ListenerList>(NLB_LISTENERS, q),
  get: (id: string) => api.get<Record<string, unknown>>(`${NLB_LISTENERS}/${id}`),
  create: (body: unknown): Promise<{ operation: Operation }> => api.create(NLB_LISTENERS, body),
  update: (id: string, body: unknown): Promise<{ operation: Operation }> =>
    api.update(`${NLB_LISTENERS}/${id}`, body),
  delete: (id: string): Promise<{ operation: Operation }> => api.delete(`${NLB_LISTENERS}/${id}`),
};

export const targetGroupsApi = {
  list: (q?: Record<string, string>) => api.list<TargetGroupList>(NLB_TG, q),
  get: (id: string) => api.get<Record<string, unknown>>(`${NLB_TG}/${id}`),
  create: (body: unknown): Promise<{ operation: Operation }> => api.create(NLB_TG, body),
  update: (id: string, body: unknown): Promise<{ operation: Operation }> => api.update(`${NLB_TG}/${id}`, body),
  delete: (id: string): Promise<{ operation: Operation }> => api.delete(`${NLB_TG}/${id}`),
};
