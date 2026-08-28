// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients_test

// subject_change_sweep_test.go — СКВОЗНАЯ проба ВТОРОЙ полосы отзыва (kacho#1022).
//
// # Почему эта полоса требует своей сквозной пробы, а не двух по половине
//
// Толчок iam доходит до ОДНОЙ реплики края. Остальные узнают об отзыве только
// перепросом, и у него есть свойство, которого у толчка нет: он получает
// `subject_id` ГОЛЫМ идентификатором. Субъектом модели прав тот становится
// только в паре с типом, а тип приезжает отдельным полем — которого у этого
// сообщения раньше не было вовсе.
//
// Половины здесь врут порознь особенно убедительно. «Адаптер отдал имя» зелено
// на голом идентификаторе. «Реестр закрывает по имени» зелено на ключе
// `user:usr…`. Обе половины исправны, а вместе не работают: реестр не содержит
// ключа `usr…` НИ ПРИ КАКИХ УСЛОВИЯХ, и отзыв на такой реплике не имеет действия
// вообще. Ровно этот дефект и был написан по дороге, пока проба не была
// сквозной.
//
// Поэтому здесь настоящий gRPC iam, настоящий адаптер, настоящий перепрос,
// настоящая проекция и настоящий открытый поток SSE.

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

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"

	"github.com/PRO-Robotech/kacho/gateway/internal/clients"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
	"github.com/PRO-Robotech/kacho/gateway/internal/watcher"
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
	close(o.started)
	<-stream.Context().Done()
	return nil
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

	owner := &journalOwnerStub{started: make(chan struct{})}
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

	w, err := watcher.New(watcher.Config{
		Poller:   clients.NewSubjectChangePoller(iamConn),
		Flush:    func() {},
		Interval: 20 * time.Millisecond,
		Closer:   projection,
		// Заведомо больше пробы: предмет здесь — приехавший отзыв, а не
		// исчерпание срока неподтверждённого чтения.
		StaleAfter: 10 * time.Minute,
		Logger:     quiet,
	})
	if err != nil {
		t.Fatalf("сборка наблюдателя: %v", err)
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
	select {
	case <-owner.started:
	case <-time.After(10 * time.Second):
		t.Fatal("поток не открылся — предъявлять нечего")
	}

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
		t.Fatal("поток пережил отзыв, приехавший перепросом — реплика, до которой не дозвонился " +
			"толчок iam, отзыва не исполняет вовсе")
	}
}

// TestPolledRowWithoutSubjectTypeNamesNobody — строка БЕЗ типа субъекта.
//
// Такие строки записаны до того, как производители начали проставлять тип.
// Собрать из них субъекта нельзя, и выводить тип из написания идентификатора
// запрещено: этот приём уже давал совпадение с тем, чего продукт не производит.
// Строка обязана двигать курсор и НЕ закрывать никого — в том числе не закрывать
// «на всякий случай» всех.
func TestPolledRowWithoutSubjectTypeNamesNobody(t *testing.T) {
	iamStub := &iamJournalStub{batches: [][]*iamv1.SubjectChange{
		{{Id: 1, SubjectId: "usr00000000000000009", SubjectType: "user"}},
		{{Id: 2, SubjectId: "usr00000000000000001", Op: "binding_revoke"}},
	}}

	iamConn := dial(t, func(s *grpc.Server) {
		iamv1.RegisterInternalIAMServiceServer(s, iamStub)
	})
	poller := clients.NewSubjectChangePoller(iamConn)

	if _, _, err := poller.PollSubjectChanges(context.Background(), 0, 256); err != nil {
		t.Fatalf("первый перепрос: %v", err)
	}
	changes, _, err := poller.PollSubjectChanges(context.Background(), 1, 256)
	if err != nil {
		t.Fatalf("второй перепрос: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("строк %d, ожидалась 1", len(changes))
	}
	if changes[0].ID != 2 {
		t.Errorf("номер строки %d — курсор обязан двигаться и по неименованной строке", changes[0].ID)
	}
	if changes[0].Subject != "" {
		t.Errorf("субъект собран как %q из строки без типа — тип выведен из написания идентификатора",
			changes[0].Subject)
	}
}

// TestPolledSubjectIsNamedTheWayTheRegistryKeysIt — форма имени, названная
// ДОСЛОВНО, а не вызовом того же кодека.
//
// Ожидание записано литералом намеренно: сверка с `listnarrow.Subject` была бы
// тождеством и осталась бы зелёной, если бы кодек сменил форму, — то есть
// утверждала бы о себе, а не о том, что реестр такой ключ содержит.
func TestPolledSubjectIsNamedTheWayTheRegistryKeysIt(t *testing.T) {
	iamStub := &iamJournalStub{batches: [][]*iamv1.SubjectChange{
		{{Id: 1, SubjectId: "usr00000000000000009", SubjectType: "user"}},
		{
			{Id: 2, SubjectId: "usr00000000000000001", SubjectType: "user"},
			{Id: 3, SubjectId: "sva00000000000000001", SubjectType: "service_account"},
			{Id: 4, SubjectId: "grp00000000000000001", SubjectType: "group"},
		},
	}}
	iamConn := dial(t, func(s *grpc.Server) {
		iamv1.RegisterInternalIAMServiceServer(s, iamStub)
	})
	poller := clients.NewSubjectChangePoller(iamConn)
	if _, _, err := poller.PollSubjectChanges(context.Background(), 0, 256); err != nil {
		t.Fatalf("первый перепрос: %v", err)
	}
	changes, _, err := poller.PollSubjectChanges(context.Background(), 1, 256)
	if err != nil {
		t.Fatalf("второй перепрос: %v", err)
	}

	want := []string{
		"user:usr00000000000000001",
		"service_account:sva00000000000000001",
		// Группа субъектом ПОТОКА не бывает: поток учитывается под тем, кто
		// предъявил удостоверение, а удостоверение предъявляет человек либо
		// служебная учётка. Смена состава группы приезжает отдельной строкой,
		// называющей УЧАСТНИКА.
		"",
	}
	if len(changes) != len(want) {
		t.Fatalf("строк %d, ожидалось %d", len(changes), len(want))
	}
	for i, w := range want {
		if changes[i].Subject != w {
			t.Errorf("строка %d названа %q, ожидалось %q", i, changes[i].Subject, w)
		}
	}
}
