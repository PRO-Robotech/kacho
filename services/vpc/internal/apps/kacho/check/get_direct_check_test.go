// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/check"
)

// Единичное чтение по id авторизуется ПРЯМЫМ per-object Check'ом в per-RPC
// interceptor'е — это несущая опора, а не деталь.
//
// Раньше поверх неё в use-case стоял второй гейт, который спрашивал «перечисли
// ВСЕ объекты, которые subject'у можно» (AuthorizeService.ListObjects) и искал
// id в ответе. Перечисление упирается в жёсткий предел OpenFGA
// (OPENFGA_LIST_OBJECTS_MAX_RESULTS, default 1000, без continuation-token'а),
// поэтому на долгоживущем сторе собственный ресурс тенанта выпадал за префикс и
// Get отдавал 404 при существующей строке и существующем гранте. Гейт удалён;
// авторизацию Get целиком несёт interceptor.
//
// Тест фиксирует предпосылки этого решения: если `Get` пометить ScopeFiltered
// (interceptor пропускает Check, ожидая data-level фильтр) или забыть у него
// per-object extractor, чтение останется БЕЗ object-scope авторизации вовсе.
func TestGetRPCsCarryPerObjectCheck(t *testing.T) {
	m := check.PermissionMap()

	var gets int
	for full, entry := range m {
		// Только ресурсные Get'ы vpc-домена. `OperationService/Get` — общий
		// LRO-поллинг (Public by design: клиент опрашивает свою же операцию по
		// её непредсказуемому id), он не читает vpc-ресурс и в это правило не
		// входит.
		if !strings.HasSuffix(full, "/Get") || !strings.HasPrefix(full, "/kacho.cloud.vpc.v1.") {
			continue
		}
		gets++
		if entry.Public {
			t.Errorf("%s: Get must not be Public — object-scope authz would be skipped", full)
		}
		if entry.ScopeFiltered {
			t.Errorf("%s: Get must NOT be ScopeFiltered — the interceptor would skip the "+
				"per-object Check and there is no data-level filter on the read-by-id path", full)
		}
		if entry.Relation == "" {
			t.Errorf("%s: Get must declare an FGA relation for the per-object Check", full)
		}
		if entry.Extract == nil {
			t.Errorf("%s: Get must declare a per-object extractor", full)
		}
	}
	if gets == 0 {
		t.Fatal("no /Get RPCs found in PermissionMap — the guard would be vacuous")
	}
}

// Ни один vpc-RPC не полагается на «перечисли всё разрешённое» вместо Check'а.
// ScopeFiltered легитимен только там, где хендлер сам авторизует данные; в vpc
// таких нет, и появление нового обязано быть осознанным (этот тест — стоп-кран).
func TestNoScopeFilteredRPCsInVPC(t *testing.T) {
	if got := check.ScopeFilteredRPCs(); len(got) != 0 {
		t.Fatalf("vpc must have no ScopeFiltered RPCs (every RPC carries a per-RPC Check), got %v", got)
	}
}
