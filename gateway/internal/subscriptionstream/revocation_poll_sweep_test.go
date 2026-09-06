// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionstream_test

// revocation_poll_sweep_test.go — СКВОЗНАЯ проба отзыва, доезжающего до
// ОТКРЫТОГО соединения (kacho#1022).
//
// # Почему сквозная, а не две по половине
//
// Перепрос — единственный путь, которым отзыв доходит до длинных соединений:
// толчок от владельца прав снят вместе с обратным ребром (kacho#1024). У этого
// пути есть свойство, которого у толчка не было: он получает `subject_id` ГОЛЫМ
// идентификатором, а субъектом модели прав тот становится только в паре с типом.
//
// Половины здесь врут порознь особенно убедительно. «Адаптер отдал имя» зелено
// на голом идентификаторе. «Реестр закрывает по имени» зелено на ключе
// `user:usr…`. Обе половины исправны, а вместе не работают: реестр не содержит
// ключа `usr…` НИ ПРИ КАКИХ УСЛОВИЯХ, и отзыв не имел бы действия вообще.
//
// Поэтому здесь настоящий gRPC владельца прав, настоящий адаптер, настоящий
// перепрос, настоящая проекция и настоящий открытый поток SSE.

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
	"google.golang.org/grpc/test/bufconn"

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
	"github.com/PRO-Robotech/kacho/pkg/subjectchange"
)

// iamJournalStub — внутренний глагол iam, отдающий заготовленные порции.
//
// Порции ЗА ПЕРВОЙ отдаются только после того, как проба их отпустит: иначе
// «праймящий перепрос никого не закрыл» и «отзыв ещё не приехал» — одно и то же
// наблюдение, и утверждение о первом ничего бы не значило.
type iamJournalStub struct {
	iamv1.UnimplementedInternalIAMServiceServer
	mu      sync.Mutex
	batches [][]*iamv1.SubjectChange
	calls   int
	primed  chan struct{}
	release chan struct{}
}

func newIamJournalStub(batches ...[]*iamv1.SubjectChange) *iamJournalStub {
	return &iamJournalStub{
		batches: batches,
		primed:  make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (s *iamJournalStub) PollSubjectChanges(
	_ context.Context, _ *iamv1.PollSubjectChangesRequest,
) (*iamv1.PollSubjectChangesResponse, error) {
	s.mu.Lock()
	i := s.calls
	s.calls++
	var b []*iamv1.SubjectChange
	if i < len(s.batches) {
		b = s.batches[i]
	}
	s.mu.Unlock()

	if s.primed != nil {
		if i == 0 {
			close(s.primed)
		} else {
			<-s.release
		}
	}

	var head int64
	for _, c := range b {
		if c.GetId() > head {
			head = c.GetId()
		}
	}
	return &iamv1.PollSubjectChangesResponse{Changes: b, HeadId: head}, nil
}

// journalOwnerStub — владелец журнала подписки: открывает поток и держит его.
type journalOwnerStub struct {
	subscriptionv1.UnimplementedInternalSubscriptionServiceServer

	mu sync.Mutex
	// served — сколько потоков стенд обслужил. Отвечает на вопрос, на который
	// сигнал не отвечает: «дошло до стенда» и «дошло до ждущего» — разные факты,
	// и [journalOwnerStub.awaitStreams] называет оба, когда истекает срок.
	served int

	// started получает по одному значению НА КАЖДЫЙ обслуженный поток.
	//
	// Прежняя редакция ЗАКРЫВАЛА канал, и цена этого названа задачей #1482.
	// Во-первых, второе открытие роняло ПРОЦЕСС двойным закрытием — то есть
	// отказ приходил не упавшим утверждением, а гибелью всего пакета, и
	// читался бы как дефект продукта, а не стенда. Во-вторых, закрытый канал
	// отдаёт ВСЕМ и СРАЗУ, поэтому ожидание n потоков выполнялось тождественно
	// после первого; условие, выполняющееся тождественно, условием не является.
	// Тот же приём и по тем же двум причинам снят у соседнего стенда пакета
	// (`gateway/internal/subscriptionstream/harness_test.go`, #1485) — идиома
	// здесь повторена, а не изобретена.
	//
	// Ждут через [journalOwnerStub.awaitStreams]; глубина буфера —
	// [startedDepth], общая с соседним стендом: величина у них одна и та же —
	// сколько потоков стенд обслужит за пробу.
	started chan struct{}
}

func (o *journalOwnerStub) Subscribe(
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
	o.mu.Lock()
	o.served++
	o.mu.Unlock()
	if o.started != nil {
		// Отправка НЕБЛОКИРУЮЩАЯ: поток, которого проба не ждёт, обязан
		// обслуживаться как обычно, а не замирать до чьего-то чтения — иначе
		// стенд стал бы снисходительнее продукта в одну сторону и строже в
		// другую. Значит буфер обязан вмещать все потоки пробы; переполнение
		// теряет сигнал, и awaitStreams называет это числом, а не молчит.
		select {
		case o.started <- struct{}{}:
		default:
		}
	}
	<-stream.Context().Done()
	return nil
}

// servedStreams — сколько потоков стенд обслужил на данный момент.
func (o *journalOwnerStub) servedStreams() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.served
}

// awaitStreams ждёт, пока стенд не примет n потоков.
//
// Ждётся УСЛОВИЕ — приход n-го потока, — а не срок. Срок здесь страховочный, и
// его истечение — падение С ЧИСЛАМИ: «ноль сигналов» обязано быть отличимо от
// «ноль потоков», иначе разбор упавшей пробы начинается с догадки.
func (o *journalOwnerStub) awaitStreams(t *testing.T, n int) {
	t.Helper()
	for got := 0; got < n; got++ {
		select {
		case <-o.started:
		case <-time.After(10 * time.Second):
			t.Fatalf("стенд принял потоков %d из ожидавшихся %d (обслужено всего %d) — "+
				"поток не открылся; если обслужено не меньше ожидаемого, мал буфер канала started",
				got, n, o.servedStreams())
		}
	}
}

// dial поднимает названную службу на bufconn и отдаёт соединение.
func dial(t *testing.T, register func(*grpc.Server)) *grpc.ClientConn {
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

// TestPolledRevocationClosesTheOpenStreamEndToEnd — журнал iam называет субъекта
// ГОЛЫМ идентификатором и типом; поток к этому моменту открыт. Утверждается
// исход: поток закрыт.
func TestPolledRevocationClosesTheOpenStreamEndToEnd(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(&strings.Builder{}, &slog.HandlerOptions{Level: slog.LevelError + 1}))

	owner := &journalOwnerStub{started: make(chan struct{}, startedDepth)}
	ownerConn := dial(t, func(s *grpc.Server) {
		subscriptionv1.RegisterInternalSubscriptionServiceServer(s, owner)
	})
	projection, err := subscriptionstream.NewHandler(subscriptionstream.Config{
		Owners: subscriptionstream.Owners{
			"probe": subscriptionv1.NewInternalSubscriptionServiceClient(ownerConn),
		},
		// Срок жизни заведомо больше пробы: закрытие обязано прийти от отзыва, а
		// не от истечения бюджета — совпади они, проба зеленела бы на механизме,
		// которого нет.
		StreamBudget:         60 * time.Second,
		Heartbeat:            20 * time.Second,
		MaxStreams:           4,
		MaxStreamsPerSubject: 4,
		Logger:               quiet,
	})
	if err != nil {
		t.Fatalf("сборка проекции: %v", err)
	}

	// Журнал iam отдаёт ГОЛЫЙ идентификатор — так, как он лежит в строке.
	iamStub := newIamJournalStub(
		[]*iamv1.SubjectChange{{Id: 1, SubjectId: "usr00000000000000009", SubjectType: "user", Op: "binding_upsert"}},
		[]*iamv1.SubjectChange{{Id: 2, SubjectId: "usr00000000000000001", SubjectType: "user", Op: "binding_revoke"}},
	)
	iamConn := dial(t, func(s *grpc.Server) {
		iamv1.RegisterInternalIAMServiceServer(s, iamStub)
	})

	w, err := subjectchange.New(subjectchange.Config{
		Poller:   subjectchange.NewReader(iamConn),
		Flush:    func() {},
		Interval: 20 * time.Millisecond,
		Closer:   projection,
		// Заведомо больше пробы: предмет здесь — приехавший отзыв, а не
		// исчерпание срока неподтверждённого чтения.
		StaleAfter: 10 * time.Minute,
		Logger:     quiet,
	})
	if err != nil {
		t.Fatalf("сборка читателя отзыва: %v", err)
	}

	// Открытый поток субъекта, чей отзыв приедет второй порцией.
	r := httptest.NewRequest(http.MethodGet, subscriptionstream.Path+"?owner=probe", nil)
	r.Header.Set(principalmeta.HeaderPrincipalType, "user")
	r.Header.Set(principalmeta.HeaderPrincipalID, "usr00000000000000001")
	done := make(chan struct{})
	go func() {
		defer close(done)
		projection.ServeHTTP(httptest.NewRecorder(), r)
	}()
	owner.awaitStreams(t, 1)

	runCtx, stopRun := context.WithCancel(context.Background())
	defer stopRun()
	go w.Run(runCtx)

	// Праймящий перепрос состоялся; вторая порция ещё удержана стендом.
	select {
	case <-iamStub.primed:
	case <-time.After(10 * time.Second):
		t.Fatal("перепрос не состоялся ни разу")
	}
	select {
	case <-done:
		t.Fatal("праймящий перепрос закрыл поток — принятие курсора отзывом не является")
	case <-time.After(200 * time.Millisecond):
	}

	close(iamStub.release) // отпускаем порцию с отзывом
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("поток пережил отзыв, приехавший перепросом — перепрос есть ЕДИНСТВЕННЫЙ путь " +
			"отзыва к длинным соединениям, и не исполнив его, край не исполняет отзыв вовсе")
	}
}

// TestJournalOwnerStubServesASecondStream — стенд обязан обслуживать КАЖДЫЙ
// поток, а не только первый.
//
// Проба стенда, а не продукта, и она здесь по той же причине, по какой корпус
// требует доказывать способность гейта упасть: подделка, ЛОМАЮЩАЯСЯ там, где
// настоящий владелец работает, отравляет вердикт сильнее снисходительной. Её
// отказ приходит паникой процесса — то есть не упавшим утверждением, а гибелью
// всего пакета, — и читается как дефект продукта.
//
// Предмет соседней пробы этого файла — ПЕРЕОПРОС при отзыве, то есть повторное
// открытие принадлежит природе вещей: первое же изменение, заводящее второй
// поток, уронило бы её здесь, а не в продукте (#1482).
func TestJournalOwnerStubServesASecondStream(t *testing.T) {
	owner := &journalOwnerStub{started: make(chan struct{}, startedDepth)}
	conn := dial(t, func(s *grpc.Server) {
		subscriptionv1.RegisterInternalSubscriptionServiceServer(s, owner)
	})
	client := subscriptionv1.NewInternalSubscriptionServiceClient(conn)

	// Два ПОСЛЕДОВАТЕЛЬНЫХ открытия к ОДНОМУ дублёру: второе — предмет пробы,
	// первое — положительный контроль (без него утверждение о втором зеленело бы
	// на стенде, не обслуживающем вовсе никого).
	for i := 1; i <= 2; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		stream, err := client.Subscribe(ctx, &subscriptionv1.SubscriptionRequest{})
		if err != nil {
			cancel()
			t.Fatalf("поток %d: открытие: %v", i, err)
		}
		if _, err := stream.Recv(); err != nil {
			cancel()
			t.Fatalf("поток %d: служебное сообщение открытия не пришло: %v", i, err)
		}
		cancel()
	}

	// Сигналит КАЖДЫЙ поток. Утверждать это ожиданием ДВУХ сигналов обязательно:
	// прежняя редакция ЗАКРЫВАЛА канал, а закрытый отдаёт всем и сразу — то есть
	// ожидание любого числа потоков выполнялось бы тождественно после первого.
	owner.awaitStreams(t, 2)

	// И независимо от сигналов — сколько потоков стенд на самом деле обслужил.
	// Сигнал доказывает «дошло до нас», обслуженное — «дошло до стенда»; на
	// тождественно истинном ожидании расходятся именно эти два числа.
	if got := owner.servedStreams(); got != 2 {
		t.Fatalf("стенд обслужил потоков %d, ожидалось 2", got)
	}
}
