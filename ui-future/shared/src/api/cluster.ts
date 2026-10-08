// Cluster API — публичная ClusterService (PRO-Robotech/kaname#661) через край
// (kacho#3093). Консоль ходит ТОЛЬКО сюда (kacho#3094): внутренний близнец
// `InternalClusterService` живёт на cluster-internal слушателе, и на посадке
// внешнего края его маршрутов нет вовсе.
//
//   GET    /iam/v1/cluster                       → Cluster (sync)
//   GET    /iam/v1/cluster/admins                → {admins: ClusterAdminEntry[]}
//   POST   /iam/v1/cluster/admins                → Operation
//   DELETE /iam/v1/cluster/admins/{subject_id}   → Operation
//
// Wire-format quirk: grpc-gateway сериализует proto-сообщения в JSON camelCase;
// `api/client.ts` адаптер уже конвертирует camelCase ↔ snake_case на границе,
// поэтому здесь поля snake_case (как в proto-схеме).
//
// Права: каждый RPC — `system_admin` на `cluster:cluster_root`, с теми же
// аннотациями, что у внутреннего близнеца. Выдача и снятие несут пол уровня
// уверенности «2»: на сессии «1» край отвечает вызовом повышения (RFC 9470), и
// общий клиент (`api/client.ts`) открывает окно подтверждения и повторяет
// действие один раз. Вызывающий без права получает 403 — отказ словами.

import { api } from "./client";
import type { OpenEnum, Operation } from "./types";

/** Cluster singleton — единственная row с id `cluster_root`. */
export interface Cluster {
  id: string;
  name?: string;
  description?: string;
  created_at?: string;
}

/** Proto enum `ClusterGrantSubjectType` — в этой версии только USER (D-2). */
export type ClusterGrantSubjectType = OpenEnum<"USER" | "SERVICE_ACCOUNT">;

/**
 * Денормализованный snapshot активной cluster_admin_grants row + JOIN на
 * kaname.users. Email/display_name — output-only.
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
  root: "/iam/v1/cluster",
  admins: "/iam/v1/cluster/admins",
} as const;

export const clusterApi = {
  /** GET /iam/v1/cluster — singleton Cluster row. */
  get: (): Promise<Cluster> => api.get<Cluster>(CLUSTER.root),

  /** GET /iam/v1/cluster/admins — список активных admins. */
  listAdmins: (): Promise<ClusterAdminEntry[]> =>
    api.get<ListClusterAdminsResponse>(CLUSTER.admins).then((r) => r.admins ?? []),

  /**
   * POST /iam/v1/cluster/admins — выдать admin указанному USER.
   * Backend идемпотентный: повторный grant активному admin'у возвращает success
   * с тем же `cluster_admin_grant_id` (acceptance D-4).
   */
  grantAdmin: (subject_id: string): Promise<{ operation: Operation }> =>
    api.create(CLUSTER.admins, {
      subject_type: "USER",
      subject_id,
    }),

  /**
   * DELETE /iam/v1/cluster/admins/{subject_id} — отозвать admin'а.
   * Backend guards: self-revoke (D-5) и last-admin-revoke (D-6) → FailedPrecondition;
   * non-existent / уже-отозванный → NotFound (D-12).
   */
  revokeAdmin: (subject_id: string): Promise<{ operation: Operation }> =>
    api.delete(`${CLUSTER.admins}/${encodeURIComponent(subject_id)}`),
};
