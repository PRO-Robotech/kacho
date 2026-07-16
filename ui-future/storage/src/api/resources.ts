// Per-resource API helpers storage-домена. Обёртки над generic api.* (client.ts),
// который выполняет case-конверсию (snake→camel на отправку, camel→snake на
// приём) и заворачивает мутации в Operation-envelope. URL'ы — verbatim из proto
// google.api.http annotations (kacho.cloud.storage.v1).
//
// Generic ResourceListPage/Shell/Create ходят напрямую через api.* по
// spec.apiPath; эти helpers дают типизированные доменные вызовы (напр. снимок из
// тома) для мест, где нужен явный контракт.

import { api } from "./client";
import type { Operation, VolumeList, SnapshotList, DiskTypeList } from "./types";

const VOLUMES = "/storage/v1/volumes";
const SNAPSHOTS = "/storage/v1/snapshots";
const DISK_TYPES = "/storage/v1/diskTypes";

export const volumesApi = {
  list: (q?: Record<string, string>) => api.list<VolumeList>(VOLUMES, q),
  get: (id: string) => api.get<Record<string, unknown>>(`${VOLUMES}/${id}`),
  create: (body: unknown): Promise<{ operation: Operation }> => api.create(VOLUMES, body),
  update: (id: string, body: unknown): Promise<{ operation: Operation }> => api.update(`${VOLUMES}/${id}`, body),
  delete: (id: string): Promise<{ operation: Operation }> => api.delete(`${VOLUMES}/${id}`),
};

export const snapshotsApi = {
  list: (q?: Record<string, string>) => api.list<SnapshotList>(SNAPSHOTS, q),
  get: (id: string) => api.get<Record<string, unknown>>(`${SNAPSHOTS}/${id}`),
  // Снимок создаётся ИЗ тома: тело несёт source_volume_id (+ project_id).
  create: (body: unknown): Promise<{ operation: Operation }> => api.create(SNAPSHOTS, body),
  update: (id: string, body: unknown): Promise<{ operation: Operation }> => api.update(`${SNAPSHOTS}/${id}`, body),
  delete: (id: string): Promise<{ operation: Operation }> => api.delete(`${SNAPSHOTS}/${id}`),
};

export const diskTypesApi = {
  // Read-only каталог (cluster-scoped, без project_id). Admin-CRUD — Internal* API.
  list: (q?: Record<string, string>) => api.list<DiskTypeList>(DISK_TYPES, q),
  get: (id: string) => api.get<Record<string, unknown>>(`${DISK_TYPES}/${id}`),
};
