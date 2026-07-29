// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import "testing"

func TestPermissionMap_tiersAndPermissions(t *testing.T) {
	m := PermissionMap()

	// GEO-1-20/21: Region/Zone Get/List — ambient project-scope EXEMPT
	// (authN-only, снят per-RPC ReBAC-Check). Regression-lock: если кто-то вернёт
	// viewer/scope-Check на catalog-read — zero-binding tenant снова получит отказ.
	read := []string{
		"/kacho.cloud.geo.v1.RegionService/Get",
		"/kacho.cloud.geo.v1.RegionService/List",
		"/kacho.cloud.geo.v1.ZoneService/Get",
		"/kacho.cloud.geo.v1.ZoneService/List",
	}
	for _, method := range read {
		e, ok := m.Lookup(method)
		if !ok {
			t.Fatalf("%s missing from PermissionMap", method)
		}
		if !e.Public {
			t.Errorf("%s Public = false, want true (ambient project-scope EXEMPT, GEO-1 documented-exception)", method)
		}
	}

	admin := []struct{ method, perm string }{
		{"/kacho.cloud.geo.v1.InternalRegionService/Create", "geo.regions.create"},
		{"/kacho.cloud.geo.v1.InternalRegionService/Update", "geo.regions.update"},
		{"/kacho.cloud.geo.v1.InternalRegionService/Delete", "geo.regions.delete"},
		{"/kacho.cloud.geo.v1.InternalZoneService/Create", "geo.zones.create"},
		{"/kacho.cloud.geo.v1.InternalZoneService/Update", "geo.zones.update"},
		{"/kacho.cloud.geo.v1.InternalZoneService/Delete", "geo.zones.delete"},
	}
	for _, a := range admin {
		e, ok := m.Lookup(a.method)
		if !ok {
			t.Fatalf("%s missing from PermissionMap", a.method)
		}
		if e.Relation != relationSystemAdmin {
			t.Errorf("%s relation = %q, want system_admin", a.method, e.Relation)
		}
		if e.Permission != a.perm {
			t.Errorf("%s permission = %q, want %q", a.method, e.Permission, a.perm)
		}
	}

	// Защита от регрессии опечатки regionses/zoneses в permission-строках.
	for method, e := range m {
		if e.Permission == "geo.regionses.list" || e.Permission == "geo.zoneses.list" {
			t.Errorf("%s carries the regionses/zoneses typo: %q", method, e.Permission)
		}
	}

	// Каждая gated-запись должна резолвиться в (cluster, cluster_kacho_root).
	// Public-exempt записи (OperationService LRO) не имеют Extract — пропускаем.
	for method, e := range m {
		if e.Public {
			continue
		}
		ot, oid, err := e.Extract(nil)
		if err != nil {
			t.Fatalf("%s extract err = %v", method, err)
		}
		if ot != objectTypeCluster || oid != clusterSingletonObject {
			t.Errorf("%s extract = (%s,%s), want (cluster,cluster_kacho_root)", method, ot, oid)
		}
	}
}

// TestPermissionMap_publicSetIsClosed — множество RPC с `Public:true` ЗАКРЫТО.
//
// Почему закрыто, а не «по вкусу»: на такой записи per-RPC Check отдаёт allow ДО
// извлечения субъекта, а principal-цепочка geo личность только проставляет и
// никого не отвергает — собственного барьера «нет принципала» у сервиса на этих
// RPC нет. Для шести перечисленных ниже это защитимо: четыре читают
// admin-curated ГЛОБАЛЬНЫЙ каталог, ответ которого ни от какой личности не
// зависит, а два LRO гейтятся владельцем в handler'е (ownership-предикат в SQL).
//
// Седьмая запись, добавленная «по аналогии», такой защиты не наследует: если её
// ответ зависит от вызывающего, она отдаст его любому, кто дозвонился. Поэтому
// новая запись обязана пройти через этот список — то есть через обоснование.
func TestPermissionMap_publicSetIsClosed(t *testing.T) {
	allowed := map[string]string{
		"/kacho.cloud.geo.v1.RegionService/Get":          "глобальный каталог, ответ не зависит от вызывающего",
		"/kacho.cloud.geo.v1.RegionService/List":         "глобальный каталог, ответ не зависит от вызывающего",
		"/kacho.cloud.geo.v1.ZoneService/Get":            "глобальный каталог, ответ не зависит от вызывающего",
		"/kacho.cloud.geo.v1.ZoneService/List":           "глобальный каталог, ответ не зависит от вызывающего",
		"/kacho.cloud.operation.OperationService/Get":    "owner-гейт в handler'е (GetOwned), запрос без личности ключа не получает",
		"/kacho.cloud.operation.OperationService/Cancel": "owner-гейт в handler'е (CancelOwned), запрос без личности ключа не получает",
	}

	var public []string
	for method, e := range PermissionMap() {
		if e.Public {
			public = append(public, method)
		}
	}
	if len(public) == 0 {
		t.Fatalf("в карте нет ни одной Public-записи — проверка не осмотрела ничего")
	}
	for _, method := range public {
		if _, ok := allowed[method]; !ok {
			t.Errorf("%s помечен Public:true, но не входит в закрытый список: на такой записи "+
				"per-RPC Check отдаёт allow ДО извлечения субъекта, и собственного барьера "+
				"«нет принципала» у geo нет. Либо гейтите RPC relation'ом, либо добавьте его "+
				"сюда с обоснованием, почему его ответ безопасен для запроса без личности", method)
		}
	}
	for method := range allowed {
		if e, ok := PermissionMap().Lookup(method); !ok || !e.Public {
			t.Errorf("%s числится в закрытом списке Public-записей, но таковой не является — "+
				"список пережил свой предмет", method)
		}
	}
	t.Logf("Public-записей: %d (закрытый список: %d)", len(public), len(allowed))
}

// TestPermissionMap_operationServiceLROExempt защищает от регрессии, при которой
// OperationService.Get/Cancel отсутствуют в PermissionMap. Оба RPC подняты на
// public (:9090) и internal (:9091) листенерах и проходят fail-closed authz-
// interceptor: не-замапленный RPC → PermissionDenied, что делает поллинг любой
// async admin-мутации (Region/Zone Create/Update/Delete → Operation) невозможным
// в secure-by-default конфиге. Зеркалит kacho-vpc / kacho-compute (Public:true).
func TestPermissionMap_operationServiceLROExempt(t *testing.T) {
	m := PermissionMap()

	for _, method := range []string{
		"/kacho.cloud.operation.OperationService/Get",
		"/kacho.cloud.operation.OperationService/Cancel",
	} {
		e, ok := m.Lookup(method)
		if !ok {
			t.Fatalf("%s missing from PermissionMap: LRO polling fail-closes to PermissionDenied", method)
		}
		if !e.Public {
			t.Errorf("%s Public = false, want true (LRO exempt from tenant-authz Check)", method)
		}
	}
}
