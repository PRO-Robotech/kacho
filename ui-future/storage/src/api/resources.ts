// Per-resource API helpers storage-домена. Обёртки над generic api.* (client.ts),
// который выполняет case-конверсию (snake→camel на отправку, camel→snake на
// приём) и заворачивает мутации в Operation-envelope. URL'ы — verbatim из proto
// google.api.http annotations (kacho.cloud.storage.v1).
//
// Generic ResourceListPage/Shell/Create ходят напрямую через api.* по
// spec.apiPath; эти helpers дают типизированные доменные вызовы (напр. снимок из
// тома) для мест, где нужен явный контракт.

import { catalogApi, resourceApi } from "@shared/api/resources";

import { api } from "./client";
import type { Operation, VolumeList, SnapshotList, DiskTypeList, ImageList } from "./types";

const VOLUMES = "/storage/v1/volumes";
const SNAPSHOTS = "/storage/v1/snapshots";
const DISK_TYPES = "/storage/v1/diskTypes";
const IMAGES = "/storage/v1/images";

export const volumesApi = {
  ...resourceApi<VolumeList>(VOLUMES),
  // Перевод тома на другой тип диска — ОТДЕЛЬНЫЙ глагол, а не поле правки: это
  // перемещение данных, оно длится (том всё это время MIGRATING) и может отказать
  // на половине, оставив данные на исходном типе. `disk_type_id` в update_mask
  // не входит вовсе.
  changeDiskType: (id: string, diskTypeId: string): Promise<{ operation: Operation }> =>
    api.action(`${VOLUMES}/${id}:changeDiskType`, { disk_type_id: diskTypeId }),
};

export const snapshotsApi = {
  // Снимок создаётся ИЗ тома: тело `create` несёт source_volume_id (+ project_id).
  ...resourceApi<SnapshotList>(SNAPSHOTS),
  // Копия снимка в ДРУГУЮ зону. `project_id` обязателен, хотя выглядит выводимым
  // из источника: именно он — объект вопроса о правах («создать» спрашивают у
  // проекта). Метки и имя источника НЕ наследуются — имя уникально в проекте, а
  // метки несут смысл, вложенный в исходный снимок.
  copy: (id: string, body: unknown): Promise<{ operation: Operation }> => api.action(`${SNAPSHOTS}/${id}:copy`, body),
};

// Read-only каталог (cluster-scoped, без project_id). Admin-CRUD — Internal* API.
export const diskTypesApi = catalogApi<DiskTypeList>(DISK_TYPES);

export const imagesApi = {
  // Образ создаётся РОВНО из одного источника: `create` принимает
  // source_snapshot_id XOR source_volume_id.
  ...resourceApi<ImageList>(IMAGES),
  // Копия образа в ДРУГОЙ регион (образ REGIONAL/anycast). Копия остаётся в
  // проекте источника, поэтому имя источника не наследуется: оно уникально в паре
  // с проектом и столкнулось бы с самим собой.
  copy: (id: string, body: unknown): Promise<{ operation: Operation }> => api.action(`${IMAGES}/${id}:copy`, body),
};
