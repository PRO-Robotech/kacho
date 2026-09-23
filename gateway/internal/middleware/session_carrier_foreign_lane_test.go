// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// session_carrier_foreign_lane_test.go — ЧУЖАЯ ПОЛОСА (посадка `external`)
// спрашивает поставщика РОВНО его печеньем и доносит его уровень до замка.
//
// # Предмет
//
// Под `external` край читает только носитель поставщика (`tryKratosSession`),
// а наш не читает вовсе. Браузер при этом может нести оба печенья сразу: наше
// остаётся со стенда, переведённого между посадками, либо приходит с соседней
// установки того же домена. Значение нашего носителя предъявительское — кто
// его держит, тот и предъявляет сессию, — поэтому чужой стороне уходит только
// её печенье, а не заголовок `Cookie` целиком. Предикаты присутствия обоих
// носителей судят печенье по ИМЕНИ, а не по подстроке заголовка.
//
// Дублёры ЗАПИСЫВАЮТ полученное: свойство читается из наблюдения, а не из кода.
package middleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// recordingProviderStub — чужая сторона, запоминающая КАЖДЫЙ полученный `Cookie`.
func recordingProviderStub(t *testing.T, got *[]string, mu *sync.Mutex) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*got = append(*got, r.Header.Get("Cookie"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"active":true,"authenticated_at":"`+
			ownAuthAt.UTC().Format(time.RFC3339Nano)+
			`","identity":{"id":"kid-foreign","traits":{"email":"foreign@example.com"}}}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

const ourBearerValue = "OURS-LIVE-BEARER"

func TestForeignLane_NeverCarriesOurBearerToTheForeignSide(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	provider := recordingProviderStub(t, &seen, &mu)

	// Посадка `external`: нашего читателя нет, а наш носитель в браузере есть —
	// например, оставшийся со стенда, переведённого между посадками. Чужая
	// полоса обязана сработать — и обязана уйти без нашего значения.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	foreignSubject := cutoffLookup{subj: Subject{Type: "user", ID: "usr-foreign", DisplayName: "F"}}
	a := NewAuthInterceptor(AuthModeDev, "", foreignSubject, logger).
		WithKratos(NewKratosClient(provider.URL)).
		WithSessionCutoffCheck(&fakeCutoff{}, time.Hour)
	who := NewSessionIdentityHandler(logger).
		WithKratos(NewKratosClient(provider.URL), foreignSubject).
		WithSessionCutoff(&fakeCutoff{})
	mux := http.NewServeMux()
	who.Register(mux)
	mux.Handle("/", &countingNext{})
	chain := a.HTTP(mux)

	paths := []string{platformPath, "/iam/v1/auth/me"}
	for _, path := range paths {
		serve(chain, withForeignCarrier(
			withOurCarrier(httptest.NewRequest(http.MethodGet, path, nil), ourBearerValue),
			"foreign-live"))
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 {
		t.Fatalf("чужая сторона не спрошена ни разу на %d путях — проба судила бы о непроисходившем",
			len(paths))
	}
	leaked := 0
	for _, hdr := range seen {
		if strings.Contains(hdr, ourBearerValue) || strings.Contains(hdr, OurSessionCarrierName) {
			leaked++
			t.Errorf("чужой стороне ушёл наш носитель в заголовке %q: значение предъявительское, "+
				"и державший его предъявляет нашу сессию", hdr)
		}
		if !strings.Contains(hdr, providerSessionCarrierName) {
			t.Errorf("чужой стороне ушёл заголовок БЕЗ её печенья (%q) — она не смогла бы ответить", hdr)
		}
	}
	t.Logf("перепись: обращений к чужой стороне %d · несущих значение нашего носителя %d · путей %d",
		len(seen), leaked, len(paths))
}

// Граница имени. Предикат присутствия чужого носителя искал ПОДСТРОКУ в
// заголовке: имя, оказавшееся ЧАСТЬЮ чужого имени печенья, считалось
// предъявлением. Половины парные — и сужение обязано не съесть законное.
func TestProviderCarrierPredicate_MatchesTheNameAndNotItsSubstring(t *testing.T) {
	cases := []struct {
		name    string
		cookies []*http.Cookie
		want    bool
	}{
		{"своё имя — предъявлено", []*http.Cookie{{Name: providerSessionCarrierName, Value: "v"}}, true},
		{"рядом с нашим — предъявлено", []*http.Cookie{
			{Name: OurSessionCarrierName, Value: "o"},
			{Name: providerSessionCarrierName, Value: "v"},
		}, true},
		{"имя как ПРИСТАВКА чужого печенья", []*http.Cookie{
			{Name: providerSessionCarrierName + "_debug", Value: "v"}}, false},
		{"имя как ОКОНЧАНИЕ чужого печенья", []*http.Cookie{
			{Name: "x_" + providerSessionCarrierName, Value: "v"}}, false},
		{"имя в ЗНАЧЕНИИ чужого печенья", []*http.Cookie{
			{Name: "note", Value: providerSessionCarrierName}}, false},
		{"печенья нет вовсе", nil, false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, platformPath, nil)
		for _, c := range tc.cookies {
			req.AddCookie(c)
		}
		if got := providerSessionCarrierPresented(req); got != tc.want {
			t.Errorf("%s: предикат ответил %v, ожидалось %v (заголовок %q)",
				tc.name, got, tc.want, req.Header.Get("Cookie"))
		}
	}
	t.Logf("перепись: форм заголовка проверено %d · положительных 2 · отрицательных 4", len(cases))
}

// Решение, ставшее наблюдаемым вместе с разбором по имени: значение, которого
// браузер провести НЕ МОЖЕТ, носителем не является — ни нашим, ни чужим.
//
// Выбор fail-closed и назван вслух: принять неразбираемое значение значило бы
// вынести соседу то, чего мы сами не прочитали, а отказ здесь неотличим для
// человека от «сессии нет» — состояния, в котором он и находится, раз его
// печенье до нас не доехало целым.
func TestCarrierPredicates_AValueNoBrowserCanFrameIsNotACarrier(t *testing.T) {
	// Октеты вне RFC 6265 для значения печенья: разбор их отвергает, а клиент
	// Go при отправке печатает «dropping invalid bytes».
	const unframeable = "знач;ение"
	req := httptest.NewRequest(http.MethodGet, platformPath, nil)
	req.Header.Set("Cookie",
		OurSessionCarrierName+"="+unframeable+"; "+providerSessionCarrierName+"="+unframeable)

	if _, ours := ourSessionCarrierOf(req); ours {
		t.Error("наш носитель признан предъявленным на значении, которого браузер провести не может")
	}
	if providerSessionCarrierPresented(req) {
		t.Error("чужой носитель признан предъявленным на значении, которого браузер провести не может")
	}
	if got := ProviderSessionCarrierHeader(req); got != "" {
		t.Errorf("чужой стороне собран заголовок %q из непрочитанного значения", got)
	}

	// Положительная половина: то же имя с ПРОВОДИМЫМ значением — носитель.
	// Без неё проба зеленела бы и на предикатах, отвергающих всё подряд.
	ok := httptest.NewRequest(http.MethodGet, platformPath, nil)
	ok.AddCookie(&http.Cookie{Name: OurSessionCarrierName, Value: "v-own"})
	ok.AddCookie(&http.Cookie{Name: providerSessionCarrierName, Value: "v-foreign"})
	if _, ours := ourSessionCarrierOf(ok); !ours {
		t.Fatal("наш носитель с проводимым значением не признан — предикат отвергает законный вход")
	}
	if !providerSessionCarrierPresented(ok) {
		t.Fatal("чужой носитель с проводимым значением не признан — предикат отвергает законный вход")
	}
	t.Logf("перепись: сторон проверено 2 · непроводимых значений отвергнуто 2 · проводимых принято 2")
}

// ─────────────────────────────────────────────────────────────────────────────
// УРОВЕНЬ ЧУЖОЙ СЕССИИ ДОЕЗЖАЕТ ДО ЗАМКА.
//
// Под `external` сессия поставщика — единственная сессия человека, и её
// уровень уверенности (`aal2`) обязан доехать до замка второго фактора как
// «2». Отнять его значило бы отнять второй фактор у тех, кому его нечем
// заменить: нашей чеканки на этой посадке нет.

// stepUpFloorPath — глагол с ПОЛОЖИТЕЛЬНЫМ полом в каталоге прав.
const stepUpFloorPath = "/iam/v1/users/usr-abc/tokens"

// mfaProviderStub — чужая сторона, называющая уровень `aal2`.
func mfaProviderStub(t *testing.T, asked *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"active":true,"authenticated_at":"`+
			ownAuthAt.UTC().Format(time.RFC3339Nano)+
			`","authenticator_assurance_level":"aal2",`+
			`"identity":{"id":"kid-foreign","traits":{"email":"foreign@example.com"}}}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// serveForACR прогоняет запрос и возвращает уровень, доехавший до следующего
// звена. Отказ замка — тоже исход: уровень при нём пуст либо недостаточен.
func serveForACR(t *testing.T, a *AuthInterceptor, req *http.Request) string {
	t.Helper()
	got := ""
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(principalmeta.HeaderTokenACR)
	})
	serve(a.HTTP(next), req)
	return got
}

func withForeignCarrier(req *http.Request, value string) *http.Request {
	req.AddCookie(&http.Cookie{Name: providerSessionCarrierName, Value: value})
	return req
}

func TestForeignLane_CarriesTheForeignSecondFactorToTheLock(t *testing.T) {
	var asked atomic.Int64
	provider := mfaProviderStub(t, &asked)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	foreignOnly := NewAuthInterceptor(AuthModeDev, "",
		cutoffLookup{subj: Subject{Type: "user", ID: "usr-foreign", DisplayName: "F"}}, logger).
		WithKratos(NewKratosClient(provider.URL)).
		WithSessionCutoffCheck(&fakeCutoff{}, time.Hour)

	got := serveForACR(t, foreignOnly, withForeignCarrier(
		httptest.NewRequest(http.MethodGet, stepUpFloorPath, nil), "foreign-live"))
	if asked.Load() == 0 {
		t.Fatalf("чужая сторона не спрошена ни разу — проба судила бы о непроисходившем")
	}
	if got != "2" {
		t.Fatalf("под `external` уровень чужой сессии доехал как %q, ожидалось \"2\" — "+
			"второй фактор отнят у тех, кому его нечем заменить", got)
	}
	t.Logf("перепись: посадка external · обращений к чужой стороне %d · уровень, доехавший до замка %q",
		asked.Load(), got)
}
