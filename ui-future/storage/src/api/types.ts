// TS-типы для flat storage API (kacho.cloud.storage.v1). Ресурсы — плоские
// объекты (нет metadata/spec/status envelope).
//
// Имена полей: край отдаёт camelCase — оба mux'а api-gateway собираются с
// `UseProtoNames: false` (`gateway/internal/restmux/strict_enum.go`). Объявления
// ниже — в snake_case, как в proto, и мост между ними один: `api/client.ts`
// переводит тело запроса snake_case → camelCase, а ответ обратно (`lib/case.ts`,
// пользовательские map'ы вроде labels не трогаются). То есть snake_case здесь —
// не то, что приходит с провода, а то, во что уже переведено. Убрать перевод,
// поверив, что имена совпадают, значит начать читать поля, которых в ответе нет.

// ====== Operation ======
//
// Конверт операции — ОДИН на всю платформу, поэтому берётся из общего
// объявления. Копия здесь была байт-в-байт общей, то есть расхождения ещё не
// случилось; но у двух объявлений одного контракта оно случается молча — и
// обнаруживается там, где его не видно.
export type { Operation, OperationList } from "@shared/api/types";

// ====== storage: StatusReason (kacho.cloud.storage.v1.StatusReason) ======
//
// Закрытый словарь НАШИХ полос, общий для тома / снимка / образа. Свободной
// строки причины на контракте нет и не будет: в неё попадал бы текст бэкенда
// целиком (имя пула, координата узла), а гейт двухпроекционности перечисляет
// ИМЕНА полей и такого значения не увидел бы.
//
// Отсюда следствие для консоли: текст причины берётся из ФИКСИРОВАННОЙ таблицы
// по значению перечисления (`lib/storage-enums.ts`), а не печатается как есть.
export type StatusReason =
  | "STATUS_REASON_UNSPECIFIED"
  | "BACKEND_UNAVAILABLE"
  | "BACKEND_REJECTED"
  | "BACKEND_CAPACITY_EXHAUSTED"
  | "SOURCE_NOT_READY"
  | "PRECONDITION_FAILED"
  | "INTERNAL_ERROR"
  | string;

// ====== reference (output-only used_by / kacho.cloud.reference.Reference) ======
//
// Своего объявления здесь больше нет. Домен держал его потому, что одноимённый
// общий тип был БЕДНЕЕ контракта — без `referrer.name` и без `owned`, — и
// прямая замена сняла бы у карточки тома оба поля молча. Общий тип обогащён до
// контракта (#1467), поэтому остаётся ссылка.
import type { ResourceReference } from "@shared/api/types";
export type { ReferenceType, Referrer, ResourceReference } from "@shared/api/types";

// ====== storage: Volume ======
// proto: kacho.cloud.storage.v1.VolumeService (/storage/v1/volumes).

export interface VolumeAttachment {
  instance_id?: string;
  instance_name?: string;
  device_name?: string;
  is_boot?: boolean;
  mode?: "MODE_UNSPECIFIED" | "READ_WRITE" | "READ_ONLY" | string;
  auto_delete?: boolean;
  attached_at?: string;
}

export interface Volume {
  id: string;
  project_id?: string;
  created_at?: string;
  updated_at?: string;
  name?: string;
  description?: string;
  labels?: Record<string, string>;
  zone_id?: string;
  disk_type_id?: string;
  // proto3 int64 сериализуется в JSON как СТРОКА.
  size_bytes?: string | number;
  // Номер 11 занимало `block_size`. Оно принималось на Create, хранилось и
  // возвращалось в ответе — и ни один путь не читал его значение. Снято с
  // контракта (`reserved 11; reserved "block_size"`), поэтому и здесь его нет:
  // объявленное поле, которого край не отдаёт, консоль читала бы вечно пустым.
  //
  // Фактически занятые байты. ОТСУТСТВУЮТ, когда бэкенд потребление не сообщил,
  // — и это не ноль: ноль означал бы «том пуст», а такое утверждение на
  // неотвеченном бэкенде было бы правдоподобной ложью. Отсюда `optional` в
  // контракте и необязательность здесь — различие несёт само значение.
  used_bytes?: string | number;
  source_snapshot_id?: string;
  // ID образа, из которого материализован boot-том. Provenance (ON DELETE SET NULL),
  // immutable, взаимоисключающий с source_snapshot_id. Output/провенанс.
  source_image_id?: string;
  // MIGRATING — том переезжает на другой класс диска (принят ChangeDiskType).
  // Состояние отдельное потому, что перенос ДЛИТСЯ и наблюдаем: без него том всё
  // это время выглядел бы готовым.
  status?: "STATUS_UNSPECIFIED" | "CREATING" | "AVAILABLE" | "IN_USE" | "DELETING" | "ERROR" | "MIGRATING" | string;
  // Почему том оказался в своём состоянии. Заполняется у ERROR; в штатных
  // состояниях STATUS_REASON_UNSPECIFIED.
  status_reason?: StatusReason;
  attachments?: VolumeAttachment[];
  used_by?: ResourceReference[];
}

export interface VolumeList {
  volumes: Volume[];
  next_page_token?: string;
}

// ====== storage: Image (STOR-1 — boot-image ресурс) ======
// proto: kacho.cloud.storage.v1.ImageService (/storage/v1/images). REGIONAL/anycast
// (region_id). Создаётся РОВНО из одного источника — Snapshot XOR Volume.
export interface Image {
  id: string;
  project_id?: string;
  created_at?: string;
  updated_at?: string;
  name?: string;
  description?: string;
  labels?: Record<string, string>;
  region_id?: string;
  placement_type?: "PLACEMENT_TYPE_UNSPECIFIED" | "REGIONAL" | string;
  // Ровно один из источников (immutable).
  source_snapshot_id?: string;
  source_volume_id?: string;
  // Output-only (выводится из источника).
  size_bytes?: string | number;
  min_disk_bytes?: string | number;
  format?: "FORMAT_UNSPECIFIED" | "STANDARD" | string;
  status?: "STATUS_UNSPECIFIED" | "CREATING" | "READY" | "DELETING" | "ERROR" | string;
  status_reason?: StatusReason;
  // Тома, засеянные этим образом. Output-only. Нужны ДО удаления, а не после:
  // удаление образа проходит и очищает происхождение засеянных томов.
  used_by?: ResourceReference[];
}

export interface ImageList {
  images: Image[];
  next_page_token?: string;
}

// ====== storage: Snapshot ======
export interface Snapshot {
  id: string;
  project_id?: string;
  created_at?: string;
  updated_at?: string;
  name?: string;
  description?: string;
  labels?: Record<string, string>;
  // Собственный якорь размещения снимка: снимается с зоны исходного тома на
  // Create и неизменяем. Не «зона исходного тома» — ссылка на источник
  // обнуляется при его удалении (снимок обязан пережить свой том), и зона,
  // добираемая через источник, однажды стала бы пустой строкой.
  zone_id?: string;
  source_volume_id?: string;
  size_bytes?: string | number;
  status?: "STATUS_UNSPECIFIED" | "CREATING" | "READY" | "DELETING" | "ERROR" | string;
  status_reason?: StatusReason;
  // Кто засеян этим снимком: тома, созданные из него. Output-only.
  used_by?: ResourceReference[];
}

export interface SnapshotList {
  snapshots: Snapshot[];
  next_page_token?: string;
}

// ====== storage: DiskType (read-only catalog) ======
//
// Класс несёт ПОЛИТИКУ, а не косметику: ярус, состояние обращения, границы
// размера, способности. Чисел производительности, координаты бэкенда, пула и
// шаблона пространства имён здесь нет и не будет — они меняются вместе с
// бэкендом и живут на ревизии привязки (:9091).

/** Ярус класса — ЗАКРЫТЫЙ словарь, а не свободная строка. Порядок значений
 *  шкалой НЕ является: сравнивать ярусы между собой нельзя. */
export type PerformanceTier =
  "PERFORMANCE_TIER_UNSPECIFIED" | "CAPACITY" | "BALANCED" | "FAST" | "SINGLE" | "IO_MAX" | string;

/** Состояние обращения класса: принимает ли он НОВЫЕ тома. Существующие тома
 *  живут при любом значении — правка справочника не отзывает уже выданное. */
export type DiskTypeLifecycle = "LIFECYCLE_UNSPECIFIED" | "ACTIVE" | "DEPRECATED" | "RETIRED" | string;

/** Что класс умеет. Output-only: выводится ПЕРЕСЕЧЕНИЕМ действующих ревизий
 *  привязки (класс предлагается в нескольких зонах, а зоны могут обслуживаться
 *  разными бэкендами), поэтому на вход Create/Update не принимается. */
export interface DiskTypeCapabilities {
  snapshots?: boolean;
  clone_from_snapshot?: boolean;
  clone_from_image?: boolean;
  online_grow?: boolean;
  multi_attach?: boolean;
  encryption_at_rest?: boolean;
}

/** Границы размера тома, объявленные КЛАССОМ. Ноль означает «класс не сужает»:
 *  отсутствие границы отличается от границы, равной нулю, и различие выражено
 *  самим значением, а не отдельным флагом присутствия. */
export interface DiskTypeSizeLimits {
  min_size_bytes?: string | number;
  max_size_bytes?: string | number;
  size_step_bytes?: string | number;
}

export interface DiskType {
  id: string;
  name?: string;
  description?: string;
  zone_ids?: string[];
  // Номер 5 занимал `performance_tier` — ярус СВОБОДНОЙ СТРОКОЙ. Значение вида
  // "pool-b-replicated" проходило сквозь гейт двухпроекционности целиком: канал,
  // закрытый по именам полей, оставался открыт по значениям. Закрыты И номер, И
  // имя, поэтому поля здесь нет — ни под старым именем, ни под новым номером.
  tier?: PerformanceTier;
  lifecycle?: DiskTypeLifecycle;
  capabilities?: DiskTypeCapabilities;
  limits?: DiskTypeSizeLimits;
}

export interface DiskTypeList {
  disk_types: DiskType[];
  next_page_token?: string;
}
