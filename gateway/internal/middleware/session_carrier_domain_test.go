// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

// session_carrier_domain_test.go — kacho#3174: при заданном домене печенья
// сессии отказ края по отсечке гасит печенье с атрибутами выдачи, и БРАУЗЕР его
// удаляет.
//
// Судья — `net/http/cookiejar`: он сопоставляет гашение с хранимым печеньем по
// имени, пути и домену, как браузер. Выдача повторяет атрибуты службы
// (kaname `loginlanehttp.sessionCookie`: `Path=/`, `Domain` = настройка
// `authn.login.cookie-domain`, `HttpOnly`, `Secure`, `SameSite=Lax`); консоль
// и край — разные узлы под одним доменом, ровно ради этого домен и задаётся.

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

const probeCookieDomain = "kacho.example"

// issuedByService — печенье сессии, как его выдаёт служба при домене d
// (пусто — только на хост).
func issuedByService(d string) *http.Cookie {
	return &http.Cookie{
		Name: OurSessionCarrierName, Value: "v-own", Path: "/", Domain: d,
		MaxAge: 3600, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	}
}

// cutoffRefusalOnJar — браузер с печеньем, выданным службой на консоли при
// домене issuedDomain, обращается к краю, сконфигурированному доменом
// edgeDomain; сессия за отсечкой. Возвращает, осталось ли печенье в браузере.
func cutoffRefusalOnJar(t *testing.T, issuedDomain, edgeDomain string) (stillThere bool) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	console, _ := url.Parse("https://console." + probeCookieDomain + "/")
	edge, _ := url.Parse("https://api." + probeCookieDomain + "/vpc/v1/networks")
	jar.SetCookies(console, []*http.Cookie{issuedByService(issuedDomain)})
	sent := jar.Cookies(edge)
	if issuedDomain != "" && len(sent) != 1 {
		t.Fatalf("предпосылка: браузер не несёт печенье домена %q краю (%d печений)", issuedDomain, len(sent))
	}

	authAt := time.Now().Add(-2 * time.Hour)
	a := NewAuthInterceptor(AuthModeDev, "",
		cutoffLookup{subj: Subject{Type: "user", ID: "usr-1", DisplayName: "A"}},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	).WithHumanSession(cookieLaneSession(authAt)).
		WithSessionCutoffCheck(&fakeCutoff{cutoff: authAt.Add(time.Hour), found: true}, time.Hour).
		WithSessionCookieDomain(edgeDomain)

	req := httptest.NewRequest(http.MethodGet, edge.String(), nil)
	req.AddCookie(&http.Cookie{Name: OurSessionCarrierName, Value: "v-own"})
	rec := httptest.NewRecorder()
	a.HTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("сессия за отсечкой дошла до бэкенда")
	})).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("предпосылка: отказ по отсечке 401, получено %d", rec.Code)
	}
	jar.SetCookies(edge, rec.Result().Cookies())
	for _, c := range jar.Cookies(console) {
		if c.Name == OurSessionCarrierName {
			return true
		}
	}
	return false
}

// TestCutoffRefusalEndsTheCarrierUnderTheConfiguredDomain — домен задан у
// службы и у края: браузер печенье удаляет.
func TestCutoffRefusalEndsTheCarrierUnderTheConfiguredDomain(t *testing.T) {
	if cutoffRefusalOnJar(t, probeCookieDomain, probeCookieDomain) {
		t.Fatal("печенье сессии домена " + probeCookieDomain + " осталось в браузере после отказа по отсечке: " +
			"гашение края не совпало с выдачей службы, отказ стоящий")
	}
}

// TestCutoffRefusalEndsAHostOnlyCarrier — законный близнец: посадка без
// домена (`none` у службы, пусто у края) гасит печенье, выданное только на хост.
func TestCutoffRefusalEndsAHostOnlyCarrier(t *testing.T) {
	jar, _ := cookiejar.New(nil)
	edge, _ := url.Parse("https://api." + probeCookieDomain + "/vpc/v1/networks")
	jar.SetCookies(edge, []*http.Cookie{issuedByService("")})
	if len(jar.Cookies(edge)) != 1 {
		t.Fatal("предпосылка: печенье только на хост не записано")
	}
	a := NewAuthInterceptor(AuthModeDev, "",
		cutoffLookup{subj: Subject{Type: "user", ID: "usr-1", DisplayName: "A"}},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	).WithHumanSession(cookieLaneSession(time.Now().Add(-2*time.Hour))).
		WithSessionCutoffCheck(&fakeCutoff{cutoff: time.Now().Add(-time.Hour), found: true}, time.Hour).
		WithSessionCookieDomain("")
	req := httptest.NewRequest(http.MethodGet, edge.String(), nil)
	req.AddCookie(&http.Cookie{Name: OurSessionCarrierName, Value: "v-own"})
	rec := httptest.NewRecorder()
	a.HTTP(http.NotFoundHandler()).ServeHTTP(rec, req)
	jar.SetCookies(edge, rec.Result().Cookies())
	if len(jar.Cookies(edge)) != 0 {
		t.Fatal("печенье только на хост осталось после отказа по отсечке")
	}
}

// TestEndingAttributesAreTheIssuanceAttributes — атрибуты гашения равны
// атрибутам выдачи службы при том же домене: имя, путь, домен, HttpOnly,
// Secure, SameSite. Срок — единственное, чем они различаются.
func TestEndingAttributesAreTheIssuanceAttributes(t *testing.T) {
	for _, d := range []string{"", probeCookieDomain} {
		endings := SessionCarrierEndings(d)
		if len(endings) != 1 {
			t.Fatalf("домен %q: гашений %d, ожидалось 1", d, len(endings))
		}
		got, want := *endings[0], *issuedByService(d)
		if got.Name != want.Name || got.Path != want.Path || got.Domain != want.Domain ||
			got.HttpOnly != want.HttpOnly || got.Secure != want.Secure || got.SameSite != want.SameSite {
			t.Errorf("домен %q: гашение %q расходится с выдачей %q", d, got.String(), want.String())
		}
		if got.MaxAge >= 0 || got.Value != "" {
			t.Errorf("домен %q: гашение не гасит: %q", d, got.String())
		}
	}
}
