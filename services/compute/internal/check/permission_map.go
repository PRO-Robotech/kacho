// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	"github.com/PRO-Robotech/kacho/pkg/authz"
)

// FGA object types для kacho-compute (соответствуют FGA-модели kacho-iam).
const (
	objectTypeProject = "project"

	objectTypeInstance = "compute_instance"

	// MachineType — глобальный read-only справочник. Доступ —
	// "viewer on project:<project_id>" недоступен (request не несёт project_id).
	// Решение: справочник видим всем authenticated principal'ам на
	// cluster singleton (паттерн `viewer on cluster:cluster_kacho_root`).
	// Catalog object — "cluster:cluster_kacho_root": FGA model имеет `type cluster`
	// с `viewer: [user, user:*] or ...`. cluster_kacho_root — singleton (kacho-iam
	// ClusterSingletonID), один на весь deploy. Region/Zone serving снят — Geography
	// принадлежит kacho-geo.
	objectTypeCluster      = "cluster"
	clusterSingletonObject = "cluster_kacho_root"
)

const (
	// relationViewer / relationEditor — tier-relations. Сохраняются для
	// create-child (на parent project, F-7), top-level project-List (visibility
	// per-object идёт per-page через iam BatchCheck `viewer ∪ v_list`, не через
	// per-RPC Check) и MachineType read-only catalog (cluster — не verb-bearing). Для
	// object-self CRUD энфорс — verb-bearing relations ниже.
	relationViewer = "viewer"
	relationEditor = "editor"

	// verb-bearing relations (v_*) — enforcement резолвит object-self action на
	// verb, а не на tier (anchor-эпик «Explicit RBAC model 2026», D-6/D-6a:
	// доступ по verb развязан с tier). Материализуются per-object reconciler'ом
	// kacho-iam; consumer гейтит ими object-self RPC. Source of truth relation-имён
	// — kacho-iam/internal/authzmap; тут — backend view-only.
	//
	//	v_get    — чтение содержимого самого ресурса (Get / GetSerialPortOutput);
	//	v_list   — видимость операций/дочерних на самом ресурсе (ListOperations)
	//	           — НЕ top-level project-List;
	//	v_update — мутация самого ресурса (Update + lifecycle/attach/detach verb'ы);
	//	v_delete — удаление самого ресурса.
	relationVGet    = "v_get"
	relationVList   = "v_list"
	relationVUpdate = "v_update"
	relationVDelete = "v_delete"

	// relationSystemAdmin — cluster-scoped admin relation for kacho-only catalog
	// mutations (InternalMachineTypeService Create/Update/Delete).
	// Mirrors proto annotation `required_relation=system_admin`, object_type=cluster
	// in internal_machine_type_service.proto. Checked on the cluster singleton
	// (`system_admin on cluster:cluster_kacho_root`).
	relationSystemAdmin = "system_admin"
)

// staticClusterCatalog — extractor, всегда возвращающий (cluster, cluster_kacho_root).
// Используется для MachineType read-only RPC.
func staticClusterCatalog() authz.ObjectExtractor {
	return func(req any) (string, string, error) {
		return objectTypeCluster, clusterSingletonObject, nil
	}
}

// unscopedProject — extractor, всегда возвращающий (project, "*").
//
// Зеркалит `scope_extractor: {object_type: "project", from_request_field: "*"}`
// из permission_catalog.json — ровно то, что gateway шлёт в iam для RPC, чей
// request НЕ несёт скоупового поля (`access.*AccessBindingsRequest` знает только
// resource_id). Не «дырка»: iam на `Resource.ID == "*"` отдаёт
// `no path: unscoped resource` → PermissionDenied для всех, кроме cluster-admin
// (authorize_service.go). То есть оба тира выносят ОДНО И ТО ЖЕ решение.
//
// Почему это вообще нужно: без записи в карте corelib-интерсептор fail-close'ит
// RPC как DecisionUnmapped → `permission denied (rpc not mapped)`. Это не
// authz-решение, а дыра в проводке, маскирующаяся под отказ: cluster-admin,
// которому gateway доступ РАЗРЕШАЕТ, на backend'е получал 403 вместо честного
// Unimplemented от AAA-скелета (handler не переопределён, см. handler/*.go).
func unscopedProject() authz.ObjectExtractor {
	return func(req any) (string, string, error) {
		return objectTypeProject, "*", nil
	}
}

// PermissionMap — карта RPC → required relation+extract.
//
// Семантика per-RPC (enforcement по verb, развязан с tier):
//   - Create                          — parent scope `project:<project_id>`, tier `editor` (F-7)
//   - top-level List                  — parent scope `project:<project_id>`, tier `viewer`;
//     visibility per-object — через iam BatchCheck `viewer ∪ v_list` по СТРАНИЦЕ
//   - Get / GetSerialPortOutput       — на самом ресурсе, `v_get`
//   - ListOperations/Snapshot… (on res) — на самом ресурсе, `v_list`
//   - Update + lifecycle/attach verb'ы — на самом ресурсе, `v_update`
//   - Delete                          — на самом ресурсе, `v_delete`
//   - MachineType Get/List            — `viewer` на cluster singleton (не verb-bearing)
//   - OperationService.Get            — Public (op-id opaque, поллится creator'ом)
//
// scope-guard: для object-self RPC мы НЕ резолвим project_id из БД
// заранее — проверяем v_*-relation на самом ресурсе. reconciler kacho-iam
// материализует per-object `v_get/v_list/v_update/v_delete` для grant'а
// соответствующего verb'а; cluster-admin резолвится через iam short-circuit.
func PermissionMap() authz.RPCMap {
	return authz.RPCMap{
		// access-bindings — AAA-скелет (handler не переопределён → Unimplemented).
		// Записи зеркалят permission_catalog.json 1:1 (relation + unscoped project-scope),
		// см. unscopedProject(). Без них интерсептор отдавал «rpc not mapped».

		// access-bindings — AAA-скелет, зеркало каталога (см. DiskService выше).

		// access-bindings — AAA-скелет, зеркало каталога (см. DiskService выше).

		// =========================
		// InstanceService — lifecycle-heavy ресурс
		// =========================
		"/kacho.cloud.compute.v1.InstanceService/Get": {
			Relation: relationVGet,
			Extract: authz.StaticExtractor(objectTypeInstance, func(req any) (string, error) {
				return req.(*computev1.GetInstanceRequest).GetInstanceId(), nil
			}),
		},
		"/kacho.cloud.compute.v1.InstanceService/List": {
			Relation: relationViewer,
			Extract: authz.StaticExtractor(objectTypeProject, func(req any) (string, error) {
				return req.(*computev1.ListInstancesRequest).GetProjectId(), nil
			}),
		},
		"/kacho.cloud.compute.v1.InstanceService/Create": {
			Relation: relationEditor,
			Extract: authz.StaticExtractor(objectTypeProject, func(req any) (string, error) {
				return req.(*computev1.CreateInstanceRequest).GetProjectId(), nil
			}),
		},
		"/kacho.cloud.compute.v1.InstanceService/Update": {
			Relation: relationVUpdate,
			Extract: authz.StaticExtractor(objectTypeInstance, func(req any) (string, error) {
				return req.(*computev1.UpdateInstanceRequest).GetInstanceId(), nil
			}),
		},
		"/kacho.cloud.compute.v1.InstanceService/UpdateMetadata": {
			Relation: relationVUpdate,
			Extract: authz.StaticExtractor(objectTypeInstance, func(req any) (string, error) {
				return req.(*computev1.UpdateInstanceMetadataRequest).GetInstanceId(), nil
			}),
		},
		"/kacho.cloud.compute.v1.InstanceService/Delete": {
			Relation: relationVDelete,
			Extract: authz.StaticExtractor(objectTypeInstance, func(req any) (string, error) {
				return req.(*computev1.DeleteInstanceRequest).GetInstanceId(), nil
			}),
		},
		"/kacho.cloud.compute.v1.InstanceService/GetSerialPortOutput": {
			Relation: relationVGet,
			Extract: authz.StaticExtractor(objectTypeInstance, func(req any) (string, error) {
				return req.(*computev1.GetInstanceSerialPortOutputRequest).GetInstanceId(), nil
			}),
		},
		"/kacho.cloud.compute.v1.InstanceService/Start": {
			Relation: relationVUpdate,
			Extract: authz.StaticExtractor(objectTypeInstance, func(req any) (string, error) {
				return req.(*computev1.StartInstanceRequest).GetInstanceId(), nil
			}),
		},
		"/kacho.cloud.compute.v1.InstanceService/Stop": {
			Relation: relationVUpdate,
			Extract: authz.StaticExtractor(objectTypeInstance, func(req any) (string, error) {
				return req.(*computev1.StopInstanceRequest).GetInstanceId(), nil
			}),
		},
		"/kacho.cloud.compute.v1.InstanceService/Restart": {
			Relation: relationVUpdate,
			Extract: authz.StaticExtractor(objectTypeInstance, func(req any) (string, error) {
				return req.(*computev1.RestartInstanceRequest).GetInstanceId(), nil
			}),
		},
		"/kacho.cloud.compute.v1.InstanceService/AttachDisk": {
			Relation: relationVUpdate,
			Extract: authz.StaticExtractor(objectTypeInstance, func(req any) (string, error) {
				return req.(*computev1.AttachInstanceDiskRequest).GetInstanceId(), nil
			}),
		},
		"/kacho.cloud.compute.v1.InstanceService/DetachDisk": {
			Relation: relationVUpdate,
			Extract: authz.StaticExtractor(objectTypeInstance, func(req any) (string, error) {
				return req.(*computev1.DetachInstanceDiskRequest).GetInstanceId(), nil
			}),
		},
		"/kacho.cloud.compute.v1.InstanceService/AddOneToOneNat": {
			Relation: relationVUpdate,
			Extract: authz.StaticExtractor(objectTypeInstance, func(req any) (string, error) {
				return req.(*computev1.AddInstanceOneToOneNatRequest).GetInstanceId(), nil
			}),
		},
		"/kacho.cloud.compute.v1.InstanceService/RemoveOneToOneNat": {
			Relation: relationVUpdate,
			Extract: authz.StaticExtractor(objectTypeInstance, func(req any) (string, error) {
				return req.(*computev1.RemoveInstanceOneToOneNatRequest).GetInstanceId(), nil
			}),
		},
		"/kacho.cloud.compute.v1.InstanceService/UpdateNetworkInterface": {
			Relation: relationVUpdate,
			Extract: authz.StaticExtractor(objectTypeInstance, func(req any) (string, error) {
				return req.(*computev1.UpdateInstanceNetworkInterfaceRequest).GetInstanceId(), nil
			}),
		},
		// Attach/DetachNetworkInterface — NIC lifecycle verb'ы на самом Instance
		// (proto required_relation=v_update). Были пропущены при COMP-1 (map имел
		// UpdateNetworkInterface, но не Attach/Detach) → корелиб authz fail-closes
		// "rpc not mapped" на :attachNetworkInterface/:detachNetworkInterface.
		"/kacho.cloud.compute.v1.InstanceService/AttachNetworkInterface": {
			Relation: relationVUpdate,
			Extract: authz.StaticExtractor(objectTypeInstance, func(req any) (string, error) {
				return req.(*computev1.AttachInstanceNetworkInterfaceRequest).GetInstanceId(), nil
			}),
		},
		"/kacho.cloud.compute.v1.InstanceService/DetachNetworkInterface": {
			Relation: relationVUpdate,
			Extract: authz.StaticExtractor(objectTypeInstance, func(req any) (string, error) {
				return req.(*computev1.DetachInstanceNetworkInterfaceRequest).GetInstanceId(), nil
			}),
		},
		"/kacho.cloud.compute.v1.InstanceService/ListOperations": {
			Relation: relationVList,
			Extract: authz.StaticExtractor(objectTypeInstance, func(req any) (string, error) {
				return req.(*computev1.ListInstanceOperationsRequest).GetInstanceId(), nil
			}),
		},
		// access-bindings — AAA-скелет, зеркало каталога (см. DiskService выше).
		"/kacho.cloud.compute.v1.InstanceService/ListAccessBindings": {
			Relation:   relationViewer,
			Extract:    unscopedProject(),
			Permission: "compute.access_bindingses.listAccessBindings",
		},
		"/kacho.cloud.compute.v1.InstanceService/SetAccessBindings": {
			Relation:   relationEditor,
			Extract:    unscopedProject(),
			Permission: "compute.access_bindingses.setAccessBindings",
		},
		"/kacho.cloud.compute.v1.InstanceService/UpdateAccessBindings": {
			Relation:   relationEditor,
			Extract:    unscopedProject(),
			Permission: "compute.access_bindingses.updateAccessBindings",
		},
		"/kacho.cloud.compute.v1.InstanceService/Relocate": {
			Relation: relationVUpdate,
			Extract: authz.StaticExtractor(objectTypeInstance, func(req any) (string, error) {
				return req.(*computev1.RelocateInstanceRequest).GetInstanceId(), nil
			}),
		},
		"/kacho.cloud.compute.v1.InstanceService/SimulateMaintenanceEvent": {
			Relation: relationVUpdate,
			Extract: authz.StaticExtractor(objectTypeInstance, func(req any) (string, error) {
				return req.(*computev1.SimulateInstanceMaintenanceEventRequest).GetInstanceId(), nil
			}),
		},

		// =========================
		// Internal admin CRUD on the cluster-internal listener (:9091). The
		// internal listener runs the same per-RPC FGA Check as public and the
		// pinned corelib authz.Interceptor has NO name-based "methodIsInternal"
		// fallback — every RPC absent from this map fails closed with
		// PermissionDenied ("rpc not mapped"), unary AND stream alike. So every
		// internal RPC MUST have an explicit entry: relation-gated
		// (`system_admin on cluster:cluster_kacho_root` — mirrors proto
		// `required_relation=system_admin`, object_type=cluster) or
		// Public=true for an explicit exempt (like InternalWatchService/Watch
		// further down, and OperationService.Get/Cancel above).

		// MachineType — read-only sizing catalog (viewer on cluster singleton).
		// Was unmapped → the public Get/List used across machine-type +
		// instance/list-filter suites (and Instance.Create's machineTypeId
		// resolve) failed closed "rpc not mapped".
		"/kacho.cloud.compute.v1.MachineTypeService/Get": {
			Relation: relationViewer,
			Extract:  staticClusterCatalog(),
		},
		"/kacho.cloud.compute.v1.MachineTypeService/List": {
			Relation: relationViewer,
			Extract:  staticClusterCatalog(),
		},

		// InternalMachineTypeService — kacho-only sizing-catalog admin CRUD
		// (Create/Update/Delete) on :9091 (proto required_relation=system_admin,
		// object_type=cluster in internal_machine_type_service.proto). Omitted at
		// COMP-1: since the pinned corelib authz.Interceptor has NO methodIsInternal
		// fallback, an unmapped internal RPC fails closed PermissionDenied ("rpc not
		// mapped") — MachineType admin-seed 403'd and cascaded (Instance/list-filter
		// reference machineTypeId). system_admin @ cluster:cluster_kacho_root.
		"/kacho.cloud.compute.v1.InternalMachineTypeService/Create": {
			Relation: relationSystemAdmin,
			Extract:  staticClusterCatalog(),
		},
		"/kacho.cloud.compute.v1.InternalMachineTypeService/Update": {
			Relation: relationSystemAdmin,
			Extract:  staticClusterCatalog(),
		},
		"/kacho.cloud.compute.v1.InternalMachineTypeService/Delete": {
			Relation: relationSystemAdmin,
			Extract:  staticClusterCatalog(),
		},

		// InternalWatchService/Watch — internal server-stream over compute_outbox
		// (LISTEN/NOTIFY; see internal/handler/internal_watch_handler.go), registered
		// on the same internal :9091 listener/authzIntr.Stream() chain as the
		// catalog-admin RPCs above (cmd/compute/main.go).
		//
		// ScopeFiltered, NOT Public. The distinction is the whole point of this entry,
		// so it is worth stating why, because the previous entry said Public and
		// justified it by a parity that did not hold.
		//
		// True premise: there is no per-object FGA target for a cursor-based outbox
		// tail. The request names no resource — it carries a cursor and an optional
		// kind list — while the rows it returns belong to objects with individual
		// owners. So a single per-RPC Check has nothing to ask about, and asking one
		// anyway would deny the whole call `no path` before any narrowing could run.
		//
		// Wrong conclusion, now corrected: that this makes the RPC exempt "the same
		// way as OperationService.Get/Cancel below". The mechanism was the same; the
		// thing that makes it safe there was absent here. Those two authorise ON THE
		// DATA — the operation's creating principal is a predicate in the SQL WHERE
		// clause, so a non-owner gets NotFound (operation_handler.go GetOwned /
		// CancelOwned). The stream had no predicate of any kind, and `Public` makes
		// the interceptor answer allow BEFORE the subject is read, so one call
		// returned every tenant's change journal — full resource snapshots, user-data
		// included.
		//
		// What ScopeFiltered means here, concretely: the interceptor still performs no
		// single-object Check (correct, per the true premise above) but DOES require a
		// subject — unconditionally, since a scope-filtered RPC has no per-RPC Check
		// underneath to refuse an unidentified caller (pkg/authz/interceptor.go). The
		// handler then narrows the stream per delivered row, in partitions of ≤100,
		// fail-closed on an unanswerable question. The marker also places the RPC
		// under the production boot guard for that filter (requireListFilter), which
		// `Public` did not.
		//
		// The entry must exist either way: the pinned corelib has no name-based
		// "methodIsInternal" skip — an unmapped RPC, stream included, fails closed
		// with PermissionDenied.
		"/kacho.cloud.compute.v1.InternalWatchService/Watch": {
			ScopeFiltered: true,
			Permission:    "compute.instances.list",
		},

		// =========================
		// OperationService (LRO; viewer на operation-id).
		// Proto-пакет — `kacho.cloud.operation` (без `.v1`); fullMethod
		// соответственно `/kacho.cloud.operation.OperationService/*`.
		// =========================
		// Operation poll is NOT gated by the FGA per-RPC Check: the FGA model has
		// no `compute_operation` object type and no per-operation tuples are
		// emitted, so a `viewer on compute_operation:<id>` Check has no path and
		// every poll — including the creating client's own poll right after a
		// successful mutation — would be denied. The api-gateway already marks
		// these `<exempt>`; Public here keeps the compute interceptor consistent
		// (a map-miss would fail-closed with ErrUnmapped).
		//
		// Access is instead bound to the operation's creating principal INSIDE the
		// OperationHandler (GetOwned/CancelOwned, ownership-predicate in SQL WHERE)
		// — a non-owner gets NotFound, no BOLA on the opaque id. See
		// internal/handler/operation_handler.go.
		"/kacho.cloud.operation.OperationService/Get":    {Public: true},
		"/kacho.cloud.operation.OperationService/Cancel": {Public: true},
	}
}
