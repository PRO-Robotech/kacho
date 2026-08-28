// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionstream_test

// revocation_sweep_test.go — СКВОЗНАЯ проба kacho#1022: отзыв прав закрывает
// открытый поток.
//
// # Почему проба сквозная, а не две по половине
//
// Половин здесь ровно две, и каждая по отдельности была зелёной ДО этой задачи:
// отзыв записывался (внутренний глагол края принимал имя субъекта и сбрасывал
// свои записи), а поток умел закрываться (реестр проекции закрывал субъекта
// обходом одного ключа). Вопрос, который задавала первая, был не тем, на который
// отвечала вторая: их ничто не соединяло. Это класс «разрыв, невидимый ни с
// одной стороны по отдельности» — и ловится он только вопросом СКВОЗЬ обе:
// записали отзыв → предъявили открытый поток → получили отказ.
//
// Поэтому здесь настоящий gRPC-вызов внутреннего глагола, настоящий обработчик
// этого глагола и настоящая ручка проекции с настоящим потоком. Подменён один
// журнал владельца — тот же, что и во всём остальном стенде проекции.

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	apigatewayv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/apigateway/v1"
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"

	"github.com/PRO-Robotech/kacho/gateway/internal/handler"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
)

// cacheStub — сброс записей кэша решений. Подмена ЗДЕСЬ законна: предмет пробы —
// доезжает ли отзыв до потока, а не что делает кэш. Стенд считает вызовы, чтобы
// «сброс не позвали вовсе» не осталось незамеченным.
type cacheStub struct {
	dropped  int
	subjects []string
	flushes  int
}

func (c *cacheStub) InvalidateSubject(subject string) int {
	c.subjects = append(c.subjects, subject)
	return c.dropped
}

func (c *cacheStub) Invalidate() { c.flushes++ }

// revocationEdge поднимает внутренний глагол отзыва НАСТОЯЩИМ gRPC и отдаёт его
// клиента. Именно так его зовёт сливщик iam: по проводу, с именем субъекта.
func revocationEdge(
	t *testing.T, inv handler.Invalidator, closer handler.SubjectStreamCloser,
) apigatewayv1.InternalAuthzCacheServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 16)
	srv := grpc.NewServer()
	apigatewayv1.RegisterInternalAuthzCacheServiceServer(srv,
		handler.NewInternalAuthzCacheServer(inv, slog.New(slog.NewTextHandler(&strings.Builder{}, nil))).
			WithSubjectStreamCloser(closer))
	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("дозвон до внутреннего глагола отзыва: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		srv.Stop()
		_ = lis.Close()
	})
	return apigatewayv1.NewInternalAuthzCacheServiceClient(conn)
}

// openHeldStream открывает поток названного субъекта и ждёт, пока владелец его
// примет. Возвращает канал, закрывающийся вместе с потоком.
func openHeldStream(
	t *testing.T, h *subscriptionstream.Handler, principalType, principalID string,
) <-chan struct{} {
	t.Helper()
	r := request("owner=probe")
	r.Header.Set(principalmeta.HeaderPrincipalType, principalType)
	r.Header.Set(principalmeta.HeaderPrincipalID, principalID)

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		serve(t, h, r)
	}()
	return finished
}

// waitStreams ждёт, пока ручка НАСЧИТАЕТ ровно столько открытых потоков.
//
// Ожидание по СОСТОЯНИЮ, а не паузой: пауза либо мала (проба падает не на своём
// предмете), либо велика (каждый прогон платит за худший случай).
func waitStreams(t *testing.T, h *subscriptionstream.Handler, want int64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if h.Stats().Open == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("открытых потоков %d, ожидалось %d", h.Stats().Open, want)
}

// TestRevocationOverTheWireClosesTheOpenStream — НЕСУЩЕЕ утверждение задачи.
//
// Отзыв приходит ПО ПРОВОДУ на внутренний глагол края, поток к этому моменту уже
// открыт и обслуживается. Утверждается исход: поток закрыт.
func TestRevocationOverTheWireClosesTheOpenStream(t *testing.T) {
	held := &ownerStub{
		script:  []*subscriptionv1.SubscriptionMessage{openedMessage("p", false)},
		hold:    true,
		started: make(chan struct{}),
	}
	h := newHandler(t, held, func(c *subscriptionstream.Config) {
		// Срок жизни потока заведомо больше времени пробы: закрытие обязано
		// прийти от ОТЗЫВА, а не от истечения бюджета. Совпади они — проба
		// зеленела бы на механизме, которого нет.
		c.StreamBudget = 60 * time.Second
		c.Heartbeat = 20 * time.Second
	})
	cache := &cacheStub{dropped: 0}
	client := revocationEdge(t, cache, h)

	done := openHeldStream(t, h, "user", "usr-revoked")
	select {
	case <-held.started:
	case <-time.After(10 * time.Second):
		t.Fatal("поток не открылся — предъявлять нечего")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := client.InvalidateSubject(ctx, &apigatewayv1.InvalidateSubjectRequest{
		Subject:   "user:usr-revoked",
		EventType: "binding_revoke",
	})

	// НЕСУЩЕЕ утверждение — первым: иначе отказ по коду ответа сработал бы
	// раньше и назвал бы предметом пробы форму ответа, а не судьбу потока.
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("поток пережил отзыв прав — контроль стоит на выдаче и не стоит на предъявлении")
	}

	// Записей кэша нет, но поток закрыт — значит сделано БЫЛО, и ответ обязан
	// это признать: `NotFound` здесь означал бы «делать было нечего».
	if err != nil {
		t.Fatalf("отзыв закрыл поток, но глагол ответил отказом: %v", err)
	}
	if len(cache.subjects) != 1 || cache.subjects[0] != "user:usr-revoked" {
		t.Errorf("сброс кэша позван %v, ожидался ровно один по «user:usr-revoked»", cache.subjects)
	}
}

// TestRevocationOverTheWireLeavesTheNeighbourAlone — РАДИУС закрытия.
//
// Отрицание («чужой не задет») зеленело бы на устройстве, которое не закрывает
// НИКОГО, — поэтому положительный контроль стоит в той же пробе: отозванный
// закрыт, сосед жив и закрывается только своим отзывом.
func TestRevocationOverTheWireLeavesTheNeighbourAlone(t *testing.T) {
	held := &ownerStub{
		script:  []*subscriptionv1.SubscriptionMessage{openedMessage("p", false)},
		hold:    true,
		started: make(chan struct{}),
	}
	h := newHandler(t, held, func(c *subscriptionstream.Config) {
		c.StreamBudget = 60 * time.Second
		c.Heartbeat = 20 * time.Second
	})
	client := revocationEdge(t, &cacheStub{}, h)

	revoked := openHeldStream(t, h, "user", "usr-revoked")
	select {
	case <-held.started:
	case <-time.After(10 * time.Second):
		t.Fatal("первый поток не открылся")
	}
	neighbour := openHeldStream(t, h, "service_account", "sva-neighbour")
	// Второй поток обязан ДОЙТИ до владельца прежде, чем придёт отзыв: иначе
	// «сосед не задет» означало бы «соседа ещё не было».
	waitStreams(t, h, 2)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, revokeErr := client.InvalidateSubject(ctx, &apigatewayv1.InvalidateSubjectRequest{
		Subject: "user:usr-revoked",
	})
	select {
	case <-revoked:
	case <-time.After(10 * time.Second):
		t.Fatal("отозванный поток не закрылся")
	}
	if revokeErr != nil {
		t.Fatalf("отзыв отозванного: %v", revokeErr)
	}

	select {
	case <-neighbour:
		t.Fatal("отзыв одного субъекта закрыл поток другого — радиус обязан быть один субъект")
	case <-time.After(500 * time.Millisecond):
	}

	_, neighbourErr := client.InvalidateSubject(ctx, &apigatewayv1.InvalidateSubjectRequest{
		Subject: "service_account:sva-neighbour",
	})
	select {
	case <-neighbour:
	case <-time.After(10 * time.Second):
		t.Fatal("поток соседа не закрылся своим отзывом — значит первая половина пробы ничего не доказала")
	}
	if neighbourErr != nil {
		t.Fatalf("отзыв соседа: %v", neighbourErr)
	}
}

// TestRevocationOfASubjectWithoutStreamsStaysIdempotent — отзыв без предмета.
//
// Ни записей кэша, ни потоков: глагол обязан остаться идемпотентным
// (`NotFound`), иначе сливщик iam перестанет помечать строку доставленной и
// будет повторять её до исчерпания попыток.
func TestRevocationOfASubjectWithoutStreamsStaysIdempotent(t *testing.T) {
	h := newHandler(t, &ownerStub{})
	client := revocationEdge(t, &cacheStub{dropped: 0}, h)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := client.InvalidateSubject(ctx, &apigatewayv1.InvalidateSubjectRequest{
		Subject: "user:usr-nobody",
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("отзыв без предмета ответил %v, ожидался NotFound (идемпотентный промах)", status.Code(err))
	}
}

// TestEmptySubjectIsRefusedUnconditionally — пустой субъект отсекается ВСЕГДА.
//
// Не «когда провязан закрыватель потоков»: устройство, отвечающее по-разному в
// зависимости от того, что подключено, превращает проверку в свойство посадки.
func TestEmptySubjectIsRefusedUnconditionally(t *testing.T) {
	for _, tc := range []struct {
		name   string
		closer handler.SubjectStreamCloser
	}{
		{name: "закрыватель провязан", closer: newHandler(t, &ownerStub{})},
		{name: "закрывателя нет", closer: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := revocationEdge(t, &cacheStub{}, tc.closer)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, err := client.InvalidateSubject(ctx, &apigatewayv1.InvalidateSubjectRequest{Subject: ""})
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("пустой субъект ответил %v, ожидался InvalidArgument", status.Code(err))
			}
		})
	}
}

// TestUnnameableSubjectIsRefusedBeforeItIsRegistered — ПУНКТ 4 предиката задачи.
//
// Поток учитывается под ключом «тип:идентификатор». Если тип не называет
// тенантного субъекта, такой ключ не сможет назвать НИ ОДИН отзыв: iam говорит о
// субъектах модели прав («user:…», «service_account:…»), и `»:usr-x»` либо
// `«workload:wid-x»` в этом словаре не существует. Поток под таким ключом
// закрыть нечем — то есть он неотзываем ПО ПОСТРОЕНИЮ.
//
// Отсекается это безусловно и ДО постановки на учёт, а не «когда провязан
// закрыватель»: иначе отзываемость потока становится свойством посадки.
func TestUnnameableSubjectIsRefusedBeforeItIsRegistered(t *testing.T) {
	for _, tc := range []struct {
		name          string
		principalType string
		principalID   string
		wantStatus    int
	}{
		{name: "тип не назван", principalType: "", principalID: "usr-x", wantStatus: 401},
		{name: "тип вне словаря модели", principalType: "workload", principalID: "wid-x", wantStatus: 401},
		{name: "тип написан псевдонимом", principalType: "sva", principalID: "sva-x", wantStatus: 401},
		{name: "идентификатор двигает границу типа", principalType: "user", principalID: "a:b", wantStatus: 401},
		{name: "идентификатор ссылается на набор", principalType: "user", principalID: "a#member", wantStatus: 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			held := &ownerStub{
				script:  []*subscriptionv1.SubscriptionMessage{openedMessage("p", false)},
				hold:    true,
				started: make(chan struct{}),
			}
			h := newHandler(t, held)
			r := request("owner=probe")
			r.Header.Set(principalmeta.HeaderPrincipalType, tc.principalType)
			r.Header.Set(principalmeta.HeaderPrincipalID, tc.principalID)
			rec := serve(t, h, r)
			if rec.Code != tc.wantStatus {
				t.Fatalf("ответ %d, ожидался %d", rec.Code, tc.wantStatus)
			}
			if got := h.Stats().Open; got != 0 {
				t.Fatalf("на учёте %d потоков — неотзываемый поток был зарегистрирован", got)
			}
		})
	}
}

// TestNameableSubjectIsAdmitted — положительный контроль к предыдущей пробе.
//
// Без него отсечение зеленело бы на ручке, отвергающей ВСЕХ, и «неотзываемых
// потоков нет» означало бы «потоков нет».
func TestNameableSubjectIsAdmitted(t *testing.T) {
	for _, principalType := range []string{"user", "service_account"} {
		t.Run(principalType, func(t *testing.T) {
			held := &ownerStub{
				script:  []*subscriptionv1.SubscriptionMessage{openedMessage("p", false)},
				hold:    true,
				started: make(chan struct{}),
			}
			h := newHandler(t, held, func(c *subscriptionstream.Config) {
				c.StreamBudget = 60 * time.Second
				c.Heartbeat = 20 * time.Second
			})
			done := openHeldStream(t, h, principalType, "id-probe")
			select {
			case <-held.started:
			case <-time.After(10 * time.Second):
				t.Fatal("поток тенантного субъекта не открылся")
			}
			// Ключ учёта — тот же субъект модели прав, которым назовёт его отзыв.
			if n := h.CloseSubject(principalType + ":id-probe"); n != 1 {
				t.Fatalf("закрыто %d потоков по ключу «%s:id-probe» — ключ учёта разошёлся с именем отзыва",
					n, principalType)
			}
			<-done
		})
	}
}
