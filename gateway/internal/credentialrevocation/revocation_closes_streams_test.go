// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package credentialrevocation_test

// revocation_closes_streams_test.go — СКВОЗНАЯ проба отзыва УДОСТОВЕРЕНИЯ,
// доезжающего до ОТКРЫТОГО соединения (kacho#1410).
//
// # Что здесь другое по сравнению с kacho#1022
//
// Та задача закрыла отзыв ПРАВ: привязку сняли — поток субъекта закрылся. Здесь
// предмет другой — отзыв УДОСТОВЕРЕНИЯ: выход человека, административное
// принудительное завершение, отзыв токена. Строк в журнал смены субъекта он не
// пишет, поэтому перепрос прав его не видит ВООБЩЕ: до этой пробы поток жил до
// своего бюджета, и граница отзыва для него равнялась сроку жизни соединения.
//
// # Почему сквозная, а не две по половине
//
// Половины врут порознь особенно убедительно. «Край исправно спрашивает про
// отзыв» зелено на вопросе, заданном ПРО ЗАПРОС. «Владелец исправно пишет
// отзыв» зелено на записи, которую никто не читает. Обе исправны, а вместе не
// работают — это класс «разрыв, невидимый ни с одной стороны по отдельности», и
// он в этой линии уже срабатывал.
//
// Поэтому здесь настоящий gRPC авторитета отзыва, настоящий адаптер края,
// настоящий сметатель, настоящая проекция и настоящий открытый поток SSE.

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"

	"github.com/PRO-Robotech/kacho/gateway/internal/clients"
	"github.com/PRO-Robotech/kacho/gateway/internal/credentialrevocation"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
)

// authorityStub — внутренний глагол авторитета отзыва.
//
// Отвечает ЗАГОТОВЛЕННЫМ состоянием и считает вопросы: по числу вопросов проба
// отличает «сметатель спросил и получил „жив“» от «сметатель не спрашивал».
type authorityStub struct {
	iamv1.UnimplementedInternalSessionRevocationsServiceServer

	mu sync.Mutex
	// revoked — идентификаторы удостоверений, объявленных отозванными.
	revoked map[string]bool
	// cutoff — отсечка субъекта; ключ — идентификатор человека.
	cutoff map[string]time.Time
	// unsupported — отвечать «метода нет» (окно раската).
	unsupported bool
	// unanswered — отвечать «не отвечаю» на оба вопроса.
	unanswered bool

	asked   int
	askedCh chan struct{}
}

func newAuthorityStub() *authorityStub {
	return &authorityStub{
		revoked: map[string]bool{},
		cutoff:  map[string]time.Time{},
		askedCh: make(chan struct{}, 64),
	}
}

func (a *authorityStub) note() {
	a.asked++
	select {
	case a.askedCh <- struct{}{}:
	default:
	}
}

func (a *authorityStub) IsRevoked(
	_ context.Context, in *iamv1.IsRevokedRequest,
) (*iamv1.IsRevokedResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.note()
	if a.unanswered {
		return nil, status.Error(codes.Unavailable, "authority is down")
	}
	return &iamv1.IsRevokedResponse{Revoked: a.revoked[in.GetTokenJti()]}, nil
}

func (a *authorityStub) SessionCutoffOf(
	_ context.Context, in *iamv1.SessionCutoffOfRequest,
) (*iamv1.SessionCutoffOfResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.note()
	if a.unsupported {
		return nil, status.Error(codes.Unimplemented, "not served yet")
	}
	if a.unanswered {
		return nil, status.Error(codes.Unavailable, "authority is down")
	}
	at, ok := a.cutoff[in.GetUserId()]
	if !ok {
		return &iamv1.SessionCutoffOfResponse{Found: false}, nil
	}
	return &iamv1.SessionCutoffOfResponse{Found: true, RevokeBefore: timestamppb.New(at)}, nil
}

func (a *authorityStub) revoke(jti string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.revoked[jti] = true
}

func (a *authorityStub) setCutoff(userID string, at time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cutoff[userID] = at
}

func (a *authorityStub) goSilent() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.unanswered = true
}

func (a *authorityStub) declareUnsupported() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.unsupported = true
}

// journalOwnerStub — владелец журнала подписки: открывает поток и держит его.
type journalOwnerStub struct {
	subscriptionv1.UnimplementedInternalSubscriptionServiceServer
	mu      sync.Mutex
	started int
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
	o.started++
	o.mu.Unlock()
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

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&strings.Builder{}, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

// stand — проекция с настоящим владельцем и сметатель с настоящим адаптером.
type stand struct {
	projection *subscriptionstream.Handler
	sweeper    *credentialrevocation.Sweeper
	authority  *authorityStub
	owner      *journalOwnerStub
}

func newStand(t *testing.T, tune ...func(*credentialrevocation.Config)) *stand {
	t.Helper()
	owner := &journalOwnerStub{}
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
		MaxStreams:           8,
		MaxStreamsPerSubject: 8,
		Logger:               quietLogger(),
	})
	if err != nil {
		t.Fatalf("сборка проекции: %v", err)
	}

	authority := newAuthorityStub()
	authorityConn := dial(t, func(s *grpc.Server) {
		iamv1.RegisterInternalSessionRevocationsServiceServer(s, authority)
	})

	sweepCfg := credentialrevocation.Config{
		// НАСТОЯЩИЙ адаптер края: перевод кодов транспорта в исходы полосы —
		// его работа, и проба, подменившая его своим, утверждала бы о переводе,
		// которого в бою нет.
		Reader:   clients.NewSessionRevocationsAdapter(authorityConn),
		Streams:  projection,
		Interval: 20 * time.Millisecond,
		// Заведомо больше пробы: предмет здесь — приехавший отзыв, а не
		// исчерпание срока неподтверждённого чтения.
		StaleAfter: 10 * time.Minute,
		Logger:     quietLogger(),
	}
	for _, f := range tune {
		f(&sweepCfg)
	}
	sweeper, err := credentialrevocation.New(sweepCfg)
	if err != nil {
		t.Fatalf("сборка сметателя: %v", err)
	}
	return &stand{projection: projection, sweeper: sweeper, authority: authority, owner: owner}
}

// openStream открывает поток названного вызывающего с названным удостоверением
// и возвращает канал, закрывающийся вместе с потоком.
//
// jti пустой означает браузерную сессию: у неё удостоверения нет вовсе, и
// заголовок его идентификатора полоса аутентификации не выставляет.
func (s *stand) openStream(
	t *testing.T, principalType, principalID, jti string, authAt time.Time,
) <-chan struct{} {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, subscriptionstream.Path+"?owner=probe", nil)
	r.Header.Set(principalmeta.HeaderPrincipalType, principalType)
	r.Header.Set(principalmeta.HeaderPrincipalID, principalID)
	if jti != "" {
		r.Header.Set(principalmeta.HeaderTokenJti, jti)
	}
	if !authAt.IsZero() {
		r.Header.Set(principalmeta.HeaderTokenMfaAt, formatUnix(authAt))
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.projection.ServeHTTP(httptest.NewRecorder(), r)
	}()
	return done
}

func formatUnix(t time.Time) string {
	return strconv.FormatInt(t.UTC().Truncate(time.Second).Unix(), 10)
}

// waitOpen ждёт, пока проекция НАСЧИТАЕТ ровно столько открытых потоков.
func waitOpen(t *testing.T, h *subscriptionstream.Handler, want int64) {
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

// TestRevokedCredentialClosesTheOpenStreamEndToEnd — ПРЕДИКАТ ЗАДАЧИ.
//
// Поток открыт под подписанным удостоверением; авторитет объявляет это
// удостоверение отозванным; поток обязан закрыться — и закрыться от ОТЗЫВА, а
// не от истечения своего бюджета (бюджет здесь заведомо больше пробы).
func TestRevokedCredentialClosesTheOpenStreamEndToEnd(t *testing.T) {
	s := newStand(t)
	const jti = "jti-revoked"

	done := s.openStream(t, "user", "usr00000000000000001", jti, time.Time{})
	waitOpen(t, s.projection, 1)

	// Первый обход: удостоверение живо — поток обязан выжить. Без этой половины
	// утверждение ниже зеленело бы на сметателе, закрывающем всех подряд.
	s.sweeper.Sweep(context.Background())
	select {
	case <-done:
		t.Fatal("поток закрыт на живом удостоверении — сметатель обязан закрывать отозванных, а не всех")
	case <-time.After(100 * time.Millisecond):
	}

	s.authority.revoke(jti)
	s.sweeper.Sweep(context.Background())

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("поток пережил отзыв удостоверения — контроль стоит на предъявлении запроса " +
			"и не стоит на предъявлении открытого соединения, то есть отзыв не исполняется вовсе")
	}
}

// TestEndedBrowserSessionClosesTheOpenStreamEndToEnd — вторая полоса того же
// предмета: у браузерной сессии удостоверения НЕТ, и спросить про неё можно
// только по паре «человек + момент аутентификации».
//
// Полоса не выводится из первой и первой не покрывается: консоль открывает поток
// на вкладку именно ею, поэтому оставить её незакрытой значило бы закрыть отзыв
// у всех, кроме основного потребителя подписки.
func TestEndedBrowserSessionClosesTheOpenStreamEndToEnd(t *testing.T) {
	s := newStand(t)
	const user = "usr00000000000000002"
	authAt := time.Now().Add(-time.Hour)

	done := s.openStream(t, "user", user, "", authAt)
	waitOpen(t, s.projection, 1)

	s.sweeper.Sweep(context.Background())
	select {
	case <-done:
		t.Fatal("поток закрыт при отсутствующей отсечке — человек, которого никто не отзывал, работает")
	case <-time.After(100 * time.Millisecond):
	}

	s.authority.setCutoff(user, authAt.Add(time.Minute))
	s.sweeper.Sweep(context.Background())

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("поток пережил принудительное завершение браузерной сессии")
	}
}

// TestSessionReauthenticatedAfterTheCutoffKeepsItsStream — РАДИУС отсечки во
// времени.
//
// Отсечка действует ВПЕРЁД: человек, вошедший заново, работает. Без этой пробы
// предыдущая зеленела бы на устройстве, закрывающем всякий поток человека, у
// которого отсечка вообще есть, — то есть отзыв блокировал бы принципала
// навсегда вместо того, чтобы снять выданное.
func TestSessionReauthenticatedAfterTheCutoffKeepsItsStream(t *testing.T) {
	s := newStand(t)
	const user = "usr00000000000000003"
	cutoff := time.Now().Add(-time.Hour)
	s.authority.setCutoff(user, cutoff)

	// Сессия аутентифицировалась ПОСЛЕ отсечки — законна.
	done := s.openStream(t, "user", user, "", cutoff.Add(time.Minute))
	waitOpen(t, s.projection, 1)

	s.sweeper.Sweep(context.Background())
	select {
	case <-done:
		t.Fatal("закрыт поток сессии, аутентифицировавшейся ПОСЛЕ отсечки — " +
			"отзыв обязан снимать выданное, а не блокировать человека навсегда")
	case <-time.After(200 * time.Millisecond):
	}
}

// TestRevokingOneCredentialLeavesTheOtherAlone — РАДИУС закрытия, и обе его
// стороны в одной пробе.
//
// Оба потока принадлежат ОДНОМУ человеку и различаются только удостоверением:
// так выглядит выход из одной вкладки при живой второй. Закрытие «по субъекту»
// — механизм отзыва прав — здесь было бы отказом в обслуживании своими руками, и
// проба на двух разных субъектах этого не различила бы вовсе.
func TestRevokingOneCredentialLeavesTheOtherAlone(t *testing.T) {
	s := newStand(t)
	const user = "usr00000000000000004"

	revoked := s.openStream(t, "user", user, "jti-one", time.Time{})
	waitOpen(t, s.projection, 1)
	survivor := s.openStream(t, "user", user, "jti-two", time.Time{})
	waitOpen(t, s.projection, 2)

	s.authority.revoke("jti-one")
	s.sweeper.Sweep(context.Background())

	select {
	case <-revoked:
	case <-time.After(10 * time.Second):
		t.Fatal("отозванное удостоверение не закрыло свой поток")
	}
	select {
	case <-survivor:
		t.Fatal("отзыв одного удостоверения закрыл поток другого — у того же человека это " +
			"означало бы, что выход из одной вкладки выкидывает его из всех")
	case <-time.After(300 * time.Millisecond):
	}
}

// TestUnansweredAuthorityClosesEveryStreamOnlyAfterTheDeclaredTerm — FAIL-CLOSED
// и его срок.
//
// Неполученный ответ авторитета не есть «никого не отзывали». Обе половины стоят
// вместе: до срока потоки живут (иначе всякая заминка сети закрывала бы флот), за
// сроком закрываются все (иначе поток переживал бы аварию читателя целиком — то
// есть контроль отключался бы тем самым событием, ради которого заведён).
func TestUnansweredAuthorityClosesEveryStreamOnlyAfterTheDeclaredTerm(t *testing.T) {
	now := time.Now()
	// Часы подставляются ПОСАДКОЙ, а не тест-только-швом: срок обязан быть
	// свойством решения, а не занятости машины, — и вызывающий в бою объявляет
	// их тем же полем.
	s := newStand(t, func(c *credentialrevocation.Config) {
		c.Now = func() time.Time { return now }
	})

	done := s.openStream(t, "user", "usr00000000000000005", "jti-live", time.Time{})
	waitOpen(t, s.projection, 1)

	s.authority.goSilent()
	s.sweeper.Sweep(context.Background())
	select {
	case <-done:
		t.Fatal("поток закрыт первой же неудачей опроса — заминка соседа не есть потеря читателя")
	case <-time.After(100 * time.Millisecond):
	}

	now = now.Add(11 * time.Minute) // за объявленным сроком
	s.sweeper.Sweep(context.Background())
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("поток пережил потерю читателя отзыва — неполученный ответ авторитета " +
			"обязан читаться как отказ, а не как «удостоверение живо»")
	}
}

// TestAuthorityWithoutTheQuestionDoesNotCloseStreams — окно раската.
//
// «Метода нет» — НЕ «не ответил»: край поднимается раньше службы прав, и в этом
// окне вопрос не предлагается вовсе. Считать это аварией значило бы закрывать
// каждый поток на всё окно раската; состояние сходится само.
//
// Проход здесь ГРОМКИЙ — он считается отдельно, иначе застрявшее расхождение
// версий тихо оставило бы полосу без проверки.
func TestAuthorityWithoutTheQuestionDoesNotCloseStreams(t *testing.T) {
	s := newStand(t)
	s.authority.declareUnsupported()

	done := s.openStream(t, "user", "usr00000000000000006", "", time.Now().Add(-time.Hour))
	waitOpen(t, s.projection, 1)

	s.sweeper.Sweep(context.Background())
	select {
	case <-done:
		t.Fatal("поток закрыт на авторитете, который такого вопроса не предлагает — " +
			"расхождение версий не есть отзыв")
	case <-time.After(200 * time.Millisecond):
	}
	if st := s.sweeper.Stats(); st.Unsupported == 0 {
		t.Fatal("проход по расхождению версий не сосчитан — застрявшее расхождение " +
			"оставило бы полосу без проверки молча")
	}
}

// TestCredentialWithoutAQuestionIsCountedNotIgnored — ОСТАТОК назван числом.
//
// Поток, открытый удостоверением, про которое авторитету нечего задать (базовый
// секрет: ни идентификатора удостоверения, ни момента аутентификации до потока не
// доезжает), закрыть по отзыву НЕЛЬЗЯ. Молчаливое выбрасывание сделало бы
// «закрывать было нечего» неотличимым от «закрыть было нечем».
func TestCredentialWithoutAQuestionIsCountedNotIgnored(t *testing.T) {
	s := newStand(t)

	done := s.openStream(t, "service_account", "sva00000000000000001", "", time.Time{})
	waitOpen(t, s.projection, 1)
	defer func() { s.projection.CloseAll() }()

	s.sweeper.Sweep(context.Background())
	if st := s.sweeper.Stats(); st.Unaskable == 0 {
		t.Fatal("поток без спрашиваемого удостоверения не сосчитан — остаток обязан быть виден, а не выведен")
	}
	select {
	case <-done:
		t.Fatal("закрыт поток, про который вопроса не задавали — закрывать без ответа нельзя")
	case <-time.After(100 * time.Millisecond):
	}
}
