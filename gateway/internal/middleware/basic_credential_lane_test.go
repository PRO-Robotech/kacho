// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// basic_credential_lane_test.go — ПОЛОСА ПРИЁМА БАЗОВОГО СЕКРЕТА НА КРАЕ.
//
// Задача #1142, приёмка BAT-1 §5, §2.2; сценарии BAT-1-27, 29, 31, 33, 34, 35, 36.

package middleware_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/credsecret"

	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
)

// fakeAuthority — авторитет о предъявленном. Считает вызовы, чтобы стоимость
// проверялась ЧИСЛОМ, а не впечатлением.
type fakeAuthority struct {
	mu    sync.Mutex
	calls int
	// byCredential — годные удостоверения: идентификатор → предъявляемая строка.
	byCredential map[string]string
	err          error
}

func (f *fakeAuthority) Resolve(_ context.Context, presented string) (*iamv1.ResolveBasicCredentialResponse, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	p, perr := credsecret.Parse(presented)
	if perr != nil {
		return nil, status.Error(codes.Unauthenticated, "credential refused")
	}
	want, ok := f.byCredential[p.CredentialID]
	if !ok || want != presented {
		return nil, status.Error(codes.Unauthenticated, "credential refused")
	}
	return &iamv1.ResolveBasicCredentialResponse{
		PrincipalType: "user",
		PrincipalId:   "usr0000000000000bat1",
		DisplayName:   "Бат Один",
		CredentialId:  p.CredentialID,
	}, nil
}

func (f *fakeAuthority) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func newLane(t *testing.T, auth *fakeAuthority, now func() time.Time) *middleware.BasicCredentialLane {
	t.Helper()
	return middleware.NewBasicCredentialLane(auth).WithClock(now)
}

func mintFor(t *testing.T, auth *fakeAuthority, credID string) string {
	t.Helper()
	s, _, err := credsecret.Mint(credID)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if auth.byCredential == nil {
		auth.byCredential = map[string]string{}
	}
	auth.byCredential[credID] = s
	return s
}

// BAT-1-29 — классификация по МАРКЕ, а не «не подошло никуда». Положительный
// контроль в ТОМ ЖЕ сценарии двумя входами: годная строка нашей марки уходит на
// нашу полосу, подписанный токен — НЕ уходит.
func TestBAT1_29_LaneIsChosenByTheMarkAndRejectsExplicitly(t *testing.T) {
	auth := &fakeAuthority{}
	lane := newLane(t, auth, time.Now)
	good := mintFor(t, auth, "uoc_0000000000000bat1")

	// Не наша полоса — три разных негодных входа, ни один не становится нашим.
	for _, in := range []string{
		"eyJhbGciOiJFUzI1NiJ9.e30.sig",
		"просто строка",
		"",
	} {
		if lane.Owns(in) {
			t.Errorf("полоса присвоила чужой вход %q", in)
		}
	}
	// Положительный контроль: наша строка — наша полоса.
	if !lane.Owns(good) {
		t.Error("годная строка нашей марки не опознана полосой")
	}
	// И ещё один: строка нашей марки с негодной формой ОСТАЁТСЯ нашей —
	// вердикт по ней выносим МЫ, а не отдаём дальше как «удостоверения нет».
	if !lane.Owns(credsecret.Mark + "uoc_0000000000000bat1_ffffffffffffffffffffffffffffffff") {
		t.Error("негодная строка нашей марки отдана другой полосе — полоса не терминальна")
	}
}

// BAT-1-27 + BAT-1-07 — негодная контрольная сумма отвергается НА СЛОЕ
// АУТЕНТИФИКАЦИИ и стоит НОЛЬ обращений к авторитету. Утверждается ПАРА: исход
// и число вызовов — один код ответа этого свойства не измеряет.
func TestBAT1_27_MalformedIsRefusedWithZeroAuthorityCalls(t *testing.T) {
	auth := &fakeAuthority{}
	lane := newLane(t, auth, time.Now)
	good := mintFor(t, auth, "uoc_0000000000000bat1")
	p, _ := credsecret.Parse(good)

	bad := credsecret.Mark + "uoc_0000000000000bat1_" + p.SecretPart + "zzzzzz"
	if _, err := lane.Verify(context.Background(), bad); err == nil {
		t.Fatal("негодная контрольная сумма принята")
	}
	if got := auth.callCount(); got != 0 {
		t.Errorf("обращений к авторитету %d, ожидался ноль — уровень 2 не отсёк", got)
	}

	// Положительный контроль: годная строка даёт ОДИН вызов.
	if _, err := lane.Verify(context.Background(), good); err != nil {
		t.Fatalf("годная строка отвергнута: %v", err)
	}
	if got := auth.callCount(); got != 1 {
		t.Errorf("обращений к авторитету %d, ожидалось одно", got)
	}
}

// BAT-1-33 / BAT-1-34 — стоимость: M запросов ОДНИМ удостоверением внутри окна
// дают ОДИН вызов; после окна — второй; два РАЗНЫХ удостоверения дают два.
func TestBAT1_33_VerdictCacheCostsOneCallPerCredentialPerWindow(t *testing.T) {
	auth := &fakeAuthority{}
	clock := time.Now()
	lane := newLane(t, auth, func() time.Time { return clock })

	first := mintFor(t, auth, "uoc_0000000000000bat1")
	second := mintFor(t, auth, "uoc_0000000000000bat2")

	for i := 0; i < 25; i++ {
		if _, err := lane.Verify(context.Background(), first); err != nil {
			t.Fatalf("запрос %d отвергнут: %v", i, err)
		}
	}
	if got := auth.callCount(); got != 1 {
		t.Fatalf("обращений %d на 25 запросов одним удостоверением, ожидалось 1", got)
	}

	// Второе удостоверение — второй вызов: вердикт одного не отвечает за другое.
	if _, err := lane.Verify(context.Background(), second); err != nil {
		t.Fatalf("второе удостоверение отвергнуто: %v", err)
	}
	if got := auth.callCount(); got != 2 {
		t.Fatalf("обращений %d, ожидалось 2 — вердикт одного достался другому", got)
	}

	// Положительный контроль истечения: без него кэш без срока неотличим от
	// кэша со сроком.
	clock = clock.Add(middleware.BasicCredentialVerdictWindow + time.Second)
	if _, err := lane.Verify(context.Background(), first); err != nil {
		t.Fatalf("после окна отвергнуто: %v", err)
	}
	if got := auth.callCount(); got != 3 {
		t.Errorf("обращений %d после истечения окна, ожидалось 3", got)
	}
}

// BAT-1-35 — КЛЮЧ КЭША. Тот же идентификатор с ДРУГОЙ секретной частью вердикта
// из кэша не получает; сырая предъявленная строка в карту не попадает.
func TestBAT1_35_CacheKeyBindsTheIdentifierToThePresentedString(t *testing.T) {
	auth := &fakeAuthority{}
	clock := time.Now()
	lane := newLane(t, auth, func() time.Time { return clock })
	good := mintFor(t, auth, "uoc_0000000000000bat1")

	if _, err := lane.Verify(context.Background(), good); err != nil {
		t.Fatalf("годная строка отвергнута: %v", err)
	}

	// Другая секретная часть при том же идентификаторе.
	forged, _, err := credsecret.Mint("uoc_0000000000000bat1")
	if err != nil {
		t.Fatal(err)
	}
	if forged == good {
		t.Fatal("чеканка повторилась — проба беспредметна")
	}
	if _, err := lane.Verify(context.Background(), forged); err == nil {
		t.Error("подделка с верным идентификатором получила вердикт из кэша")
	}

	// Положительный контроль: годная строка по-прежнему проходит ИЗ КЭША.
	before := auth.callCount()
	if _, err := lane.Verify(context.Background(), good); err != nil {
		t.Fatalf("годная строка перестала проходить: %v", err)
	}
	if auth.callCount() != before {
		t.Error("годная строка перестала обслуживаться кэшем")
	}

	// Состав ключа: сырой строки в нём нет.
	for _, k := range lane.CacheKeysForTest() {
		if k == good || k == forged {
			t.Error("сырая предъявленная строка попала в ключ кэша")
		}
	}
}

// BAT-1-36 — fail-closed: молчание авторитета есть ОТКАЗ, и это ОТДЕЛЬНЫЙ исход
// от отказа в самом удостоверении.
func TestBAT1_36_AuthoritySilenceIsRefusalAndIsADistinctOutcome(t *testing.T) {
	auth := &fakeAuthority{err: status.Error(codes.Unavailable, "peer down")}
	lane := newLane(t, auth, time.Now)
	good, _, _ := credsecret.Mint("uoc_0000000000000bat1")

	_, err := lane.Verify(context.Background(), good)
	if err == nil {
		t.Fatal("молчание авторитета дало проход — контроль мягкий там, где отзыв наш")
	}
	if !errors.Is(err, middleware.ErrCredentialStateUnknown) {
		t.Errorf("исход = %v, ожидался ОТДЕЛЬНЫЙ «состояние не установлено»", err)
	}
	if errors.Is(err, middleware.ErrCredentialRefused) {
		t.Error("недоступность авторитета подана как отказ в удостоверении — вызывающему нечего исправлять")
	}

	// Положительный контроль: отвечающий авторитет даёт проход.
	auth2 := &fakeAuthority{}
	lane2 := newLane(t, auth2, time.Now)
	ok := mintFor(t, auth2, "uoc_0000000000000bat1")
	if _, err := lane2.Verify(context.Background(), ok); err != nil {
		t.Fatalf("отвечающий авторитет отверг годное: %v", err)
	}

	// Авторитет, ответивший НЕ ТЕМ (по адресу не тот эндпоинт), даёт тот же
	// отказ: это настройка, а не сбой, и повтором она не лечится.
	auth3 := &fakeAuthority{err: status.Error(codes.Unimplemented, "unknown method")}
	lane3 := newLane(t, auth3, time.Now)
	if _, err := lane3.Verify(context.Background(), ok); !errors.Is(err, middleware.ErrCredentialStateUnknown) {
		t.Errorf("ответ не того эндпоинта = %v, ожидалось «состояние не установлено»", err)
	}
}

// BAT-1-31 — удостоверение объявляет себя ПРЕДЪЯВИТЕЛЬСКИМ: признака привязки
// у него нет никогда.
func TestBAT1_31_TheCredentialDeclaresItselfBearerAndCarriesNoBinding(t *testing.T) {
	auth := &fakeAuthority{}
	lane := newLane(t, auth, time.Now)
	good := mintFor(t, auth, "uoc_0000000000000bat1")

	vt, err := lane.Verify(context.Background(), good)
	if err != nil {
		t.Fatalf("годное отвергнуто: %v", err)
	}
	if vt.Confirmation != "" {
		t.Errorf("признак привязки заполнен (%q) — вид предъявительский by construction", vt.Confirmation)
	}
	// BAT-1-30 — поля, читаемые нижележащими контролями, заполнены ВСЕ.
	if vt.PrincipalType == "" || vt.PrincipalID == "" || vt.CredentialID == "" {
		t.Errorf("поле проверенного удостоверения не заполнено: %+v", vt)
	}
	if vt.AuthenticationLevel != middleware.BasicCredentialLevel {
		t.Errorf("уровень = %q, объявлен %q", vt.AuthenticationLevel, middleware.BasicCredentialLevel)
	}
}

// BAT-1-58 / BAT-1-59 / BAT-1-60 — УРОВЕНЬ УДОСТОВЕРЕНИЯ ДОЕЗЖАЕТ ДО СТРАЖА
// ПОВЫШЕНИЯ.
//
// Полоса, заполнившая принципала и не заполнившая уровень, делает страж
// ПРОЙДЕННЫМ МИМО, а не успешно, — и это неотличимо от исправной работы:
// снаружи запрос проходит, страж не сработал ни разу, и «ноль отказов за всю
// жизнь контроля» никто не заметил.
//
// Утверждается ПАРА: величина доехала И она равна объявленной. Одно
// «непусто» зеленело бы на любом мусоре, одно «равно 1» — на непроставленном
// поле, если бы умолчанием оказался тот же символ.
func TestBAT1_58_TheCredentialLevelReachesTheStepUpGuardOnBothSurfaces(t *testing.T) {
	auth := &fakeAuthority{}
	lane := newLane(t, auth, time.Now)
	good := mintFor(t, auth, "uoc_0000000000000bat1")

	interceptor := middleware.NewAuthInterceptor(
		middleware.AuthModeProduction, "", nil, slog.New(slog.NewTextHandler(io.Discard, nil)),
	).WithBasicCredentialLane(lane)

	// REST-поверхность.
	var gotHeader, gotGRPCHeader string
	h := interceptor.HTTP(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Kacho-Token-Acr")
		gotGRPCHeader = r.Header.Get("Grpc-Metadata-X-Kacho-Token-Acr")
	}))
	req := httptest.NewRequest(http.MethodGet, "/iam/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+good)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("годное удостоверение отвергнуто REST-поверхностью: %d %s", rec.Code, rec.Body.String())
	}
	if gotHeader != middleware.BasicCredentialLevel {
		t.Errorf("уровень в заголовке = %q, объявлен %q — страж повышения читает пустоту",
			gotHeader, middleware.BasicCredentialLevel)
	}
	if gotGRPCHeader != middleware.BasicCredentialLevel {
		t.Errorf("уровень в grpc-зеркале заголовка = %q, объявлен %q",
			gotGRPCHeader, middleware.BasicCredentialLevel)
	}

	// Отрицательный контроль ТОЙ ЖЕ поверхности: негодная строка нашей марки
	// не проходит и уровня не проставляет — иначе «уровень доехал» было бы
	// верно и о полосе, проставляющей его всякому входу.
	gotHeader = ""
	badReq := httptest.NewRequest(http.MethodGet, "/iam/v1/me", nil)
	badReq.Header.Set("Authorization", "Bearer "+credsecret.Mark+"uoc_0000000000000bat1_zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	badRec := httptest.NewRecorder()
	h.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusUnauthorized {
		t.Errorf("негодная строка нашей марки дала %d, ожидался 401 — полоса не терминальна", badRec.Code)
	}
	if gotHeader != "" {
		t.Errorf("уровень проставлен негодному удостоверению: %q", gotHeader)
	}
}
