// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// geo_mutation_acr_floor_test.go — пол подтверждения личности мутаций каталога
// размещения против пола кластерной выдачи и отзыва администратора (приёмка
// ADM-1 geo «пол подтверждения мутаций каталога размещения», задача
// PRO-Robotech/kacho#3190, сценарии ADM-1-GEO-ACR-01, -02, -03).
//
// Обе полосы стоят под одним отношением `system_admin` на `cluster`, но пол у
// них разный: мутации geo — рутина `"1"` (класс B5 опоры SEC-acr: каталог
// кластера, не выдача права), `GrantAdmin`/`RevokeAdmin` службы доступа — `"2"`
// (класс B4: эскалация высшей привилегии). Здесь оба значения закреплены
// ПОИМЁННО и РАВЕНСТВОМ, так что сдвиг любого из шестнадцати — вверх или вниз —
// находка с его FQN. Утверждение «не "2"» для этого не годится: оно молча
// пропускает сдвиг вниз на `"0"` или `""`, а оба законны и значат «пола нет».
//
// Каждая проба устроена как функция, ВОЗВРАЩАЮЩАЯ находки по переданному
// каталогу: на вшитом каталоге находок ноль, на каталоге с одной правленой
// записью — ровно находка с FQN этой записи. Инъекция роняет только свой
// предмет: правится одно значение одной записи, остальное — вшитый документ
// байт в байт.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// geoMutationFQNs — двенадцать мутаций каталога размещения: публичные и
// внутренние Create, Update, Delete региона и зоны.
func geoMutationFQNs() []string {
	var out []string
	for _, svc := range []string{"RegionService", "ZoneService", "InternalRegionService", "InternalZoneService"} {
		for _, verb := range []string{"Create", "Update", "Delete"} {
			out = append(out, "kacho.cloud.geo.v1."+svc+"/"+verb)
		}
	}
	return out
}

// clusterAdminGrantFQNs — четыре глагола выдачи и отзыва администратора
// кластера службы доступа, публичные и внутренние.
func clusterAdminGrantFQNs() []string {
	var out []string
	for _, svc := range []string{"ClusterService", "InternalClusterService"} {
		for _, verb := range []string{"GrantAdmin", "RevokeAdmin"} {
			out = append(out, "kaname.cloud.iam.v1."+svc+"/"+verb)
		}
	}
	return out
}

const (
	geoMutationFloor  = "1"
	clusterGrantFloor = "2"
	sharedRelation    = "system_admin"
)

// catalogWithACR — вшитый каталог, в котором у ОДНОЙ записи `fqn` поле
// `required_acr_min` заменено на `acr`; прочие поля и записи не тронуты (разбор
// в обобщённые карты сохраняет и те ключи, которых нет в CatalogEntry).
func catalogWithACR(t *testing.T, fqn, acr string) *middleware.PermissionCatalog {
	t.Helper()
	var rows []map[string]any
	require.NoError(t, json.Unmarshal(middleware.EmbeddedPermissionCatalogJSON(), &rows))
	changed := 0
	for _, row := range rows {
		if row["fqn"] == fqn {
			row["required_acr_min"] = acr
			changed++
		}
	}
	require.Equal(t, 1, changed, "инъекция обязана найти ровно одну запись %s", fqn)
	buf, err := json.Marshal(rows)
	require.NoError(t, err)
	c := middleware.NewPermissionCatalog()
	require.NoError(t, c.LoadFromBytes(buf))
	return c
}

// catalogFloorFindings — ADM-1-GEO-ACR-01: каждая из двенадцати мутаций geo
// несёт ровно `"1"` и `system_admin` и не названа в перечне полосы `"2"`;
// каждая из четырёх выдач администратора кластера несёт ровно `"2"` и
// `system_admin`. Находка называет FQN, поле, ожидаемое и полученное.
func catalogFloorFindings(c *middleware.PermissionCatalog) []string {
	var findings []string
	sensitive := sensitiveACR2Set()
	check := func(fqns []string, floor string) {
		for _, fqn := range fqns {
			e, ok := c.Lookup(fqn)
			if !ok {
				findings = append(findings, fmt.Sprintf("%s: записи в каталоге нет", fqn))
				continue
			}
			if e.RequiredACRMin != floor {
				findings = append(findings, fmt.Sprintf("%s: required_acr_min = %q, ожидалось %q", fqn, e.RequiredACRMin, floor))
			}
			if e.RequiredRelation != sharedRelation {
				findings = append(findings, fmt.Sprintf("%s: required_relation = %q, ожидалось %q", fqn, e.RequiredRelation, sharedRelation))
			}
			_, inSensitive := sensitive[fqn]
			if inSensitive != (floor == clusterGrantFloor) {
				findings = append(findings, fmt.Sprintf("%s: принадлежность перечню полосы \"2\" = %v, ожидалось %v", fqn, inSensitive, floor == clusterGrantFloor))
			}
		}
	}
	check(geoMutationFQNs(), geoMutationFloor)
	check(clusterAdminGrantFQNs(), clusterGrantFloor)
	return findings
}

func TestPermissionCatalog_ADM1GEOACR01_GeoMutationAndClusterGrantFloorsAreNamedOneByOne(t *testing.T) {
	require.Len(t, geoMutationFQNs(), 12, "перечень мутаций geo — двенадцать FQN")
	require.Len(t, clusterAdminGrantFQNs(), 4, "перечень выдач администратора кластера — четыре FQN")
	assert.Empty(t, catalogFloorFindings(embeddedCatalog(t)), "вшитый каталог объявляет оба пола")
}

// Инъекция в обе стороны по каждой из двенадцати записей geo (вверх — "2",
// вниз — "0" и "") и по каждой из четырёх кластерных (вниз — "1"): находка
// ровно одна и называет правленую запись.
func TestPermissionCatalog_ADM1GEOACR01_InjectedFloorShiftIsFoundWithItsFQN(t *testing.T) {
	type shift struct{ fqn, acr string }
	var shifts []shift
	for _, fqn := range geoMutationFQNs() {
		for _, acr := range []string{"2", "0", ""} {
			shifts = append(shifts, shift{fqn, acr})
		}
	}
	for _, fqn := range clusterAdminGrantFQNs() {
		for _, acr := range []string{"1", "0", ""} {
			shifts = append(shifts, shift{fqn, acr})
		}
	}
	for _, s := range shifts {
		t.Run(fmt.Sprintf("%s=%q", s.fqn, s.acr), func(t *testing.T) {
			findings := catalogFloorFindings(catalogWithACR(t, s.fqn, s.acr))
			require.Len(t, findings, 1, "инъекция роняет ровно свой предмет: %v", findings)
			assert.True(t, strings.HasPrefix(findings[0], s.fqn+": required_acr_min"), "находка называет FQN: %s", findings[0])
		})
	}
}

// Законный близнец инъекции: правка, оставляющая значение тем же, находок не
// даёт — проба судит значение, а не факт перезаписи документа.
func TestPermissionCatalog_ADM1GEOACR01_RewrittenCatalogWithSameValuesIsSilent(t *testing.T) {
	assert.Empty(t, catalogFloorFindings(catalogWithACR(t, "kacho.cloud.geo.v1.RegionService/Create", geoMutationFloor)))
	assert.Empty(t, catalogFloorFindings(catalogWithACR(t, "kaname.cloud.iam.v1.ClusterService/GrantAdmin", clusterGrantFloor)))
}

// authWithCatalog — тот же всегда-смонтированный слой аутентификации, что
// alwaysOnAuth, но на переданном каталоге: инъекции проб края идут через него.
func authWithCatalog(t *testing.T, fix *jwksFixture, catalog *middleware.PermissionCatalog) *middleware.AuthInterceptor {
	t.Helper()
	return middleware.NewAuthInterceptor(
		middleware.AuthModeProduction, "",
		&countingLookup{subj: middleware.Subject{Type: "user", ID: "should-not-be-used"}},
		authTestLogger(),
	).
		WithVerifier(rs256Verifier(t, fix)).
		WithStepUp(
			middleware.NewStepUpGate(nil),
			middleware.NewCatalogPermissionLookup(catalog),
			middleware.NewRestRouter(),
		)
}

// stepUpFloorFindings — ADM-1-GEO-ACR-02 и -03 на краю: токен человека с
// `acr = "1"` проходит все двенадцать мутаций geo (REST `POST /geo/v1/regions`
// и нативный gRPC) и НЕ проходит выдачу и отзыв администратора кластера (REST
// `POST /iam/v1/cluster/admins` — 401 с указанием повысить уровень до "2",
// нативный gRPC — UNAUTHENTICATED с тем же текстом); токен с `acr = "2"`
// проходит выдачу и отзыв. Близнецы отличаются ровно одним фактом: метод
// (02↔03) либо уровень (внутри 03).
func stepUpFloorFindings(t *testing.T, catalog *middleware.PermissionCatalog) []string {
	t.Helper()
	fix := newJWKSFixture(t, "RS256")
	auth := authWithCatalog(t, fix, catalog)
	level1 := fix.sign(t, alwaysOnClaims(geoMutationFloor))
	level2 := fix.sign(t, alwaysOnClaims(clusterGrantFloor))

	var findings []string
	add := func(format string, args ...any) { findings = append(findings, fmt.Sprintf(format, args...)) }

	// Сценарий 02, REST.
	if rec, _, hit := serveREST(t, auth, http.MethodPost, "https://api.kacho.cloud/geo/v1/regions", level1); rec.Code == http.StatusUnauthorized || !hit {
		add("kacho.cloud.geo.v1.RegionService/Create: REST при acr=\"1\" — %d, до бэкенда дошёл = %v", rec.Code, hit)
	}
	// Сценарий 02, нативный gRPC — все двенадцать.
	for _, fqn := range geoMutationFQNs() {
		if err := callUnary(t, auth, "/"+fqn, level1); status.Code(err) == codes.Unauthenticated {
			add("%s: нативный вызов при acr=\"1\" отвергнут: %v", fqn, err)
		}
	}

	// Сценарий 03, REST: указание повысить уровень.
	rec, _, hit := serveREST(t, auth, http.MethodPost, "https://api.kacho.cloud/iam/v1/cluster/admins", level1)
	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	restMessage := ""
	if rec.Code != http.StatusUnauthorized {
		add("kaname.cloud.iam.v1.ClusterService/GrantAdmin: REST при acr=\"1\" — %d, ожидалось 401", rec.Code)
	} else if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		add("kaname.cloud.iam.v1.ClusterService/GrantAdmin: тело отказа не google.rpc.Status: %s", rec.Body.String())
	} else {
		restMessage = body.Message
		if body.Code != int(codes.Unauthenticated) {
			add("kaname.cloud.iam.v1.ClusterService/GrantAdmin: code тела = %d, ожидалось %d", body.Code, int(codes.Unauthenticated))
		}
		if body.Message == "" {
			add("kaname.cloud.iam.v1.ClusterService/GrantAdmin: message тела пуст")
		}
		challenge := rec.Header().Get("WWW-Authenticate")
		if !strings.Contains(challenge, `error="insufficient_user_authentication"`) || !strings.Contains(challenge, `acr_values="2"`) {
			add("kaname.cloud.iam.v1.ClusterService/GrantAdmin: WWW-Authenticate = %q", challenge)
		}
	}
	if hit {
		add("kaname.cloud.iam.v1.ClusterService/GrantAdmin: REST при acr=\"1\" дошёл до бэкенда")
	}
	// Сценарий 03, нативный gRPC — все четыре, текст равен тексту тела REST.
	for _, fqn := range clusterAdminGrantFQNs() {
		st, _ := status.FromError(callUnary(t, auth, "/"+fqn, level1))
		if st.Code() != codes.Unauthenticated {
			add("%s: нативный вызов при acr=\"1\" — %s, ожидалось Unauthenticated", fqn, st.Code())
		} else if restMessage != "" && st.Message() != restMessage {
			// Пустой restMessage — REST-плечо уже дало свою находку выше; сверять
			// текст не с чем, и вторая находка о том же предмете была бы шумом.
			add("%s: текст нативного отказа %q не равен тексту тела REST %q", fqn, st.Message(), restMessage)
		}
	}

	// Сценарий 03, близнец по уровню.
	if rec, _, hit := serveREST(t, auth, http.MethodPost, "https://api.kacho.cloud/iam/v1/cluster/admins", level2); !hit {
		add("kaname.cloud.iam.v1.ClusterService/GrantAdmin: REST при acr=\"2\" — %d, до бэкенда не дошёл", rec.Code)
	}
	for _, fqn := range clusterAdminGrantFQNs() {
		if err := callUnary(t, auth, "/"+fqn, level2); status.Code(err) == codes.Unauthenticated {
			add("%s: нативный вызов при acr=\"2\" отвергнут: %v", fqn, err)
		}
	}
	return findings
}

func TestStepUp_ADM1GEOACR02_03_GeoMutationsPassAtLevelOneWhereClusterGrantsDoNot(t *testing.T) {
	assert.Empty(t, stepUpFloorFindings(t, embeddedCatalog(t)))
}

// Инъекции DoD п.2: `ClusterService/GrantAdmin` опущен до "1" и
// `RegionService/Create` поднят до "2" — каждая роняет пробу края, и среди
// находок есть названная правленой записью.
func TestStepUp_ADM1GEOACR02_03_InjectedFloorShiftIsFound(t *testing.T) {
	for _, s := range []struct{ fqn, acr string }{
		{"kaname.cloud.iam.v1.ClusterService/GrantAdmin", "1"},
		{"kacho.cloud.geo.v1.RegionService/Create", "2"},
	} {
		t.Run(fmt.Sprintf("%s=%q", s.fqn, s.acr), func(t *testing.T) {
			findings := stepUpFloorFindings(t, catalogWithACR(t, s.fqn, s.acr))
			require.NotEmpty(t, findings, "сдвиг пола обязан ронять пробу края")
			named := false
			for _, f := range findings {
				named = named || strings.HasPrefix(f, s.fqn+":")
			}
			assert.True(t, named, "находка называет правленую запись %s: %v", s.fqn, findings)
			t.Logf("находки инъекции: %v", findings)
		})
	}
}
