// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/clients"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/streamrevocation"
	"github.com/PRO-Robotech/kacho/internal/privateloopback"
)

// kacho#2741 — «метода нет» несёт два диагноза с противоположным действием:
//   - слушатель СЛУЖБЫ не знает вовсе (спрошен не тот слушатель) — настройка,
//     повтор бесполезен;
//   - служба есть, глагола у сборки нет (окно раската) — повтор поможет.
//
// Входы — НАСТОЯЩИЕ отказы библиотеки на проводе: слушатель без
// зарегистрированной службы и слушатель со службой, чей глагол не реализован.

func adapterTo(t *testing.T, register func(*grpc.Server)) *clients.SessionRevocationsAdapter {
	t.Helper()
	lis := privateloopback.Listen(t)
	srv := grpc.NewServer()
	register(srv)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return clients.NewSessionRevocationsAdapter(conn, time.Second)
}

type bareServices struct {
	iamv1.UnimplementedInternalHumanSessionServiceServer
	iamv1.UnimplementedInternalSessionRevocationsServiceServer
	iamv1.UnimplementedInternalIAMServiceServer
}

func TestUnimplementedIsDiagnosedAsMisaddressedOrRolloutWindow(t *testing.T) {
	ctx := context.Background()
	subject, _ := middleware.NewCutoffSubject("user", "usr-00000000000002741")

	// Спрошен не тот слушатель: службы на нём нет.
	wrong := adapterTo(t, func(*grpc.Server) {})
	// Окно раската: служба есть, глагол у сборки не реализован.
	skew := adapterTo(t, func(s *grpc.Server) {
		b := bareServices{}
		iamv1.RegisterInternalHumanSessionServiceServer(s, b)
		iamv1.RegisterInternalSessionRevocationsServiceServer(s, b)
		iamv1.RegisterInternalIAMServiceServer(s, b)
	})

	type verb struct {
		name        string
		ask         func(*clients.SessionRevocationsAdapter) error
		unsupported error
	}
	verbs := []verb{
		{"SessionCutoffOf", func(a *clients.SessionRevocationsAdapter) error {
			_, _, err := a.SessionCutoffOf(ctx, subject)
			return err
		}, middleware.ErrSessionCutoffUnsupported},
		{"ResolveHumanSession", func(a *clients.SessionRevocationsAdapter) error {
			_, _, err := a.ResolveHumanSession(ctx, "ka1-live")
			return err
		}, middleware.ErrHumanSessionUnsupported},
		{"IsBasicCredentialLive", func(a *clients.SessionRevocationsAdapter) error {
			_, err := a.IsBasicCredentialLive(ctx, "bas-00000000000002741")
			return err
		}, streamrevocation.ErrBasicCredentialLivenessUnsupported},
	}
	for _, v := range verbs {
		// Обе корзины держат ПРЕЖНИЙ признак «вопрос не предложен» — исход
		// полосы (и ответ арендатору) от диагноза не зависит.
		werr, serr := v.ask(wrong), v.ask(skew)
		if !errors.Is(werr, v.unsupported) || !errors.Is(serr, v.unsupported) {
			t.Errorf("%s: оба входа обязаны нести признак «вопрос не предложен»: не тот слушатель %v · раскат %v", v.name, werr, serr)
		}
		// Диагноз — разный: не тот слушатель лечится настройкой и повтором не
		// лечится; раскат — наоборот.
		if !errors.Is(werr, middleware.ErrIntrospectionMisconfigured) {
			t.Errorf("%s: «службы на слушателе нет» не отнесено к неисправности настройки: %v", v.name, werr)
		}
		if errors.Is(serr, middleware.ErrIntrospectionMisconfigured) {
			t.Errorf("%s: окно раската отнесено к неисправности настройки: %v", v.name, serr)
		}
	}
}

// humanOnly — служба сессии отвечает живой сессией; службы отзыва нет либо
// она без глагола отсечки — по варианту.
type humanOnly struct {
	iamv1.UnimplementedInternalHumanSessionServiceServer
}

func (humanOnly) Resolve(context.Context, *iamv1.ResolveHumanSessionRequest) (*iamv1.ResolveHumanSessionResponse, error) {
	return &iamv1.ResolveHumanSessionResponse{Found: true, Session: &iamv1.HumanSession{
		UserId: "usr-00000000000002741", AuthenticatedAt: timestamppb.Now(), EmailVerified: true, AssuranceLevel: "1",
	}}, nil
}

// TestTenantAnswerIsTheSameForBothDiagnoses — п. 4: ответ, который видит
// арендатор, от диагноза не зависит. Полоса сессии на крае при «вопрос об
// отсечке не предложен» проходит громко в ОБОИХ диагнозах — побайтово тем же
// ответом.
func TestTenantAnswerIsTheSameForBothDiagnoses(t *testing.T) {
	serve := func(withRevocations bool) (int, string) {
		ad := adapterTo(t, func(s *grpc.Server) {
			iamv1.RegisterInternalHumanSessionServiceServer(s, humanOnly{})
			if withRevocations {
				iamv1.RegisterInternalSessionRevocationsServiceServer(s, bareServices{})
			}
		})
		auth := middleware.NewAuthInterceptor(middleware.AuthModeProduction, "", nil,
			slog.New(slog.NewJSONHandler(io.Discard, nil))).
			WithHumanSession(ad).WithSessionCutoffCheck(ad, time.Hour)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/iam/v1/accounts", nil)
		req.AddCookie(&http.Cookie{Name: middleware.OurSessionCarrierName, Value: "s-2741",
			Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		auth.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })).ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	wc, wb := serve(false)
	sc, sb := serve(true)
	if wc != sc || wb != sb || wc != http.StatusOK {
		t.Fatalf("ответ арендатору зависит от диагноза: не тот слушатель %d %q · раскат %d %q", wc, wb, sc, sb)
	}
}
