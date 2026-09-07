// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/services/compute/internal/check"
)

// catalogAdminMutations — internal catalog-admin RPC, которые ОБЯЗАНЫ быть
// замаплены на FGA-relation `system_admin` @ `cluster:cluster_root`
// (proto-аннотация `required_relation=system_admin`, object_type=cluster в
// internal_catalog_service.proto). Эти RPC живут на internal listener'е :9091 —
// он гоняет тот же authzIntr, что и public, поэтому каждая catalog-мутация должна
// резолвиться в Check, а не пропускаться methodIsInternal-фолбэком.
// Internal{Zone,Region}Service serving removed — Geography is owned by kacho-geo;
// InternalDiskTypeService removed — block storage is owned by kacho-storage.
// InternalMachineTypeService is the one compute-owned catalog admin left (every
// internal RPC MUST be mapped or it fails closed "rpc not mapped" — MachineType was
// omitted once, 403'ing the admin-seed and cascading into instance/list-filter which
// reference machineTypeId).
var catalogAdminMutations = []string{
	"/kacho.cloud.compute.v1.InternalMachineTypeService/Create",
	"/kacho.cloud.compute.v1.InternalMachineTypeService/Update",
	"/kacho.cloud.compute.v1.InternalMachineTypeService/Delete",
}

// TestPermissionMap_CatalogAdmin_SystemAdminOnCluster — каждая catalog-admin
// мутация замаплена → relation "system_admin", object "cluster:cluster_root".
func TestPermissionMap_CatalogAdmin_SystemAdminOnCluster(t *testing.T) {
	m := check.PermissionMap()
	for _, fullMethod := range catalogAdminMutations {
		entry, ok := m[fullMethod]
		require.Truef(t, ok, "catalog-admin RPC %s must be present in PermissionMap (internal listener runs authz Check)", fullMethod)
		require.Equalf(t, "system_admin", entry.Relation, "%s: required_relation must be system_admin (proto annotation)", fullMethod)
		require.NotNilf(t, entry.Extract, "%s: must carry an ObjectExtractor", fullMethod)
		require.Falsef(t, entry.Public, "%s: must NOT be Public — it is relation-gated", fullMethod)

		objType, objID, err := entry.Extract(nil)
		require.NoErrorf(t, err, "%s: cluster-scoped extractor must not error on any request", fullMethod)
		require.Equalf(t, "cluster", objType, "%s: object_type must be cluster", fullMethod)
		require.Equalf(t, "cluster_root", objID, "%s: object_id must be cluster singleton", fullMethod)
	}
}

// catalogPublicReads — the PUBLIC read-only catalog RPCs (MachineType Get/List).
// These run authz Check on the public listener too; an unmapped one fails closed
// "rpc not mapped". MachineTypeService/Get+List were omitted once → the public
// catalog reads used across the machine-type + instance/list-filter suites (and
// Instance.Create machineTypeId resolve) 403'd.
var catalogPublicReads = []string{
	"/kacho.cloud.compute.v1.MachineTypeService/Get",
	"/kacho.cloud.compute.v1.MachineTypeService/List",
}

// TestPermissionMap_CatalogReads_StandOnAProducedRelation — публичное чтение
// каталога стоит на отношении `viewer` кластерного синглтона, у которого ЕСТЬ
// производитель: системная выдача с подстановочным субъектом.
// Подстановка выполняет отношение за всякого аутентифицированного, поэтому
// арендатор с нулём собственных выдач каталог читает.
//
// ДВЕ ПРЕЖНИЕ РЕДАКЦИИ, И ОБЕ БЫЛИ ВЕРНЫ НАПОЛОВИНУ. Первая требовала `viewer`,
// которого не производил никто, — проверка отвечала отказом каждому, и машину
// нельзя было создать вовсе. Вторая требовала `<exempt>` — отказ исчез,
// но вместе с ним исчезла и видимость: доступ, выданный освобождением, не
// показывается перечислением выдач и закрывается только выкаткой. Здесь верны
// оба свойства сразу, потому что у отношения появился видимый и отзываемый
// производитель.
func TestPermissionMap_CatalogReads_StandOnAProducedRelation(t *testing.T) {
	m := check.PermissionMap()
	for _, fullMethod := range catalogPublicReads {
		entry, ok := m[fullMethod]
		require.Truef(t, ok,
			"%s: чтение каталога обязано попасть в карту проверок — иначе край его не гейтит вовсе",
			fullMethod)
		require.Equalf(t, "viewer", entry.Relation,
			"%s: отношение обязано быть тем, которое производит системная выдача", fullMethod)
	}
}

// TestPermissionMap_CatalogAdmin_EnforcedByInterceptor — sanity at interceptor
// level: a mapped catalog mutation routes to a real Check (system_admin on
// cluster) rather than being skipped, and deny fail-closes with PermissionDenied.
func TestPermissionMap_CatalogAdmin_EnforcedByInterceptor(t *testing.T) {
	intr, calls := newTestInterceptor(t, func(_ context.Context, subject, relation, object string) (bool, error) {
		require.Equal(t, "user:usr_admin", subject)
		require.Equal(t, "system_admin", relation)
		require.Equal(t, "cluster:cluster_root", object)
		return true, nil
	})
	uIntr := intr.Unary()
	called := false
	handler := func(ctx context.Context, req any) (any, error) { called = true; return "ok", nil }
	info := &grpc.UnaryServerInfo{FullMethod: "/kacho.cloud.compute.v1.InternalMachineTypeService/Create"}
	ctx := principalCtx("user", "usr_admin")

	resp, err := uIntr(ctx, struct{}{}, info, handler)
	require.NoError(t, err)
	require.Equal(t, "ok", resp)
	require.True(t, called)
	require.Equal(t, 1, *calls, "catalog mutation must trigger exactly one Check, not be bypassed")
}

// Здесь стояла проба записи каталога для потока журнала изменений: она требовала
// `ScopeFiltered` и запрещала `Public`. Поток снят, карта прав выводится из
// контракта, поэтому запись исчезла вместе с ним, а проба осталась бы утверждать
// про несуществующее.
//
// РАЗБОР, ради которого этот абзац написан, а не удалён. `Public` отвечает allow
// ДО чтения субъекта: субъект не извлекается, модель не спрашивается. Для соседних
// RPC операций это защитимо — там авторизация живёт в данных, владелец стоит
// предикатом в условии выборки, и не-владелец получает промах. У потока такого
// предиката не было: его запрос не называл ни субъекта, ни проекта, а телом шёл
// снимок ресурса целиком. `Public` на нём означал отсутствие авторизации вовсе.
// Комментарий рядом при этом заявлял паритет с операциями — механизм был тот же,
// а то, что делало его безопасным, отсутствовало.
//
// Урок применим к ЛЮБОМУ будущему стриму: «как у соседа» — не довод, пока не
// назван предикат, на котором сосед авторизует. Имя снятого RPC стоит в надгробии
// `retiredRPCSurface`, поэтому вернуться молча оно не может.
