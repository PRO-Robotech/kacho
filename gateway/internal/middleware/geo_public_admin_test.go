// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// geo_public_admin_test.go — публичные административные глаголы каталога
// размещения на крае (приёмка sub-phase-ADM-1-geo-placement-catalog-admin,
// сценарии 12 и 15).
//
// Сценарий 12 (уровень края): шесть пар `/geo/v1/{regions,zones}` разрешаются в
// ПУБЛИЧНЫЕ RegionService/ZoneService — положительный близнец к
// TestRestRouter_Geo_S5_InternalPathNotPublicCRUD, который утверждает, что те же
// пары не разрешаются во внутренние.
//
// Сценарий 15: запись каталога публичного глагола равна записи внутреннего
// близнеца по праву, отношению, области и порогу подтверждения личности. Порог
// принадлежит действию, а не адресу: разойдись значения, публичный путь стал бы
// более дешёвым способом совершить то же действие. Равенство держится пробой по
// ПАРЕ записей, а не совпадением чисел на сегодня, и проба доказана инъекцией.

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// geoAdminTwins — шесть пар (публичный, внутренний) административных глаголов.
func geoAdminTwins() [][2]string {
	var out [][2]string
	for _, res := range []string{"Region", "Zone"} {
		for _, verb := range []string{"Create", "Update", "Delete"} {
			out = append(out, [2]string{
				"kacho.cloud.geo.v1." + res + "Service/" + verb,
				"kacho.cloud.geo.v1.Internal" + res + "Service/" + verb,
			})
		}
	}
	return out
}

// twinMismatches — FQN публичных записей, расходящихся с внутренним близнецом
// хоть по одному из четырёх полей, либо отсутствующих.
func twinMismatches(c *middleware.PermissionCatalog) []string {
	var bad []string
	for _, p := range geoAdminTwins() {
		pub, okP := c.Lookup(p[0])
		intr, okI := c.Lookup(p[1])
		if !okP || !okI {
			bad = append(bad, p[0]+" (запись отсутствует)")
			continue
		}
		if pub.Permission != intr.Permission || pub.RequiredRelation != intr.RequiredRelation ||
			pub.ScopeExtractor != intr.ScopeExtractor || pub.RequiredACRMin != intr.RequiredACRMin {
			bad = append(bad, p[0])
		}
	}
	return bad
}

// TestPermissionCatalog_ADM1GEO15_PublicGeoAdminEqualsInternalTwin — сценарий 15.
func TestPermissionCatalog_ADM1GEO15_PublicGeoAdminEqualsInternalTwin(t *testing.T) {
	c, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	require.Empty(t, twinMismatches(c), "public geo admin entries must equal their internal twins")
	for _, p := range geoAdminTwins() {
		e, _ := c.Lookup(p[0])
		assert.Equal(t, "system_admin", e.RequiredRelation, p[0])
		assert.Equal(t, "cluster", e.ScopeExtractor.ObjectType, p[0])
		assert.Equal(t, "*", e.ScopeExtractor.FromRequestField, p[0])
		assert.NotEmpty(t, e.RequiredACRMin, "the floor must be declared, not defaulted: %s", p[0])
		assert.False(t, e.IsExempt(), p[0])
	}

	// system_admin @ cluster не выполняется подстановочным субъектом, и
	// публичные мутации не названы справочником в гейте подстановочных отношений.
	wildcard := wildcardSatisfiableRelations(t)
	require.Contains(t, wildcard, typeRelation{"cluster", "viewer"}, "anti-vacuum: the model parse must see the catalog wildcard")
	_, sat := wildcard[typeRelation{"cluster", "system_admin"}]
	require.False(t, sat, "system_admin on cluster must not be wildcard-satisfiable")
	named := referenceCatalogueRPCs()
	for _, p := range geoAdminTwins() {
		_, isRef := named[p[0]]
		require.False(t, isRef, "an admin verb must not be listed as a reference catalogue: %s", p[0])
	}
	t.Logf("пар осмотрено: %d, расхождений: 0", len(geoAdminTwins()))
}

// TestPermissionCatalog_ADM1GEO15_TwinGateFindsAnInjectedFloor — инъекция:
// временная правка required_acr_min одной публичной записи роняет пробу с её
// FQN; законная пара (остальные пять) проходит молча.
func TestPermissionCatalog_ADM1GEO15_TwinGateFindsAnInjectedFloor(t *testing.T) {
	raw, err := os.ReadFile("embed/permission_catalog.json")
	require.NoError(t, err)
	var entries []map[string]any
	require.NoError(t, json.Unmarshal(raw, &entries))
	const victim = "kacho.cloud.geo.v1.ZoneService/Update"
	hit := 0
	for _, e := range entries {
		if e["fqn"] == victim {
			e["required_acr_min"] = "2"
			hit++
		}
	}
	require.Equal(t, 1, hit, "injection target must exist exactly once")
	mutated, err := json.Marshal(entries)
	require.NoError(t, err)

	c := middleware.NewPermissionCatalog()
	require.NoError(t, c.LoadFromBytes(mutated))
	require.Equal(t, []string{victim}, twinMismatches(c),
		"the gate must name exactly the injected FQN and stay quiet on the legal pairs")
}

// TestRestRouter_ADM1GEO12_PublicGeoAdminPairsResolveToPublicServices — сценарий
// 12, уровень края: шесть пар разрешаются в публичные службы.
func TestRestRouter_ADM1GEO12_PublicGeoAdminPairsResolveToPublicServices(t *testing.T) {
	r := middleware.NewRestRouter()
	for _, tc := range []struct{ method, path, fqn string }{
		{"POST", "/geo/v1/regions", "kacho.cloud.geo.v1.RegionService/Create"},
		{"PATCH", "/geo/v1/regions/ru-central1", "kacho.cloud.geo.v1.RegionService/Update"},
		{"DELETE", "/geo/v1/regions/ru-central1", "kacho.cloud.geo.v1.RegionService/Delete"},
		{"POST", "/geo/v1/zones", "kacho.cloud.geo.v1.ZoneService/Create"},
		{"PATCH", "/geo/v1/zones/ru-central1-a", "kacho.cloud.geo.v1.ZoneService/Update"},
		{"DELETE", "/geo/v1/zones/ru-central1-a", "kacho.cloud.geo.v1.ZoneService/Delete"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			got, ok := r.Resolve(tc.method, tc.path)
			require.True(t, ok, "no route for %s %s", tc.method, tc.path)
			assert.Equal(t, tc.fqn, got)
			assert.False(t, strings.Contains(got, ".Internal"), "public pair resolved to an internal service: %s", got)
		})
	}
}
