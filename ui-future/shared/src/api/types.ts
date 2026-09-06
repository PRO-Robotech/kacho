// TS-типы для flat API (sub-phase 1.0, Kachō proto).
// Ресурсы — плоские объекты (нет metadata/spec/status envelope).
// grpc-gateway сериализует proto snake_case → JSON snake_case (прямой маппинг).

// ====== Open enum ======
//
// Значения proto-enum'ов приезжают строками, и набор известных имён РАСШИРЯЕТСЯ
// новыми версиями сервиса: консоль обязана пережить значение, которого ещё не
// знает. Форма `"A" | "B" | string` это выражала неверно — TypeScript сводит её
// к `string`, литералы исчезают, автодополнение и проверка опечаток пропадают,
// а тип продолжает выглядеть перечислением. `OpenEnum` даёт то же намерение
// честно: известные имена подсказываются и проверяются, любая другая строка
// принимается.
export type OpenEnum<Known extends string> = Known | (string & {});

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
// (kaname.cloud.iam.v1). Public types для tabular представления / breadcrumbs живут
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

// VPC-1 redesign: Network carries a declared supernet (ipv4/ipv6_cidr_blocks)
// and both system-provisioned default handles (SG + RT). The supernet is
// immutable through Update — grown/shrunk only via :add-cidr-blocks /
// :remove-cidr-blocks verbs. `default_*_id` are output-only, echoed in the
// op-in-response body of Create. Public projection carries no vrfId/underlay
// (two-projection — infra fields live only on InternalNetworkService).
export interface Network {
  id: string;
  project_id?: string;
  created_at?: string;
  name: string;
  description?: string;
  labels?: Record<string, string>;
  ipv4_cidr_blocks?: string[];
  ipv6_cidr_blocks?: string[];
  default_security_group_id?: string;
  default_route_table_id?: string;
}

export interface NetworkList {
  networks: Network[];
  next_page_token?: string;
}

// VPC-1 redesign: Subnet is the single placement-anchor. `placement_type` is a
// server-derived discriminator (bare token ZONAL | REGIONAL), unwritable — the
// client sends exactly one of zone_id / region_id. `ipv4_cidr_primary` /
// `ipv6_cidr_primary` are the immutable placement anchor (⊆ one network supernet
// block; at least one required). Additional ranges (ipv4/ipv6_cidr_blocks) are
// grown via :add-cidr-blocks / :remove-cidr-blocks (primary is never removable).
// DhcpOptions retired by design.
export interface Subnet {
  id: string;
  project_id?: string;
  created_at?: string;
  name: string;
  description?: string;
  labels?: Record<string, string>;
  network_id?: string;
  placement_type?: OpenEnum<"ZONAL" | "REGIONAL">;
  zone_id?: string;
  region_id?: string;
  ipv4_cidr_primary?: string;
  ipv6_cidr_primary?: string;
  ipv4_cidr_blocks?: string[];
  ipv6_cidr_blocks?: string[];
  route_table_id?: string;
}

export interface SubnetList {
  subnets: Subnet[];
  next_page_token?: string;
}

// Reference — форма kacho.cloud.reference.Reference на JSON-проводе (адаптер
// camelCase → snake_case в api/client.ts снимает конверт, поэтому `referrer.type`
// / `referrer.id` приезжают как есть).
//
// Объявлено ПО КОНТРАКТУ, а не по тому, что сегодня читает разметка. Прежняя
// редакция несла только `{type, id}` и свободную строку вместо перечисления —
// то есть была БЕДНЕЕ контракта на два поля, которые сервер уже шлёт:
// `Referrer.name` (том отдаёт имя машины, адрес — имя балансировщика, группа
// правил — имя сети) и `Reference.owned`. Значения приезжали на клиент и
// выбрасывались на границе типа; из-за этого домен storage был вынужден держать
// СВОЁ объявление того же контракта (#1467).
//
// `type` — перечисление `Reference.Type`, закрытое самим контрактом. Свободная
// строка здесь не «на будущее»: новое значение перечисления есть правка
// контракта, и она обязана приехать сюда вместе с ним, а не просочиться молча.
export type ReferenceType = "TYPE_UNSPECIFIED" | "MANAGED_BY" | "USED_BY";

// Referrer — дескриптор зависимости на чужой ресурс (class-C, graceful-dangling).
// `name` — зеркало имени НА МОМЕНТ ПРИВЯЗКИ, output-only и best-effort: оно может
// устареть, поэтому источником истины не является и во вход мутации не идёт.
// Читает его человек — без него в строке «кем используется» стоит машинный
// идентификатор, а для кросс-модульного потребителя резолвить имя запросом
// нечем: чужого ресурса в реестре модуля нет by construction.
export interface Referrer {
  type?: string;
  id?: string;
  name?: string;
}

export interface ResourceReference {
  referrer?: Referrer;
  type?: ReferenceType;
  // owned — референт ВЛАДЕЕТ ресурсом (его жизненный цикл связан с референтом), а
  // не просто ссылается: вложение, заказанное потребителем неявно, уедет вместе с
  // ним. Отвечает на вопрос «что случится при удалении». Output-only.
  //
  // proto3 не отличает ложь от отсутствия (`false` не сериализуется), поэтому
  // незаданное значение означает «не объявлено владение», а НЕ «точно не
  // удалится»; читающая разметка вправе пометить только истину.
  owned?: boolean;
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
  direction?: OpenEnum<"DIRECTION_UNSPECIFIED" | "INGRESS" | "EGRESS">;
  ports?: { from_port?: number; to_port?: number };
  protocol_name?: string;
  protocol_number?: number;
  // oneof target — РОВНО ОДНА ветвь, и ветвей в контракте ТРИ. Предустановленная
  // цель снята (номер и имя зарезервированы); сервис отвергает и ноль целей, и две.
  //
  // `cidr_group_id` до #512 здесь не объявлялся, хотя край его отдаёт: тип
  // описывал контракт БЕДНЕЕ, чем он есть, и всякий, кто читал правило через этот
  // тип, не знал о третьей цели вовсе — то же расхождение «два места об одном
  // предмете», что и лишняя ветвь, только с другой стороны.
  cidr_blocks?: { v4_cidr_blocks?: string[]; v6_cidr_blocks?: string[] };
  security_group_id?: string;
  cidr_group_id?: string;
}

export interface SecurityGroup {
  id: string;
  project_id?: string;
  created_at?: string;
  name: string;
  description?: string;
  labels?: Record<string, string>;
  network_id?: string;
  status?: OpenEnum<"STATUS_UNSPECIFIED" | "CREATING" | "ACTIVE" | "UPDATING" | "DELETING">;
  rules?: SecurityGroupRule[];
  default_for_network?: boolean;
}

export interface SecurityGroupList {
  security_groups: SecurityGroup[];
  next_page_token?: string;
}

// ====== compute / storage ======
//
// Здесь лежал набор Disk/DiskList/Image/ImageList/Snapshot/SnapshotList/
// AttachedDisk/InstanceNetworkInterface/Instance/InstanceList/DiskType/
// DiskTypeList/ComputeZone/ComputeZoneList — форма compute ДО раскола блочного
// хранения: family/os/min_disk_size/storage_size/product_ids/pooled у образа,
// disk_size/source_disk_id у снимка, platform_id/resources/service_account_id
// у машины. Ни одного из этих полей нет у ресурсов ствола, а `compute.Zone`
// вообще никогда не существовал: каталог размещения принадлежит geo.
//
// Импортёров у них не было ни одного — ни в shared, ни в остальных восьми
// приложениях (предикат: `grep -rw <имя>` по ui-future без node_modules); свои
// типы каждое приложение держит в собственном api/types.ts. Мёртвое объявление
// снятого контракта — это не «про запас»: следующий, кто станет типизировать
// storage из shared, скопирует отсюда форму, которой нет.
