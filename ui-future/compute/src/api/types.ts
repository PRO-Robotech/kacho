// TS-типы для flat compute API (kacho.cloud.compute.v1). Ресурсы — плоские
// объекты (нет metadata/spec/status envelope). grpc-gateway сериализует proto
// snake_case → JSON snake_case (client.ts делает camel↔snake на wire).
//
// COMP-1 REDESIGN: Instance приведён к новой форме — instanceKind-дискриминатор
// (VM XOR CONTAINER), единый канал sizing'а machineTypeId (raw ResourcesSpec/
// platformId retired, ban #2), единый канал ОС bootSource{type,id} (storage.image
// vs registry.image), serviceAccount как reference.Referrer (F4). MachineType —
// новый sync-каталог sizing'а (read-only public; admin-CRUD → Internal*, :9091).

// ====== Operation ======

export interface Operation {
  id: string;
  description?: string;
  created_at?: string;
  created_by?: string;
  modified_at?: string;
  done: boolean;
  metadata?: { "@type": string; [key: string]: unknown };
  error?: { code: number; message: string; details?: unknown[] };
  response?: { "@type": string; [key: string]: unknown };
}

export interface OperationList {
  operations: Operation[];
  next_page_token?: string;
}

// ====== reference.Referrer (cross-owner dependency handle, F4) ======
// Class-C graceful-dangling ссылка: {type,id,name°}. name° — output-only зеркало
// на момент привязки.
export interface Referrer {
  type?: string;
  id?: string;
  name?: string;
}

// ====== compute: EffectiveResources (output-only authoritative size) ======
// Разрешается из каталога MachineType и зеркалится в Instance. Память в МиБ (не байтах).
export interface EffectiveResources {
  v_cpu?: number;
  // proto3 int64 сериализуется в JSON как СТРОКА.
  memory_mib?: string | number;
  gpus?: number;
  gpu_type?: string;
}

// ====== compute: BootSource (единый канал ОС, F3) ======
// На вход принимаются только {type,id}; name/resolved_digest/materialized_volume —
// output-only (резолв/материализация — COMP-2).
export interface MaterializedVolume {
  volume_id?: string;
  size_bytes?: string | number;
  size_gib?: string | number;
  volume_type_id?: string;
}

export interface BootSource {
  // "storage.image" (VM, OS-образ) | "registry.image" (CONTAINER, OCI-артефакт).
  type?: string;
  // "img-<base32>:<tag>" / "img-<base32>@sha256:<hex>" (storage) | "repo/name:tag" (registry).
  id?: string;
  // Output-only (заполняется резолвом, COMP-2).
  name?: string;
  resolved_digest?: string;
  materialized_volume?: MaterializedVolume;
  image_kind?: "IMAGE_KIND_UNSPECIFIED" | "STORAGE_IMAGE" | "OCI_IMAGE" | string;
}

// ====== compute: kind-gated spec (VmSpec | ContainerSpec) ======
export interface VmMetadataOptions {
  metadata_endpoint?: "METADATA_OPTION_UNSPECIFIED" | "ENABLED" | "DISABLED" | string;
  metadata_token_required?: boolean;
}

export interface VmSpec {
  user_data?: string;
  metadata_options?: VmMetadataOptions;
}

export interface ContainerPort {
  container_port?: number;
  protocol?: string;
}

export interface ContainerSpec {
  command?: string[];
  args?: string[];
  env?: Record<string, string>;
  working_dir?: string;
  ports?: ContainerPort[];
  restart_policy?: "RESTART_POLICY_UNSPECIFIED" | "NEVER" | "ON_FAILURE" | "ALWAYS" | string;
  // Output-only (терминальный SUCCEEDED/FAILED job).
  exit_code?: number;
}

// ====== compute: Instance (COMP-1 redesign) ======
// proto: kacho.cloud.compute.v1.InstanceService (/compute/v1/instances).

// AttachedDisk — том, подключённый к инстансу (boot_disk / secondary_disks,
// output-only зеркала). volume_id — cross-service ref на storage Volume (prefix "vol").
export interface AttachedDisk {
  mode?: "MODE_UNSPECIFIED" | "READ_ONLY" | "READ_WRITE" | string;
  device_name?: string;
  auto_delete?: boolean;
  volume_id?: string;
}

export interface InstanceNetworkInterface {
  index?: string;
  mac_address?: string;
  subnet_id?: string;
  primary_v4_address?: { address?: string; one_to_one_nat?: { address?: string; ip_version?: string } };
  security_group_ids?: string[];
  // ID kacho-vpc NetworkInterface (NIC) — источник истины интерфейса.
  nic_id?: string;
}

export interface Instance {
  id: string;
  project_id?: string;
  created_at?: string;
  name?: string;
  description?: string;
  labels?: Record<string, string>;
  zone_id?: string;
  status?:
    | "STATUS_UNSPECIFIED"
    | "PROVISIONING"
    | "RUNNING"
    | "STOPPING"
    | "STOPPED"
    | "STARTING"
    | "RESTARTING"
    | "UPDATING"
    | "ERROR"
    | "CRASHED"
    | "DELETING"
    | string;
  metadata?: Record<string, string>;

  // --- COMP-1 redesign (NET-NEW) ---
  // Сильный первый дискриминатор (F1): VM XOR CONTAINER. Immutable после Create.
  instance_kind?: "INSTANCE_KIND_UNSPECIFIED" | "VM" | "CONTAINER" | string;
  // Единый канал sizing'а (F2): канонический "mt-" slug.
  machine_type_id?: string;
  // Output-only authoritative size-зеркало из каталога MachineType (F2).
  effective_resources?: EffectiveResources;
  // Единый канал ОС (F3): {type,id} на входе; name°/resolved_digest°/materialized_volume° — output-only.
  boot_source?: BootSource;
  // Opaque placement-group slug (COMP-1 passthrough).
  placement_group_id?: string;
  // Output-only человекочитаемая причина текущего статуса / отложенного изменения.
  status_reason?: string;
  // Service account внутри инстанса (F4): class-C graceful-dangling Referrer.
  service_account?: Referrer;
  // Гарантированный baseline CPU на vCPU, в процентах (0..100).
  cpu_guarantee_percent?: number;
  // kind-gated spec (F1): ровно один, соответствующий instance_kind.
  vm_spec?: VmSpec;
  container_spec?: ContainerSpec;

  // --- output-only launch-зеркала (материализуются attach-сагами COMP-2) ---
  boot_disk?: AttachedDisk;
  secondary_disks?: AttachedDisk[];
  network_interfaces?: InstanceNetworkInterface[];
  fqdn?: string;
}

export interface InstanceList {
  instances: Instance[];
  next_page_token?: string;
}

// ====== compute: MachineType (read-only sizing catalog, F2/F7) ======
// proto: kacho.cloud.compute.v1.MachineTypeService (/compute/v1/machineTypes).
// Public Get/List (ambient cluster-scoped read); admin-CRUD — InternalMachineTypeService
// (:9091, system_admin, ban #6) — не выведен на tenant-facing UI (follow-on).
export interface MachineType {
  id: string;
  name?: string;
  description?: string;
  family?: "FAMILY_UNSPECIFIED" | "STANDARD" | "COMPUTE" | "MEMORY" | "GPU" | string;
  effective_resources?: EffectiveResources;
  available_zones?: string[];
  status?: "STATUS_UNSPECIFIED" | "AVAILABLE" | "DEPRECATED" | "RETIRED" | string;
  labels?: Record<string, string>;
  created_at?: string;
}

export interface MachineTypeList {
  machine_types: MachineType[];
  next_page_token?: string;
}
