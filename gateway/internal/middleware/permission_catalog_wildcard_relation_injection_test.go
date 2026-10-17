// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// permission_catalog_wildcard_relation_injection_test.go — гейт подстановочных
// отношений способен упасть, и различает он ровно то, что объявляет
// (kacho#3251, REG-PUB-1).
//
// Различение: подстановка `<тип>:*` не сужает ничего всегда; системный класс
// сужает строку, спрашивающую об объекте, НАЗВАННОМ запросом, — до объектов, на
// которых класс выдан явно. Инъекции — в обе стороны: законный близнец (класс на
// названном объекте) молчит, дефект каждой из трёх форм краснеет с FQN.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// syntheticEveryoneModel — модель из двух типов: синглтон `cluster` и
// контейнер `reg` с указателем на него. Каждое отношение `reg` — одна форма.
const syntheticEveryoneModel = `
model
  schema 1.1

type user

type system_class

type cluster
  relations
    define viewer: [user, system_class]

type reg
  relations
    define cluster: [cluster]
    define v_get: [system_class, user]
    define v_pub: [user:*, user]
    define v_casc: [user] or viewer from cluster
    define v_alias: [user] or v_get
    define v_owner: [user]
`

func TestWildcardGate_ClassOnANamedObjectNarrows_EveryOtherFormDoesNot(t *testing.T) {
	m := parseEveryoneModel(t, syntheticEveryoneModel)
	fixed := map[string]struct{}{"cluster": {}}

	cases := []struct {
		name     string
		tr       typeRelation
		concrete bool
		want     bool // true — строка не сужает ничего
	}{
		{"законный близнец: класс на объекте, названном запросом", typeRelation{"reg", "v_get"}, true, false},
		{"законный близнец: класс через вычисление на том же объекте", typeRelation{"reg", "v_alias"}, true, false},
		{"дефект: подстановка на объекте, названном запросом", typeRelation{"reg", "v_pub"}, true, true},
		{"дефект: класс без явной выдачи на объекте — каскадом от синглтона", typeRelation{"reg", "v_casc"}, true, true},
		{"дефект: класс, но объект строки не назван запросом", typeRelation{"reg", "v_get"}, false, true},
		{"синглтон: класс на единственном объекте", typeRelation{"cluster", "viewer"}, false, true},
	}
	for _, tc := range cases {
		_, sat := m.sat[tc.tr]
		require.True(t, sat, "%s: отношение обязано распознаваться выполнимым «каждым» — иначе случай не о том", tc.name)
		assert.Equal(t, tc.want, m.rowNarrowsNothing(tc.tr, tc.concrete, fixed), tc.name)
	}
	_, ownerSat := m.sat[typeRelation{"reg", "v_owner"}]
	assert.False(t, ownerSat, "отношение без субъекта «каждый» не выполнимо «каждым»")
}

// mutatedRealGate — гейт на НАСТОЯЩИХ модели и каталоге с одной правкой: строки,
// которые он объявил бы не сужающими ничего и не названными справочником.
func mutatedRealGate(t *testing.T, model string, mutateRow func(*middleware.CatalogEntry)) []string {
	t.Helper()
	m := parseEveryoneModel(t, model)
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)
	var rows []catalogRow
	for _, fqn := range c.FQNs() {
		e, ok := c.Lookup(fqn)
		require.True(t, ok)
		if mutateRow != nil {
			mutateRow(&e)
		}
		rows = append(rows, catalogRow{
			FQN:      fqn,
			TR:       typeRelation{e.ScopeExtractor.ObjectType, e.RequiredRelation},
			Concrete: middleware.IsConcreteResourceScope(e),
			Field:    strings.TrimSpace(e.ScopeExtractor.FromRequestField),
		})
	}
	standing, _ := judgeRows(m, rows, fixedObjectTypes(rows))
	named := referenceCatalogueRPCs()
	var unnamed []string
	for fqn := range standing {
		if _, ok := named[fqn]; !ok {
			unnamed = append(unnamed, fqn)
		}
	}
	return unnamed
}

const (
	realRegistryVGet  = "    define v_get: [system_class, user, service_account, group#member] or owner or super_admin\n"
	realRegistryVList = "    define v_list: [system_class, user, service_account, group#member] or owner or super_admin\n"
)

func TestWildcardGate_RealTree_ControlIsSilent(t *testing.T) {
	assert.Empty(t, mutatedRealGate(t, readCanonicalModel(t), nil), "контроль: неправленые модель и каталог")
}

func TestWildcardGate_RealTree_WildcardOnTheRegistryIsFound(t *testing.T) {
	model := readCanonicalModel(t)
	require.Equal(t, 1, strings.Count(model, realRegistryVGet), "предпосылка: строка v_get реестра в модели одна")
	require.Equal(t, 1, strings.Count(model, realRegistryVList), "предпосылка: строка v_list реестра в модели одна")
	model = strings.Replace(model, realRegistryVGet, strings.Replace(realRegistryVGet, "system_class", "user:*", 1), 1)
	model = strings.Replace(model, realRegistryVList, strings.Replace(realRegistryVList, "system_class", "user:*", 1), 1)

	unnamed := mutatedRealGate(t, model, nil)
	assert.Contains(t, unnamed, "kacho.cloud.registry.v1.RegistryService/Get")
	assert.Contains(t, unnamed, "kacho.cloud.registry.v1.RegistryService/ListOperations")
}

func TestWildcardGate_RealTree_ClassCascadedFromTheSingletonIsFound(t *testing.T) {
	model := readCanonicalModel(t)
	require.Equal(t, 1, strings.Count(model, realRegistryVGet), "предпосылка: строка v_get реестра в модели одна")
	model = strings.Replace(model, realRegistryVGet,
		"    define cluster: [cluster]\n"+
			"    define v_get: [user, service_account, group#member] or owner or super_admin or viewer from cluster\n", 1)

	assert.Contains(t, mutatedRealGate(t, model, nil), "kacho.cloud.registry.v1.RegistryService/Get")
}

func TestWildcardGate_RealTree_ClassOnAnUnnamedObjectIsFound(t *testing.T) {
	const victim = "kacho.cloud.registry.v1.RegistryService/Get"
	hit := 0
	unnamed := mutatedRealGate(t, readCanonicalModel(t), func(e *middleware.CatalogEntry) {
		if e.FQN == victim {
			e.ScopeExtractor.FromRequestField = "*"
			hit++
		}
	})
	require.Equal(t, 1, hit, "предпосылка: строка-жертва в каталоге есть")
	assert.Contains(t, unnamed, victim)
}
