// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// session_carrier_posture_test.go — ЧЬЁ ПЕЧЕНЬЕ КРАЙ ЧИТАЕТ, и это наблюдается
// на конфигурации, разобранной из окружения.
//
// # Что судится и чем
//
// Посадка берётся тем же разбором, что у процесса (`config.Load` →
// `ResolvedIdentityProvider`), читатель заводится теми же функциями
// композиционного корня (`wireLaneCarrierReader`, `wireWhoAmICarrierReader`), а
// вердикт читается из НАБЛЮДЕНИЯ: дублёр нашего читателя считает обращения, и
// «не читается» означает ноль обращений при предъявленном печенье, а не
// отсутствие строки в коде.
//
// Читатель у края один — наш (#2792). Под `own` край читает наш носитель и
// только его: запрос с одним лишь чужим печеньем сессии не доходит ни до кого.
// Законный близнец меняет ОДИН факт — посадку в окружении: вне `own` не
// читается и наш носитель, потому что читателя под другой посадкой не
// заводится. Путей два — полоса личности и маршрут «кто я», — потому что читают
// сессию обе.
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

// postureSubjects — резолвер субъекта полосы.
type postureSubjects struct{}

func (postureSubjects) LookupByExternalID(context.Context, string) (middleware.Subject, error) {
	return middleware.Subject{Type: "user", ID: "usr-own", DisplayName: "O"}, nil
}

// foreignCarrierName — печенье сессии, которого край не читает: носитель у
// края один, наш, и всякое другое печенье носителем не является.
const foreignCarrierName = "foreign_session"

func TestSessionCarrierReader_FollowsThePostureParsedFromTheEnvironment(t *testing.T) {
	cases := []struct {
		posture  string
		wantOurs bool
	}{
		{posture: "own", wantOurs: true},
		{posture: "external", wantOurs: false},
	}
	paths := []string{"/vpc/v1/networks", "/iam/v1/auth/me"}

	for _, tc := range cases {
		t.Run(tc.posture, func(t *testing.T) {
			t.Setenv("KACHO_API_GATEWAY_IDENTITY_PROVIDER", tc.posture)

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
				lane, ours, logger)
			who := wireWhoAmICarrierReader(middleware.NewSessionIdentityHandler(logger), lane, ours)
			mux := http.NewServeMux()
			who.Register(mux)
			mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			chain := auth.HTTP(mux)

			// Каждое печенье предъявляется ОДНО — так ответ «чьё печенье читается»
			// не зависит от старшинства между двумя предъявленными.
			for _, path := range paths {
				foreign := httptest.NewRequest(http.MethodGet, path, nil)
				foreign.AddCookie(&http.Cookie{Name: foreignCarrierName, Value: "foreign-" + tc.posture})
				chain.ServeHTTP(httptest.NewRecorder(), foreign)
			}
			if n := ours.asked.Load(); n != 0 {
				t.Fatalf("посадка %q: чужое печенье дошло до нашего читателя (%d обращений)", tc.posture, n)
			}
			for _, path := range paths {
				withOurs := httptest.NewRequest(http.MethodGet, path, nil)
				withOurs.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: "ours-" + tc.posture})
				chain.ServeHTTP(httptest.NewRecorder(), withOurs)
			}

			if oursRead := ours.asked.Load() > 0; oursRead != tc.wantOurs {
				t.Errorf("посадка %q: наш носитель читается=%v (обращений %d), ожидалось %v",
					tc.posture, oursRead, ours.asked.Load(), tc.wantOurs)
			}
			t.Logf("посадка %q · путей %d · обращений к нашему читателю %d",
				tc.posture, len(paths), ours.asked.Load())
		})
	}
}
