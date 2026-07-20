// TS-типы для flat API (sub-phase 1.0, verbatim YC proto).
// Ресурсы — плоские объекты (нет metadata/spec/status envelope).
// grpc-gateway сериализует proto snake_case → JSON snake_case (прямой маппинг).

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

// ====== IAM (KAC-124: заменил organization-manager + resource-manager) ======
//
// Organization / Cloud / Folder упразднены в KAC-124 → заменены на Account / Project
// (kacho.cloud.iam.v1). Public types для tabular представления / breadcrumbs живут
// в api/iam.ts; здесь — минимальные интерфейсы под reverse-lookup в admin UI.

export interface AccountSummary {
  id: string;
  created_at?: string;
  name: string;
  description?: string;
  labels?: Record<string, string>;
  owner_user_id?: string;
}

export interface AccountSummaryList {
  accounts: AccountSummary[];
  next_page_token?: string;
}

export interface ProjectSummary {
  id: string;
  created_at?: string;
  name: string;
  description?: string;
  account_id?: string;
  labels?: Record<string, string>;
}

export interface ProjectSummaryList {
  projects: ProjectSummary[];
  next_page_token?: string;
}

// ====== vpc ======

export interface Network {
  id: string;
  project_id?: string;
  created_at?: string;
  name: string;
  description?: string;
  labels?: Record<string, string>;
  default_security_group_id?: string;
}

export interface NetworkList {
  networks: Network[];
  next_page_token?: string;
}

export interface Subnet {
  id: string;
  project_id?: string;
  created_at?: string;
  name: string;
  description?: string;
  labels?: Record<string, string>;
  network_id?: string;
  zone_id?: string;
  v4_cidr_blocks?: string[];
  v6_cidr_blocks?: string[];
  route_table_id?: string;
}

export interface SubnetList {
  subnets: Subnet[];
  next_page_token?: string;
}

// Reference — minimal shape of kacho.cloud.reference.Reference as it appears on
// the JSON wire (camelCase → snake_case adapter in api/client.ts strips the
// envelope, so `referrer.type` / `referrer.id` come through as-is). `type` on
// the outer object is the Reference.Type enum serialized as a string
// ("USED_BY" / "MANAGED_BY" / "TYPE_UNSPECIFIED").
export interface ResourceReference {
  referrer?: { type?: string; id?: string };
  type?: string;
}

export interface Address {
  id: string;
  project_id?: string;
  created_at?: string;
  name: string;
  description?: string;
  labels?: Record<string, string>;
  external_ipv4_address?: { address: string; zone_id: string };
  internal_ipv4_address?: { address: string; subnet_id: string };
  internal_ipv6_address?: { address: string; subnet_id: string };
  reserved?: boolean;
  used?: boolean;
  type?: string;
  ip_version?: string;
  deletion_protection?: boolean;
  dns_record?: string;
  // Output-only: who uses this address. Populated by kacho-vpc
  // AddressService.Get/List (an `Address.used_by` list of kacho.cloud.reference.Reference).
  // For ephemeral compute NIC addresses each entry's `referrer.type` is
  // "compute_instance" and `referrer.id` is the instance id.
  used_by?: ResourceReference[];
}

export interface AddressList {
  addresses: Address[];
  next_page_token?: string;
}

export interface RouteTable {
  id: string;
  project_id?: string;
  created_at?: string;
  name: string;
  description?: string;
  labels?: Record<string, string>;
  network_id?: string;
  static_routes?: Array<{
    destination_prefix?: string;
    next_hop_address?: string;
    labels?: Record<string, string>;
  }>;
}

export interface RouteTableList {
  route_tables: RouteTable[];
  next_page_token?: string;
}

export interface SecurityGroupRule {
  id?: string;
  description?: string;
  labels?: Record<string, string>;
  direction?: "DIRECTION_UNSPECIFIED" | "INGRESS" | "EGRESS" | string;
  ports?: { from_port?: number; to_port?: number };
  protocol_name?: string;
  protocol_number?: number;
  // oneof target
  cidr_blocks?: { v4_cidr_blocks?: string[]; v6_cidr_blocks?: string[] };
  security_group_id?: string;
  predefined_target?: string;
}

export interface SecurityGroup {
  id: string;
  project_id?: string;
  created_at?: string;
  name: string;
  description?: string;
  labels?: Record<string, string>;
  network_id?: string;
  status?: "STATUS_UNSPECIFIED" | "CREATING" | "ACTIVE" | "UPDATING" | "DELETING" | string;
  rules?: SecurityGroupRule[];
  default_for_network?: boolean;
}

export interface SecurityGroupList {
  security_groups: SecurityGroup[];
  next_page_token?: string;
}

// ====== compute ======

export interface Disk {
  id: string;
  project_id?: string;
  created_at?: string;
  name: string;
  description?: string;
  labels?: Record<string, string>;
  type_id?: string;
  zone_id?: string;
  size?: string | number;
  block_size?: string | number;
  status?: "STATUS_UNSPECIFIED" | "CREATING" | "READY" | "ERROR" | "DELETING" | string;
  source_image_id?: string;
  source_snapshot_id?: string;
  instance_ids?: string[];
}

export interface DiskList {
  disks: Disk[];
  next_page_token?: string;
}

export interface Image {
  id: string;
  project_id?: string;
  created_at?: string;
  name: string;
  description?: string;
  labels?: Record<string, string>;
  family?: string;
  storage_size?: string | number;
  min_disk_size?: string | number;
  product_ids?: string[];
  status?: "STATUS_UNSPECIFIED" | "CREATING" | "READY" | "ERROR" | "DELETING" | string;
  os?: { type?: string };
  pooled?: boolean;
}

export interface ImageList {
  images: Image[];
  next_page_token?: string;
}

export interface Snapshot {
  id: string;
  project_id?: string;
  created_at?: string;
  name: string;
  description?: string;
  labels?: Record<string, string>;
  storage_size?: string | number;
  disk_size?: string | number;
  product_ids?: string[];
  status?: "STATUS_UNSPECIFIED" | "CREATING" | "READY" | "ERROR" | "DELETING" | string;
  source_disk_id?: string;
}

export interface SnapshotList {
  snapshots: Snapshot[];
  next_page_token?: string;
}

export interface InstanceNetworkInterface {
  index?: string;
  subnet_id?: string;
  primary_v4_address?: { address?: string; one_to_one_nat?: { address?: string; ip_version?: string } };
  security_group_ids?: string[];
}

export interface AttachedDisk {
  mode?: string;
  device_name?: string;
  auto_delete?: boolean;
  disk_id?: string;
}

export interface Instance {
  id: string;
  project_id?: string;
  created_at?: string;
  name: string;
  description?: string;
  labels?: Record<string, string>;
  zone_id?: string;
  platform_id?: string;
  resources?: {
    memory?: string | number;
    cores?: string | number;
    core_fraction?: string | number;
    gpus?: string | number;
  };
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
  boot_disk?: AttachedDisk;
  secondary_disks?: AttachedDisk[];
  network_interfaces?: InstanceNetworkInterface[];
  fqdn?: string;
  service_account_id?: string;
}

export interface InstanceList {
  instances: Instance[];
  next_page_token?: string;
}

export interface DiskType {
  id: string;
  description?: string;
  zone_ids?: string[];
}

export interface DiskTypeList {
  disk_types: DiskType[];
  next_page_token?: string;
}

// compute.v1.Zone — read-only справочник зон, зеркало vpc zones.
export interface ComputeZone {
  id: string;
  region_id?: string;
  status?: string;
}

export interface ComputeZoneList {
  zones: ComputeZone[];
  next_page_token?: string;
}

// ====== registry (Container Registry) ======
// proto: kacho.cloud.registry.v1. Ресурсы плоские; мутации async → Operation.
//
// REG-1 REDESIGN: Registry становится REGIONAL-anycast (region_id + placement_type
// const REGIONAL, оба immutable; peer-validate geo.Region). default_repository_visibility
// (rename default_visibility) сидит visibility новых Repository (any-path-to-PUBLIC
// admin-gated). id — единственная идентичность/URL-адресация (pull-путь
// $domain/$registryId/$repo:$tag); name — mutable косметический label (НЕ в URL).
// Явно НЕТ: globalSlug / displayName / top-level visibility / :rename.

// Registry — реестр контейнерных образов (project-scoped, tenant-facing).
export interface Registry {
  id: string;
  project_id: string;
  created_at?: string;
  name?: string;
  description?: string;
  labels?: Record<string, string>;
  // Endpoint для docker login / push / pull (output-only; derived по id: <host>/<id>).
  endpoint?: string;
  // Число репозиториев в реестре (output-only; растёт с docker push).
  repository_count?: number;
  status?: string;
  // --- REG-1 (NET-NEW) ---
  // Регион размещения (REG-1 F4): required на Create, immutable, peer-validate geo.Region.
  region_id?: string;
  // Тип размещения — всегда REGIONAL (REG-1 F4, regional-anycast const, immutable).
  // zone_id отсутствует by construction (зоне-независимый ресурс).
  placement_type?: "PLACEMENT_TYPE_UNSPECIFIED" | "REGIONAL" | string;
  // Видимость по умолчанию для новых Repository (REG-1 F5, rename default_visibility).
  // any-path-to-PUBLIC admin-gated (enforced server-side).
  default_repository_visibility?: "PRIVATE" | "PUBLIC" | string;
}

export interface RegistryList {
  registries: Registry[];
  next_page_token?: string;
}

// Repository — read-only: материализуется при первом docker push, через API не
// создаётся. Идентифицируется полным именем (OCI-путь внутри реестра).
export interface Repository {
  name: string;
  registry_id?: string;
  // Класс исчезаемости репозитория (REG-1 F7): output-only enum. DURABLE —
  // survives-empty (явный CreateRepository / установленный overlay); EPHEMERAL —
  // register-on-first-push, unregister-on-last-tag. Установка overlay AUTO-PROMOTE'ит
  // EPHEMERAL→DURABLE. Понижение через API не выразимо (снимается DeleteRepository).
  lifecycle?: "REPOSITORY_LIFECYCLE_UNSPECIFIED" | "DURABLE" | "EPHEMERAL" | string;
  // Видимость репозитория (авторитетный per-repo гейт). Сидится из
  // Registry.default_repository_visibility на create; any-path-to-PUBLIC admin-gated.
  visibility?: "PRIVATE" | "PUBLIC" | string;
  // Число тегов образов в репозитории (output-only).
  tag_count?: number;
  // Агрегатный размер репозитория; proto3 int64 сериализуется как СТРОКА.
  size_bytes?: string;
  // Время последнего push (last pushed).
  updated_at?: string;
  // Время последнего pull; zero/пусто = ни разу не скачивался.
  last_pulled_at?: string;
  // Основной тип артефакта (enum-имя ARTIFACT_TYPE_*).
  artifact_type?: string;
  // Все типы артефактов репозитория (смешанный: docker + helm) — enum-имена.
  artifact_types?: string[];
  // Суммарное число pull'ов; proto3 int64 сериализуется как СТРОКА.
  download_count?: string;
}

export interface RepositoryList {
  repositories: Repository[];
  next_page_token?: string;
}

// Tag — тег образа в репозитории. Единственная мутация — DeleteTag (async).
export interface Tag {
  tag: string;
  registry_id?: string;
  repository?: string;
  digest?: string;
  // proto3 int64 сериализуется в JSON как СТРОКА.
  size_bytes?: string;
  media_type?: string;
  // Время push этого тега.
  created_at?: string;
  architecture?: string;
  // Время последнего pull; zero/пусто = ни разу не скачивался.
  last_pulled_at?: string;
  // Кем запушен (identity/subject).
  pushed_by?: string;
  // Число pull'ов тега; proto3 int64 сериализуется как СТРОКА.
  download_count?: string;
}

export interface TagList {
  tags: Tag[];
  next_page_token?: string;
}
