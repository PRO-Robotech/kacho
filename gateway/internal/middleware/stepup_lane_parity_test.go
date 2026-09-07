// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware_test

// stepup_lane_parity_test.go — сравнение ПОЛОС аутентификации по одному
// свойству: спрашивает ли полоса пол ступенчатой аутентификации
// (`required_acr_min`), объявленный каталогом прав для вызываемого глагола.
//
// ПОЧЕМУ СРАВНЕНИЕ, А НЕ ПРОБА КАЖДОЙ ПОЛОСЫ ОТДЕЛЬНО. Проба одной полосы
// требует знать, каким свойство ДОЛЖНО быть, — а это и есть спорный вопрос
// («освобождена ли браузерная полоса by design?»). Сравнение спрашивает другое:
// «решал ли кто-нибудь, что полосы различаются», и на это ответ есть всегда.
// Соседний stepup_alwayson_test.go утверждает, что пол применяет слой, через
// который проходит КАЖДЫЙ запрос, — и проверяет это, подавая только подписанного
// предъявителя. «Каждый запрос» и «каждый запрос ЭТОЙ полосы» — разные
// утверждения, и разница между ними ровно та, что осталась незамеченной.
//
// ПОЧЕМУ ПОЛОСЫ ВЫВОДЯТСЯ ИЗ ДЕРЕВА (#1215). До этой правки перечень полос был
// ЛИТЕРАЛОМ в теле пробы — и потому третья полоса (#1142) завелась, не покраснев:
// перепись честно печатала «полос 2 · спрашивают пол 2» при трёх полосах в
// дереве. Число, принадлежащее пробе, а не дереву, измеряет память автора.
// Теперь перечень выводит `identityLanesFromTree` (stepup_lane_census_test.go),
// а у КАЖДОЙ выведенной полосы обязан быть ПРИВОД здесь: четвёртая полоса
// роняет пробу своим именем, пока привод к ней не написан осознанно.
//
// Обе величины переписи печатаются («полос N · спрашивают пол M»): одно число
// скрывает ровно тот случай, ради которого перепись заведена.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// laneOutcome — что край сделал с одним запросом на одной полосе.
type laneOutcome struct {
	lane           string
	method         string // имя метода-полосы в дереве — связь привода с переписью
	status         int
	backendReached bool
	principalID    string // личность, дошедшая до backend; "" = полоса её не установила
	forwardedACR   string // X-Kacho-Token-Acr — вход ВТОРОГО замка (iam ACRFloor)
	challenge      string
	// cred — ПРЕДЪЯВЛЕННОЕ в том виде, в каком его соберёт перепрос отзыва на
	// открытом соединении (`streamrevocation`). Собирается ИЗ ТОГО ЖЕ запроса,
	// что дошёл до backend, и тем же строителем, что зовёт перепрос, — второй
	// сборщик разошёлся бы с первым молча (см. lane_names_askable_credential_test.go).
	cred principalmeta.Credential
}

// laneProbe — одна полоса, прогнанная по ОБОИМ глаголам: обычному (пол не
// поднят) и с объявленным полом. Пара, а не один прогон, — см. askedTheFloor.
type laneProbe struct {
	driver   laneDriver
	routine  laneOutcome
	elevated laneOutcome
}

// askedTheFloor — спросила ли полоса пол.
//
// ПРЕДИКАТ НАБЛЮДАЕМЫЙ И БЕЗ ЕДИНОГО СЛОВА ИЗ ТЕКСТА ОТКАЗА, и это не
// косметика. Прежняя редакция требовала в ответе вызов RFC 9470
// (`insufficient_user_authentication`) — то есть меряла ФОРМУЛИРОВКУ, а не
// свойство. Полоса, которой запрещено выдавать этот вызов (базовое
// удостоверение: церемонии повышения у неё нет, и вызов подтверждал бы годность
// предъявленного — auth_basic_stepup.go), выглядела бы «не спросившей пол»,
// хотя спрашивает его тем же вердиктом, что и соседи. Текстовый предикат
// объявил бы починенное сломанным, а закрытие оракула — регрессией.
//
// Свойство же формулируется парой прогонов и от текста не зависит вовсе:
//
//	на обычном глаголе полоса ДОНОСИТ запрос до backend  (она работает), И
//	на глаголе с полом — ОТВЕРГАЕТ, не доводя               (пол применён).
//
// Первая половина обязательна: без неё «отвергает» тождественно истинно для
// полосы, которая просто не аутентифицирует, и сравнение зеленело бы на
// сломанной полосе.
func (p laneProbe) askedTheFloor() bool {
	return p.routine.backendReached &&
		!p.elevated.backendReached &&
		p.elevated.status == http.StatusUnauthorized
}

// laneName — имя полосы для сообщений.
func (p laneProbe) laneName() string { return p.driver.name }

// fakeKratos — провайдер сессий, отвечающий ЖИВОЙ сессией уровня aal1: человек
// вошёл, второго фактора НЕ предъявлял. Это тот же уровень уверенности, что
// acr="1" у подписанного предъявителя и уровень «1» у базового удостоверения, —
// полосы сравниваются на РАВНОМ входе.
func fakeKratos(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions/whoami" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
		  "active": true,
		  "authenticated_at": "2026-08-24T10:00:00Z",
		  "authenticator_assurance_level": "aal1",
		  "identity": {
		    "id": "dc609064-d9f3-4e24-b574-d561c9f18359",
		    "traits": {"email": "alice@example.test", "name": {"first": "Alice", "last": "A"}}
		  }
		}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// laneRig — композиционный корень, несущий ВСЕ полосы личности сразу и
// смонтированный пол, как это делает край в production-посадке. Один корень на
// все полосы, а не корень на полосу: сравниваются полосы, а не сборки.
type laneRig struct {
	auth        *middleware.AuthInterceptor
	jwks        *jwksFixture
	basicSecret string
	devSecret   string
}

// laneDevSecret — симметричный ключ. В production-посадке он ОТВЕРГАЕТСЯ стражем
// старта края (`cmd/api-gateway/authz_validation.go`) под всякой меткой
// окружения; здесь он задаётся намеренно, чтобы полоса симметричного токена
// ВООБЩЕ ПЫТАЛАСЬ отработать — иначе утверждение о ней было бы тождественно
// истинным (полоса, не смонтированная в фикстуре, ничего не доказывает).
const laneDevSecret = "lane-parity-symmetric-key"

func newLaneRig(t *testing.T) *laneRig {
	t.Helper()
	catalog, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)

	fix := newJWKSFixture(t, "RS256")
	authority := &fakeAuthority{}
	secret := mintFor(t, authority, "bcr00000000000012150")

	it := middleware.NewAuthInterceptor(
		middleware.AuthModeProduction, laneDevSecret,
		&countingLookup{subj: middleware.Subject{
			Type: "user", ID: "usr_alice_acc_a1b2", DisplayName: "Alice A",
		}},
		authTestLogger(),
	).
		WithVerifier(rs256Verifier(t, fix)).
		WithKratos(middleware.NewKratosClient(fakeKratos(t).URL)).
		WithBasicCredentialLane(middleware.NewBasicCredentialLane(authority)).
		WithStepUp(
			middleware.NewStepUpGate(nil),
			middleware.NewCatalogPermissionLookup(catalog),
			middleware.NewRestRouter(),
		)
	return &laneRig{auth: it, jwks: fix, basicSecret: secret, devSecret: laneDevSecret}
}

// laneDriver — привод к одной полосе. Связан с деревом ИМЕНЕМ МЕТОДА: перепись
// выводит методы, реестр приводов ключуется ими же, и полоса без привода
// называется по имени, а не теряется.
type laneDriver struct {
	// name — имя для человека; попадает в перепись и в текст отказа.
	name string
	// carriesIdentityInProduction — устанавливает ли полоса личность в
	// production-посадке. `false` НЕ объявляется, а ДОКАЗЫВАЕТСЯ приводом:
	// проба требует, чтобы личность до backend не дошла. Полоса, начавшая
	// устанавливать личность, роняет это утверждение — то есть требует
	// осознанного решения, а не проходит молча.
	carriesIdentityInProduction bool
	arrange                     func(t *testing.T, rig *laneRig, r *http.Request)
}

// laneDrivers — реестр приводов, ключ = имя метода-полосы В ДЕРЕВЕ.
func laneDrivers() map[string]laneDriver {
	return map[string]laneDriver{
		"tryKratosSession": {
			name:                        "сессия провайдера (ory_kratos_session, aal1)",
			carriesIdentityInProduction: true,
			arrange: func(_ *testing.T, _ *laneRig, r *http.Request) {
				r.Header.Set("Cookie", "ory_kratos_session=opaque-a")
			},
		},
		"tryHydraJWT": {
			name:                        "подписанный предъявитель (Hydra JWT, acr=1)",
			carriesIdentityInProduction: true,
			arrange: func(t *testing.T, rig *laneRig, r *http.Request) {
				r.Header.Set("Authorization", "Bearer "+rig.jwks.sign(t, alwaysOnClaims("1")))
			},
		},
		"tryBasicCredential": {
			name:                        "базовое удостоверение (однострочный секрет, уровень 1)",
			carriesIdentityInProduction: true,
			arrange: func(_ *testing.T, rig *laneRig, r *http.Request) {
				r.Header.Set("Authorization", "Bearer "+rig.basicSecret)
			},
		},
		"tryDevSecretJWT": {
			name: "симметричный токен разработки (HS256)",
			// В production-посадке эта полоса личности НЕ устанавливает:
			// симметричный путь отвергается целиком (`validateJWT`), и это
			// утверждается приводом ниже, а не принимается на веру.
			carriesIdentityInProduction: false,
			arrange: func(t *testing.T, rig *laneRig, r *http.Request) {
				r.Header.Set("Authorization", "Bearer "+
					makeDevJWT(t, rig.devSecret, "usr_alice_acc_a1b2"))
			},
		},
	}
}

// driveLane прогоняет ОДИН запрос через настоящую точку входа края.
func driveLane(t *testing.T, rig *laneRig, method, treeMethod, url string,
	d laneDriver) laneOutcome {
	t.Helper()
	out := laneOutcome{lane: d.name, method: treeMethod}
	handler := rig.auth.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out.backendReached = true
		out.principalID = r.Header.Get(principalmeta.HeaderPrincipalID)
		out.forwardedACR = r.Header.Get(principalmeta.HeaderTokenACR)
		out.cred = principalmeta.CredentialFromRequest(r)
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(method, url, nil)
	d.arrange(t, rig, req)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	out.status = rec.Code
	out.challenge = rec.Header().Get("WWW-Authenticate")
	return out
}

// resolveDrivers сверяет ВЫВЕДЕННЫЕ ИЗ ДЕРЕВА полосы с реестром приводов.
//
// Обе стороны обязательны, и каждая роняет пробу СВОИМ именем:
//   - полоса в дереве без привода — четвёртая полоса, заведённая молча (тот
//     самый предмет #1215);
//   - привод без полосы в дереве — исключение, потерявшее предмет: полосу сняли,
//     а утверждение о ней осталось и переживёт её (`testing.md` §«Гейт на
//     класс», п. 5).
func resolveDrivers(t *testing.T) (laneCensus, map[string]laneDriver) {
	t.Helper()
	c := identityLanesFromTree(t)
	drivers := laneDrivers()

	var undriven, orphaned []string
	for _, m := range c.lanes {
		if _, ok := drivers[m]; !ok {
			undriven = append(undriven, m)
		}
	}
	for m := range drivers {
		found := false
		for _, l := range c.lanes {
			if l == m {
				found = true
				break
			}
		}
		if !found {
			orphaned = append(orphaned, m)
		}
	}
	sort.Strings(orphaned)

	require.Empty(t, undriven,
		"в дереве заведена полоса личности, о которой эта проба не утверждает НИЧЕГО: %v.\n"+
			"Ровно так третья полоса (#1142) прошла мимо сравнения, оставив перепись зелёной.\n"+
			"Напишите привод в laneDrivers() и решите ОСОЗНАННО, спрашивает ли полоса пол.",
		undriven)
	require.Empty(t, orphaned,
		"привод есть, а полосы в дереве нет: %v — утверждение пережило свой предмет", orphaned)
	return c, drivers
}

// catalogFloor — пол ВЫВОДИТСЯ из каталога, а не выписывается константой:
// величина принадлежит каталогу, и проба обязана спрашивать его, а не помнить.
func catalogFloor(t *testing.T, fqn string) string {
	t.Helper()
	catalog, err := middleware.LoadEmbeddedPermissionCatalog("")
	require.NoError(t, err)
	return middleware.NewCatalogPermissionLookup(catalog).Lookup(fqn).RequiredACRMin
}

// Целевой глагол: чеканка удостоверения пользователя. Каталог объявляет ему пол
// уровня «2»; REST-адрес разрешает та же таблица маршрутов, что и в проде.
const (
	elevatedFQN   = "kaname.cloud.iam.v1.UserTokenService/Issue"
	elevatedRoute = "https://api.kacho.cloud/iam/v1/users/usr-abc/tokens"
	routineFQN    = "kacho.cloud.vpc.v1.NetworkService/Create"
	routineRoute  = "https://api.kacho.cloud/vpc/v1/networks"
)

// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ. Полосы действительно аутентифицируют: на обычном
// глаголе (пол не поднят) каждая доносит личность до backend. Без этого
// сравнение ниже зеленело бы на полосе, которая просто не работает.
func TestStepUpLaneParity_EveryLaneCarriesAssuranceToTheInnerLock(t *testing.T) {
	c, drivers := resolveDrivers(t)
	rig := newLaneRig(t)

	carrying, forwarding := 0, 0
	var outs []laneOutcome
	for _, m := range c.lanes {
		d := drivers[m]
		o := driveLane(t, rig, http.MethodPost, m, routineRoute, d)
		outs = append(outs, o)
		if d.carriesIdentityInProduction {
			carrying++
			if o.forwardedACR != "" {
				forwarding++
			}
		}
	}

	// ВТОРАЯ ОСЬ того же расхождения, и спрашивается она ЗДЕСЬ, а не на глаголе
	// с поднятым полом: там отвергнутая полоса до backend не доходит, поэтому её
	// «acr не донесён» тождественно истинно, и сравнение было бы вакуумным.
	//
	// x-kacho-token-acr — вход ВТОРОГО замка (iam authzguard.ACRFloor) на
	// внутреннем слушателе. Полоса, не выставляющая его, оставляет внутренний
	// замок без входа.
	t.Logf("перепись: полос аутентификации %d · устанавливают личность в production %d · "+
		"доносят уровень уверенности до внутреннего замка %d",
		len(c.lanes), carrying, forwarding)
	for _, o := range outs {
		t.Logf("  %-52s статус=%d личность=%q acr-вперёд=%q", o.lane, o.status, o.principalID, o.forwardedACR)
	}

	require.Positive(t, carrying,
		"ни одна полоса не объявлена устанавливающей личность — сравнение было бы вакуумным")
	for _, o := range outs {
		d := drivers[o.method]
		if d.carriesIdentityInProduction {
			require.NotEmpty(t, o.principalID,
				"положительный контроль: полоса %q обязана аутентифицировать, иначе всё "+
					"сказанное о ней ниже тождественно истинно", o.lane)
			assert.NotEmpty(t, o.forwardedACR,
				"полоса %q не доносит уровень уверенности до внутреннего замка: "+
					"замок читает отсутствующее значение на каждом её обращении", o.lane)
			continue
		}
		// Отрицательное утверждение с ЖИВЫМ предикатом: полоса объявлена не
		// устанавливающей личность в production — и это проверяется, а не
		// принимается. Начнёт устанавливать — проба покраснеет и потребует
		// решить, спрашивает ли она пол.
		require.Empty(t, o.principalID,
			"полоса %q объявлена не устанавливающей личность в production-посадке, "+
				"но личность %q дошла до backend — исключение потеряло предикат, "+
				"полосу надо вносить в сравнение пола",
			o.lane, o.principalID)
	}
}

// СРАВНЕНИЕ ПОЛОС. Один и тот же глагол с полом уровня «2», один и тот же
// уровень уверенности предъявленного (второго фактора НЕ было) — полосы обязаны
// дать один и тот же вердикт. Расхождение здесь означает, что механизм
// разошёлся сам, без чьего-либо решения.
func TestStepUpLaneParity_EveryLaneAsksTheDeclaredFloor(t *testing.T) {
	floor := catalogFloor(t, elevatedFQN)
	require.Equal(t, "2", floor,
		"предпосылка пробы: каталог объявляет этому глаголу пол уровня 2 (иначе сравнивать нечего)")
	// Обычный глагол обязан быть ПРОХОДИМ на уровне «1» — том самом, что
	// предъявляют все полосы. Иначе прогон по нему перестал бы быть
	// положительным контролем, и «отвергает на глаголе с полом» стало бы
	// тождественно истинным.
	require.Contains(t, []string{"", "0", "1"}, catalogFloor(t, routineFQN),
		"предпосылка пробы: пол обычного глагола %s обязан удовлетворяться уровнем «1»", routineFQN)

	c, drivers := resolveDrivers(t)
	rig := newLaneRig(t)

	var probes []laneProbe
	for _, m := range c.lanes {
		d := drivers[m]
		if !d.carriesIdentityInProduction {
			continue
		}
		probes = append(probes, laneProbe{
			driver:   d,
			routine:  driveLane(t, rig, http.MethodPost, m, routineRoute, d),
			elevated: driveLane(t, rig, http.MethodPost, m, elevatedRoute, d),
		})
	}

	asked := 0
	for _, p := range probes {
		if p.askedTheFloor() {
			asked++
		}
	}
	t.Logf("перепись: полос аутентификации %d · спрашивают пол %d (глагол %s, пол %q)",
		len(probes), asked, elevatedFQN, floor)
	t.Logf("  (выведено из дерева: файлов %d · методов %s %d · полос личности %d, "+
		"из них устанавливают личность в production %d)",
		c.filesRead, c.receiverTyp, c.methods, len(c.lanes), len(probes))
	for _, p := range probes {
		t.Logf("  %-52s обычный:бэкенд=%-5v · с полом:статус=%d бэкенд=%-5v личность=%q · "+
			"пол-спрошен=%v · ответ=%q",
			p.laneName(), p.routine.backendReached,
			p.elevated.status, p.elevated.backendReached, p.elevated.principalID,
			p.askedTheFloor(), p.elevated.challenge)
	}

	require.NotEmpty(t, probes, "полос для сравнения не выведено — перепись беспредметна")

	// Положительный контроль механизма: пол действует хотя бы на одной полосе.
	// Без него попарное равенство зеленело бы на выключенном поле — то есть на
	// самом опасном исходе.
	require.Positive(t, asked,
		"пол не спросила НИ ОДНА полоса — сравнение ниже было бы вакуумным")

	// Собственно сравнение: попарно, к первой полосе как к опорной.
	for i := 1; i < len(probes); i++ {
		assert.Equal(t, probes[0].askedTheFloor(), probes[i].askedTheFloor(),
			"полосы одного механизма разошлись, и этого никто не объявлял: "+
				"%q пол-спрошен=%v, а %q пол-спрошен=%v — при одном глаголе, одном поле "+
				"каталога и одном уровне предъявленной уверенности",
			probes[0].laneName(), probes[0].askedTheFloor(),
			probes[i].laneName(), probes[i].askedTheFloor())
	}
}
