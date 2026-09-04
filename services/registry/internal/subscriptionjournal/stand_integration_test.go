// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	kachopg "github.com/PRO-Robotech/kacho/services/registry/internal/repo/kacho/pg"
	"github.com/PRO-Robotech/kacho/services/registry/internal/subscriptionjournal"
)

const (
	probeProject      = "prj-SUBA"
	probeOtherProject = "prj-SUBB"
	probeEndpointBase = "registry.kacho.local"
	probeRegion       = "eu-north-1"
)

// probeFixtureProjects — идентичности, которым фикстура учёта заводит место.
//
// Вставка строки реестра СПИСЫВАЕТ место, и списать его не с чего, пока у
// проекта нет строки учёта: «не сказано» — отказ, а не «без предела». На живом
// пути строку заводит материализация перед writer-транзакцией; эти пробы идут
// мимо use-case'а, прямо в репозиторий, и обязаны привести базу в то же
// состояние.
var probeFixtureProjects = []string{probeProject, probeOtherProject}

// stand — сервер потока НАСТОЯЩИЙ, за настоящим gRPC, над настоящей схемой
// реестра.
//
// Предмет этих проб — «провязан и ОТВЕЧАЕТ», а это проверяется вызовом. Проба,
// читающая объявление, осталась бы зелёной у сервиса, чей журнал разошёлся со
// своей схемой: имя колонки живёт значением, и ошибка в нём наступает первым
// запросом в бою, а не сборкой. То же и с триггером: он либо висит на таблице,
// либо нет, и узнать это можно только мутацией.
type stand struct {
	pool   *pgxpool.Pool
	repo   *kachopg.RegistryRepo
	client subscriptionv1.InternalSubscriptionServiceClient
}

func newStand(t *testing.T) *stand {
	t.Helper()
	return newStandWithNarrower(t, narrowtest.AllowingAll())
}

// newStandWithNarrower — тот же стенд с ЗАДАННЫМ сужателем.
//
// Отдельный вход нужен там, где предмет пробы — не «доезжает ли событие вообще»,
// а «доезжает ли оно тому, чьи права изменились»: разрешающий всё сужатель по
// построению не отличает эти два вопроса.
func newStandWithNarrower(t *testing.T, narrower *listnarrow.Narrower) *stand {
	t.Helper()
	if testing.Short() {
		t.Skip("интеграционная проба: нужна настоящая база")
	}

	dsn := pgtest.NewDB(t)
	pool, err := coredb.NewPool(context.Background(), dsn)
	if err != nil {
		t.Fatalf("пул не собрался: %v", err)
	}
	pgtest.ClosePoolAtEnd(t, pool)
	seedFixtureQuotas(t, pool)

	gate, err := subscriptionjournal.ProjectGate()
	if err != nil {
		t.Fatalf("страж не собрался: %v", err)
	}
	srv, err := subscription.NewServer(subscription.Config{
		Journal:      subscriptionjournal.Journal(probeEndpointBase),
		DSN:          dsn,
		Narrower:     narrower,
		ProjectGate:  gate,
		MaxStreams:   4,
		IdlePoll:     150 * time.Millisecond,
		StreamBudget: 30 * time.Second,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("сервер потока не поднялся: %v", err)
	}

	lis := bufconn.Listen(1 << 20)
	// Личность вызывающего кладёт звено — там же, где её кладёт боевая цепочка.
	caller := narrowtest.Caller()
	gsrv := grpc.NewServer(grpc.StreamInterceptor(
		func(s any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, h grpc.StreamHandler) error {
			return h(s, principalStream{ServerStream: ss, ctx: mergeCancel(caller, ss.Context())})
		}))
	subscriptionv1.RegisterInternalSubscriptionServiceServer(gsrv, srv)
	go func() { _ = gsrv.Serve(lis) }()
	t.Cleanup(gsrv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("клиент не собрался: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return &stand{
		pool:   pool,
		repo:   kachopg.NewRegistryRepo(pool),
		client: subscriptionv1.NewInternalSubscriptionServiceClient(conn),
	}
}

// seedFixtureQuotas приводит свежую базу в состояние «проекты материализованы».
//
// Строки заводятся ТЕМИ ЖЕ операторами, что и продукт
// (`kachopg.MaterializeQuotas` / `MaterializeNestedDefaults` — единственные): это
// не подставная реализация, а вызов настоящей. Механизм учёта продолжает
// работать на каждой вставке; меняется ровно одно — величина у этих проектов
// заведомо больше, чем нужно, потому что предмет этих проб лежит в другом месте.
func seedFixtureQuotas(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	const limit = 1_000_000
	ctx := context.Background()

	rows := make([]kachopg.QuotaRow, 0, len(probeFixtureProjects)*2)
	nested := make([]kachopg.QuotaRow, 0, len(probeFixtureProjects))
	for _, p := range probeFixtureProjects {
		for _, kind := range []string{"registry.registries", "registry.repositories"} {
			rows = append(rows, kachopg.QuotaRow{
				CarrierType: "project", CarrierID: p, Kind: kind,
				Limit: limit, SourceScope: "DEFAULT", LimitRevision: 0, AccountID: "acc-fixture",
			})
		}
		nested = append(nested, kachopg.QuotaRow{
			CarrierID: p, Kind: "registry.registries.repositories",
			Limit: limit, SourceScope: "DEFAULT", LimitRevision: 0, AccountID: "acc-fixture",
		})
	}
	n, err := kachopg.MaterializeQuotas(ctx, pool, rows)
	if err != nil || n != int64(len(rows)) {
		t.Fatalf("фикстура учёта: заведено строк %d из %d, ошибка %v", n, len(rows), err)
	}
	nn, err := kachopg.MaterializeNestedDefaults(ctx, pool, nested)
	if err != nil || nn != int64(len(nested)) {
		t.Fatalf("фикстура учёта: резолв вложенного вида %d из %d, ошибка %v", nn, len(nested), err)
	}
}

// newReg — реестр в минимально-законной форме.
func newReg(projectID, name string, labels map[string]string) *domain.Registry {
	return &domain.Registry{
		ID:            ids.NewID(ids.PrefixRegistry),
		ProjectID:     projectID,
		Name:          name,
		Labels:        labels,
		Status:        domain.RegistryStatusActive,
		RegionID:      probeRegion,
		PlacementType: domain.PlacementTypeRegional,
	}
}

// create заводит реестр ТЕМ ЖЕ репозиторием, каким его заводит сервис.
//
// Через настоящий репозиторий, а не своим INSERT: свой разошёлся бы с боевым
// молча — и разошёлся бы ровно там, где проба и написана, потому что журнал
// пишет ТРИГГЕР, а триггер видит только то, что действительно легло в таблицу.
func (s *stand) create(t *testing.T, projectID, name string, labels map[string]string) *domain.Registry {
	t.Helper()
	reg := newReg(projectID, name, labels)
	created, _, err := s.repo.Insert(context.Background(), reg,
		domain.RegisterIntentForCreate(reg, "user", "usr-alice"))
	if err != nil {
		t.Fatalf("реестр не создался: %v", err)
	}
	return created
}

type principalStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s principalStream) Context() context.Context { return s.ctx }

func mergeCancel(values, cancel context.Context) context.Context {
	ctx, stop := context.WithCancel(values)
	go func() {
		<-cancel.Done()
		stop()
	}()
	return ctx
}

// recv читает поток до первого СОБЫТИЯ, пропуская служебное сообщение открытия.
func recv(t *testing.T, stream subscriptionv1.InternalSubscriptionService_SubscribeClient) *subscriptionv1.SubscriptionEvent {
	t.Helper()
	for {
		msg, err := stream.Recv()
		if err != nil {
			t.Fatalf("поток оборвался до события: %v", err)
		}
		if ev := msg.GetEvent(); ev != nil {
			return ev
		}
	}
}
