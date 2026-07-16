// TS-типы для flat storage API (kacho.cloud.storage.v1). Ресурсы — плоские
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

// ====== reference (output-only used_by) ======
export interface ResourceReference {
  referrer?: { type?: string; id?: string };
  type?: string;
}

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
  block_size?: string | number;
  source_snapshot_id?: string;
  status?: "STATUS_UNSPECIFIED" | "CREATING" | "AVAILABLE" | "IN_USE" | "DELETING" | "ERROR" | string;
  attachments?: VolumeAttachment[];
  used_by?: ResourceReference[];
}

export interface VolumeList {
  volumes: Volume[];
  next_page_token?: string;
}

// ====== storage: Snapshot ======
export interface Snapshot {
  id: string;
  project_id?: string;
  created_at?: string;
  name?: string;
  description?: string;
  labels?: Record<string, string>;
  source_volume_id?: string;
  size_bytes?: string | number;
  status?: "STATUS_UNSPECIFIED" | "CREATING" | "READY" | "DELETING" | "ERROR" | string;
}

export interface SnapshotList {
  snapshots: Snapshot[];
  next_page_token?: string;
}

// ====== storage: DiskType (read-only catalog) ======
export interface DiskType {
  id: string;
  name?: string;
  description?: string;
  zone_ids?: string[];
  performance_tier?: string;
}

export interface DiskTypeList {
  disk_types: DiskType[];
  next_page_token?: string;
}
