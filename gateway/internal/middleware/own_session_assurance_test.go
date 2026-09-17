// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_session_assurance_test.go — край решает по уровню НАШЕЙ сессии
// (приёмка Ф11, Р7; §3.3): Ф11-15, Ф11-17, Ф11-18, Ф11-19 и половина края
// Ф11-01 и Ф11-20.
//
// Уровень наблюдается по исходу, как определяет §3.0: глагол с полом ≤ L
// проходит пол; глагол с полом > L отвергается — 401, `code` 16, вызов с
// `acr_values`, равным полу, и строкой «presented ACR L». Пол берётся из
// вшитого каталога прав, а не выписывается: величина принадлежит каталогу.
//
// Все пробы идут ЧЕРЕЗ ЦЕПОЧКУ `AuthInterceptor.HTTP` со смонтированным полом —
// той же, что в production-посадке. Дублёр службы отдаёт ровно тот ответ,
// который назван; для Ф11-18 и Ф11-19 подстановка ответа законна (§8: предмет
// этих сценариев — что край делает с ответом, а не источник уровня). Ф11-16
// — различитель своего и чужого — подстановкой НЕ исполняется by construction
// (§0.4) и здесь не утверждается: его держит стенд с прямой записью в
// хранилище службы.
package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// Три глагола — по одному на каждый пол, встречающийся в каталоге. Пол
// каждого спрашивается у каталога предпосылкой пробы (assuranceRoutes), а не
// принимается на веру: сменится каталог — покраснеет предпосылка, а не
// утверждение о полосе.
const (
	floorTwoFQN   = "kaname.cloud.iam.v1.UserTokenService/Issue"
	floorTwoRoute = "/iam/v1/users/usr-abc/tokens"
	floorOneFQN   = "kacho.cloud.vpc.v1.NetworkService/Create"
	floorOneRoute = "/vpc/v1/networks"
	noFloorFQN    = "kaname.cloud.iam.v1.ProjectService/List"
	noFloorRoute  = "/iam/v1/projects"
)

// assuranceRig — полоса нашей сессии со смонтированным полом; журнал пишется в
// буфер, чтобы громкое состояние (Ф11-19) утверждалось текстом записи, а не
// фактом вызова.
type assuranceRig struct {
	a      *AuthInterceptor
	reader *fakeHumanSession
	next   *countingNext
	chain  http.Handler
	log    *bytes.Buffer
}

func newAssuranceRig(t *testing.T, level string) *assuranceRig {
	t.Helper()
	catalog, err := LoadEmbeddedPermissionCatalog("")
	if err != nil {
		t.Fatalf("каталог прав: %v", err)
	}
	lookup := NewCatalogPermissionLookup(catalog)
	// Предпосылка: три пола каталога — «2», «1» и пустой. Без неё «отвергает на
	// полу 2» было бы тождественно истинно на каталоге, требующем «2» всюду.
	for fqn, want := range map[string]string{floorTwoFQN: "2", floorOneFQN: "1", noFloorFQN: ""} {
		if got := lookup.Lookup(fqn).RequiredACRMin; got != want {
			t.Fatalf("предпосылка: каталог объявляет %s пол %q, ожидался %q", fqn, got, want)
		}
	}
	sess := liveOwnSession()
	sess.AssuranceLevel = level
	reader := &fakeHumanSession{found: true, sess: sess}
	log := &bytes.Buffer{}
	a := NewAuthInterceptor(AuthModeDev, "", cutoffLookup{}, slog.New(slog.NewTextHandler(log, nil))).
		WithHumanSession(reader).
		WithSessionCutoffCheck(&fakeCutoff{}, 0).
		WithStepUp(NewStepUpGate(nil), lookup, NewRestRouter())
	// Окно доклада — управляемыми часами, по минуте на предъявление: каждый
	// ответ вне оси даёт свою строку, и нарастающий итог виден в журнале, а не
	// только в клетке. В бою окно 30 с подавляет повторы, итог при этом растёт.
	clock := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	a.ownAssuranceOffAxis = newIntrospectionFailureReporter(time.Second, func() time.Time {
		clock = clock.Add(time.Minute)
		return clock
	})
	next := &countingNext{}
	return &assuranceRig{a: a, reader: reader, next: next, chain: a.HTTP(next), log: log}
}

// present предъявляет наш носитель на маршруте и возвращает ответ края.
func (r *assuranceRig) present(method, route string, arrange func(*http.Request)) *httptest.ResponseRecorder {
	req := withOurCarrier(httptest.NewRequest(method, route, nil), "opaque-own")
	if arrange != nil {
		arrange(req)
	}
	return serve(r.chain, req)
}

// requireFloorRefusal — форма отказа по полу (§3.0, П1): 401 · code 16 ·
// вызов с acr_values, равным полу, и строкой «presented ACR L».
func requireFloorRefusal(t *testing.T, rec *httptest.ResponseRecorder, required, presented string) {
	t.Helper()
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("отказ по полу обязан быть 401, получено %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":16`) {
		t.Fatalf("тело отказа обязано нести code 16 (UNAUTHENTICATED): %s", rec.Body.String())
	}
	ch := rec.Header().Get("WWW-Authenticate")
	for _, want := range []string{`acr_values="` + required + `"`, "presented ACR " + presented} {
		if !strings.Contains(ch, want) {
			t.Fatalf("вызов обязан нести %q, получено %q", want, ch)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ф11-15 (и половина края Ф11-01): сессия уровня «1» на глаголе с полом «2» —
// отказ, вызов называет предъявленный «1» и требуемый «2». Вторая половина
// Ф11-01 — что вход паролем ВЫДАЁТ «1» — принадлежит службе.

func TestOwnSessionAssurance_F11_15_LevelOneIsRefusedOnFloorTwoNamingBoth(t *testing.T) {
	rig := newAssuranceRig(t, "1")
	rec := rig.present(http.MethodPost, floorTwoRoute, nil)
	requireFloorRefusal(t, rec, "2", "1")
	if rig.next.served != 0 {
		t.Fatalf("отвергнутый по полу запрос дошёл до следующего звена %d раз", rig.next.served)
	}
	if len(rec.Result().Header["Set-Cookie"]) != 0 {
		t.Fatalf("отказ по полу не гасит носитель — предъявленное ГОДНО, лишь недостаточно сильно: %v",
			rec.Result().Header["Set-Cookie"])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ф11-17 — положительный контроль пары: та же сессия проходит пол «1» и глагол
// без пола. Без него Ф11-15 зеленел бы на полосе, отвергающей всё.

func TestOwnSessionAssurance_F11_17_LevelOnePassesFloorOneAndNoFloor(t *testing.T) {
	rig := newAssuranceRig(t, "1")
	for _, c := range []struct{ method, route string }{
		{http.MethodPost, floorOneRoute},
		{http.MethodGet, noFloorRoute},
	} {
		rec := rig.present(c.method, c.route, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s: сессия уровня «1» обязана проходить пол ≤ 1, получено %d %s",
				c.method, c.route, rec.Code, rec.Body.String())
		}
		if got := rig.next.lastReq.Header.Get(principalmeta.HeaderPrincipalID); got != "usr-own-1" {
			t.Fatalf("%s: личность нашей сессии не выставлена: %q", c.route, got)
		}
		if got := rig.next.lastReq.Header.Get(principalmeta.HeaderTokenACR); got != "1" {
			t.Fatalf("%s: второму замку переслан уровень %q, а сессия несёт «1»", c.route, got)
		}
	}
	if rig.next.served != 2 {
		t.Fatalf("до следующего звена дошло %d из 2", rig.next.served)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ф11-18 — клиент приложил заголовок пространства `x-kacho-` с уровнем «3»:
// исход побайтово равен Ф11-15, и уровень, названный клиентом, не читается
// нигде — ни первым замком, ни вторым.

func TestOwnSessionAssurance_F11_18_ClientHeaderNamingLevelThreeIsNotRead(t *testing.T) {
	rig := newAssuranceRig(t, "1")
	plain := rig.present(http.MethodPost, floorTwoRoute, nil)
	requireFloorRefusal(t, plain, "2", "1")

	forge := func(req *http.Request) {
		req.Header.Set(principalmeta.HeaderTokenACR, "3")
		req.Header.Set(principalmeta.HeaderGRPCMetaTokenACR, "3")
		req.Header.Set("grpc-metadata-x-kacho-token-acr", "3")
	}
	forged := rig.present(http.MethodPost, floorTwoRoute, forge)
	if forged.Code != plain.Code || forged.Body.String() != plain.Body.String() ||
		forged.Header().Get("WWW-Authenticate") != plain.Header().Get("WWW-Authenticate") {
		t.Fatalf("исход с подложенным уровнем отличается от Ф11-15:\n%d %s %q\n%d %s %q",
			forged.Code, forged.Body.String(), forged.Header().Get("WWW-Authenticate"),
			plain.Code, plain.Body.String(), plain.Header().Get("WWW-Authenticate"))
	}
	// Второй замок читает пересланный уровень: на проходимом глаголе он обязан
	// быть уровнем СЕССИИ, а не клиента.
	rec := rig.present(http.MethodPost, floorOneRoute, forge)
	if rec.Code != http.StatusOK {
		t.Fatalf("положительный контроль: пол «1» обязан проходить, получено %d", rec.Code)
	}
	for _, h := range []string{principalmeta.HeaderTokenACR, principalmeta.HeaderGRPCMetaTokenACR} {
		if got := rig.next.lastReq.Header.Get(h); got != "1" {
			t.Fatalf("%s: до следующего звена дошёл уровень %q — клиентский заголовок пережил полосу", h, got)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ф11-19 — ответ службы о живой сессии без уровня, с «0» либо со значением вне
// оси сессии (в том числе словарное `aal2` поставщика): глаголы с полом «1» и
// «2» отвергаются вызовом, называющим предъявленный «0»; глагол без пола
// проходит; полоса докладывает записью уровня ошибки с нарастающим итогом — и
// для «0» тоже. `aal2` край не переводит в «2»: сопоставления словаря
// поставщика на полосе нашей сессии нет (Р7).

func TestOwnSessionAssurance_F11_19_OffAxisAnswerIsRefusedLoudlyOnEveryPositiveFloor(t *testing.T) {
	for _, level := range []string{"", "0", "aal2", " 4 "} {
		t.Run("уровень "+strings.TrimSpace(level)+"|", func(t *testing.T) {
			rig := newAssuranceRig(t, level)
			for _, c := range []struct{ route, floor string }{{floorOneRoute, "1"}, {floorTwoRoute, "2"}} {
				requireFloorRefusal(t, rig.present(http.MethodPost, c.route, nil), c.floor, "0")
			}
			rec := rig.present(http.MethodGet, noFloorRoute, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("глагол без пола обязан проходить и на безуровневой сессии: %d %s", rec.Code, rec.Body.String())
			}
			if got := rig.next.lastReq.Header.Get(principalmeta.HeaderTokenACR); got != "" {
				t.Fatalf("второму замку переслан уровень %q — значение вне оси обязано уезжать пустым", got)
			}
			// Громко: запись уровня ошибки с нарастающим итогом — три предъявления,
			// три ответа вне оси.
			logs := rig.log.String()
			if !strings.Contains(logs, "level=ERROR") || !strings.Contains(logs, "session_assurance_off_axis_total=3") {
				t.Fatalf("полоса обязана доложить состояние записью уровня ошибки с нарастающим итогом 3, журнал:\n%s", logs)
			}
			if !strings.Contains(logs, "lane=session") {
				t.Fatalf("запись обязана называть полосу, журнал:\n%s", logs)
			}
			if s := rig.a.SessionLane().Snapshot(); s.AssuranceOffAxis != 3 {
				t.Fatalf("клетка «уровень вне оси» = %d, ожидалось 3", s.AssuranceOffAxis)
			}
		})
	}
}

// Ф11-19, обе стороны инъекции: тот же «0» на полосе базового удостоверения
// МОЛЧИТ — её множество другое: «0» у долгоживущего секрета законен (П6), у
// сессии — нет, потому что сессии без уровня не бывает (Р1, Р7). Одна проверка
// на обе полосы пропустила бы «0» сессии без счётчика.
func TestOwnSessionAssurance_F11_19_ZeroIsOffTheSessionAxisButOnTheCredentialAxis(t *testing.T) {
	if acr, on := acrFromSessionLevel("0"); on || acr != "" {
		t.Fatalf("«0» на оси сессии: acr=%q on=%v — ожидалось вне оси и пустой уровень", acr, on)
	}
	if acr, ok := acrFromCredentialLevel("0"); !ok || acr != "0" {
		t.Fatalf("«0» на оси удостоверения: acr=%q ok=%v — ожидалось признанным «0»", acr, ok)
	}
	for _, level := range []string{"1", "2", "3", " 2 "} {
		if acr, on := acrFromSessionLevel(level); !on || acr != strings.TrimSpace(level) {
			t.Fatalf("%q на оси сессии: acr=%q on=%v", level, acr, on)
		}
	}
	for _, level := range []string{"", "aal1", "aal2", "AAL2", "4", "-1"} {
		if acr, on := acrFromSessionLevel(level); on || acr != "" {
			t.Fatalf("%q обязано быть вне оси сессии: acr=%q on=%v", level, acr, on)
		}
	}

	// Та же величина через обе полосы: доклад есть у сессии и отсутствует у
	// удостоверения.
	log := &bytes.Buffer{}
	a := NewAuthInterceptor(AuthModeDev, "", cutoffLookup{}, slog.New(slog.NewTextHandler(log, nil))).
		WithHumanSession(&fakeHumanSession{})
	a.basicAssuranceUnknown = newIntrospectionFailureReporter(0, nil)
	sess := liveOwnSession()
	sess.AssuranceLevel = "0"
	as := a.ownSessionAssurance(Subject{Type: "user", ID: sess.UserID}, sess, floorOneRoute)
	if as.ACR != "" || !strings.Contains(log.String(), "level=ERROR") {
		t.Fatalf("«0» у сессии: acr=%q, журнал:\n%s", as.ACR, log.String())
	}
	log.Reset()
	basic := a.basicCredentialAssurance(BasicVerifiedCredential{PrincipalType: "user", AuthenticationLevel: "0"}, floorOneRoute)
	if basic.ACR != "0" || strings.Contains(log.String(), "level=ERROR") {
		t.Fatalf("«0» у удостоверения обязан быть признан молча: acr=%q, журнал:\n%s", basic.ACR, log.String())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ф11-20, половина края: край пересылает второму замку уровень, РАВНЫЙ уровню
// нашей сессии. Согласованное состояние «2» здесь подставляет дублёр так, как
// его отдаёт служба (§3.0а); второй замок и `system_admin` на кластере —
// половина на границе службы.

func TestOwnSessionAssurance_F11_20_ForwardedLevelEqualsTheSessionLevel(t *testing.T) {
	rig := newAssuranceRig(t, "2")
	rec := rig.present(http.MethodPost, floorTwoRoute, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("сессия уровня «2» обязана проходить пол «2»: %d %s", rec.Code, rec.Body.String())
	}
	for _, h := range []string{principalmeta.HeaderTokenACR, principalmeta.HeaderGRPCMetaTokenACR} {
		if got := rig.next.lastReq.Header.Get(h); got != "2" {
			t.Fatalf("%s: переслан уровень %q, а сессия несёт «2»", h, got)
		}
	}
	// Способов и момента последнего предъявления полоса нашей сессии не
	// производит (Р7): довод о виде способа отсутствует.
	if got := rig.next.lastReq.Header.Get(principalmeta.HeaderTokenAMR); got != "" {
		t.Fatalf("полоса нашей сессии не производит довода о способах, а переслала %q", got)
	}
}
