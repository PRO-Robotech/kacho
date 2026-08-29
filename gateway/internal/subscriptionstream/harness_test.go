// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionstream_test

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
)

// harness_test.go — стенд проекции: НАСТОЯЩИЙ gRPC поверх bufconn, настоящие
// сообщения контракта, настоящие коды состояния.
//
// # Что подменено и что этим НЕ доказывается
//
// Подменён ЖУРНАЛ владельца — и только он. Всё остальное настоящее: транспорт,
// сериализация, метаданные, коды. Это сказано вслух, потому что подделка,
// которой проверяют провязку, обязана быть НЕ СНИСХОДИТЕЛЬНЕЕ продукта, а
// граница её доверия — знанием, а не догадкой.
//
// Не доказывается здесь: устоявшаяся граница журнала и порядок выдачи (предмет
// собственных интеграционных проб `pkg/subscription`, где журнал настоящий) и
// браузерная нога цепи (предмет сквозных проб `ui-future/e2e`).
//
// Владелец-стенд ЗАПИСЫВАЕТ полученный запрос и метаданные: именно по ним
// проверяется несущее утверждение задачи — позиция, названная браузером,
// доезжает до владельца.

// ownerStub — владелец журнала на стенде.
type ownerStub struct {
	subscriptionv1.UnimplementedInternalSubscriptionServiceServer

	// script — что владелец отдаёт: последовательность сообщений, затем ошибка.
	script []*subscriptionv1.SubscriptionMessage
	// failWith — ошибка вместо/после сообщений; ноль означает «держать поток».
	failWith error
	// failFirst — отказать ДО первого сообщения (то есть до заголовков ответа).
	failFirst bool
	// hold — держать поток открытым после сценария, пока не уйдёт вызывающий.
	hold bool

	// Записанное. Пишется КАЖДЫМ обслуженным потоком, поэтому живёт под замком:
	// ручка держит несколько потоков одновременно by design — иначе пределы
	// реплики и субъекта не наблюдаемы вовсе, — и стенд обязан это выдерживать.
	// Читается через receivedRequest/receivedMD.
	mu       sync.Mutex
	requests []*subscriptionv1.SubscriptionRequest
	mds      []metadata.MD
	// started закрывается на ПЕРВОМ потоке: стенд обслуживает и второй, и
	// закрытие канала дважды роняет процесс — то есть проба падала бы не на
	// своём предмете, а на устройстве стенда.
	started   chan struct{}
	startOnce sync.Once
}

func (o *ownerStub) Subscribe(
	req *subscriptionv1.SubscriptionRequest,
	stream subscriptionv1.InternalSubscriptionService_SubscribeServer,
) error {
	md, _ := metadata.FromIncomingContext(stream.Context())
	o.mu.Lock()
	o.requests = append(o.requests, req)
	o.mds = append(o.mds, md)
	o.mu.Unlock()
	if o.started != nil {
		o.startOnce.Do(func() { close(o.started) })
	}
	if o.failFirst {
		return o.failWith
	}
	for _, msg := range o.script {
		if err := stream.Send(msg); err != nil {
			return err
		}
	}
	if o.failWith != nil {
		return o.failWith
	}
	if o.hold {
		<-stream.Context().Done()
	}
	return nil
}

// receivedRequest — запрос ЕДИНСТВЕННОГО обслуженного потока.
//
// «Взять последний» здесь не годится: при нескольких потоках у поля нет
// определённого значения, и проба утверждала бы о произвольном из них.
// Неоднозначность обязана быть падением с числом, а не молчаливым выбором.
func (o *ownerStub) receivedRequest(t *testing.T) *subscriptionv1.SubscriptionRequest {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.requests) != 1 {
		t.Fatalf("владелец обслужил потоков %d, ожидался ровно один — "+
			"запрос «того самого» потока не определён", len(o.requests))
	}
	return o.requests[0]
}

// receivedMD — метаданные ЕДИНСТВЕННОГО обслуженного потока; см. receivedRequest.
func (o *ownerStub) receivedMD(t *testing.T) metadata.MD {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.mds) != 1 {
		t.Fatalf("владелец обслужил потоков %d, ожидался ровно один — "+
			"метаданные «того самого» потока не определены", len(o.mds))
	}
	return o.mds[0]
}

// dialStub поднимает владельца на bufconn и отдаёт его клиента.
func dialStub(t *testing.T, owner *ownerStub) subscriptionstream.OwnerConn {
	t.Helper()
	lis := bufconn.Listen(1 << 16)
	srv := grpc.NewServer()
	subscriptionv1.RegisterInternalSubscriptionServiceServer(srv, owner)
	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("дозвон до владельца-стенда: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		srv.Stop()
		_ = lis.Close()
	})
	return subscriptionv1.NewInternalSubscriptionServiceClient(conn)
}

// newHandler собирает ручку со стендовым владельцем под именем `probe`.
func newHandler(t *testing.T, owner *ownerStub, tune ...func(*subscriptionstream.Config)) *subscriptionstream.Handler {
	t.Helper()
	cfg := subscriptionstream.Config{
		Owners:               subscriptionstream.Owners{"probe": dialStub(t, owner)},
		StreamBudget:         5 * time.Second,
		Heartbeat:            2 * time.Second,
		MaxStreams:           4,
		MaxStreamsPerSubject: 4,
		Logger:               slog.New(slog.NewTextHandler(&strings.Builder{}, nil)),
	}
	for _, f := range tune {
		f(&cfg)
	}
	h, err := subscriptionstream.NewHandler(cfg)
	if err != nil {
		t.Fatalf("сборка ручки: %v", err)
	}
	return h
}

// request собирает запрос НАЗВАННОГО вызывающего — то есть такой, каким его
// увидит ручка после полосы аутентификации.
func request(query string, headers ...string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, subscriptionstream.Path+"?"+query, nil)
	r.Header.Set(principalmeta.HeaderPrincipalType, "user")
	r.Header.Set(principalmeta.HeaderPrincipalID, "usr-probe")
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	return r
}

// serve гоняет один запрос через ручку и возвращает записанный ответ.
func serve(t *testing.T, h *subscriptionstream.Handler, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(rec, r)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("ручка не завершила ответ за 20 с — поток не закрылся ни по сроку, ни по уходу вызывающего")
	}
	return rec
}

// openedMessage — служебное сообщение открытия с названной позицией.
func openedMessage(position string, caughtUp bool) *subscriptionv1.SubscriptionMessage {
	return &subscriptionv1.SubscriptionMessage{
		Message: &subscriptionv1.SubscriptionMessage_Opened{
			Opened: &subscriptionv1.SubscriptionOpened{
				Position:          position,
				CaughtUp:          caughtUp,
				HonoredFilters:    []string{"project_id"},
				RetainsEverything: true,
			},
		},
	}
}

// eventMessage — событие с названной позицией и предметом.
func eventMessage(position, kind, resourceID string) *subscriptionv1.SubscriptionMessage {
	return &subscriptionv1.SubscriptionMessage{
		Message: &subscriptionv1.SubscriptionMessage_Event{
			Event: &subscriptionv1.SubscriptionEvent{
				Position:   position,
				Kind:       kind,
				ResourceId: resourceID,
				ProjectId:  "prj-probe",
				Change:     subscriptionv1.SubscriptionEvent_CREATED,
			},
		},
	}
}

// frames разбирает тело ответа в кадры SSE.
type frame struct {
	id    string
	event string
	data  string
}

func frames(t *testing.T, body string) []frame {
	t.Helper()
	out := make([]frame, 0, 4)
	for _, block := range strings.Split(body, "\n\n") {
		if block = strings.TrimRight(block, "\n"); block == "" {
			continue
		}
		if strings.HasPrefix(block, ":") {
			// Служебный кадр поддержания связи — не событие.
			continue
		}
		f := frame{}
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "id: "):
				f.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				f.event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				f.data += strings.TrimPrefix(line, "data: ")
			}
		}
		out = append(out, f)
	}
	return out
}

// heartbeats считает служебные кадры поддержания связи.
func heartbeats(body string) int {
	return strings.Count(body, ": keep-alive\n\n")
}

// eventWithState — событие, несущее НАСТОЯЩЕЕ состояние на проводе.
//
// Ветвь состояния иначе не исполняется ни разу: без неё зелёными остаются и
// разбор носителя, и подстановка признака недоступности, то есть ровно та
// половина кадрирования, которая работает с полезной нагрузкой.
func eventWithState(t *testing.T, position string, state proto.Message) *subscriptionv1.SubscriptionMessage {
	t.Helper()
	packed, err := anypb.New(state)
	if err != nil {
		t.Fatalf("упаковка состояния: %v", err)
	}
	return &subscriptionv1.SubscriptionMessage{
		Message: &subscriptionv1.SubscriptionMessage_Event{
			Event: &subscriptionv1.SubscriptionEvent{
				Position: position, Kind: "vpc.network", ResourceId: "net-1",
				ProjectId: "prj-probe", Change: subscriptionv1.SubscriptionEvent_UPDATED,
				Carrier: &subscriptionv1.SubscriptionEvent_State{State: packed},
			},
		},
	}
}

// eventWithUnresolvableState — событие, чьё состояние край РАЗОБРАТЬ НЕ МОЖЕТ.
//
// Это не выдуманный случай: владелец вправе класть в носитель тип, которого нет
// в двоичном файле края. Контракт для него завёл своё значение, и проверяется
// именно оно — что край подставляет признак владельца, а не сочиняет свой и не
// роняет поток.
// eventWithAbsentState — событие, у которого ВЛАДЕЛЕЦ назвал причину отсутствия
// состояния. Край такое событие только везёт: разбирать в нём нечего.
func eventWithAbsentState(
	position string,
	reason subscriptionv1.SubscriptionEvent_StateUnavailable_Reason,
) *subscriptionv1.SubscriptionMessage {
	return &subscriptionv1.SubscriptionMessage{
		Message: &subscriptionv1.SubscriptionMessage_Event{
			Event: &subscriptionv1.SubscriptionEvent{
				Position: position, Kind: "elsewhere.thing", ResourceId: "thing-2",
				ProjectId: "prj-probe", Change: subscriptionv1.SubscriptionEvent_UPDATED,
				Carrier: &subscriptionv1.SubscriptionEvent_StateUnavailable_{
					StateUnavailable: &subscriptionv1.SubscriptionEvent_StateUnavailable{
						Reason: reason,
					},
				},
			},
		},
	}
}

func eventWithUnresolvableState(position string) *subscriptionv1.SubscriptionMessage {
	return &subscriptionv1.SubscriptionMessage{
		Message: &subscriptionv1.SubscriptionMessage_Event{
			Event: &subscriptionv1.SubscriptionEvent{
				Position: position, Kind: "elsewhere.thing", ResourceId: "thing-1",
				ProjectId: "prj-probe", Change: subscriptionv1.SubscriptionEvent_CREATED,
				Carrier: &subscriptionv1.SubscriptionEvent_State{
					State: &anypb.Any{TypeUrl: "type.googleapis.com/kacho.nowhere.v1.Absent"},
				},
			},
		},
	}
}
