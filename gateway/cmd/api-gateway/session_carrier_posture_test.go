// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// session_carrier_posture_test.go — ЧЬЁ ПЕЧЕНЬЕ КРАЙ ЧИТАЕТ, РЕШАЕТ ПОСАДКА, и
// это наблюдается на конфигурации, разобранной из окружения.
//
// # Что судится и чем
//
// Посадка берётся тем же разбором, что у процесса (`config.Load` →
// `ResolvedIdentityProvider`), читатели заводятся теми же функциями
// композиционного корня (`wireLaneCarrierReader`, `wireWhoAmICarrierReader`), а
// вердикт читается из НАБЛЮДЕНИЯ: дублёры обоих читателей считают обращения, и
// «не читается» означает ноль обращений при предъявленном печенье, а не
// отсутствие строки в коде.
//
// Под `own` край читает только наш носитель: запрос с одним лишь печеньем
// поставщика до поставщика не доходит. Под `external` — только носитель
// поставщика: запрос с одним лишь нашим печеньем до нашего читателя не
// доходит. Законный близнец меняет ОДИН факт — посадку в окружении, — и
// читатель меняется вместе с ней. Путей два — полоса личности и маршрут «кто
// я», — потому что читают сессию обе.
package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/internal/privateloopback"
)

// countingOurSession — наш читатель сессии, считающий обращения. Отвечает
// живой сессией: в выигрыше он отказать не может, поэтому ноль обращений —
// решение края, а не неудача дублёра.
type countingOurSession struct{ asked atomic.Int64 }

func (c *countingOurSession) ResolveHumanSession(context.Context, string) (middleware.HumanSession, bool, error) {
	c.asked.Add(1)
	at := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	return middleware.HumanSession{
		UserID: "usr-own", DisplayName: "O", AuthenticatedAt: at,
		ExpiresAt: at.Add(time.Hour), AssuranceLevel: "1",
	}, true, nil
}

// postureSubjects — резолвер субъекта для полосы поставщика.
type postureSubjects struct{}

func (postureSubjects) LookupByExternalID(context.Context, string) (middleware.Subject, error) {
	return middleware.Subject{Type: "user", ID: "usr-foreign", DisplayName: "F"}, nil
}

// countingProvider — поставщик, считающий обращения и отвечающий живой сессией.
func countingProvider(t *testing.T, asked *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := privateloopback.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"active":true,"authenticated_at":"2026-09-22T12:00:00Z",`+
			`"identity":{"id":"kid-foreign","traits":{"email":"foreign@example.com"}}}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// providerCarrierName — имя печенья поставщика, взятое у ПРОИЗВОДИТЕЛЯ
// перечня имён, а не выписанное здесь.
func providerCarrierName(t *testing.T) string {
	t.Helper()
	for _, n := range middleware.SessionCarrierNames() {
		if n != middleware.OurSessionCarrierName {
			return n
		}
	}
	t.Fatal("в перечне имён носителя нет имени поставщика — судить нечего")
	return ""
}

func TestSessionCarrierReader_FollowsThePostureParsedFromTheEnvironment(t *testing.T) {
	cases := []struct {
		posture          string
		wantOurs         bool
		wantProviderRead bool
	}{
		{posture: "own", wantOurs: true, wantProviderRead: false},
		{posture: "external", wantOurs: false, wantProviderRead: true},
	}
	provider := providerCarrierName(t)
	paths := []string{"/vpc/v1/networks", "/iam/v1/auth/me"}

	for _, tc := range cases {
		t.Run(tc.posture, func(t *testing.T) {
			var providerAsked atomic.Int64
			srv := countingProvider(t, &providerAsked)
			t.Setenv("KACHO_API_GATEWAY_IDENTITY_PROVIDER", tc.posture)
			t.Setenv("KACHO_API_GATEWAY_KRATOS_PUBLIC_URL", srv.URL)

			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("конфигурация края не разобралась: %v", err)
			}
			lane, err := cfg.ResolvedIdentityProvider()
			if err != nil {
				t.Fatalf("посадка %q не разобралась: %v", tc.posture, err)
			}

			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			ours := &countingOurSession{}
			auth := wireLaneCarrierReader(
				middleware.NewAuthInterceptor(middleware.AuthModeDev, "", postureSubjects{}, logger),
				lane, cfg.KratosPublicURL, ours, logger)
			who := wireWhoAmICarrierReader(middleware.NewSessionIdentityHandler(logger),
				lane, cfg.KratosPublicURL, ours, postureSubjects{})
			mux := http.NewServeMux()
			who.Register(mux)
			mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			chain := auth.HTTP(mux)

			// Каждое печенье предъявляется ОДНО — так ответ «чьё печенье читается»
			// не зависит от старшинства между двумя предъявленными.
			for _, path := range paths {
				withOurs := httptest.NewRequest(http.MethodGet, path, nil)
				withOurs.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: "ours-" + tc.posture})
				chain.ServeHTTP(httptest.NewRecorder(), withOurs)

				withProvider := httptest.NewRequest(http.MethodGet, path, nil)
				withProvider.AddCookie(&http.Cookie{Name: provider, Value: "provider-" + tc.posture + path})
				chain.ServeHTTP(httptest.NewRecorder(), withProvider)
			}

			oursRead, providerRead := ours.asked.Load() > 0, providerAsked.Load() > 0
			if oursRead != tc.wantOurs {
				t.Errorf("посадка %q: наш носитель читается=%v (обращений %d), ожидалось %v",
					tc.posture, oursRead, ours.asked.Load(), tc.wantOurs)
			}
			if providerRead != tc.wantProviderRead {
				t.Errorf("посадка %q: носитель поставщика читается=%v (обращений %d), ожидалось %v",
					tc.posture, providerRead, providerAsked.Load(), tc.wantProviderRead)
			}
			t.Logf("посадка %q · путей %d · обращений к нашему читателю %d · к поставщику %d",
				tc.posture, len(paths), ours.asked.Load(), providerAsked.Load())
		})
	}
}
