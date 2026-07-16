// Per-resource API helpers compute-домена. Обёртки над generic api.* (client.ts),
// который выполняет case-конверсию (snake→camel на отправку, camel→snake на
// приём) и заворачивает мутации в Operation-envelope. URL'ы — verbatim из proto
// google.api.http annotations (kacho.cloud.compute.v1).
//
// Generic ResourceListPage/Shell/Create ходят напрямую через api.* по
// spec.apiPath; эти helpers дают доменные действия жизненного цикла инстанса
// (start/stop/restart) и attach/detach тома и сетевого интерфейса, которых нет
// в generic-конвейере. Все действия async → { operation }.

import { api } from "./client";
import type { Operation, InstanceList } from "./types";

const INSTANCES = "/compute/v1/instances";

// buildAttachDiskPayload — тело :attachDisk. Request несёт ВЛОЖЕННЫЙ
// attached_disk_spec (AttachedDiskSpec, oneof disk → volume_id).
export function buildAttachDiskPayload(
  volumeId: string | undefined,
  deviceName: string | undefined,
  autoDelete: boolean,
): { attached_disk_spec: { volume_id: string; device_name?: string; auto_delete: boolean } } | null {
  if (!volumeId) return null;
  const spec: { volume_id: string; device_name?: string; auto_delete: boolean } = {
    volume_id: volumeId,
    auto_delete: autoDelete,
  };
  if (deviceName) spec.device_name = deviceName;
  return { attached_disk_spec: spec };
}

// buildAttachNicPayload — тело :attachNetworkInterface. Request несёт вложенный
// attached_nic_spec (AttachedNicSpec, nic_id обязателен).
export function buildAttachNicPayload(nicId: string | undefined): { attached_nic_spec: { nic_id: string } } | null {
  return nicId ? { attached_nic_spec: { nic_id: nicId } } : null;
}

export const instancesApi = {
  list: (q?: Record<string, string>) => api.list<InstanceList>(INSTANCES, q),
  get: (id: string) => api.get<Record<string, unknown>>(`${INSTANCES}/${id}`),
  create: (body: unknown): Promise<{ operation: Operation }> => api.create(INSTANCES, body),
  update: (id: string, body: unknown): Promise<{ operation: Operation }> => api.update(`${INSTANCES}/${id}`, body),
  delete: (id: string): Promise<{ operation: Operation }> => api.delete(`${INSTANCES}/${id}`),

  // Lifecycle — суффикс-действия (:verb), async Operation.
  start: (id: string): Promise<{ operation: Operation }> => api.action(`${INSTANCES}/${id}:start`),
  stop: (id: string): Promise<{ operation: Operation }> => api.action(`${INSTANCES}/${id}:stop`),
  restart: (id: string): Promise<{ operation: Operation }> => api.action(`${INSTANCES}/${id}:restart`),

  // Attach/detach тома (storage Volume). detach — oneof disk → volume_id (плоский).
  attachDisk: (id: string, volumeId: string, deviceName: string | undefined, autoDelete: boolean) =>
    api.action(`${INSTANCES}/${id}:attachDisk`, buildAttachDiskPayload(volumeId, deviceName, autoDelete) ?? {}),
  detachDisk: (id: string, volumeId: string): Promise<{ operation: Operation }> =>
    api.action(`${INSTANCES}/${id}:detachDisk`, { volume_id: volumeId }),

  // Attach/detach сетевого интерфейса (kacho-vpc NIC). detach — oneof → nic_id.
  attachNetworkInterface: (id: string, nicId: string) =>
    api.action(`${INSTANCES}/${id}:attachNetworkInterface`, buildAttachNicPayload(nicId) ?? {}),
  detachNetworkInterface: (id: string, nicId: string): Promise<{ operation: Operation }> =>
    api.action(`${INSTANCES}/${id}:detachNetworkInterface`, { nic_id: nicId }),
};
