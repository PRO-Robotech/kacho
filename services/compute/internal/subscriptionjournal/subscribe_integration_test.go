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

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/pkg/outbox"
	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/pkg/subscription"
	"github.com/PRO-Robotech/kacho/services/compute/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/subscriptionjournal"
)

const (
	probeProject = "prj-1234567890abcdef"
	probeOther   = "prj-abcdef1234567890"
	probeMachine = "epd-1234567890abcdef"
)

// stand — сервер потока НАСТОЯЩИЙ, за настоящим gRPC, над настоящей схемой
// compute.
//
// Предмет этих проб — «провязан и ОТВЕЧАЕТ», а это проверяется вызовом. Проба,
// читающая объявление, осталась бы зелёной у сервиса, чей журнал разошёлся со
// своей схемой: имя колонки живёт значением, и ошибка в нём наступает первым
// запросом в бою, а не сборкой.
type stand struct {
	pool   *pgxpool.Pool
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
	if err := outbox.EmitAnchored(ctx, tx, "compute_outbox", kind, id, projectID, change, payload); err != nil {
		t.Fatalf("строка журнала не записалась: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("транзакция не зафиксировалась: %v", err)
	}
}

func instancePayload(in *domain.Instance) map[string]any {
	b, err := json.Marshal(in)
	if err != nil {
		panic(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		panic(err)
	}
	return m
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

// TestSubscribeAnswersOverTheWire — сервер ПРОВЯЗАН И ОТВЕЧАЕТ, и это проверено
// вызовом.
//
// Объявление журнала несёт имена колонок ЗНАЧЕНИЯМИ. Проба, читающая объявление,
// зеленела бы у сервиса, чей журнал разошёлся со своей схемой: ошибка в имени
// наступает первым запросом в бою, а не сборкой. Поэтому здесь настоящая схема,
// настоящий сервер, настоящий транспорт.
func TestSubscribeAnswersOverTheWire(t *testing.T) {
	s := newStand(t)

	in := &domain.Instance{
		ID:        probeMachine,
		ProjectID: probeProject,
		Name:      "web-1",
		ZoneID:    "ru-central1-a",
		Labels:    map[string]string{"env": "prod"},
		Status:    domain.InstanceStatusRunning,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	s.emit(t, "Instance", probeMachine, probeProject, "CREATED", instancePayload(in))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stream, err := s.client.Subscribe(ctx, &subscriptionv1.SubscriptionRequest{
		Kinds:     []string{authzfilter.ResourceTypeInstance},
		ProjectId: probeProject,
		// НАЧАЛО названо словом: незаданное начало по контракту означает «с
		// текущего конца», и строки, записанные ДО открытия, не пришли бы вовсе.
		// Проба на умолчании зеленела бы только по случайности расписания.
		Start: &subscriptionv1.SubscriptionRequest_Anchor{
			Anchor: subscriptionv1.SubscriptionAnchor_BEGINNING,
		},
	})
	if err != nil {
		t.Fatalf("подписка не открылась: %v", err)
	}

	ev := recv(t, stream)
	// На проводе — ТИП ОБЪЕКТА (`compute_instance`), а не слово, которым
	// репозиторий записал колонку (`Instance`). Здесь они различаются заметнее
	// всего в дереве, и утверждение потому различающее.
	if ev.Kind != authzfilter.ResourceTypeInstance || ev.ResourceId != probeMachine {
		t.Fatalf("пришло не то событие: вид %q, предмет %q", ev.Kind, ev.ResourceId)
	}
	if ev.ProjectId != probeProject {
		t.Fatalf("якорь проекта %q, ожидался %q — решение о показе принимается по нему",
			ev.ProjectId, probeProject)
	}
	if ev.Change != subscriptionv1.SubscriptionEvent_CREATED {
		t.Fatalf("род изменения %v, ожидалось создание", ev.Change)
	}

	// Состояние — ОБЪЯВЛЕННЫЙ тип владельца, а не свободная структура: ключи
	// нагрузки суть поля контракта, и внутренний рефактор не может молча их
	// переименовать.
	any := ev.GetState()
	if any == nil {
		t.Fatalf("событие создания пришло БЕЗ состояния (%v) — подписчик остался бы без предмета",
			ev.GetStateUnavailable())
	}
	var got computev1.Instance
	if err := any.UnmarshalTo(&got); err != nil {
		t.Fatalf("нагрузка не разбирается как объявленный тип: %v", err)
	}
	if got.Id != probeMachine || got.Name != "web-1" || got.ZoneId != "ru-central1-a" {
		t.Fatalf("состояние доехало неполным: id=%q name=%q zone=%q", got.Id, got.Name, got.ZoneId)
	}
	if got.Labels["env"] != "prod" {
		t.Fatalf("метки не доехали (%v) — клиентский отбор по меткам остался бы без источника", got.Labels)
	}
}

// TestRemovalReachesTheSubscriberWithItsProjectAnchor — СОБЫТИЕ СНЯТИЯ доезжает
// до подписки, отобранной по проекту.
//
// Это проба того, ради чего заведена колонка якоря. Нагрузка снятия несёт один
// идентификатор: держи compute якорь только в нагрузке — у снятий он был бы
// пуст, ось `project_id` их не пропускала бы, и потребитель, снявший опрос,
// держал бы удалённые строки ВЕЧНО. Отказ этот тихий: ни ошибки, ни пропуска в
// нумерации, — поэтому он проверяется, а не подразумевается.
func TestRemovalReachesTheSubscriberWithItsProjectAnchor(t *testing.T) {
	s := newStand(t)

	// Нагрузка — ДОСЛОВНО та, что пишет репозиторий на пути удаления.
	s.emit(t, "Instance", probeMachine, probeProject, "DELETED", map[string]any{"id": probeMachine})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stream, err := s.client.Subscribe(ctx, &subscriptionv1.SubscriptionRequest{
		Kinds:     []string{authzfilter.ResourceTypeInstance},
		ProjectId: probeProject,
		// НАЧАЛО названо словом: незаданное начало по контракту означает «с
		// текущего конца», и строки, записанные ДО открытия, не пришли бы вовсе.
		// Проба на умолчании зеленела бы только по случайности расписания.
		Start: &subscriptionv1.SubscriptionRequest_Anchor{
			Anchor: subscriptionv1.SubscriptionAnchor_BEGINNING,
		},
	})
	if err != nil {
		t.Fatalf("подписка не открылась: %v", err)
	}

	ev := recv(t, stream)
	if ev.Change != subscriptionv1.SubscriptionEvent_DELETED {
		t.Fatalf("род изменения %v, ожидалось снятие", ev.Change)
	}
	if ev.ResourceId != probeMachine {
		t.Fatalf("предмет %q, ожидался %q", ev.ResourceId, probeMachine)
	}
	if ev.ProjectId != probeProject {
		t.Fatalf("у СНЯТИЯ якорь проекта %q, ожидался %q. Пустой якорь означает "+
			"«предмет уровня аккаунта», и подписка с осью проекта событие не "+
			"пропустила бы — потребитель держал бы удалённую машину вечно",
			ev.ProjectId, probeProject)
	}
	// Состояния у снятия нет, и это НЕ сбой: предмета больше нет. Пустой предмет
	// вместо признака отсутствия солгал бы подписчику, что у машины не осталось
	// ни имени, ни зоны, ни меток.
	if ev.GetState() != nil {
		t.Fatal("у снятия отдано состояние: подписчик вправе читать непустую нагрузку " +
			"как ПОЛНОЕ состояние и записал бы пустые поля как факт")
	}
	if ev.GetStateUnavailable() == nil {
		t.Fatal("носитель нагрузки не выбран вовсе — форма требует одну из двух ветвей")
	}
	// ПРИЧИНА названа, и названа ТА: у снятия собирать было нечего, попытки не
	// было. «Не удалось сериализовать» здесь — утверждение о неудавшейся попытке,
	// а действия у двух причин противоположны: на сбой разумно перечитать, на
	// снятие — убрать предмет из своего состояния и не читать вовсе.
	if got := ev.GetStateUnavailable().GetReason(); got != subscriptionv1.SubscriptionEvent_StateUnavailable_NOT_PRODUCED {
		t.Fatalf("причина отсутствия состояния у снятия %v, ожидалась NOT_PRODUCED", got)
	}
}

// TestSerializationFailureKeepsItsOwnReason — ВТОРАЯ ПОЛОСА той же развилки, и
// без неё утверждение выше зеленело бы на сервере, который называет NOT_PRODUCED
// ВСЕГДА.
//
// Нагрузка здесь заведомо не разбирается объявленным типом: отображение compute
// читает её `encoding/json` в доменную машину, и строка вместо объекта даёт
// отказ сборки. Это НАСТОЯЩИЙ сбой — состояние есть, собрать не удалось, — и
// причина у него обязана остаться прежней.
func TestSerializationFailureKeepsItsOwnReason(t *testing.T) {
	s := newStand(t)

	// Через прямой INSERT, а не через `emit`: тот принимает `map[string]any` и
	// негодную нагрузку выразить не даёт by construction. Предмет пробы — именно
	// негодная строка, и завести её надо там, где журнал её примет.
	ctxEmit := context.Background()
	if _, err := s.pool.Exec(ctxEmit, `
		INSERT INTO compute_outbox (resource_kind, resource_id, project_id, event_type, payload)
		VALUES ('Instance', $1, $2, 'UPDATED', '"не объект"'::jsonb)`,
		probeMachine, probeProject); err != nil {
		t.Fatalf("негодная строка журнала не записалась: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stream, err := s.client.Subscribe(ctx, &subscriptionv1.SubscriptionRequest{
		Kinds:     []string{authzfilter.ResourceTypeInstance},
		ProjectId: probeProject,
		Start: &subscriptionv1.SubscriptionRequest_Anchor{
			Anchor: subscriptionv1.SubscriptionAnchor_BEGINNING,
		},
	})
	if err != nil {
		t.Fatalf("подписка не открылась: %v", err)
	}

	ev := recv(t, stream)
	// Событие ДОСТАВЛЕНО: отказ сборки состояния его не отменяет — подписчик
	// обязан узнать, что предмет менялся, даже когда состояние не доехало.
	if ev.ResourceId != probeMachine {
		t.Fatalf("предмет %q, ожидался %q", ev.ResourceId, probeMachine)
	}
	if ev.GetStateUnavailable() == nil {
		t.Fatalf("носитель нагрузки не выбран вовсе (state=%v)", ev.GetState())
	}
	if got := ev.GetStateUnavailable().GetReason(); got != subscriptionv1.SubscriptionEvent_StateUnavailable_NOT_SERIALIZABLE {
		t.Fatalf("причина отказа сборки %v, ожидалась NOT_SERIALIZABLE — состояние здесь "+
			"ЕСТЬ, и собрать его не удалось; назови это свойством журнала — и клиент "+
			"перестанет перечитывать там, где перечитать и надо", got)
	}
}

// TestTheProjectAxisNarrowsByTheColumn — ось проекта ОТБИРАЕТ, а не украшает.
//
// Отрицание в паре с положительным контролем той же пробы: без положительного
// «чужого не видно» зеленело бы на потоке, который не отдаёт ничего вовсе.
func TestTheProjectAxisNarrowsByTheColumn(t *testing.T) {
	s := newStand(t)

	other := &domain.Instance{ID: "epd-000000000000000f", ProjectID: probeOther, Name: "other"}
	mine := &domain.Instance{ID: probeMachine, ProjectID: probeProject, Name: "mine"}
	s.emit(t, "Instance", other.ID, probeOther, "CREATED", instancePayload(other))
	s.emit(t, "Instance", mine.ID, probeProject, "CREATED", instancePayload(mine))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stream, err := s.client.Subscribe(ctx, &subscriptionv1.SubscriptionRequest{
		Kinds:     []string{authzfilter.ResourceTypeInstance},
		ProjectId: probeProject,
		// НАЧАЛО названо словом: незаданное начало по контракту означает «с
		// текущего конца», и строки, записанные ДО открытия, не пришли бы вовсе.
		// Проба на умолчании зеленела бы только по случайности расписания.
		Start: &subscriptionv1.SubscriptionRequest_Anchor{
			Anchor: subscriptionv1.SubscriptionAnchor_BEGINNING,
		},
	})
	if err != nil {
		t.Fatalf("подписка не открылась: %v", err)
	}

	ev := recv(t, stream)
	if ev.ResourceId == other.ID {
		t.Fatal("отдано событие ЧУЖОГО проекта: ось project_id не отбирает, и подписчик " +
			"видит ресурсы, которых не называл")
	}
	if ev.ResourceId != mine.ID {
		t.Fatalf("отдан предмет %q, ожидался свой %q", ev.ResourceId, mine.ID)
	}
}

// TestRemovalReachesASubscriberWhoMayNoLongerSeeThePredmet — событие снятия
// доезжает до того, кто вправе видеть ПРОЕКТ, даже когда предмета он уже видеть
// не вправе.
//
// # Почему это не край, а обычный ход событий
//
// Путь удаления коммитит в ОДНОЙ транзакции строку журнала о снятии и намерение
// снять кортеж владения; кортеж снимает дренаж, асинхронно. Значит к моменту, когда
// подписчик читает событие, предмета в модели прав уже нет — и построчный вопрос
// «вправе ли он видеть эту машину» получает «нет» ЗАКОННО.
//
// Событие при этом не приходит вовсе: ни ошибки, ни пропуска в нумерации, поток
// открыт и молчит. Это ровно тот исход, против которого заведён якорь проекта, —
// «потребитель держал бы удалённую машину вечно», — только наступающий на шаг
// позже: якорь спас событие от отбора ОСЬЮ, а построчное сужение отсеяло его
// потом.
//
// Контракт формы называет оба негодных исхода прямо и выбирает между ними:
// «Якорь внутри нагрузки означал бы выбор из двух негодных: спрашивать модель прав
// про несуществующий объект либо не показывать удаления вовсе». Якорь стоит полем
// ОБОЛОЧКИ именно затем, чтобы решение о показе снятия принималось ПО НЕМУ — без
// обращения к предмету.
//
// Сужатель здесь разрешает ПРОЕКТ и не разрешает машину — то есть ровно то
// состояние, в котором подписчик оказывается через доли секунды после всякого
// удаления.
func TestRemovalReachesASubscriberWhoMayNoLongerSeeThePredmet(t *testing.T) {
	s := newStandWithNarrower(t, narrowtest.Allowing(probeProject))

	s.emit(t, "Instance", probeMachine, probeProject, "DELETED", map[string]any{"id": probeMachine})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	stream, err := s.client.Subscribe(ctx, &subscriptionv1.SubscriptionRequest{
		Kinds:     []string{authzfilter.ResourceTypeInstance},
		ProjectId: probeProject,
		Start: &subscriptionv1.SubscriptionRequest_Anchor{
			Anchor: subscriptionv1.SubscriptionAnchor_BEGINNING,
		},
	})
	if err != nil {
		t.Fatalf("подписка не открылась: %v", err)
	}

	ev := recv(t, stream)
	if ev.Change != subscriptionv1.SubscriptionEvent_DELETED {
		t.Fatalf("род изменения %v, ожидалось снятие", ev.Change)
	}
	if ev.ResourceId != probeMachine {
		t.Fatalf("предмет %q, ожидался %q", ev.ResourceId, probeMachine)
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
	const (
		otherProject = "prj-000000000000000f"
		mineMachine  = "epd-aaaaaaaaaaaaaaaa"
	)
	// Разрешены СВОЙ проект и своя машина; чужой проект — нет.
	s := newStandWithNarrower(t, narrowtest.Allowing(probeProject, mineMachine))

	// Снятие в ЧУЖОМ проекте — его вызывающий видеть не вправе.
	s.emit(t, "Instance", probeMachine, otherProject, "DELETED", map[string]any{"id": probeMachine})
	// Видимое событие следом — положительный контроль живости потока.
	mine := &domain.Instance{ID: mineMachine, ProjectID: probeProject, Name: "mine"}
	s.emit(t, "Instance", mineMachine, probeProject, "CREATED", instancePayload(mine))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Подписка БЕЗ оси проекта: с осью страж отверг бы открытие, и предмет пробы
	// (суждение о СТРОКЕ) не наступил бы вовсе.
	stream, err := s.client.Subscribe(ctx, &subscriptionv1.SubscriptionRequest{
		Kinds: []string{authzfilter.ResourceTypeInstance},
		Start: &subscriptionv1.SubscriptionRequest_Anchor{
			Anchor: subscriptionv1.SubscriptionAnchor_BEGINNING,
		},
	})
	if err != nil {
		t.Fatalf("подписка не открылась: %v", err)
	}

	ev := recv(t, stream)
	if ev.ResourceId == probeMachine {
		t.Fatalf("отдано снятие в ЧУЖОМ проекте (%s): суждение по якорю вышло за проект, "+
			"и подписчик узнал о существовании предмета, которого видеть не вправе",
			ev.ProjectId)
	}
	if ev.ResourceId != mineMachine || ev.Change != subscriptionv1.SubscriptionEvent_CREATED {
		t.Fatalf("пришло не ожидаемое видимое событие: предмет %q, род %v", ev.ResourceId, ev.Change)
	}
}
