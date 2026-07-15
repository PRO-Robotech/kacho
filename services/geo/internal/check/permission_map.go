// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
	"github.com/PRO-Robotech/kacho/pkg/authz"
)

// FGA-скоупинг для kacho-geo: Region/Zone — глобальные cluster-scoped read-only
// справочники. Публичные Get/List гейтятся viewer-tier на cluster-синглтоне;
// admin Internal* CRUD — system_admin на том же синглтоне (зеркалит аннотации
// proto geo.v1: required_relation viewer / system_admin,
// scope_extractor.object_type=cluster). cluster_kacho_root — ClusterSingletonID
// из kacho-iam, один на деплой.
const (
	objectTypeCluster      = "cluster"
	clusterSingletonObject = "cluster_kacho_root"

	relationViewer      = "viewer"
	relationSystemAdmin = "system_admin"
)

// staticClusterCatalog — extractor, всегда возвращающий (cluster, cluster_kacho_root).
func staticClusterCatalog() authz.ObjectExtractor {
	return func(req any) (string, string, error) {
		return objectTypeCluster, clusterSingletonObject, nil
	}
}

// PermissionMap сопоставляет каждый geo-RPC → требуемое relation + object extractor.
//
// Публичное чтение (Region/Zone Get/List) → viewer на cluster:cluster_kacho_root.
// Admin Internal* CRUD → system_admin на cluster:cluster_kacho_root.
//
// Permission-строки используют корректный geo.* FQN. Интерсептор сейчас гейтит по
// Relation; Permission несется ради будущей fine-grained модели и parity с
// permission_catalog в api-gateway.
func PermissionMap() authz.RPCMap {
	return authz.RPCMap{
		// ---- публичный read-only справочник (viewer floor) ----
		"/kacho.cloud.geo.v1.RegionService/Get": {
			Relation:   relationViewer,
			Extract:    staticClusterCatalog(),
			Permission: "geo.regions.get",
		},
		"/kacho.cloud.geo.v1.RegionService/List": {
			Relation:   relationViewer,
			Extract:    staticClusterCatalog(),
			Permission: "geo.regions.list",
		},
		"/kacho.cloud.geo.v1.ZoneService/Get": {
			Relation:   relationViewer,
			Extract:    staticClusterCatalog(),
			Permission: "geo.zones.get",
		},
		"/kacho.cloud.geo.v1.ZoneService/List": {
			Relation:   relationViewer,
			Extract:    staticClusterCatalog(),
			Permission: "geo.zones.list",
		},

		// ---- admin CRUD (Internal*, system_admin) ----
		"/kacho.cloud.geo.v1.InternalRegionService/Create": {
			Relation:   relationSystemAdmin,
			Extract:    staticClusterCatalog(),
			Permission: "geo.regions.create",
		},
		"/kacho.cloud.geo.v1.InternalRegionService/Update": {
			Relation:   relationSystemAdmin,
			Extract:    staticClusterCatalog(),
			Permission: "geo.regions.update",
		},
		"/kacho.cloud.geo.v1.InternalRegionService/Delete": {
			Relation:   relationSystemAdmin,
			Extract:    staticClusterCatalog(),
			Permission: "geo.regions.delete",
		},
		"/kacho.cloud.geo.v1.InternalZoneService/Create": {
			Relation:   relationSystemAdmin,
			Extract:    staticClusterCatalog(),
			Permission: "geo.zones.create",
		},
		"/kacho.cloud.geo.v1.InternalZoneService/Update": {
			Relation:   relationSystemAdmin,
			Extract:    staticClusterCatalog(),
			Permission: "geo.zones.update",
		},
		"/kacho.cloud.geo.v1.InternalZoneService/Delete": {
			Relation:   relationSystemAdmin,
			Extract:    staticClusterCatalog(),
			Permission: "geo.zones.delete",
		},

		// ---- LRO Operation-envelope (ReBAC-exempt, owner-scoped в handler) ----
		// Все admin Region/Zone Create/Update/Delete асинхронны и возвращают
		// Operation, который клиент поллит через OperationService.Get. Оба RPC
		// подняты на public (:9090) и internal (:9091) листенерах и помечены
		// Public:true.
		//
		// `Public:true` тут означает «снят per-RPC ReBAC-Check», а НЕ
		// «unauthenticated / без owner-гейта»: в FGA-модели нет object type
		// `geo_operation` и per-operation tuple'ы не эмитятся, поэтому
		// `Check viewer on geo_operation:<id>` не имеет пути и отверг бы даже
		// owner-poll. Anti-anon (principal-цепочка на обоих листенерах) сохраняется.
		//
		// Ownership энфорсится В HANDLER'е (sec-hardening-r3): OperationHandler.
		// Get/Cancel через ownership-scoped GetOwned/CancelOwned (owner —
		// creator-principal из доверенного ctx, предикат в SQL WHERE; чужой op-id →
		// NotFound, no-leak). Op-id опакен, но это прямой object-reference — раньше
		// ЕДИНСТВЕННЫМ барьером был негадаемый id (capability-style), что позволяло
		// BOLA (read/cancel чужой in-flight admin-мутации по утёкшему id). Теперь
		// закрыто owner-предикатом. Анти-регресс: пометка Public тут НЕ освобождает
		// Operation RPC ни от anti-anon, ни от ownership. Зеркалит kacho-vpc.
		"/kacho.cloud.operation.OperationService/Get":    {Public: true},
		"/kacho.cloud.operation.OperationService/Cancel": {Public: true},
	}
}
