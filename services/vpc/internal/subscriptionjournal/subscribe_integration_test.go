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

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/pkg/outbox"
	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/subscriptionjournal"
)

const (
	probeProject = "prj-1234567890abcdef"
	probeNetwork = "net-1234567890abcdef"
)

// stand — сервер потока НАСТОЯЩИЙ, за настоящим gRPC, над настоящей схемой vpc.
//
// Предмет этих проб — «провязан и ОТВЕЧАЕТ», а это проверяется вызовом. Проба,
// читающая объявление, осталась бы зелёной у сервиса, чей журнал разошёлся со
// своей схемой: имена колонок живут значениями, и ошибка в них наступает первым
// запросом в бою, а не сборкой.
type stand struct {
	pool   *pgxpool.Pool
	client subscriptionv1.InternalSubscriptionServiceClient
}

func newStand(t *testing.T) *stand { return newStandWithNarrower(t, narrowtest.AllowingAll()) }

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
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("пул не собрался: %v", err)
	}
	pgtest.ClosePoolAtEnd(t, pool)

	gate, err := subscriptionjournal.ProjectGate()
	if err != nil {
		t.Fatalf("страж не собрался: %v", err)
	}
	srv, err := subscription.NewServer(subscription.Config{
		Journal:      subscriptionjournal.Journal(),
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

	return &stand{pool: pool, client: subscriptionv1.NewInternalSubscriptionServiceClient(conn)}
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

// emit пишет строку журнала ТЕМ ЖЕ способом, каким её пишет репозиторий.
//
// Через `outbox.EmitAnchored`, а не своим INSERT: свой разошёлся бы с боевым
// молча — и разошёлся бы ровно в той колонке, ради которой проба написана.
func (s *stand) emit(t *testing.T, kind, id, projectID, change string, payload map[string]any) {
	t.Helper()
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("транзакция не началась: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := outbox.EmitAnchored(ctx, tx, subscriptionjournal.Table, kind, id, projectID, change, payload); err != nil {
		t.Fatalf("строка журнала не записалась: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("транзакция не зафиксировалась: %v", err)
	}
}

func networkPayload(n *kachorepo.NetworkRecord) map[string]any {
	b, err := json.Marshal(n)
	if err != nil {
		panic(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		panic(err)
	}
	return m
}

func network(id, project, name string) *kachorepo.NetworkRecord {
	return &kachorepo.NetworkRecord{
		Network: domain.Network{ID: id, ProjectID: project, Name: domain.RcNameVPC(name)},
	}
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

func subscribe(t *testing.T, s *stand, ctx context.Context, req *subscriptionv1.SubscriptionRequest) subscriptionv1.InternalSubscriptionService_SubscribeClient {
	t.Helper()
	req.Start = &subscriptionv1.SubscriptionRequest_Anchor{Anchor: subscriptionv1.SubscriptionAnchor_BEGINNING}
	stream, err := s.client.Subscribe(ctx, req)
	if err != nil {
		t.Fatalf("подписка не открылась: %v", err)
	}
	return stream
}

// TestSubscribeAnswersOverTheWire — сервер ПРОВЯЗАН И ОТВЕЧАЕТ, и это проверено
// вызовом, а не наличием кода.
//
// Заодно утверждается СОСТОЯНИЕ: журнал vpc несёт полную запись ресурса, поэтому
// событие обязано приехать с ним, а не с одной оболочкой.
func TestSubscribeAnswersOverTheWire(t *testing.T) {
	s := newStand(t)
	n := network(probeNetwork, probeProject, "net-probe")
	s.emit(t, subscriptionjournal.KindNetwork, n.ID, n.ProjectID, "CREATED", networkPayload(n))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// ДВА РАЗНЫХ СЛОВА ОБ ОДНОМ ПРЕДМЕТЕ, и подменять их местами нельзя.
	//
	// В `emit` выше стоит слово ХРАНИЛИЩА (`KindNetwork` = "Network") — им владелец
	// записал строку в свою таблицу; оно частное и наружу не выходит. Здесь, на
	// проводе, стоит ТИП ОБЪЕКТА модели прав — единственное платформенное имя
	// предмета, и только его знает словарь владельца ([subscription.Journal.KindDictionary]).
	// Подать сюда слово хранилища значит получить отказ на открытии.
	stream := subscribe(t, s, ctx, &subscriptionv1.SubscriptionRequest{
		Kinds:     []string{authzfilter.ResourceTypeNetwork},
		ProjectId: probeProject,
	})

	ev := recv(t, stream)
	if ev.Change != subscriptionv1.SubscriptionEvent_CREATED {
		t.Fatalf("род изменения %v, ожидалось создание", ev.Change)
	}
	if ev.ResourceId != probeNetwork {
		t.Fatalf("предмет %q, ожидался %q", ev.ResourceId, probeNetwork)
	}
	if ev.ProjectId != probeProject {
		t.Fatalf("якорь проекта %q, ожидался %q", ev.ProjectId, probeProject)
	}
	if ev.GetState() == nil {
		t.Fatal("событие пришло БЕЗ состояния: журнал vpc несёт полную запись, " +
			"и подписчик, снявший опрос, обязан получить её, а не одну оболочку")
	}
}

// TestRemovalReachesTheSubscriberWithItsProjectAnchor — СОБЫТИЕ СНЯТИЯ доезжает
// и несёт якорь.
//
// Нагрузка снятия — один идентификатор, поэтому якорь мог приехать только
// колонкой. Проба утверждает именно его: без якоря подписка с осью проекта это
// событие не пропустит.
func TestRemovalReachesTheSubscriberWithItsProjectAnchor(t *testing.T) {
	s := newStand(t)
	s.emit(t, subscriptionjournal.KindNetwork, probeNetwork, probeProject, "DELETED",
		map[string]any{"id": probeNetwork})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	stream := subscribe(t, s, ctx, &subscriptionv1.SubscriptionRequest{
		Kinds:     []string{authzfilter.ResourceTypeNetwork},
		ProjectId: probeProject,
	})

	ev := recv(t, stream)
	if ev.Change != subscriptionv1.SubscriptionEvent_DELETED {
		t.Fatalf("род изменения %v, ожидалось снятие", ev.Change)
	}
	if ev.ProjectId != probeProject {
		t.Fatalf("якорь проекта %q, ожидался %q", ev.ProjectId, probeProject)
	}
	if ev.GetState() != nil {
		t.Fatal("снятие пришло с состоянием: предмета больше нет, и собранное «состояние» " +
			"подписчик записал бы как факт — имя исчезло, метки исчезли")
	}
}

// TestTheProjectAxisNarrowsByTheColumn — ось проекта ОТБИРАЕТ, а не украшает.
//
// Отрицание в паре с положительным контролем в той же пробе: без него оно
// зеленело бы и на сервере, который не отдаёт ничего.
func TestTheProjectAxisNarrowsByTheColumn(t *testing.T) {
	const otherProject = "prj-000000000000000f"
	const mineNetwork = "net-aaaaaaaaaaaaaaaa"
	s := newStand(t)

	alien := network("net-bbbbbbbbbbbbbbbb", otherProject, "alien")
	s.emit(t, subscriptionjournal.KindNetwork, alien.ID, alien.ProjectID, "CREATED", networkPayload(alien))
	mine := network(mineNetwork, probeProject, "mine")
	s.emit(t, subscriptionjournal.KindNetwork, mine.ID, mine.ProjectID, "CREATED", networkPayload(mine))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	stream := subscribe(t, s, ctx, &subscriptionv1.SubscriptionRequest{
		Kinds:     []string{authzfilter.ResourceTypeNetwork},
		ProjectId: probeProject,
	})

	ev := recv(t, stream)
	if ev.ResourceId == alien.ID {
		t.Fatalf("отдано событие ЧУЖОГО проекта (%s): ось не отбирает", ev.ProjectId)
	}
	if ev.ResourceId != mineNetwork {
		t.Fatalf("пришло не своё событие: %q", ev.ResourceId)
	}
}

// TestRemovalReachesASubscriberWhoMayNoLongerSeeThePredmet — событие снятия
// доезжает до того, кто вправе видеть ПРОЕКТ, даже когда предмета он уже видеть
// не вправе.
//
// # Почему это не край, а обычный ход событий
//
// Путь удаления коммитит в ОДНОЙ транзакции строку журнала о снятии и намерение
// снять кортеж владения; кортеж снимает дренаж, асинхронно. Значит к моменту,
// когда подписчик читает событие, предмета в модели прав уже нет — и построчный
// вопрос «вправе ли он видеть эту сеть» получает «нет» ЗАКОННО.
//
// Событие при этом не приходит вовсе: ни ошибки, ни пропуска в нумерации, поток
// открыт и молчит. Это ровно тот исход, против которого заведён якорь проекта, —
// «потребитель держал бы удалённую сеть вечно», — только наступающий на шаг
// позже: якорь спас событие от отбора ОСЬЮ, а построчное сужение отсеяло бы его
// потом.
//
// Сужатель здесь разрешает ПРОЕКТ и не разрешает сеть — то есть ровно то
// состояние, в котором подписчик оказывается через доли секунды после всякого
// удаления.
func TestRemovalReachesASubscriberWhoMayNoLongerSeeThePredmet(t *testing.T) {
	s := newStandWithNarrower(t, narrowtest.Allowing(probeProject))

	s.emit(t, subscriptionjournal.KindNetwork, probeNetwork, probeProject, "DELETED",
		map[string]any{"id": probeNetwork})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	stream := subscribe(t, s, ctx, &subscriptionv1.SubscriptionRequest{
		Kinds:     []string{authzfilter.ResourceTypeNetwork},
		ProjectId: probeProject,
	})

	ev := recv(t, stream)
	if ev.Change != subscriptionv1.SubscriptionEvent_DELETED {
		t.Fatalf("род изменения %v, ожидалось снятие", ev.Change)
	}
	if ev.ResourceId != probeNetwork {
		t.Fatalf("предмет %q, ожидался %q", ev.ResourceId, probeNetwork)
	}
	if ev.ProjectId != probeProject {
		t.Fatalf("якорь проекта %q, ожидался %q", ev.ProjectId, probeProject)
	}
}

// TestRemovalIsWithheldFromASubscriberWhoMayNotSeeTheProject — ОТРИЦАНИЕ в паре с
// пробой выше: суждение по якорю не выходит за проект.
//
// Без этой пробы предыдущая зеленела бы и на сервере, который отдаёт снятия
// ВСЕМ: «событие пришло» выполняется и тогда, когда якорь не спрашивают вовсе.
//
// Положительный контроль внутри самой пробы обязателен и по второй причине:
// подписка молчит и тогда, когда снятие законно отсеяно, и тогда, когда поток
// сломан. Различает их видимое событие, пришедшее СЛЕДОМ, — по нему видно, что
// поток жив, дочитал до конца окна и именно ОТСЕЯЛ снятие, а не отстал.
func TestRemovalIsWithheldFromASubscriberWhoMayNotSeeTheProject(t *testing.T) {
	const otherProject = "prj-000000000000000f"
	const mineNetwork = "net-aaaaaaaaaaaaaaaa"
	// Разрешены СВОЙ проект и своя сеть; чужой проект — нет.
	s := newStandWithNarrower(t, narrowtest.Allowing(probeProject, mineNetwork))

	// Снятие в ЧУЖОМ проекте — его вызывающий видеть не вправе.
	s.emit(t, subscriptionjournal.KindNetwork, probeNetwork, otherProject, "DELETED",
		map[string]any{"id": probeNetwork})
	// Видимое событие следом — положительный контроль живости потока.
	mine := network(mineNetwork, probeProject, "mine")
	s.emit(t, subscriptionjournal.KindNetwork, mine.ID, mine.ProjectID, "CREATED", networkPayload(mine))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Подписка БЕЗ оси проекта: с осью страж отверг бы открытие, и предмет пробы
	// (суждение о СТРОКЕ) не наступил бы вовсе.
	stream := subscribe(t, s, ctx, &subscriptionv1.SubscriptionRequest{
		Kinds: []string{authzfilter.ResourceTypeNetwork},
	})

	ev := recv(t, stream)
	if ev.ResourceId == probeNetwork {
		t.Fatalf("отдано снятие в ЧУЖОМ проекте (%s): суждение по якорю вышло за проект, "+
			"и подписчик узнал о существовании предмета, которого видеть не вправе",
			ev.ProjectId)
	}
	if ev.ResourceId != mineNetwork || ev.Change != subscriptionv1.SubscriptionEvent_CREATED {
		t.Fatalf("пришло не ожидаемое видимое событие: предмет %q, род %v", ev.ResourceId, ev.Change)
	}
}

// TestUndeclaredKindIsRefusedAtOpening — вид вне словаря отвергается НА ОТКРЫТИИ.
//
// Админские предметы журнала (`AddressPool` и его привязка к сети) в словарь не
// входят намеренно: проектного измерения у них нет — а сужает подписка именно по
// нему, — поэтому вопрос о видимости строки задать нечем (#1494: про модель прав
// это основание не утверждает ничего). Открыть на них подписку значило бы отдать
// поток, в который никогда ничего не придёт, — а молчание читается как
// «изменений нет».
func TestUndeclaredKindIsRefusedAtOpening(t *testing.T) {
	s := newStand(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	stream, err := s.client.Subscribe(ctx, &subscriptionv1.SubscriptionRequest{
		Kinds:     []string{"AddressPool"},
		ProjectId: probeProject,
		Start:     &subscriptionv1.SubscriptionRequest_Anchor{Anchor: subscriptionv1.SubscriptionAnchor_BEGINNING},
	})
	if err == nil {
		_, err = stream.Recv()
	}
	if err == nil {
		t.Fatal("подписка на админский вид открылась: поток, в который ничего не придёт, " +
			"читается подписчиком как «изменений нет»")
	}
}
