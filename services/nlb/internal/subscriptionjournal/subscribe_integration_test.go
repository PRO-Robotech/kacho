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

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/subscriptionjournal"
)

const (
	probeProject  = "prj-1234567890abcdef"
	probeLB       = "nlb-1234567890abcdef"
	probeListener = "nlb-l-1234567890abcd"
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
	ev := recv(t, s.subscribe(t, ctx, []string{authzfilter.ResourceTypeLoadBalancer}))

	// На проводе — ТИП ОБЪЕКТА, а не слово журнала. У балансировщика они
	// РАЗНЫЕ (`nlb_load_balancer` в колонке против `nlb_network_load_balancer` в
	// модели), поэтому именно здесь утверждение различающее: у двух остальных
	// видов nlb они совпадают, и на них проба зеленела бы при любом устройстве.
	if ev.Kind != authzfilter.ResourceTypeLoadBalancer || ev.ResourceId != probeLB {
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
	_ = recv(t, s.subscribe(t, ctx, []string{authzfilter.ResourceTypeLoadBalancer}))

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

// TestListenerEventsCarryTheFullSubjectWithLabels — событие вида `nlb_listener`
// доезжает ДО КЛИЕНТА с полным состоянием предмета, и метки в нём есть.
//
// Утверждение сквозное: нагрузка кладётся тем же строителем, каким её кладёт
// эмиттер, читается настоящим сервером потока над настоящей схемой и
// распаковывается из конверта контракта. Проба над одной функцией сборки этого не
// сказала бы: между ней и клиентом лежат выбор колонки запросом, сужение по
// правам и упаковка в `Any`.
func TestListenerEventsCarryTheFullSubjectWithLabels(t *testing.T) {
	s := newStand(t)

	rec := &kachorepo.ListenerRecord{CreatedAt: time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)}
	rec.ID = domain.ResourceID(probeListener)
	rec.ProjectID = domain.ProjectID(probeProject)
	rec.LoadBalancerID = domain.ResourceID(probeLB)
	rec.RegionID = domain.RegionID("ru-central1")
	rec.Name = domain.LbName("front")
	rec.Labels = domain.LabelsFromMap(map[string]string{"env": "prod"})
	rec.Protocol = domain.ProtoTCP
	rec.Port = domain.LbPort(443)
	rec.Status = domain.ListenerStatusActive

	s.emit(t, kachorepo.OutboxResourceListener, probeListener, probeProject,
		kachorepo.OutboxActionCreated, kachorepo.StateEnvelope(rec))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ev := recv(t, s.subscribe(t, ctx, []string{authzfilter.ResourceTypeListener}))

	if ev.GetState() == nil {
		t.Fatalf("состояние НЕ доехало (причина %v) — клиентский отбор по меткам для "+
			"слушателя остался бы без источника", ev.GetStateUnavailable().GetReason())
	}
	var got lbv1.Listener
	if err := ev.GetState().UnmarshalTo(&got); err != nil {
		t.Fatalf("в конверте состояния не контракт слушателя: %v", err)
	}
	if got.GetId() != probeListener || got.GetProjectId() != probeProject {
		t.Fatalf("состояние про другой предмет: id %q, проект %q", got.GetId(), got.GetProjectId())
	}
	if got.GetLabels()["env"] != "prod" {
		t.Fatalf("метки не доехали (%v) — клиентский отбор по меткам остался бы без источника",
			got.GetLabels())
	}
	if got.GetName() != "front" || got.GetPort() != 443 {
		t.Fatalf("предмет доехал урезанным: имя %q, порт %d", got.GetName(), got.GetPort())
	}
}

// TestLoadBalancerRowOfTheOldShapeArrivesWithoutState — строка ПРЕЖНЕЙ,
// минимальной формы у балансировщика состоянием не притворяется.
//
// # Здесь стояло утверждение, ПЕРЕЖИВШЕЕ свой предмет
//
// Прежняя редакция звалась «у балансировщика и целевой группы состояния нет» и
// подавала минимальный снимок. К моменту, когда состояние появилось у обоих
// (#1551), её заголовок утверждал о видах то, что перестало быть верным, а тело
// оставалось зелёным — оно проверяло не вид, а ФОРМУ строки. Ослабить её было
// нельзя, поэтому она ЗАМЕНЕНА утверждением о том, что проверяла на самом деле.
//
// Предмет у нового утверждения не исчезнет: журнал не чистится
// (`RetainsEverything`), подписчик вправе открыть поток с начала, и строки,
// записанные ДО обогащения этого вида, доезжают до сборщика и сегодня. Полноту
// объявляет КОНВЕРТ, а не удача разбора: `encoding/json` сопоставляет имена без
// учёта регистра, поэтому `id`, `name`, `status` минимального снимка попали бы в
// поля записи, а проект, отметка создания и МЕТКИ — нет.
//
// Отрицание не вакуумно: положительный контроль того же вида — сквозная проба
// `TestLoadBalancerStateIsTheSameFromTheTriggerAndFromGo` в этом же пакете.
func TestLoadBalancerRowOfTheOldShapeArrivesWithoutState(t *testing.T) {
	s := newStand(t)
	s.emit(t, kachorepo.OutboxResourceLoadBalancer, probeLB, probeProject,
		kachorepo.OutboxActionUpdated,
		map[string]any{
			"id": probeLB, "project_id": probeProject, "region_id": "ru-central1",
			"name": "front", "status": "ACTIVE", "type": "EXTERNAL",
		})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ev := recv(t, s.subscribe(t, ctx, []string{authzfilter.ResourceTypeLoadBalancer}))

	if ev.GetState() != nil {
		var got lbv1.NetworkLoadBalancer
		_ = ev.GetState().UnmarshalTo(&got)
		t.Fatalf("минимальный снимок доехал как ПОЛНОЕ состояние (%v): подписчик записал "+
			"бы как факт, что у балансировщика нет ни меток, ни адреса", &got)
	}
	if ev.GetStateUnavailable() == nil {
		t.Fatal("носитель нагрузки не выбран вовсе — форма требует одну из двух ветвей")
	}
	// ПРИЧИНА — часть контракта, и клиент ключуется на неё, а не на прозу.
	// «Не удалось собрать» здесь было бы утверждением о неудавшейся попытке там,
	// где попытки не было. Действия у двух причин противоположны — на сбой
	// разумно перечитать событие, на свойство строки разумно сразу идти за
	// предметом, — и неразличимые причины заставляют клиента выбрать неверное.
	if got := ev.GetStateUnavailable().GetReason(); got != subscriptionv1.SubscriptionEvent_StateUnavailable_NOT_PRODUCED {
		t.Fatalf("причина отсутствия состояния %v, ожидалась NOT_PRODUCED — сбоя сборки "+
			"не было, состояние в этой строке просто не производилось", got)
	}
}

// TestListenerRowOfTheOldShapeArrivesWithoutState — строка ПРЕЖНЕЙ формы,
// лежащая в журнале с до-обогащения времён, состоянием не притворяется.
//
// Журнал не чистится, а подписчик вправе открыть поток с начала — значит такие
// строки доезжают до клиента и сегодня. Разбор их молча не отвергнет (имена
// сопоставляются без учёта регистра), поэтому полноту объявляет КОНВЕРТ.
func TestListenerRowOfTheOldShapeArrivesWithoutState(t *testing.T) {
	s := newStand(t)
	s.emit(t, kachorepo.OutboxResourceListener, probeListener, probeProject,
		kachorepo.OutboxActionUpdated,
		map[string]any{
			"id": probeListener, "project_id": probeProject, "parent_resource_id": probeLB,
			"name": "front", "protocol": "TCP", "port": 443, "status": "ACTIVE",
		})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ev := recv(t, s.subscribe(t, ctx, []string{authzfilter.ResourceTypeListener}))

	if ev.GetState() != nil {
		var got lbv1.Listener
		_ = ev.GetState().UnmarshalTo(&got)
		t.Fatalf("минимальный снимок доехал как ПОЛНОЕ состояние (%v): подписчик записал "+
			"бы как факт, что у слушателя нет меток", &got)
	}
	if got := ev.GetStateUnavailable().GetReason(); got != subscriptionv1.SubscriptionEvent_StateUnavailable_NOT_PRODUCED {
		t.Fatalf("причина отсутствия %v, ожидалась NOT_PRODUCED — сбоя сборки не было, "+
			"состояние в этой строке просто не производилось", got)
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
	ev := recv(t, s.subscribe(t, ctx, []string{authzfilter.ResourceTypeLoadBalancer}))

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
