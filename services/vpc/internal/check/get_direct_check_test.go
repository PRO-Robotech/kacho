// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/check"
)

// Единичное чтение по id авторизуется ПРЯМЫМ per-object Check'ом в per-RPC
// interceptor'е — это несущая опора, а не деталь.
//
// Раньше поверх неё в use-case стоял второй гейт, который спрашивал «перечисли
// ВСЕ объекты, которые subject'у можно», и искал id в ответе. Перечисление
// упиралось в жёсткий серверный предел (тысяча, без курсора продолжения), поэтому
// на долгоживущем хранилище собственный ресурс тенанта выпадал за префикс и Get
// отдавал 404 при существующей строке и существующем гранте. Гейт удалён;
// авторизацию Get целиком несёт interceptor.
//
// Со стадии S6 перечисляющего RPC нет вовсе — он снят вместе с внешним движком
// отношений. Это НЕ делает пробу лишней: она стережёт не тот RPC, а предпосылки
// решения (пометка ScopeFiltered и per-object extractor), и нарушить их можно
// правкой каталога, ни к какому перечислению не обращаясь.
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

// Стоп-кран на ScopeFiltered жив, но живёт в одном месте — списке
// `scopeFilteredByDesign` (scope_filtered_rpcs_test.go), который сверяется точным
// равенством: новый метод роняет тест, исчезнувший роняет тоже. Держать здесь второй
// перечень значило бы завести два источника истины, которые разъедутся.
//
// Само правило не ослаблено: ScopeFiltered снимает per-RPC Check, поэтому допустим
// только там, где единичного объекта для вопроса нет И ответ реально сужается
// per-object в хендлере. «Перечисли всё разрешённое» (ListObjects) не является таким
// сужением ни при каких условиях — см. package-doc internal/authzfilter.
