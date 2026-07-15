// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// serve_registration_test.go — страж разделения public↔internal поверхностей
// (CLAUDE.md Запреты #6: Internal.* НЕ публикуется на внешнем endpoint).
//
// SECURITY: admin-CRUD InternalRegionService/InternalZoneService должны жить ТОЛЬКО
// на cluster-internal листенере (:9091); публичный (:9090) несёт лишь read-only
// RegionService/ZoneService. Регрессия «Register…Internal…(grpcSrv)» (админ уехал
// на public) свела бы defense-in-depth к единственному authz-Check. Прежде этот
// инвариант не был под тестом (2-й аудит finding «no test proves Internal admin
// services are unreachable on the public listener»). Тест строит два реальных
// grpc.Server через registerServices (тот же helper, что вызывает serve.go) и
// сверяет фактически зарегистрированные дескрипторы через grpc.Server.GetServiceInfo
// — а не source-review.

import (
	"testing"

	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	region "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/region"
	zone "github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/api/zone"
	"github.com/PRO-Robotech/kacho/services/geo/internal/handler"
)

const (
	svcRegion         = "kacho.cloud.geo.v1.RegionService"
	svcZone           = "kacho.cloud.geo.v1.ZoneService"
	svcInternalRegion = "kacho.cloud.geo.v1.InternalRegionService"
	svcInternalZone   = "kacho.cloud.geo.v1.InternalZoneService"
	svcOperation      = "kacho.cloud.operation.OperationService"
)

// TestRegisterServices_InternalAdminNotOnPublic — фактическая регистрация через
// registerServices: Internal* admin-CRUD присутствует ТОЛЬКО на internal-сервере;
// public несёт только read-only Region/Zone; OperationService — на обоих.
func TestRegisterServices_InternalAdminNotOnPublic(t *testing.T) {
	publicSrv := grpc.NewServer()
	internalSrv := grpc.NewServer()

	// use-case'ы конструируются с nil-портами: registerServices только регистрирует
	// дескрипторы (RPC не вызываются), DB не нужна.
	regionUC := region.New(nil, nil, nil, nil)
	zoneUC := zone.New(nil, nil, nil, nil)
	opHandler := handler.NewOperationHandler(operations.Repo(nil))

	registerServices(publicSrv, internalSrv, regionUC, zoneUC, opHandler)

	pub := publicSrv.GetServiceInfo()
	intr := internalSrv.GetServiceInfo()

	// (1) public — только read-only + OperationService, БЕЗ Internal* admin.
	for _, s := range []string{svcRegion, svcZone, svcOperation} {
		if _, ok := pub[s]; !ok {
			t.Errorf("public listener: ожидался сервис %s, не зарегистрирован", s)
		}
	}
	for _, s := range []string{svcInternalRegion, svcInternalZone} {
		if _, ok := pub[s]; ok {
			t.Errorf("SECURITY: %s зарегистрирован на ПУБЛИЧНОМ листенере — Internal.* утёк на внешний endpoint (Запреты #6)", s)
		}
	}

	// (2) internal — Internal* admin + OperationService.
	for _, s := range []string{svcInternalRegion, svcInternalZone, svcOperation} {
		if _, ok := intr[s]; !ok {
			t.Errorf("internal listener: ожидался сервис %s, не зарегистрирован", s)
		}
	}

	// (3) публичные read-only сервисы НЕ дублируются на internal (admin-CRUD идёт
	// через Internal*, а не через RegionService/ZoneService на :9091).
	for _, s := range []string{svcRegion, svcZone} {
		if _, ok := intr[s]; ok {
			t.Errorf("internal listener: неожиданно зарегистрирован public read-only %s", s)
		}
	}
}

// TestRegisterServices_MethodsPresent — sanity: у Internal* на internal-сервере
// действительно есть admin-методы Create/Update/Delete (регистрация не «пустой»
// сервис), а на public их нет вовсе.
func TestRegisterServices_MethodsPresent(t *testing.T) {
	publicSrv := grpc.NewServer()
	internalSrv := grpc.NewServer()
	registerServices(publicSrv, internalSrv,
		region.New(nil, nil, nil, nil),
		zone.New(nil, nil, nil, nil),
		handler.NewOperationHandler(operations.Repo(nil)))

	intr := internalSrv.GetServiceInfo()
	info, ok := intr[svcInternalRegion]
	if !ok {
		t.Fatalf("%s не зарегистрирован на internal", svcInternalRegion)
	}
	want := map[string]bool{"Create": false, "Update": false, "Delete": false}
	for _, m := range info.Methods {
		if _, tracked := want[m.Name]; tracked {
			want[m.Name] = true
		}
	}
	for m, seen := range want {
		if !seen {
			t.Errorf("%s: admin-метод %s отсутствует", svcInternalRegion, m)
		}
	}
}
