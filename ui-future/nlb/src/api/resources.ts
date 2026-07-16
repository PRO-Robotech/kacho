// Per-resource API helpers NLB-домена. Обёртки над generic api.* (client.ts),
// который выполняет case-конверсию (snake→camel на отправку, camel→snake на
// приём) и заворачивает мутации в Operation-envelope. URL'ы — verbatim из proto
// google.api.http annotations (kacho.cloud.nlb.v1).
//
// Generic ResourceListPage/Shell/Create ходят напрямую через api.* по spec.apiPath;
// эти helpers — для доменных действий (Start/Stop, attach/detach TargetGroup),
// которых нет в generic-конвейере.

import { api } from "./client";
import type { Operation, NetworkLoadBalancerList, ListenerList, TargetGroupList } from "./types";

const NLB_LB = "/nlb/v1/networkLoadBalancers";
const NLB_LISTENERS = "/nlb/v1/listeners";
const NLB_TG = "/nlb/v1/targetGroups";

// buildAttachPayload — тело :attachTargetGroup. Request-message несёт ВЛОЖЕННЫЙ
// `attached_target_group` (AttachedTargetGroup), а не плоский target_group_id.
export function buildAttachPayload(
  targetGroupId: string | undefined,
): { attached_target_group: { target_group_id: string } } | null {
  return targetGroupId ? { attached_target_group: { target_group_id: targetGroupId } } : null;
}

// buildDetachPayload — тело :detachTargetGroup. Здесь request ПЛОСКИЙ:
// target_group_id верхним уровнем (парная операция с асимметричной формой —
// строить payload по proto-форме, не «по симметрии» с attach).
export function buildDetachPayload(targetGroupId: string | undefined): { target_group_id: string } | null {
  return targetGroupId ? { target_group_id: targetGroupId } : null;
}

// attachedTargetGroupIds — снимок id приаттаченных TG из Get(LB)
// (output-only pivot attached_target_groups).
export function attachedTargetGroupIds(data: Record<string, unknown> | undefined): string[] {
  const arr = (data?.attached_target_groups as { target_group_id?: string }[] | undefined) ?? [];
  return arr.map((a) => a.target_group_id).filter((x): x is string => !!x);
}

export const loadBalancersApi = {
  list: (q?: Record<string, string>) => api.list<NetworkLoadBalancerList>(NLB_LB, q),
  get: (id: string) => api.get<Record<string, unknown>>(`${NLB_LB}/${id}`),
  create: (body: unknown): Promise<{ operation: Operation }> => api.create(NLB_LB, body),
  update: (id: string, body: unknown): Promise<{ operation: Operation }> => api.update(`${NLB_LB}/${id}`, body),
  delete: (id: string): Promise<{ operation: Operation }> => api.delete(`${NLB_LB}/${id}`),
  start: (id: string): Promise<{ operation: Operation }> => api.action(`${NLB_LB}/${id}:start`),
  stop: (id: string): Promise<{ operation: Operation }> => api.action(`${NLB_LB}/${id}:stop`),
  // Парные действия — РАЗНЫЕ формы тела (attach вложенный, detach плоский).
  attachTargetGroup: (id: string, targetGroupId: string): Promise<{ operation: Operation }> =>
    api.action(`${NLB_LB}/${id}:attachTargetGroup`, buildAttachPayload(targetGroupId) ?? {}),
  detachTargetGroup: (id: string, targetGroupId: string): Promise<{ operation: Operation }> =>
    api.action(`${NLB_LB}/${id}:detachTargetGroup`, buildDetachPayload(targetGroupId) ?? {}),
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
