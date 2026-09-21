// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_session_lane_test.go — полоса личности под посадкой `own` читает НАШУ
// сессию (приёмка Ф3, Р7): Ф3-10, Ф3-13, Ф3-14, Ф3-17 (половина полосы),
// Ф3-23 (донос поля до решения по каталогу), Ф3-51 (половина полосы).
//
// Все пробы идут ЧЕРЕЗ ЦЕПОЧКУ `AuthInterceptor.HTTP(mux)` с зарегистрированными
// маршрутами, а не зовут обработчик в изоляции (Д13, `kacho#2688`): обработчик
// «кто я» в изоляции на отвергнутой сессии отвечает `{"user":null}`, и такая
// проба зеленела бы на продукте, отвечающем 401.
//
// Дублёр службы — на ОДИН вопрос и не снисходительнее настоящей: он отвечает
// ровно тем исходом, который назван, и считает, сколько раз его спросили.
package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// cutoffLookup — SubjectLookuper полосы: резолв идёт простой веткой и доходит
// до вопроса про отсечку.
//
// ЖИЛ В `auth_session_cutoff_test.go`, снятом вместе с полосой чужого
// поставщика. Переехал сюда, а не остался сиротой в снятом файле: его читают
// полоса нашей сессии и проба уровня уверенности, и оба переживают снятие.
type cutoffLookup struct{ subj Subject }

func (c cutoffLookup) LookupByExternalID(context.Context, string) (Subject, error) {
	return c.subj, nil
}

// fakeCutoff — НАШ авторитет отзыва. Три исхода задаются явно, потому что
// именно их различение и есть предмет полосы.
type fakeCutoff struct {
	cutoff time.Time
	found  bool
	err    error
	asked  int
	forID  string
}

func (f *fakeCutoff) SessionCutoffOf(_ context.Context, userID string) (time.Time, bool, error) {
	f.asked++
	f.forID = userID
	return f.cutoff, f.found, f.err
}

// fakeHumanSession — дублёр `InternalHumanSessionService.Resolve` на один вопрос.
type fakeHumanSession struct {
	sess       HumanSession
	found      bool
	err        error
	asked      int
	lastBearer string
}

func (f *fakeHumanSession) ResolveHumanSession(_ context.Context, bearer string) (HumanSession, bool, error) {
	f.asked++
	f.lastBearer = bearer
	return f.sess, f.found, f.err
}

// fakeAdmin — AdminChecker: признаёт администратором ровно названного субъекта.
type fakeAdmin struct{ admin string }

func (f fakeAdmin) IsSystemAdmin(_ context.Context, subject string) (bool, error) {
	return subject == f.admin, nil
}

// ownAuthAt — момент аутентификации с НЕНУЛЕВОЙ микросекундной частью: замок
// разрешения на проводе (Ф3-16) требует, чтобы усечение до секунды было
// заметно.
var ownAuthAt = time.Date(2026, 9, 16, 12, 0, 0, 123456000, time.UTC)

func liveOwnSession() HumanSession {
	return HumanSession{
		UserID:          "usr-own-1",
		Email:           "a@example.com",
		DisplayName:     "A",
		AuthenticatedAt: ownAuthAt,
		ExpiresAt:       ownAuthAt.Add(24 * time.Hour),
		AssuranceLevel:  "1",
		EmailVerified:   true,
	}
}

// ownLane — полоса личности под `own`: наш читатель сессии + наш читатель
// отсечки; поставщик НЕ провязан.
func ownLane(t *testing.T, reader HumanSessionReader, cut SessionCutoffReader) *AuthInterceptor {
	t.Helper()
	a := NewAuthInterceptor(AuthModeDev, "", cutoffLookup{}, slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithHumanSession(reader)
	if cut != nil {
		a = a.WithSessionCutoffCheck(cut, time.Hour)
	}
	return a
}

// ownWhoAmI — маршрут «кто я» под `own`, зарегистрированный на mux.
func ownWhoAmI(t *testing.T, mux *http.ServeMux, reader HumanSessionReader, cut SessionCutoffReader, admin AdminChecker) {
	t.Helper()
	h := NewSessionIdentityHandler(slog.New(slog.NewTextHandler(io.Discard, nil))).
		WithHumanSession(reader).
		WithSessionCutoff(cut)
	if admin != nil {
		h = h.WithAdminChecker(admin)
	}
	h.Register(mux)
}

// foreignCarrierName — печенье, выписанное НЕ НАМИ. Имя произвольное и
// намеренно не взято из края: предмет случая — «печенье, которого край не
// выписывал, носителем не является», и он живёт независимо от того, чьи именно
// печенья ходили тут раньше.
const foreignCarrierName = "foreign_session"

func withOurCarrier(req *http.Request, bearer string) *http.Request {
	req.AddCookie(&http.Cookie{Name: OurSessionCarrierName, Value: bearer})
	return req
}

// ourCarrierEnded — погашен ли НАШ носитель ответом.
func ourCarrierEnded(res *http.Response) bool {
	for _, c := range res.Cookies() {
		if c.Name == OurSessionCarrierName && c.MaxAge < 0 {
			return true
		}
	}
	return false
}

func serve(chain http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	chain.ServeHTTP(rec, req)
	return rec
}

const platformPath = "/vpc/v1/networks"

// countingNext — следующее звено: считает, дошёл ли до него запрос, и
// запоминает то, что полоса в него положила.
type countingNext struct {
	served  int
	lastReq *http.Request
}

func (c *countingNext) ServeHTTP(_ http.ResponseWriter, r *http.Request) {
	c.served++
	c.lastReq = r
}

// ─────────────────────────────────────────────────────────────────────────────
// Ф3-10 — «сессии нет» — один ответ; на путях платформы и на «кто я» → F4d-22.

func TestOwnSessionLane_F3_10_NoSessionIsOneRefusalThatEndsTheCarrier(t *testing.T) {
	reader := &fakeHumanSession{found: false}
	cut := &fakeCutoff{}
	a := ownLane(t, reader, cut)
	next := &countingNext{}
	mux := http.NewServeMux()
	ownWhoAmI(t, mux, reader, cut, nil)
	mux.Handle("/", next)
	chain := a.HTTP(mux)

	var bodies []string
	for _, path := range []string{platformPath, "/iam/v1/auth/me"} {
		rec := serve(chain, withOurCarrier(httptest.NewRequest(http.MethodGet, path, nil), "n1-unknown"))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: «сессии нет» при носителе обязано отвергаться F4d-22 (401), получено %d %s", path, rec.Code, rec.Body.String())
		}
		if !ourCarrierEnded(rec.Result()) {
			t.Fatalf("%s: носитель обязан гаситься (F4d-24), Set-Cookie: %v", path, rec.Result().Header["Set-Cookie"])
		}
		bodies = append(bodies, rec.Body.String())
	}
	if bodies[0] != bodies[1] {
		t.Fatalf("тела отказа на пути платформы и на «кто я» различаются:\n%s\n%s", bodies[0], bodies[1])
	}
	if !strings.Contains(bodies[0], sessionCutoffDenyDescription) {
		t.Fatalf("текст отказа обязан быть текстом отсечки (один отказ на пять причин): %s", bodies[0])
	}
	if next.served != 0 {
		t.Fatalf("запрос с носителем без сессии дошёл до следующего звена %d раз", next.served)
	}
	if reader.asked != 2 || reader.lastBearer != "n1-unknown" {
		t.Fatalf("дублёр спрошен %d раз с носителем %q — ожидалось 2 раза значением печенья как есть", reader.asked, reader.lastBearer)
	}
	if s := a.SessionLane().Snapshot(); s.NoSession != 2 {
		t.Fatalf("клетка «сессии нет» = %d, ожидалось 2", s.NoSession)
	}
	// Отказ по отсечке — ТОТ ЖЕ ответ побайтово (Ф1-17: пять причин, один отказ).
	reader.found, reader.sess = true, liveOwnSession()
	cut.found, cut.cutoff = true, ownAuthAt
	rec := serve(chain, withOurCarrier(httptest.NewRequest(http.MethodGet, platformPath, nil), "n5-cut"))
	if rec.Code != http.StatusUnauthorized || rec.Body.String() != bodies[0] {
		t.Fatalf("отказ по отсечке отличается от отказа «сессии нет»: %d %s против %s", rec.Code, rec.Body.String(), bodies[0])
	}
	if !ourCarrierEnded(rec.Result()) {
		t.Fatal("отказ по отсечке обязан гасить носитель")
	}
	if s := a.SessionLane().Snapshot(); s.CutoffDenied != 1 {
		t.Fatalf("клетка «отказ по отсечке» = %d, ожидалось 1", s.CutoffDenied)
	}
}

// Запрос БЕЗ нашего носителя полосу не занимает — анонимен, как сегодня; ЧУЖОЕ
// печенье без нашего носителем не является (Ф1-52).
//
// Имя чужого печенья — литерал пробы, а не константа края: носитель поставщика
// снят с перечня гасимых имён вместе с его читателем, и привязывать фикстуру к
// снятому предмету значило бы получить пробу, краснеющую на исчезновении своей
// подпорки, а не на дефекте.
func TestOwnSessionLane_F3_10_ARequestWithoutOurCarrierStaysAnonymous(t *testing.T) {
	reader := &fakeHumanSession{found: true, sess: liveOwnSession()}
	a := ownLane(t, reader, &fakeCutoff{})
	next := &countingNext{}
	chain := a.HTTP(next)

	serve(chain, httptest.NewRequest(http.MethodGet, platformPath, nil))
	req := httptest.NewRequest(http.MethodGet, platformPath, nil)
	req.AddCookie(&http.Cookie{Name: foreignCarrierName, Value: "foreign-only"})
	serve(chain, req)

	if next.served != 2 {
		t.Fatalf("запросы без нашего носителя обязаны идти дальше анонимно: дошло %d из 2", next.served)
	}
	if reader.asked != 0 {
		t.Fatalf("службу спросили %d раз при отсутствии нашего носителя", reader.asked)
	}
	if got := next.lastReq.Header.Get(principalmeta.HeaderPrincipalID); got != "" {
		t.Fatalf("анонимный запрос несёт личность %q", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ф3-13 — недоступность, UNIMPLEMENTED о годности, UNIMPLEMENTED об отсечке.

func TestOwnSessionLane_F3_13_UnavailableAndUnimplementedRefuseWithTheCutoffTextAndKeepTheCarrier(t *testing.T) {
	reader := &fakeHumanSession{found: true, sess: liveOwnSession()}
	cut := &fakeCutoff{found: true, cutoff: ownAuthAt}
	a := ownLane(t, reader, cut)
	chain := a.HTTP(&countingNext{})

	// Эталон: отказ по отсечке.
	denied := serve(chain, withOurCarrier(httptest.NewRequest(http.MethodGet, platformPath, nil), "s1"))
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("эталонный отказ по отсечке: %d", denied.Code)
	}

	cut.found = false
	for name, err := range map[string]error{
		"(а) Resolve не ответил / UNAVAILABLE": errors.New("rpc error: code = Unavailable desc = down"),
		"(б) Resolve UNIMPLEMENTED":            ErrHumanSessionUnsupported,
	} {
		reader.err = err
		rec := serve(chain, withOurCarrier(httptest.NewRequest(http.MethodGet, platformPath, nil), "s1"))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: обязан быть отказ F4d-23 (401), получено %d", name, rec.Code)
		}
		if rec.Body.String() != denied.Body.String() {
			t.Fatalf("%s: текст недоступности отличается от текста отсечки — оракул исправности соседа (Д3):\n%s\n%s",
				name, rec.Body.String(), denied.Body.String())
		}
		if len(rec.Result().Header["Set-Cookie"]) != 0 {
			t.Fatalf("%s: носитель обязан остаться целым, Set-Cookie: %v", name, rec.Result().Header["Set-Cookie"])
		}
	}
	if s := a.SessionLane().Snapshot(); s.Unavailable != 2 {
		t.Fatalf("клетка «недоступность» = %d, ожидалось 2", s.Unavailable)
	}
	// Повтор через мгновение при ответившей службе проходит.
	reader.err = nil
	rec := serve(chain, withOurCarrier(httptest.NewRequest(http.MethodGet, platformPath, nil), "s1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("после восстановления службы предъявление обязано проходить: %d %s", rec.Code, rec.Body.String())
	}
}

func TestOwnSessionLane_F3_13_CutoffUnsupportedPassesLoudlyWithACounter(t *testing.T) {
	reader := &fakeHumanSession{found: true, sess: liveOwnSession()}
	a := ownLane(t, reader, &fakeCutoff{err: ErrSessionCutoffUnsupported})
	next := &countingNext{}
	rec := serve(a.HTTP(next), withOurCarrier(httptest.NewRequest(http.MethodGet, platformPath, nil), "s1"))
	if rec.Code != http.StatusOK || next.served != 1 {
		t.Fatalf("UNIMPLEMENTED только об отсечке — проход (окно раската): %d, дошло %d", rec.Code, next.served)
	}
	if s := a.SessionLane().Snapshot(); s.RolloutWindow != 1 {
		t.Fatalf("клетка «окно раската» = %d, ожидалось 1 — проход обязан быть громким", s.RolloutWindow)
	}
	// Личность при этом выставлена — проход, а не анонимность.
	if got := next.lastReq.Header.Get(principalmeta.HeaderPrincipalID); got != "usr-own-1" {
		t.Fatalf("личность нашей сессии не выставлена: %q", got)
	}
}

// ЗДЕСЬ СТОЯЛА ТА ЖЕ ПРОБА Д3 НА ПОЛОСЕ `external`. Она снята вместе со своей
// полосой: читателя печенья чужого поставщика больше нет, и поднять эту полосу
// нечем. Свойство — «недоступность отсечки отвечает тем же текстом, что отказ
// по отсечке» — держится на полосе, которая осталась
// (TestOwnSessionLane_F3_13_UnavailableAndUnimplementedRefuseWithTheCutoffTextAndKeepTheCarrier
// выше сверяет те же два текста).

// ─────────────────────────────────────────────────────────────────────────────
// Ф3-14 — «кто я» из нашей сессии через цепочку.

func TestOwnSessionLane_F3_14_WhoAmIAnswersFromOurSessionThroughTheChain(t *testing.T) {
	reader := &fakeHumanSession{found: true, sess: liveOwnSession()}
	cut := &fakeCutoff{}
	a := ownLane(t, reader, cut)
	mux := http.NewServeMux()
	ownWhoAmI(t, mux, reader, cut, fakeAdmin{admin: "user:usr-own-1"})
	chain := a.HTTP(mux)

	rec := serve(chain, withOurCarrier(httptest.NewRequest(http.MethodGet, "/iam/v1/auth/me", nil), "s1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("«кто я» с живым носителем: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		User    map[string]any `json:"user"`
		Session map[string]any `json:"session"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("ответ не JSON: %v: %s", err, rec.Body.String())
	}
	for k, want := range map[string]any{"id": "usr-own-1", "email": "a@example.com", "displayName": "A", "subjectType": "user"} {
		if body.User[k] != want {
			t.Errorf("user.%s = %v, ожидалось %v", k, body.User[k], want)
		}
	}
	if perms, _ := body.User["permissions"].([]any); len(perms) != 2 {
		t.Errorf("permissions по нашему субъекту (system_admin) = %v, ожидалось [* admin]", body.User["permissions"])
	}
	if body.Session == nil {
		t.Fatalf("объект session отсутствует: %s", rec.Body.String())
	}
	if body.Session["expiresAt"] != ownAuthAt.Add(24*time.Hour).Format(time.RFC3339) {
		t.Errorf("session.expiresAt = %v — срок показывается усечённым до секунды", body.Session["expiresAt"])
	}
	if body.Session["assuranceLevel"] != "1" || body.Session["emailVerified"] != true {
		t.Errorf("поля сессии: %v", body.Session)
	}
	// Поле «требуется сменить пароль» снято с контракта службы (kaname#201,
	// kacho#2707): «кто я» его не несёт — ни истиной, ни ложью.
	if _, present := body.Session["passwordChangeRequired"]; present {
		t.Errorf("session несёт снятое поле passwordChangeRequired: %v", body.Session)
	}

	// Без носителя и с ЧУЖИМ печеньем без нашего — `{"user":null}` побайтово.
	anon := serve(chain, httptest.NewRequest(http.MethodGet, "/iam/v1/auth/me", nil))
	req := httptest.NewRequest(http.MethodGet, "/iam/v1/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: foreignCarrierName, Value: "foreign-only"})
	foreign := serve(chain, req)
	if anon.Code != http.StatusOK || anon.Body.String() != `{"user":null}` {
		t.Fatalf("без носителя: %d %q", anon.Code, anon.Body.String())
	}
	if foreign.Body.String() != anon.Body.String() || foreign.Code != anon.Code {
		t.Fatalf("чужое печенье без нашего отвечает иначе, чем отсутствие носителя: %q против %q", foreign.Body.String(), anon.Body.String())
	}

	// С отвергнутым носителем — 401 от ПОЛОСЫ с гашением, а не `{"user":null}`.
	cut.found, cut.cutoff = true, ownAuthAt.Add(time.Minute)
	refused := serve(chain, withOurCarrier(httptest.NewRequest(http.MethodGet, "/iam/v1/auth/me", nil), "s1"))
	if refused.Code != http.StatusUnauthorized || !ourCarrierEnded(refused.Result()) {
		t.Fatalf("«кто я» на отвергнутой сессии обязан получать F4d-22 от полосы с гашением: %d %s", refused.Code, refused.Body.String())
	}
}

// Точки предъявления ВЛОЖЕНЫ: на «кто я» обработчик сохраняет СВОЙ читатель и
// на отвергнутой между вопросами сессии отвечает анонимом, а не человеком.
func TestOwnSessionLane_F3_52_WhoAmIKeepsItsOwnCutoffReader(t *testing.T) {
	reader := &fakeHumanSession{found: true, sess: liveOwnSession()}
	laneCut := &fakeCutoff{}
	handlerCut := &fakeCutoff{found: true, cutoff: ownAuthAt}
	a := ownLane(t, reader, laneCut)
	mux := http.NewServeMux()
	ownWhoAmI(t, mux, reader, handlerCut, nil)
	rec := serve(a.HTTP(mux), withOurCarrier(httptest.NewRequest(http.MethodGet, "/iam/v1/auth/me", nil), "s1"))
	if rec.Code != http.StatusOK || rec.Body.String() != `{"user":null}` {
		t.Fatalf("собственный читатель обработчика не спрошен: %d %s", rec.Code, rec.Body.String())
	}
	if handlerCut.asked != 1 || laneCut.asked != 1 {
		t.Fatalf("вопрос об отсечке задаётся дважды на одном запросе (Д13): полоса %d, обработчик %d", laneCut.asked, handlerCut.asked)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ф5-24 — сессия, выданная восстановлением, полноправна: полоса личности
// пропускает её к решению по каталогу как всякую сессию входа. ЗДЕСЬ СТОЯЛА
// проба Ф3-23 «требование сменить пароль доносится до решения по каталогу» —
// снята вместе с предметом (kaname#201, kacho#2707): поле с контракта службы
// снято, производителя значения `true` не было ни одного. Положительный близнец
// остался: сессия доходит до следующего звена с личностью и уровнем.

func TestOwnSessionLane_F5_24_RecoveryIssuedSessionIsJudgedByTheCatalogLikeAnyLogin(t *testing.T) {
	reader := &fakeHumanSession{found: true, sess: liveOwnSession()}
	a := ownLane(t, reader, &fakeCutoff{})
	next := &countingNext{}
	chain := a.HTTP(next)

	serve(chain, withOurCarrier(httptest.NewRequest(http.MethodGet, platformPath, nil), "r"))
	if next.served != 1 {
		t.Fatalf("полоса личности обязана ПРОПУСТИТЬ запрос — решение принадлежит каталогу прав")
	}
	// Личность и уровень выставлены — по тем же именам, что у полосы поставщика.
	if got := next.lastReq.Header.Get(principalmeta.HeaderPrincipalID); got != "usr-own-1" {
		t.Fatalf("личность: %q", got)
	}
	if got := next.lastReq.Header.Get(principalmeta.HeaderTokenACR); got != "1" {
		t.Fatalf("уровень уверенности нашей сессии не выставлен на ось каталога: %q", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ф3-51 (половина полосы) — глаголы формы стоят ЗА читателем отсечки.

func TestOwnSessionLane_F3_51_FormVerbsRefuseTheCutOffCarrierAndRelayNoSession(t *testing.T) {
	reader := &fakeHumanSession{found: true, sess: liveOwnSession()}
	cut := &fakeCutoff{found: true, cutoff: ownAuthAt} // отсечка НЕ ниже момента
	a := ownLane(t, reader, cut)
	relay := &countingNext{}
	mux := http.NewServeMux()
	for _, rt := range LoginLaneRoutes() {
		mux.Handle(rt.Path, relay)
	}
	chain := a.HTTP(mux)

	verbs := []string{LoginLanePathPassword, LoginLanePathLogout, LoginLanePathLogin}
	for _, p := range verbs {
		rec := serve(chain, withOurCarrier(httptest.NewRequest(http.MethodPost, p, strings.NewReader(`{}`)), "s1-cut"))
		if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), sessionCutoffDenyDescription) {
			t.Fatalf("%s с носителем отсечённой сессии: обязан быть F4d-22 на крае, получено %d %s", p, rec.Code, rec.Body.String())
		}
		if !ourCarrierEnded(rec.Result()) {
			t.Fatalf("%s: носитель обязан гаситься", p)
		}
	}
	if relay.served != 0 {
		t.Fatalf("ретранслировано %d при отсечённом носителе — запрос не должен доходить до службы", relay.served)
	}

	// Положительный контроль ветки (Р7): носитель СНЯТОЙ сессии (found=false) —
	// ретранслируется, счёт 3; личность при этом не выставлена.
	reader.found = false
	for _, p := range verbs {
		rec := serve(chain, withOurCarrier(httptest.NewRequest(http.MethodPost, p, strings.NewReader(`{}`)), "s3-gone"))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s с носителем снятой сессии: «сессии нет» обязано ретранслироваться, получено %d %s", p, rec.Code, rec.Body.String())
		}
		if len(rec.Result().Header["Set-Cookie"]) != 0 {
			t.Fatalf("%s: край не гасит носитель на ретрансляции — исход судит служба", p)
		}
	}
	if relay.served != 3 {
		t.Fatalf("ретранслировано %d, ожидалось 3", relay.served)
	}
	if got := relay.lastReq.Header.Get(principalmeta.HeaderPrincipalID); got != "" {
		t.Fatalf("на «сессии нет» полоса выставила личность %q", got)
	}
	// Признак формы — тоже ретранслируется.
	serve(chain, withOurCarrier(httptest.NewRequest(http.MethodGet, LoginLanePathCSRF+"?form=login", nil), "s3-gone"))
	if relay.served != 4 {
		t.Fatalf("csrf не ретранслирован: %d", relay.served)
	}

	// Отрицательный контроль Ф3-51 (последняя «And»): путь ВНЕ перечня глаголов
	// на том же носителе снятой сессии — отказ, не проход: ветка снимает исход
	// ровно на путях объявления, а не на всём `isPublicHTTPPath`.
	rec := serve(chain, withOurCarrier(httptest.NewRequest(http.MethodGet, "/iam/v1/auth/me", nil), "s3-gone"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("«кто я» на носителе снятой сессии обязан отвергаться F4d-22: %d", rec.Code)
	}

	// Живая сессия на глаголе формы — ретрансляция С выставленной личностью
	// (её снимает ретранслятор, Р2; здесь — что полоса её выставила).
	reader.found = true
	cut.found = false
	serve(chain, withOurCarrier(httptest.NewRequest(http.MethodPost, LoginLanePathLogout, strings.NewReader(`{}`)), "s2-live"))
	if got := relay.lastReq.Header.Get(principalmeta.HeaderPrincipalID); got != "usr-own-1" {
		t.Fatalf("на живой сессии полоса не выставила личность перед ретрансляцией: %q", got)
	}
	if s := a.SessionLane().Snapshot(); s.CutoffDenied != 3 || s.NoSession != 1 {
		t.Fatalf("клетки: отсечка %d (ожидалось 3), «сессии нет» %d (ожидалось 1 — только «кто я»)", s.CutoffDenied, s.NoSession)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ф3-17 / Ф3-20 «д» (половина полосы) — недоступность ретранслируется на
// выходе, входе и признаке; на смене пароля — F4d-23.

func TestOwnSessionLane_F3_17_UnavailabilityIsRelayedExceptOnPasswordChange(t *testing.T) {
	reader := &fakeHumanSession{err: errors.New("rpc error: code = Unavailable")}
	a := ownLane(t, reader, &fakeCutoff{})
	relay := &countingNext{}
	mux := http.NewServeMux()
	for _, rt := range LoginLaneRoutes() {
		mux.Handle(rt.Path, relay)
	}
	chain := a.HTTP(mux)

	for _, p := range []string{LoginLanePathLogout, LoginLanePathLogin, LoginLanePathCSRF} {
		rec := serve(chain, withOurCarrier(httptest.NewRequest(http.MethodPost, p, strings.NewReader(`{}`)), "s1"))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s при недоступном Resolve обязан ретранслироваться (служба ответит своим 503): %d %s", p, rec.Code, rec.Body.String())
		}
	}
	if relay.served != 3 {
		t.Fatalf("ретранслировано %d, ожидалось 3", relay.served)
	}
	rec := serve(chain, withOurCarrier(httptest.NewRequest(http.MethodPost, LoginLanePathPassword, strings.NewReader(`{}`)), "s1"))
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), sessionCutoffDenyDescription) {
		t.Fatalf("смена пароля при недоступном Resolve обязана получать F4d-23 (401 текстом отсечки): %d %s", rec.Code, rec.Body.String())
	}
	if len(rec.Result().Header["Set-Cookie"]) != 0 {
		t.Fatal("носитель на F4d-23 обязан остаться целым")
	}
	if relay.served != 3 {
		t.Fatalf("смена пароля ретранслирована при недоступности: %d", relay.served)
	}
	// Та же асимметрия при недоступном ВТОРОМ вопросе (SessionCutoffOf не ответил).
	reader.err, reader.found, reader.sess = nil, true, liveOwnSession()
	a2 := ownLane(t, reader, &fakeCutoff{err: errors.New("unreachable")})
	chain2 := a2.HTTP(mux)
	serve(chain2, withOurCarrier(httptest.NewRequest(http.MethodPost, LoginLanePathLogout, strings.NewReader(`{}`)), "s1"))
	rec = serve(chain2, withOurCarrier(httptest.NewRequest(http.MethodPost, LoginLanePathPassword, strings.NewReader(`{}`)), "s1"))
	if relay.served != 4 || rec.Code != http.StatusUnauthorized {
		t.Fatalf("недоступная отсечка: выход обязан ретранслироваться (счёт %d), смена — F4d-23 (%d)", relay.served, rec.Code)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Ф4/Ф5 (kacho#2699, kacho#2701) — регистрация и восстановление на полосе.

// TestOwnSessionLane_F4_F5_RegistrationAndRecoveryFollowTheFormVerbRules —
// три новых глагола формы ведут себя на полосе сессии как глаголы формы, а не
// как пути платформы: носитель отсечённой сессии отвергается краем (Ф3-51),
// «сессии нет» ретранслируется (исход судит служба), недоступность вопросов
// края ретранслируется — ни один из трёх не меняет состояния ПОД сессией
// носителя (регистрация заводит новую личность, восстановление ключуется кодом,
// а не носителем), поэтому асимметрия смены пароля (F4d-23) на них не
// распространяется. Решение записано здесь, а не выведено умолчанием: до
// расширения перечня те же запросы получали 401 на «сессии нет» — как пути
// платформы.
func TestOwnSessionLane_F4_F5_RegistrationAndRecoveryFollowTheFormVerbRules(t *testing.T) {
	verbs := []string{LoginLanePathRegister, LoginLanePathRecovery, LoginLanePathRecoveryComplete}

	// Отсечённый носитель — отказ края, до службы не доходит.
	reader := &fakeHumanSession{found: true, sess: liveOwnSession()}
	cut := &fakeCutoff{found: true, cutoff: ownAuthAt}
	a := ownLane(t, reader, cut)
	relay := &countingNext{}
	mux := http.NewServeMux()
	for _, rt := range LoginLaneRoutes() {
		mux.Handle(rt.Path, relay)
	}
	chain := a.HTTP(mux)
	for _, p := range verbs {
		rec := serve(chain, withOurCarrier(httptest.NewRequest(http.MethodPost, p, strings.NewReader(`{}`)), "s1-cut"))
		if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), sessionCutoffDenyDescription) {
			t.Fatalf("%s с носителем отсечённой сессии: обязан быть F4d-22 на крае, получено %d %s", p, rec.Code, rec.Body.String())
		}
		if !ourCarrierEnded(rec.Result()) {
			t.Fatalf("%s: носитель обязан гаситься", p)
		}
	}
	if relay.served != 0 {
		t.Fatalf("ретранслировано %d при отсечённом носителе — запрос не должен доходить до службы", relay.served)
	}

	// «Сессии нет» — ретрансляция без личности и без гашения носителя.
	reader.found = false
	for _, p := range verbs {
		rec := serve(chain, withOurCarrier(httptest.NewRequest(http.MethodPost, p, strings.NewReader(`{}`)), "s3-gone"))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s с носителем снятой сессии: «сессии нет» обязано ретранслироваться, получено %d %s", p, rec.Code, rec.Body.String())
		}
		if len(rec.Result().Header["Set-Cookie"]) != 0 {
			t.Fatalf("%s: край не гасит носитель на ретрансляции — исход судит служба", p)
		}
		if got := relay.lastReq.Header.Get(principalmeta.HeaderPrincipalID); got != "" {
			t.Fatalf("%s: на «сессии нет» полоса выставила личность %q", p, got)
		}
	}
	if relay.served != len(verbs) {
		t.Fatalf("ретранслировано %d, ожидалось %d", relay.served, len(verbs))
	}

	// Недоступность вопросов края — ретрансляция (служба ответит своим 503), а
	// не F4d-23: состояния под сессией носителя эти глаголы не меняют.
	unavailable := ownLane(t, &fakeHumanSession{err: errors.New("rpc error: code = Unavailable")}, &fakeCutoff{})
	relay2 := &countingNext{}
	mux2 := http.NewServeMux()
	for _, rt := range LoginLaneRoutes() {
		mux2.Handle(rt.Path, relay2)
	}
	chain2 := unavailable.HTTP(mux2)
	for _, p := range verbs {
		rec := serve(chain2, withOurCarrier(httptest.NewRequest(http.MethodPost, p, strings.NewReader(`{}`)), "s1"))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s при недоступном Resolve обязан ретранслироваться (служба ответит своим 503): %d %s", p, rec.Code, rec.Body.String())
		}
	}
	if relay2.served != len(verbs) {
		t.Fatalf("ретранслировано %d при недоступности, ожидалось %d", relay2.served, len(verbs))
	}
	// Положительный контроль асимметрии: смена пароля на той же полосе —
	// по-прежнему F4d-23, иначе утверждение выше зеленело бы на полосе,
	// ретранслирующей всё подряд.
	rec := serve(chain2, withOurCarrier(httptest.NewRequest(http.MethodPost, LoginLanePathPassword, strings.NewReader(`{}`)), "s1"))
	if rec.Code != http.StatusUnauthorized || relay2.served != len(verbs) {
		t.Fatalf("смена пароля при недоступном Resolve обязана получать F4d-23, а не ретранслироваться: %d, ретранслировано %d", rec.Code, relay2.served)
	}
	t.Logf("перепись: глаголов Ф4/Ф5 %d · исходов проверено по каждому 3 (отсечка · «сессии нет» · недоступность)", len(verbs))
}
