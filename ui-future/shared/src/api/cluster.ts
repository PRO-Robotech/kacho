// Cluster API — KAC-196 InternalClusterService bindings.
//
// REST endpoints (api-gateway internal mux только, cluster-internal listener;
// см. workspace CLAUDE.md §«Запреты» #6):
//   GET    /iam/v1/internal/cluster                       → Cluster (sync)
//   GET    /iam/v1/internal/cluster/admins                → {admins: ClusterAdminEntry[]}
//   POST   /iam/v1/internal/cluster/admins                → Operation
//   DELETE /iam/v1/internal/cluster/admins/{subject_id}   → Operation
//
// Wire-format quirk: grpc-gateway сериализует proto-сообщения в JSON camelCase;
// `api/client.ts` адаптер уже конвертирует camelCase ↔ snake_case на границе,
// поэтому здесь поля snake_case (как в proto-схеме).
//
// Authorization: каждый RPC требует FGA-relation `admin@cluster:cluster_kacho_root`
// (computed `system_admin OR emergency_admin`). Ordinary user без этого
// permission'а получит HTTP 403 от api-gateway middleware.

import { api } from "./client";
import type { Operation } from "./types";

/** Cluster singleton — единственная row с id `cluster_kacho_root`. */
export interface Cluster {
  id: string;
  name?: string;
  description?: string;
  created_at?: string;
}

/** Proto enum `ClusterGrantSubjectType` — в этой версии только USER (D-2). */
export type ClusterGrantSubjectType = "USER" | "SERVICE_ACCOUNT" | string;

/**
 * Денормализованный snapshot активной cluster_admin_grants row + JOIN на
 * kacho_iam.users. Email/display_name — output-only.
 */
export interface ClusterAdminEntry {
  cluster_admin_grant_id: string;
  subject_type: ClusterGrantSubjectType;
  subject_id: string;
  subject_email: string;
  subject_display_name: string;
  /** `usr_<17>` или литерал `"bootstrap"` для seed-grant'а. */
  granted_by_user_id: string;
  granted_by_email: string;
  granted_at?: string;
}

export interface ListClusterAdminsResponse {
  admins: ClusterAdminEntry[];
}

const CLUSTER = {
  root: "/iam/v1/internal/cluster",
  admins: "/iam/v1/internal/cluster/admins",
} as const;

export const clusterApi = {
  /** GET /iam/v1/internal/cluster — singleton Cluster row. */
  get: (): Promise<Cluster> => api.get<Cluster>(CLUSTER.root),

  /** GET /iam/v1/internal/cluster/admins — список активных admins. */
  listAdmins: (): Promise<ClusterAdminEntry[]> =>
    api.get<ListClusterAdminsResponse>(CLUSTER.admins).then((r) => r.admins ?? []),

  /**
   * POST /iam/v1/internal/cluster/admins — выдать admin указанному USER.
   * Backend идемпотентный: повторный grant активному admin'у возвращает success
   * с тем же `cluster_admin_grant_id` (acceptance D-4).
   */
  grantAdmin: (subject_id: string): Promise<{ operation: Operation }> =>
    api.create(CLUSTER.admins, {
      subject_type: "USER",
      subject_id,
    }),

  /**
   * DELETE /iam/v1/internal/cluster/admins/{subject_id} — отозвать admin'а.
   * Backend guards: self-revoke (D-5) и last-admin-revoke (D-6) → FailedPrecondition;
   * non-existent / уже-отозванный → NotFound (D-12).
   */
  revokeAdmin: (subject_id: string): Promise<{ operation: Operation }> =>
    api.delete(`${CLUSTER.admins}/${encodeURIComponent(subject_id)}`),
};
