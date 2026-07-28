// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/authz"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/check"
)

// TestScopeFilteredRPCs_MatchesMap — ScopeFilteredRPCs() обязан вернуть ровно тот
// набор методов, у которых RPCEntry.ScopeFiltered=true в PermissionMap, и в
// детерминированном (отсортированном) порядке. Композит-рут скармливает этот
// список config.ValidateListFilter (S3 boot-guard).
func TestScopeFilteredRPCs_MatchesMap(t *testing.T) {
	got := check.ScopeFilteredRPCs()

	var want []string
	for full, e := range check.PermissionMap() {
		if e.ScopeFiltered {
			want = append(want, full)
		}
	}
	sort.Strings(want)

	require.Equal(t, want, got)
	require.True(t, sort.StringsAreSorted(got), "результат детерминирован (отсортирован)")
}

// scopeFilteredByDesign — единственные RPC, авторизуемые на уровне данных вместо
// единичного per-RPC Check'а. Список закрытый, и это принципиально: пометка снимает
// Check, поэтому она допустима ТОЛЬКО там, где единичного объекта для вопроса просто
// нет, и обязана сопровождаться реальным per-object сужением ответа.
//
//   - InternalNetworkInterfaceService/ListByInstance — инстансы называет вызывающий,
//     а ответ касается интерфейсов, у каждого из которых свой владелец. Прежний
//     cluster-scoped `viewer` пропускал КАЖДОГО аутентифицированного субъекта
//     (bootstrap пишет `cluster:<root>#viewer@user:*` ради глобального справочника),
//     то есть был формой без содержания. Видимость решается в nicinternal per-object.
//
// Публичные List (NetworkService/List и соседи) сюда НЕ входят намеренно: у них есть
// осмысленный объект (`project:<project_id>`), и per-RPC Check там — единственная
// авторизация при выключенном фильтре (SEC audit 2026-07-05, CWE-862/CWE-639).
var scopeFilteredByDesign = []string{
	"/kacho.cloud.vpc.v1.InternalNetworkInterfaceService/ListByInstance",
}

// TestScopeFilteredRPCs_MatchesDesignList — набор ScopeFiltered-методов равен
// закрытому списку выше. Новый метод в наборе роняет тест: снятие per-RPC Check'а —
// решение, а не деталь, и оно вдобавок делает list-filter обязательным для старта
// сервиса в production (config.ValidateListFilter, S3). Исчезнувший — тоже роняет,
// чтобы запись не пережила свою причину.
func TestScopeFilteredRPCs_MatchesDesignList(t *testing.T) {
	want := append([]string(nil), scopeFilteredByDesign...)
	sort.Strings(want)
	require.Equal(t, want, check.ScopeFilteredRPCs())
}

// TestScopeFilteredRPCs_DetectsScopeFiltered — при наличии ScopeFiltered entry
// helper его находит (проверяем на локальной карте, не мутируя PermissionMap).
func TestScopeFilteredRPCs_DetectsScopeFiltered(t *testing.T) {
	m := authz.RPCMap{
		"/svc/A": {ScopeFiltered: true},
		"/svc/B": {ScopeFiltered: false},
		"/svc/C": {ScopeFiltered: true},
	}
	require.Equal(t, []string{"/svc/A", "/svc/C"}, check.ScopeFilteredRPCsOf(m))
}
