// TS-типы для flat compute API (kacho.cloud.compute.v1). Ресурсы — плоские
// объекты (нет metadata/spec/status envelope). grpc-gateway сериализует proto
// snake_case → JSON snake_case (client.ts делает camel↔snake на wire).

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

// ====== compute: Instance ======
// proto: kacho.cloud.compute.v1.InstanceService (/compute/v1/instances).

export interface InstanceResources {
  memory?: string | number;
  cores?: string | number;
  core_fraction?: string | number;
  gpus?: string | number;
}

// AttachedDisk — том, подключённый к инстансу (boot_disk / secondary_disks).
// volume_id — cross-service ref на storage Volume (prefix "vol").
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
  platform_id?: string;
  resources?: InstanceResources;
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
  // Зеркала подключённых томов и интерфейсов (output-only проекции).
  boot_disk?: AttachedDisk;
  secondary_disks?: AttachedDisk[];
  network_interfaces?: InstanceNetworkInterface[];
  fqdn?: string;
  service_account_id?: string;
  // OCI-образ, из которого доставляется ОС инстанса (input).
  image?: string;
  // Разрешённый immutable content-digest образа (output-only).
  image_digest?: string;
  // Гарантированный baseline CPU на vCPU, в процентах (0..100).
  cpu_guarantee_percent?: number;
}

export interface InstanceList {
  instances: Instance[];
  next_page_token?: string;
}
