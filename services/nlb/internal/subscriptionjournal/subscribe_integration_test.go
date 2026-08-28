// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/authzfilter"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/subscriptionjournal"
)

const (
	probeProject = "prj-1234567890abcdef"
	probeLB      = "nlb-1234567890abcdef"
)

type stand struct {
	pool   *pgxpool.Pool
	client subscriptionv1.InternalSubscriptionServiceClient
	peer   *narrowtest.Peer
}

// newStand поднимает НАСТОЯЩИЙ сервер потока над настоящей схемой nlb.
//
// Сосед по правам — записывающий дублёр: предмет части проб не только «что
// отдано», но и «о ЧЁМ спросили модель». Тип объекта у балансировщика не совпадает
// с его же журнальным словом, и различие это невидимо на глаз.
func newStand(t *testing.T) *stand {
	t.Helper()
	if testing.Short() {
		t.Skip("интеграционная проба: нужна настоящая база")
	}

	dsn := pgtest.NewDB(t)
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("пул не собрался: %v", err)
	}
	pgtest.ClosePoolAtEnd(t, pool)

	peer := &narrowtest.Peer{AllowAll: true}
	gate, err := subscriptionjournal.ProjectGate()
	if err != nil {
		t.Fatalf("страж не собрался: %v", err)
	}
	srv, err := subscription.NewServer(subscription.Config{
		Journal:      subscriptionjournal.Journal(),
		DSN:          dsn,
		Narrower:     narrowtest.New(peer),
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
		client: subscriptionv1.NewInternalSubscriptionServiceClient(conn),
		peer:   peer,
	}
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

// emit пишет строку журнала ТЕМ ЖЕ запросом, каким её пишет репозиторий nlb —
// со схемой в имени таблицы и его собственными именами колонок.
func (s *stand) emit(t *testing.T, kind, id, projectID, action string, payload map[string]any) {
	t.Helper()
	ctx := context.Background()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("нагрузка не собралась: %v", err)
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO kacho_nlb.nlb_outbox (resource_type, resource_id, project_id, action, payload)
		 VALUES ($1, $2, $3, $4, $5)`, kind, id, projectID, action, raw); err != nil {
		t.Fatalf("строка журнала не записалась: %v", err)
	}
}

func (s *stand) subscribe(t *testing.T, ctx context.Context, kinds []string) subscriptionv1.InternalSubscriptionService_SubscribeClient {
	t.Helper()
	stream, err := s.client.Subscribe(ctx, &subscriptionv1.SubscriptionRequest{
		Kinds:     kinds,
		ProjectId: probeProject,
		// Начало названо словом: незаданное по контракту означает «с текущего
		// конца», и строки, записанные ДО открытия, не пришли бы вовсе.
		Start: &subscriptionv1.SubscriptionRequest_Anchor{
			Anchor: subscriptionv1.SubscriptionAnchor_BEGINNING,
		},
	})
	if err != nil {
		t.Fatalf("подписка не открылась: %v", err)
	}
	return stream
}

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

// TestSubscribeAnswersOverTheWire — сервер ПРОВЯЗАН И ОТВЕЧАЕТ, проверено вызовом.
//
// Имена колонок у nlb свои (`resource_type`, `action`), а таблица
// схемо-квалифицирована при неквалифицированном канале. Всё это живёт значениями в
// объявлении, поэтому проба, читающая объявление, зеленела бы при любой описке:
// ошибка наступает первым запросом, а не сборкой.
func TestSubscribeAnswersOverTheWire(t *testing.T) {
	s := newStand(t)
	s.emit(t, kachorepo.OutboxResourceLoadBalancer, probeLB, probeProject,
		kachorepo.OutboxActionCreated, map[string]any{"id": probeLB, "projectId": probeProject})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ev := recv(t, s.subscribe(t, ctx, []string{kachorepo.OutboxResourceLoadBalancer}))

	if ev.Kind != kachorepo.OutboxResourceLoadBalancer || ev.ResourceId != probeLB {
		t.Fatalf("пришло не то событие: вид %q, предмет %q", ev.Kind, ev.ResourceId)
	}
	if ev.ProjectId != probeProject {
		t.Fatalf("якорь проекта %q, ожидался %q", ev.ProjectId, probeProject)
	}
	if ev.Change != subscriptionv1.SubscriptionEvent_CREATED {
		t.Fatalf("род изменения %v, ожидалось создание", ev.Change)
	}
}

// TestVisibilityIsAskedAboutTheModelsTypeNotTheJournalWord — модель прав
// спрашивается о ТИПЕ ОБЪЕКТА, а не о журнальном слове.
//
// У балансировщика они РАЗНЫЕ (`nlb_load_balancer` в таблице против
// `nlb_network_load_balancer` в модели), а у двух остальных видов совпадают — из-за
// чего различие невидимо на глаз. Вид, унаследовавший собственное журнальное
// слово, спрашивал бы модель о несуществующем типе и остался бы «зелёным»: модель
// ответила бы отказом, событие просто не доставлялось бы, и снаружи это выглядело
// бы как «изменений нет».
//
// Проба смотрит НЕ на исход, а на то, О ЧЁМ спросили: исход при `AllowAll`
// одинаков для любого типа, и утверждение об исходе было бы вакуумным.
func TestVisibilityIsAskedAboutTheModelsTypeNotTheJournalWord(t *testing.T) {
	s := newStand(t)
	s.emit(t, kachorepo.OutboxResourceLoadBalancer, probeLB, probeProject,
		kachorepo.OutboxActionCreated, map[string]any{"id": probeLB})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = recv(t, s.subscribe(t, ctx, []string{kachorepo.OutboxResourceLoadBalancer}))

	if s.peer.Checks == 0 {
		t.Fatal("модель прав не спрашивали ВОВСЕ — событие ушло без пообъектной проверки")
	}
	if s.peer.ResourceType == kachorepo.OutboxResourceLoadBalancer {
		t.Fatalf("модель спросили о ЖУРНАЛЬНОМ слове %q; она знает этот предмет как %q, "+
			"и вопрос уходил бы про несуществующий тип — отказ, неотличимый снаружи "+
			"от «изменений нет»", s.peer.ResourceType, authzfilter.ResourceTypeLoadBalancer)
	}
	if s.peer.ResourceType != authzfilter.ResourceTypeLoadBalancer {
		t.Fatalf("модель спросили о типе %q, ожидался %q", s.peer.ResourceType, authzfilter.ResourceTypeLoadBalancer)
	}
	if s.peer.Action != authzfilter.ActionLoadBalancerList {
		t.Fatalf("действие вопроса %q, ожидалось %q — видимость в потоке обязана "+
			"равняться видимости в списке", s.peer.Action, authzfilter.ActionLoadBalancerList)
	}
}

// TestEventsCarryNoStateBecauseTheJournalCarriesNone — состояния нет, и это
// сказано ПРИЗНАКОМ, а не пустым предметом.
//
// Нагрузка nlb — намеренно минимальный снимок. Отдай его как состояние —
// подписчик, которому контракт разрешает читать непустую нагрузку как ПОЛНОЕ
// состояние, записал бы как факт, что у балансировщика нет ни меток, ни целей.
func TestEventsCarryNoStateBecauseTheJournalCarriesNone(t *testing.T) {
	s := newStand(t)
	s.emit(t, kachorepo.OutboxResourceLoadBalancer, probeLB, probeProject,
		kachorepo.OutboxActionCreated,
		map[string]any{"id": probeLB, "projectId": probeProject, "name": "front", "status": "ACTIVE"})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ev := recv(t, s.subscribe(t, ctx, []string{kachorepo.OutboxResourceLoadBalancer}))

	if ev.GetState() != nil {
		t.Fatal("отдано состояние из МИНИМАЛЬНОГО снимка: подписчик прочёл бы его как " +
			"полное и записал бы отсутствие меток и целей как факт")
	}
	if ev.GetStateUnavailable() == nil {
		t.Fatal("носитель нагрузки не выбран вовсе — форма требует одну из двух ветвей")
	}
}

// TestProjectMoveArrivesAsAnUpdateNotARemoval — переезд между проектами доезжает
// ПРАВКОЙ.
//
// Слово `MOVED` — собственное слово журнала nlb, платформенная форма его не знает.
// Не назови его словарём — строка стала бы недоставляемой ТИХО; назови снятием —
// подписчик убрал бы из своего состояния живой ресурс.
func TestProjectMoveArrivesAsAnUpdateNotARemoval(t *testing.T) {
	s := newStand(t)
	s.emit(t, kachorepo.OutboxResourceLoadBalancer, probeLB, probeProject,
		kachorepo.OutboxActionMoved, map[string]any{"id": probeLB, "newProjectId": probeProject})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ev := recv(t, s.subscribe(t, ctx, []string{kachorepo.OutboxResourceLoadBalancer}))

	if ev.Change == subscriptionv1.SubscriptionEvent_DELETED {
		t.Fatal("переезд доехал СНЯТИЕМ: подписчик убрал бы из своего состояния ресурс, который жив")
	}
	if ev.Change != subscriptionv1.SubscriptionEvent_UPDATED {
		t.Fatalf("переезд доехал родом %v, ожидалась правка", ev.Change)
	}
}

// narrowerFor — проверка предпосылки дублёра: сужатель, собранный вокруг него,
// действительно СУЖАЕТ. Подвешенный и не сужающий сервер отверг бы на сборке, и
// все пробы выше падали бы по чужой причине.
func TestTheProbeNarrowerActuallyNarrows(t *testing.T) {
	var n *listnarrow.Narrower = narrowtest.New(&narrowtest.Peer{AllowAll: true})
	if !n.Narrows() {
		t.Fatal("дублёр сужателя не сужает — сервер отверг бы его на сборке, и все " +
			"пробы пакета падали бы по чужой причине")
	}
}
