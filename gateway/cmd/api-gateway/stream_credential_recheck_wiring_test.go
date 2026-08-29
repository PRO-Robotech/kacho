// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// stream_credential_recheck_wiring_test.go — ПРОДОВАЯ точка сборки перепроса
// состояния удостоверения (kacho#1410) и величины, которыми она распоряжается.
//
// # Почему проба здесь, а не только у механизма
//
// Инъекция «передать ноль в точке сборки» оставляет ВЕСЬ корпус проб края
// зелёным и код собирающимся: сквозные пробы зовут конструктор напрямую и
// продовую точку сборки минуют. То есть несделанная провязка была бы
// НЕОТЛИЧИМА от сделанной — ровно тот класс, ради которого задача заведена.

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
)

// revokingAuthority — авторитет, объявляющий отозванным ВСЁ. Годится ровно для
// одного вопроса: доехало ли решение перепроса до реестра, который ему передала
// продовая точка сборки.
type revokingAuthority struct {
	iamv1.UnimplementedInternalSessionRevocationsServiceServer
}

func (revokingAuthority) IsRevoked(
	_ context.Context, _ *iamv1.IsRevokedRequest,
) (*iamv1.IsRevokedResponse, error) {
	return &iamv1.IsRevokedResponse{Revoked: true}, nil
}

// holdingOwner — владелец журнала: открывает поток и держит его.
type holdingOwner struct {
	subscriptionv1.UnimplementedInternalSubscriptionServiceServer
	started chan struct{}
}

func (o *holdingOwner) Subscribe(
	_ *subscriptionv1.SubscriptionRequest,
	stream subscriptionv1.InternalSubscriptionService_SubscribeServer,
) error {
	if err := stream.Send(&subscriptionv1.SubscriptionMessage{
		Message: &subscriptionv1.SubscriptionMessage_Opened{
			Opened: &subscriptionv1.SubscriptionOpened{Position: "p", RetainsEverything: true},
		},
	}); err != nil {
		return err
	}
	close(o.started)
	<-stream.Context().Done()
	return nil
}

func bufconnDial(t *testing.T, register func(*grpc.Server)) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 16)
	srv := grpc.NewServer()
	register(srv)
	go func() { _ = srv.Serve(lis) }()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("дозвон: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		srv.Stop()
		_ = lis.Close()
	})
	return conn
}

func recheckProbeConfig() config.Config {
	return config.Config{
		IntrospectionCacheTTLSeconds: 5,
		SubscriptionStreamBudget:     90 * time.Second,
	}
}

// TestCompositionRootWiresTheStreamRegistryIntoTheCredentialRecheck — несущее
// утверждение: перепрос, собранный ПРОДОВОЙ функцией, закрывает поток ИМЕННО
// той проекции, которую ему передали.
//
// Подделкой реестра здесь пользоваться нельзя: предмет — что край передаёт
// перепросу СВОЙ реестр открытых потоков, а не что-нибудь непустое.
func TestCompositionRootWiresTheStreamRegistryIntoTheCredentialRecheck(t *testing.T) {
	owner := &holdingOwner{started: make(chan struct{})}
	ownerConn := bufconnDial(t, func(s *grpc.Server) {
		subscriptionv1.RegisterInternalSubscriptionServiceServer(s, owner)
	})
	projection, err := subscriptionstream.NewHandler(subscriptionstream.Config{
		Owners: subscriptionstream.Owners{
			"probe": subscriptionv1.NewInternalSubscriptionServiceClient(ownerConn),
		},
		StreamBudget: 90 * time.Second, Heartbeat: 20 * time.Second,
		MaxStreams: 64, MaxStreamsPerSubject: 8, Logger: quietLog(),
	})
	if err != nil {
		t.Fatalf("сборка проекции: %v", err)
	}
	iamConn := bufconnDial(t, func(s *grpc.Server) {
		iamv1.RegisterInternalSessionRevocationsServiceServer(s, revokingAuthority{})
	})

	sweeper, err := buildStreamRevocationSweeper(recheckProbeConfig(), iamConn, projection, quietLog())
	if err != nil {
		t.Fatalf("сборка перепроса: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, subscriptionstream.Path+"?owner=probe", nil)
	r.Header.Set(principalmeta.HeaderPrincipalType, "user")
	r.Header.Set(principalmeta.HeaderPrincipalID, "usr00000000000000007")
	r.Header.Set(principalmeta.HeaderTokenJti, "jti-wiring")
	done := make(chan struct{})
	go func() {
		defer close(done)
		projection.ServeHTTP(httptest.NewRecorder(), r)
	}()
	select {
	case <-owner.started:
	case <-time.After(10 * time.Second):
		t.Fatal("поток не открылся — предъявлять нечего")
	}

	sweeper.Sweep(context.Background())
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("продовая точка сборки не связала перепрос с реестром открытых потоков — " +
			"отзыв удостоверения не доезжал бы до длинных соединений вовсе")
	}
}

// TestDeclaredRevocationWindowFollowsTheRequestPathAndIsNotTheStreamLifetime —
// ПРЕДИКАТ СНЯТИЯ задачи: окно названо числом и не равно сроку жизни соединения.
//
// Читается ОБЪЯВЛЕННАЯ посадка (`config.Load` на умолчаниях), а не выписанные
// здесь величины: проба о числах, которые поедут в бою, а не о тех, что удобно
// набрать в пробе.
func TestDeclaredRevocationWindowFollowsTheRequestPathAndIsNotTheStreamLifetime(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("объявленная посадка не читается: %v", err)
	}
	window := credentialRecheckInterval(cfg)
	declared := time.Duration(cfg.IntrospectionCacheTTLSeconds) * time.Second

	if window != declared {
		t.Fatalf("окно отзыва для открытого соединения %v, а объявленная граница на пути запроса %v — "+
			"у одного механизма две величины, и вторая разошлась бы с первой молча", window, declared)
	}
	if window <= 0 {
		t.Fatalf("окно отзыва %v — не окно", window)
	}
	if window == cfg.SubscriptionStreamBudget {
		t.Fatalf("окно отзыва совпало со сроком жизни соединения (%v) — это и есть предмет задачи: "+
			"для потока границей становится его бюджет, а не объявленный отзыв", window)
	}
	if window >= cfg.SubscriptionStreamBudget {
		t.Fatalf("окно отзыва %v не меньше срока жизни соединения %v — перепрос не состоялся бы "+
			"на потоке ни разу", window, cfg.SubscriptionStreamBudget)
	}
	t.Logf("перепись поставки: окно отзыва %v · срок жизни соединения %v · отношение %d",
		window, cfg.SubscriptionStreamBudget, int64(cfg.SubscriptionStreamBudget/window))
}

// TestCredentialRecheckStaleBudgetFollowsTheWindow — срок неподтверждённого
// перепроса ВЫВОДИТСЯ из окна, а не объявляется отдельно.
func TestCredentialRecheckStaleBudgetFollowsTheWindow(t *testing.T) {
	iamConn := bufconnDial(t, func(s *grpc.Server) {
		iamv1.RegisterInternalSessionRevocationsServiceServer(s, revokingAuthority{})
	})
	sweeper, err := buildStreamRevocationSweeper(recheckProbeConfig(), iamConn, probeProjection(t), quietLog())
	if err != nil {
		t.Fatalf("сборка перепроса: %v", err)
	}
	if got, want := sweeper.Interval(), 5*time.Second; got != want {
		t.Fatalf("окно перепроса %v, объявленная граница %v", got, want)
	}
	if got, want := sweeper.StaleAfter(), revocationStaleAfter(5*time.Second); got != want {
		t.Fatalf("срок неподтверждённого перепроса %v — точка сборки объявила не ту величину "+
			"(ожидалось %v)", got, want)
	}
}

// TestCredentialRecheckIsRefusedAtStartupWhenItCannotWork — страж старта.
//
// Каждый случай здесь — состояние, при котором контроль ПРИСУТСТВУЕТ и не
// отказывает ни разу за всю свою жизнь. Такое обнаруживают отказом старта, а не
// первым пережившим отзыв потоком в бою.
func TestCredentialRecheckIsRefusedAtStartupWhenItCannotWork(t *testing.T) {
	iamConn := bufconnDial(t, func(s *grpc.Server) {
		iamv1.RegisterInternalSessionRevocationsServiceServer(s, revokingAuthority{})
	})
	cases := []struct {
		name string
		cfg  config.Config
		conn *grpc.ClientConn
		proj *subscriptionstream.Handler
	}{
		{
			name: "проекция не собрана — закрывать нечего",
			cfg:  recheckProbeConfig(), conn: iamConn, proj: nil,
		},
		{
			name: "адреса авторитета нет — спросить не у кого",
			cfg:  recheckProbeConfig(), conn: nil, proj: probeProjection(t),
		},
		{
			name: "срок кэша интроспекции неположителен — окна нет вовсе",
			cfg: config.Config{
				IntrospectionCacheTTLSeconds: 0,
				SubscriptionStreamBudget:     90 * time.Second,
			},
			conn: iamConn, proj: probeProjection(t),
		},
		{
			name: "окно шире срока жизни потока — перепрос не состоится ни разу",
			cfg: config.Config{
				IntrospectionCacheTTLSeconds: 120,
				SubscriptionStreamBudget:     90 * time.Second,
			},
			conn: iamConn, proj: probeProjection(t),
		},
		{
			name: "срок неподтверждённого перепроса не меньше срока жизни потока — fail-closed не наступит",
			cfg: config.Config{
				IntrospectionCacheTTLSeconds: 5,
				SubscriptionStreamBudget:     20 * time.Second,
			},
			conn: iamConn, proj: probeProjection(t),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := buildStreamRevocationSweeper(c.cfg, c.conn, c.proj, quietLog()); err == nil {
				t.Fatal("точка сборки приняла посадку, при которой перепрос не отказал бы НИ РАЗУ")
			}
		})
	}
}

// TestShippedPostureAssemblesTheCredentialRecheck — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ
// стража: объявленная посадка обязана собираться.
//
// Без него все отрицания выше зеленели бы на строителе, отвергающем ВСЁ.
func TestShippedPostureAssemblesTheCredentialRecheck(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("объявленная посадка не читается: %v", err)
	}
	iamConn := bufconnDial(t, func(s *grpc.Server) {
		iamv1.RegisterInternalSessionRevocationsServiceServer(s, revokingAuthority{})
	})
	if _, err := buildStreamRevocationSweeper(cfg, iamConn, probeProjection(t), quietLog()); err != nil {
		t.Fatalf("объявленная посадка не собирает перепрос: %v — тогда край не поднялся бы вовсе", err)
	}
}
