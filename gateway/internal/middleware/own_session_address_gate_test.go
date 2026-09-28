// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_session_address_gate_test.go — рубеж адреса почты на полосе сессии края
// (приёмка F6b, S1: Р3, Р4, Р5, Р10, Р12; решение владельца 2026-09-27 «вход
// дальше экрана регистрации или логина доступен только после подтверждения
// почты»).
//
// Все пробы идут ЧЕРЕЗ ЦЕПОЧКУ `AuthInterceptor.HTTP(mux)`: на mux стоят
// настоящий обработчик «кто я» и дублёры ретрансляции полосы формы и следующего
// звена, каждый считает дошедшие до него запросы. Дублёр службы — на один
// вопрос и не снисходительнее настоящей: он отвечает той парой `(found, err)` и
// тем полем подтверждённости, что названы, и ничем иным.
//
// Каждое отрицание стоит в паре с близнецом, отличающимся ОДНИМ фактом —
// `EmailVerified`, наличием носителя, ответом службы.
package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

// addressRefusalWant — отказ Р3 побайтово: значение службы (`kaname`
// `loginlanehttp.writeRefusal`), без `metadata`.
const addressRefusalWant = `{"code":7,"message":"email address is not verified","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"EMAIL_NOT_VERIFIED","domain":"iam.kaname.cloud"}]}`

// addressPlatformPaths — «путь платформы» преамбулы §4 приёмки: каждый из четырёх.
var addressPlatformPaths = []struct{ method, path string }{
	{http.MethodGet, "/iam/v1/projects"},
	{http.MethodPost, "/vpc/v1/networks"},
	{http.MethodGet, "/iam/v1/me"},
	{http.MethodGet, "/subscription/v1/events"},
}

// openNonVerbPaths — пути без записи каталога, не являющиеся глаголами формы
// (Р5 п. 1): тот же перечень `isPublicHTTPPath`.
var openNonVerbPaths = []string{"/iam/v1/auth/me", "/oauth/logout", "/healthz", "/readyz"}

func unverifiedOwnSession() HumanSession {
	s := liveOwnSession()
	s.EmailVerified = false
	return s
}

// countingAdmin — вопрос о системном администраторе: отвечает «да» и считает вопросы.
type countingAdmin struct{ asked int }

func (c *countingAdmin) IsSystemAdmin(_ context.Context, _ string) (bool, error) {
	c.asked++
	return true, nil
}

// addressRig — полоса сессии с настоящим «кто я», дублёрами ретрансляции и
// следующим звеном; журнал — в буфер (условие ревью: строка отказа несёт
// идентификатор субъекта, а не адрес).
type addressRig struct {
	a       *AuthInterceptor
	reader  *fakeHumanSession
	cut     *fakeCutoff
	admin   *countingAdmin
	next    *countingNext
	reached map[string]int
	chain   http.Handler
	log     *bytes.Buffer
}

func newAddressRig(t *testing.T, sess HumanSession) *addressRig {
	t.Helper()
	reader := &fakeHumanSession{found: true, sess: sess}
	cut := &fakeCutoff{}
	log := &bytes.Buffer{}
	a := NewAuthInterceptor(AuthModeDev, "", cutoffLookup{}, slog.New(slog.NewTextHandler(log, nil))).
		WithHumanSession(reader).
		WithSessionCutoffCheck(cut, time.Hour)
	admin := &countingAdmin{}
	mux := http.NewServeMux()
	h := NewSessionIdentityHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithHumanSession(reader).
		WithSessionCutoff(cut).
		WithAdminChecker(admin)
	h.Register(mux)
	reached := map[string]int{}
	counted := func(path string) {
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			reached[path]++
			w.WriteHeader(http.StatusOK)
		})
	}
	for _, rt := range LoginLaneRoutes() {
		counted(rt.Path)
	}
	for _, p := range []string{"/oauth/logout", "/healthz", "/readyz"} {
		counted(p)
	}
	next := &countingNext{}
	mux.Handle("/", next)
	return &addressRig{a: a, reader: reader, cut: cut, admin: admin, next: next, reached: reached, chain: a.HTTP(mux), log: log}
}

func (r *addressRig) present(method, target string, carrier bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	if carrier {
		req = withOurCarrier(req, "opaque-own")
	}
	return serve(r.chain, req)
}

func (r *addressRig) reachedTotal() int {
	n := r.next.served
	for _, v := range r.reached {
		n += v
	}
	return n
}

// requireAddressRefusal — отказ Р3: 403, тело побайтово, без вызова на
// аутентификацию и без `Set-Cookie`.
func requireAddressRefusal(t *testing.T, where string, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("%s: отказ Р3 обязан быть 403, получено %d %s", where, rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != addressRefusalWant {
		t.Fatalf("%s: тело отказа Р3 не то побайтово:\nполучено %s\nожидалось %s", where, got, addressRefusalWant)
	}
	if ch := rec.Header().Get("WWW-Authenticate"); ch != "" {
		t.Fatalf("%s: отказ Р3 — не вызов на аутентификацию, а WWW-Authenticate = %q", where, ch)
	}
	if sc := rec.Result().Header["Set-Cookie"]; len(sc) != 0 {
		t.Fatalf("%s: отказ Р3 не трогает носитель — сессия нужна экрану подтверждения, а Set-Cookie = %v", where, sc)
	}
}

func notAddressRefusal(rec *httptest.ResponseRecorder) bool {
	return !strings.Contains(rec.Body.String(), "EMAIL_NOT_VERIFIED")
}

// ─────────────────────────────────────────────────────────────────────────────
// F6b-01 / F6b-02 — путь платформы: неподтверждённая сессия дальше края не уходит.

func TestOwnSessionAddressGate_F6b_01_UnverifiedSessionIsRefusedOnEveryPlatformPath(t *testing.T) {
	rig := newAddressRig(t, unverifiedOwnSession())
	before := rig.a.SessionLane().Snapshot()
	if before.AddressNotVerified != 0 {
		t.Fatalf("клетка Р12 до первого отказа = %d, ожидался 0", before.AddressNotVerified)
	}
	for _, p := range addressPlatformPaths {
		requireAddressRefusal(t, p.method+" "+p.path, rig.present(p.method, p.path, true))
	}
	if n := rig.reachedTotal(); n != 0 {
		t.Fatalf("неподтверждённая сессия дошла до следующего звена %d раз", n)
	}
	after := rig.a.SessionLane().Snapshot()
	if after.AddressNotVerified != uint64(len(addressPlatformPaths)) {
		t.Fatalf("клетка Р12 = %d, ожидалось %d — по одной на обращение", after.AddressNotVerified, len(addressPlatformPaths))
	}
	after.AddressNotVerified = 0
	if after != before {
		t.Fatalf("прочие клетки полосы изменились: до %+v, после %+v", before, after)
	}
}

func TestOwnSessionAddressGate_F6b_02_VerifiedTwinPassesTheSamePaths(t *testing.T) {
	rig := newAddressRig(t, liveOwnSession())
	for _, p := range addressPlatformPaths {
		rec := rig.present(p.method, p.path, true)
		if !notAddressRefusal(rec) {
			t.Fatalf("%s %s: подтверждённая сессия получила отказ адреса: %s", p.method, p.path, rec.Body.String())
		}
	}
	if rig.next.served != len(addressPlatformPaths) {
		t.Fatalf("подтверждённая сессия дошла до следующего звена %d раз, ожидалось %d", rig.next.served, len(addressPlatformPaths))
	}
	if s := rig.a.SessionLane().Snapshot(); s.AddressNotVerified != 0 {
		t.Fatalf("клетка Р12 выросла на подтверждённой сессии: %d", s.AddressNotVerified)
	}
}

// Отсутствующее значение подтверждённости — «не подтверждён» (Р4): `bool`
// провода без значения равен `false`, и умолчание закрывает.
func TestOwnSessionAddressGate_F6b_01_AbsentMarkIsNotVerified(t *testing.T) {
	sess := liveOwnSession()
	sess.EmailVerified = HumanSession{}.EmailVerified
	rig := newAddressRig(t, sess)
	requireAddressRefusal(t, "нулевое значение подтверждённости", rig.present(http.MethodGet, "/iam/v1/projects", true))
}

// Ни одна ветка между «сессия найдена» и выставлением личности рубеж не
// обходит — и окно раската (служба не предлагает вопроса об отсечке) тоже.
func TestOwnSessionAddressGate_F6b_01_RolloutWindowDoesNotBypassTheGate(t *testing.T) {
	rig := newAddressRig(t, unverifiedOwnSession())
	rig.cut.err = ErrSessionCutoffUnsupported
	requireAddressRefusal(t, "окно раската", rig.present(http.MethodGet, "/iam/v1/projects", true))
	if rig.next.served != 0 {
		t.Fatalf("в окне раската неподтверждённая сессия дошла до следующего звена %d раз", rig.next.served)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// F6b-03 — отказ побайтово одинаков, что бы ни стояло в пути.

func TestOwnSessionAddressGate_F6b_03_RefusalIsByteIdenticalWhateverThePath(t *testing.T) {
	paths := []string{
		"/vpc/v1/networks/enp5d8a0c1b2e3f4g5h6",
		"/vpc/v1/networks/enp0000000000000000x",
		"/vpc/v1/networks/" + url.PathEscape("не-идентификатор"),
	}
	rig := newAddressRig(t, unverifiedOwnSession())
	var shots []string
	for _, p := range paths {
		rec := rig.present(http.MethodGet, p, true)
		requireAddressRefusal(t, p, rec)
		shots = append(shots, responseShot(rec))
	}
	for i := 1; i < len(shots); i++ {
		if shots[i] != shots[0] {
			t.Fatalf("отказ Р3 различается по пути:\n%s\n---\n%s", shots[0], shots[i])
		}
	}
	// Близнец: подтверждённая сессия — те же три обращения уходят дальше.
	twin := newAddressRig(t, liveOwnSession())
	for _, p := range paths {
		if rec := twin.present(http.MethodGet, p, true); !notAddressRefusal(rec) {
			t.Fatalf("%s: подтверждённая сессия получила отказ адреса", p)
		}
	}
	if twin.next.served != len(paths) {
		t.Fatalf("близнец дошёл до следующего звена %d раз, ожидалось %d", twin.next.served, len(paths))
	}
}

// responseShot — статус, тело и заголовки ответа, кроме идентификатора запроса и Date.
func responseShot(rec *httptest.ResponseRecorder) string {
	var keys []string
	for k := range rec.Header() {
		if k == "X-Request-Id" || k == "Date" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(http.StatusText(rec.Code))
	for _, k := range keys {
		b.WriteString("\n" + k + ": " + strings.Join(rec.Header()[k], ","))
	}
	b.WriteString("\n\n" + rec.Body.String())
	return b.String()
}

// ─────────────────────────────────────────────────────────────────────────────
// F6b-04 — перечень прохода: шесть глаголов и четыре пути без записи каталога.

func TestOwnSessionAddressGate_F6b_04_OpenListIsSixVerbsAndFourPaths(t *testing.T) {
	rig := newAddressRig(t, unverifiedOwnSession())
	var openVerbs, closedVerbs []string
	for _, rt := range LoginLaneRoutes() {
		if openBeforeAddressConfirmationWant[rt.Verb] {
			openVerbs = append(openVerbs, rt.Path)
		} else {
			closedVerbs = append(closedVerbs, rt.Path)
		}
	}
	if len(openVerbs) != 6 || len(closedVerbs) != 9 {
		t.Fatalf("предпосылка: открытых глаголов %d (ожидалось 6), закрытых %d (ожидалось 9)", len(openVerbs), len(closedVerbs))
	}
	for _, p := range append(append([]string{}, openNonVerbPaths...), openVerbs...) {
		method := http.MethodPost
		if p == "/iam/v1/auth/me" || p == "/healthz" || p == "/readyz" || p == LoginLanePathCSRF {
			method = http.MethodGet
		}
		rec := rig.present(method, p, true)
		if !notAddressRefusal(rec) {
			t.Fatalf("%s: путь перечня прохода получил отказ адреса: %s", p, rec.Body.String())
		}
	}
	for _, p := range openVerbs {
		if rig.reached[p] != 1 {
			t.Fatalf("глагол %s ретранслирован %d раз, ожидался 1", p, rig.reached[p])
		}
	}
	for _, p := range closedVerbs {
		requireAddressRefusal(t, p, rig.present(http.MethodPost, p, true))
		if rig.reached[p] != 0 {
			t.Fatalf("закрытый глагол %s ретранслирован неподтверждённой сессии %d раз", p, rig.reached[p])
		}
	}
	// Близнец: те же девять БЕЗ носителя — рубеж без носителя сессии не действует.
	for _, p := range closedVerbs {
		rec := rig.present(http.MethodPost, p, false)
		if !notAddressRefusal(rec) || rig.reached[p] != 1 {
			t.Fatalf("%s без носителя: ответ %d %s, ретранслирован %d раз — ожидался 1", p, rec.Code, rec.Body.String(), rig.reached[p])
		}
	}
	t.Logf("перепись: открытых путей %d (глаголов %d), закрытых глаголов %d", len(openNonVerbPaths)+len(openVerbs), len(openVerbs), len(closedVerbs))
}

// Условие ревью безопасности: разрешённый путь, записанный иначе (хвостовая
// косая, двойная косая, `%2F` внутри сегмента, «/./»), разрешённым не является —
// сравнение точное по пути и по его экранированной записи. Близнец —
// каноническая запись — доходит до своего обработчика.
func TestOwnSessionAddressGate_F6b_04_OpenPathVariantsAreNotOpen(t *testing.T) {
	var open []string
	open = append(open, openNonVerbPaths...)
	for _, rt := range LoginLaneRoutes() {
		if openBeforeAddressConfirmationWant[rt.Verb] {
			open = append(open, rt.Path)
		}
	}
	variants := func(p string) []string {
		last := strings.LastIndex(p, "/")
		encoded := p + "%2F"
		if last > 0 {
			encoded = p[:last] + "%2F" + p[last+1:]
		}
		return []string{p + "/", "/" + p, "/." + p, encoded}
	}
	checked := 0
	for _, p := range open {
		for _, v := range variants(p) {
			rig := newAddressRig(t, unverifiedOwnSession())
			rec := rig.present(http.MethodGet, v, true)
			requireAddressRefusal(t, v, rec)
			if n := rig.reachedTotal(); n != 0 {
				t.Fatalf("запись %q разрешённого пути %q дошла до обработчиков %d раз", v, p, n)
			}
			checked++
		}
		twin := newAddressRig(t, unverifiedOwnSession())
		if rec := twin.present(http.MethodGet, p, true); !notAddressRefusal(rec) {
			t.Fatalf("каноническая запись %q получила отказ адреса", p)
		}
	}
	if checked != len(open)*4 {
		t.Fatalf("проверено записей %d, ожидалось %d", checked, len(open)*4)
	}
	t.Logf("перепись: разрешённых путей %d · записей проверено %d", len(open), checked)
}

// ─────────────────────────────────────────────────────────────────────────────
// F6b-05 / F6b-06 — «кто я»: у неподтверждённой сессии прав нет, и о правах не спрашивали.

func TestOwnSessionAddressGate_F6b_05_WhoAmIOfUnverifiedNamesNoRightsAndAsksNone(t *testing.T) {
	rig := newAddressRig(t, unverifiedOwnSession())
	rec := rig.present(http.MethodGet, "/iam/v1/auth/me", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("«кто я» неподтверждённой сессии: %d %s", rec.Code, rec.Body.String())
	}
	got := whoAmIOf(t, rec)
	if got.User.ID != "usr-own-1" || got.Session.EmailVerified == nil || *got.Session.EmailVerified {
		t.Fatalf("«кто я» обязан назвать субъекта и emailVerified=false: %s", rec.Body.String())
	}
	if len(got.User.Permissions) != 0 || got.User.Permissions == nil {
		t.Fatalf("права неподтверждённой сессии обязаны быть [] (не null): %s", rec.Body.String())
	}
	if rig.admin.asked != 0 {
		t.Fatalf("о системном администраторе спросили %d раз — ответ на этот вопрос известен", rig.admin.asked)
	}
}

func TestOwnSessionAddressGate_F6b_06_WhoAmIOfVerifiedNamesRightsAsToday(t *testing.T) {
	rig := newAddressRig(t, liveOwnSession())
	rec := rig.present(http.MethodGet, "/iam/v1/auth/me", true)
	got := whoAmIOf(t, rec)
	if got.Session.EmailVerified == nil || !*got.Session.EmailVerified {
		t.Fatalf("«кто я» подтверждённой сессии обязан нести emailVerified=true: %s", rec.Body.String())
	}
	if strings.Join(got.User.Permissions, ",") != "*,admin" {
		t.Fatalf("права подтверждённой сессии администратора: %v", got.User.Permissions)
	}
	if rig.admin.asked != 1 {
		t.Fatalf("о системном администраторе спросили %d раз, ожидался 1", rig.admin.asked)
	}
}

type whoAmIShape struct {
	User struct {
		ID          string   `json:"id"`
		Permissions []string `json:"permissions"`
	} `json:"user"`
	Session struct {
		EmailVerified *bool `json:"emailVerified"`
	} `json:"session"`
}

func whoAmIOf(t *testing.T, rec *httptest.ResponseRecorder) whoAmIShape {
	t.Helper()
	var out whoAmIShape
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("«кто я» не JSON: %v %s", err, rec.Body.String())
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// F6b-07 — годность сессии решается раньше адреса.

func TestOwnSessionAddressGate_F6b_07_CutoffIsDecidedBeforeTheAddress(t *testing.T) {
	rig := newAddressRig(t, unverifiedOwnSession())
	rig.cut.found, rig.cut.cutoff = true, ownAuthAt
	rec := rig.present(http.MethodGet, "/iam/v1/projects", true)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), sessionCutoffDenyDescription) {
		t.Fatalf("отсечённая сессия обязана получить F4d-22, получено %d %s", rec.Code, rec.Body.String())
	}
	if !ourCarrierEnded(rec.Result()) {
		t.Fatal("отказ F4d-22 обязан гасить носитель")
	}
	s := rig.a.SessionLane().Snapshot()
	if s.CutoffDenied != 1 || s.AddressNotVerified != 0 {
		t.Fatalf("клетки: отсечка %d (ожидалась 1), Р12 %d (ожидался 0)", s.CutoffDenied, s.AddressNotVerified)
	}
}

// F6b-08 — адрес решается раньше пола уровня: вызова на повышение нет.
func TestOwnSessionAddressGate_F6b_08_AddressIsDecidedBeforeTheFloor(t *testing.T) {
	catalog, err := LoadEmbeddedPermissionCatalog("")
	if err != nil {
		t.Fatalf("каталог прав: %v", err)
	}
	lookup := NewCatalogPermissionLookup(catalog)
	if got := lookup.Lookup(floorTwoFQN).RequiredACRMin; got != "2" {
		t.Fatalf("предпосылка: пол %s = %q, ожидался «2»", floorTwoFQN, got)
	}
	for _, verified := range []bool{false, true} {
		sess := liveOwnSession()
		sess.EmailVerified = verified
		rig := newAddressRig(t, sess)
		rig.a = rig.a.WithStepUp(NewStepUpGate(nil), lookup, NewRestRouter())
		rig.chain = rig.a.HTTP(http.HandlerFunc(rig.next.ServeHTTP))
		rec := rig.present(http.MethodPost, floorTwoRoute, true)
		ch := rec.Header().Get("WWW-Authenticate")
		if !verified {
			requireAddressRefusal(t, "неподтверждённая сессия на полу «2»", rec)
			continue
		}
		if rec.Code != http.StatusUnauthorized || !strings.Contains(ch, `error="insufficient_user_authentication"`) {
			t.Fatalf("близнец: подтверждённая сессия уровня «1» на полу «2» обязана получить вызов, получено %d %q", rec.Code, ch)
		}
	}
}

// F6b-09 — служба не ответила о сессии: F4d-23, прохода «адрес неизвестен» нет.
func TestOwnSessionAddressGate_F6b_09_UnansweredSessionIsF4d23NotAPass(t *testing.T) {
	rig := newAddressRig(t, unverifiedOwnSession())
	rig.reader.err = errors.New("authority unavailable")
	rec := rig.present(http.MethodGet, "/iam/v1/projects", true)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), sessionCutoffDenyDescription) {
		t.Fatalf("служба не ответила: ожидался F4d-23, получено %d %s", rec.Code, rec.Body.String())
	}
	if ourCarrierEnded(rec.Result()) {
		t.Fatal("F4d-23 носитель не гасит")
	}
	if rig.next.served != 0 {
		t.Fatalf("при неответе службы запрос дошёл до следующего звена %d раз", rig.next.served)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// F6b-11 — глаголы подтверждения ретранслируются краем и носитель читают.

func TestOwnSessionAddressGate_F6b_11_VerificationVerbsAreRelayedAndReadTheCarrier(t *testing.T) {
	for _, p := range []string{LoginLanePathVerifyEmail, LoginLanePathVerifyEmailConfirm} {
		// (а) носитель неподтверждённой сессии — ретранслирован.
		rig := newAddressRig(t, unverifiedOwnSession())
		if rec := rig.present(http.MethodPost, p, true); !notAddressRefusal(rec) || rig.reached[p] != 1 {
			t.Fatalf("(а) %s: %d %s, ретранслирован %d", p, rec.Code, rec.Body.String(), rig.reached[p])
		}
		// (б) без носителя — ретранслирован.
		if rec := rig.present(http.MethodPost, p, false); rec.Code != http.StatusOK || rig.reached[p] != 2 {
			t.Fatalf("(б) %s: %d, ретранслирован %d", p, rec.Code, rig.reached[p])
		}
		// (в) носитель при службе, не ответившей о сессии, — F4d-23 края.
		rig.reader.err = errors.New("authority unavailable")
		rec := rig.present(http.MethodPost, p, true)
		if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), sessionCutoffDenyDescription) || rig.reached[p] != 2 {
			t.Fatalf("(в) %s: ожидался F4d-23 без ретрансляции, получено %d %s, ретранслирован %d", p, rec.Code, rec.Body.String(), rig.reached[p])
		}
	}
	// Близнец: регистрация в случае (в) ретранслируется — носителя она не читает.
	rig := newAddressRig(t, unverifiedOwnSession())
	rig.reader.err = errors.New("authority unavailable")
	if rec := rig.present(http.MethodPost, LoginLanePathRegister, true); rec.Code != http.StatusOK || rig.reached[LoginLanePathRegister] != 1 {
		t.Fatalf("близнец: регистрация при неответе службы: %d, ретранслирована %d", rec.Code, rig.reached[LoginLanePathRegister])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Условие ревью безопасности: строка журнала отказа несёт идентификатор
// субъекта, а не адрес почты.

func TestOwnSessionAddressGate_RefusalLogNamesTheSubjectNotTheAddress(t *testing.T) {
	sess := unverifiedOwnSession()
	rig := newAddressRig(t, sess)
	requireAddressRefusal(t, "журнал", rig.present(http.MethodGet, "/iam/v1/projects", true))
	log := rig.log.String()
	if !strings.Contains(log, sess.UserID) {
		t.Fatalf("положительный контроль: строка отказа обязана назвать субъекта %q, журнал:\n%s", sess.UserID, log)
	}
	if strings.Contains(log, sess.Email) {
		t.Fatalf("журнал края несёт адрес почты %q:\n%s", sess.Email, log)
	}
}
