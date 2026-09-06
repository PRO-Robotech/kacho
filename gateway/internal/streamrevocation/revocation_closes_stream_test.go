// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package streamrevocation_test

// revocation_closes_stream_test.go — СКВОЗНАЯ проба: отзыв УДОСТОВЕРЕНИЯ
// доезжает до ОТКРЫТОГО соединения (kacho#1410).
//
// # Почему сквозная, а не две по половине
//
// Половины здесь врут порознь особенно убедительно. «Авторитет отвечает
// „отозвано“» зелено на вопросе про удостоверение, которого проекция не
// захватывала. «Реестр закрывает поток» зелено на закрытии по субъекту. Обе
// половины исправны, а вместе не работают: спросить авторитет НЕ О ЧЕМ, потому
// что удостоверения открытого потока проекция не помнит, — и отзыв не имел бы
// действия вообще.
//
// Поэтому здесь настоящий gRPC авторитета отзыва, настоящий адаптер над ним,
// настоящая проекция, настоящий владелец журнала и настоящий открытый поток SSE.
//
// # Чем этот предмет отличается от kacho#1022
//
// Тот закрывает поток по отзыву ПРАВ: имена приезжают журналом смены субъекта,
// и проекция уже умеет закрывать по субъекту. Здесь отзывается САМО
// УДОСТОВЕРЕНИЕ (выход человека, принудительный выход администратора). Строк в
// журнал смены субъекта оно не пишет, поэтому тот путь его не переносит.

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

	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"
	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/gateway/internal/clients"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/gateway/internal/streamrevocation"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
)

// quiet — журнал, который ничего не печатает: предмет проб не в нём.
func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(&strings.Builder{}, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

// authorityStub — НАСТОЯЩИЙ внутренний глагол iam, отвечающий заготовленным.
//
// Отзыв вносится ПОСЛЕ того, как поток открыт: иначе «поток закрыт отзывом» и
// «поток не открылся» — одно и то же наблюдение, и утверждение о первом ничего
// бы не значило.
type authorityStub struct {
	iamv1.UnimplementedInternalSessionRevocationsServiceServer

	mu sync.Mutex
	// revokedJTI — идентификаторы отозванных удостоверений.
	revokedJTI map[string]bool
	// cutoff — отсечка субъекта: сессии, аутентифицировавшиеся не позже, мертвы.
	cutoff map[string]time.Time
	// fail — отвечать ошибкой на всякий вопрос (авторитет молчит).
	fail bool
	// unsupported — отвечать «метода нет» на вопрос про отсечку (окно раската).
	unsupported bool

	// askedJTI / askedUser — что у авторитета РЕАЛЬНО спросили. Без этого
	// «поток пережил отзыв» неотличимо от «про поток не спрашивали вовсе».
	askedJTI  []string
	askedUser []string
}

func newAuthorityStub() *authorityStub {
	return &authorityStub{revokedJTI: map[string]bool{}, cutoff: map[string]time.Time{}}
}

func (a *authorityStub) revokeJTI(jti string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.revokedJTI[jti] = true
}

func (a *authorityStub) setCutoff(userID string, at time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cutoff[userID] = at
}

func (a *authorityStub) goSilent() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.fail = true
}

func (a *authorityStub) asked() (jti, user []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.askedJTI...), append([]string(nil), a.askedUser...)
}

func (a *authorityStub) IsRevoked(
	_ context.Context, in *iamv1.IsRevokedRequest,
) (*iamv1.IsRevokedResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.askedJTI = append(a.askedJTI, in.GetTokenJti())
	if a.fail {
		return nil, status.Error(codes.Unavailable, "authority is down")
	}
	return &iamv1.IsRevokedResponse{Revoked: a.revokedJTI[in.GetTokenJti()]}, nil
}

func (a *authorityStub) SessionCutoffOf(
	_ context.Context, in *iamv1.SessionCutoffOfRequest,
) (*iamv1.SessionCutoffOfResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.askedUser = append(a.askedUser, in.GetUserId())
	if a.unsupported {
		return nil, status.Error(codes.Unimplemented, "method SessionCutoffOf not implemented")
	}
	if a.fail {
		return nil, status.Error(codes.Unavailable, "authority is down")
	}
	at, ok := a.cutoff[in.GetUserId()]
	if !ok {
		return &iamv1.SessionCutoffOfResponse{Found: false}, nil
	}
	return &iamv1.SessionCutoffOfResponse{Found: true, RevokeBefore: timestamppb.New(at)}, nil
}

// basicStub — НАСТОЯЩИЙ внутренний глагол iam о живости базового удостоверения
// (kacho#1450), отвечающий заготовленным.
//
// Отдельным типом, а не вторым лицом соседнего: службы разные, и стенд обязан
// уметь погасить одну, не трогая другую, — иначе «полоса без отзыва» и
// «авторитет лежит целиком» стали бы одним наблюдением.
type basicStub struct {
	iamv1.UnimplementedInternalIAMServiceServer

	mu sync.Mutex
	// dead — идентификаторы удостоверений, которые авторитет объявляет неживыми.
	// Умолчание — ЖИВО: неизвестное удостоверение здесь не предмет.
	dead map[string]bool
	// fail / unsupported — авторитет молчит / вопроса не предлагает (окно раската).
	fail        bool
	unsupported bool
	// asked — про что РЕАЛЬНО спросили. Без этого «поток пережил отзыв»
	// неотличимо от «про поток не спрашивали вовсе».
	asked []string
}

func newBasicStub() *basicStub { return &basicStub{dead: map[string]bool{}} }

func (b *basicStub) setDead(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dead[id] = true
}

func (b *basicStub) goSilent() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fail = true
}

func (b *basicStub) goUnsupported() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.unsupported = true
}

func (b *basicStub) askedIDs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.asked...)
}

func (b *basicStub) CheckBasicCredentialLive(
	_ context.Context, in *iamv1.CheckBasicCredentialLiveRequest,
) (*iamv1.CheckBasicCredentialLiveResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.asked = append(b.asked, in.GetCredentialId())
	if b.unsupported {
		return nil, status.Error(codes.Unimplemented, "method CheckBasicCredentialLive not implemented")
	}
	if b.fail {
		return nil, status.Error(codes.Unavailable, "authority is down")
	}
	if b.dead[in.GetCredentialId()] {
		return nil, status.Error(codes.Unauthenticated, "credential refused")
	}
	return &iamv1.CheckBasicCredentialLiveResponse{}, nil
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
	// Прежняя редакция ЗАКРЫВАЛА канал под [sync.Once]. Однократность снимала
	// панику второго закрытия, но НЕ снимала главного: закрытый канал отдаёт
	// ВСЕМ и СРАЗУ, поэтому ожидание потока выполнялось тождественно после
	// первого, а [stand.openStream] — помощник, прямо заведённый для повторного
	// вызова, — со второго раза не ждал бы ничего. Условие, истинное независимо
	// от предмета, условием не является (#1513).
	//
	// Форма повторена, а не изобретена: тот же приём и по тем же причинам снят
	// у двух стендов соседнего пакета
	// (`gateway/internal/subscriptionstream/harness_test.go`, #1485 и
	// `.../revocation_poll_sweep_test.go`, #1482).
	//
	// Ждут через [journalOwnerStub.awaitStreams]; глубина буфера — [startedDepth].
	started chan struct{}
}

// startedDepth — глубина канала [journalOwnerStub.started].
//
// Отправка сигнала НЕБЛОКИРУЮЩАЯ: поток, которого проба не ждёт, обязан
// обслуживаться как обычно, а не замирать до чьего-то чтения — иначе стенд стал
// бы снисходительнее продукта в одну сторону и строже в другую. Значит буфер
// обязан вмещать все потоки, которые стенд обслужит за пробу; переполнение
// теряет сигнал, и [journalOwnerStub.awaitStreams] называет это числом, а не
// молчит. Четыре — потолок проекции стенда ([subscriptionstream.Config.MaxStreams]
// в [newStand]): больше потоков она не примет, поэтому больше сигналов не будет.
const startedDepth = 4

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
		select {
		case o.started <- struct{}{}:
		default:
		}
	}
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

// stand — проекция с настоящим владельцем и сметатель над настоящим авторитетом.
type stand struct {
	projection *subscriptionstream.Handler
	sweeper    *streamrevocation.Sweeper
	owner      *journalOwnerStub
	authority  *authorityStub
	basic      *basicStub
}

func newStand(t *testing.T, tune func(*streamrevocation.Config)) *stand {
	t.Helper()

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
		Logger:               quiet(),
	})
	if err != nil {
		t.Fatalf("сборка проекции: %v", err)
	}

	authority := newAuthorityStub()
	basic := newBasicStub()
	// ОБЕ службы — на одном соединении, ровно как в бою: спрашивающий один,
	// сосед один, соединение одно. Разведи их по двум стендам — и проба
	// перестала бы утверждать то единственное, ради чего адаптер держит оба
	// набора глаголов вместе.
	iamConn := dial(t, func(s *grpc.Server) {
		iamv1.RegisterInternalSessionRevocationsServiceServer(s, authority)
		iamv1.RegisterInternalIAMServiceServer(s, basic)
	})

	cfg := streamrevocation.Config{
		Streams: projection,
		// НАСТОЯЩИЙ адаптер: он и переводит «метода нет» в признак окна раската,
		// и он же исполняется в бою. Подделка на его месте проверяла бы наш
		// собственный перевод, а не тот, что стоит на пути.
		Authority: clients.NewSessionRevocationsAdapter(iamConn),
		Interval:  20 * time.Millisecond,
		// Заведомо больше пробы: предмет — приехавший отзыв, а не исчерпание
		// срока неподтверждённого чтения.
		StaleAfter: 10 * time.Minute,
		Logger:     quiet(),
	}
	if tune != nil {
		tune(&cfg)
	}
	sweeper, err := streamrevocation.New(cfg)
	if err != nil {
		t.Fatalf("сборка сметателя: %v", err)
	}
	return &stand{projection: projection, sweeper: sweeper, owner: owner, authority: authority, basic: basic}
}

// openStream открывает поток названного предъявителя и ждёт, пока владелец его
// примет. Возвращает канал, закрывающийся вместе с потоком.
func (s *stand) openStream(t *testing.T, headers map[string]string) <-chan struct{} {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, subscriptionstream.Path+"?owner=probe", nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.projection.ServeHTTP(httptest.NewRecorder(), r)
	}()
	s.owner.awaitStreams(t, 1)
	return done
}

// TestRevokedCredentialClosesTheOpenStreamEndToEnd — НЕСУЩЕЕ утверждение задачи.
//
// Поток открыт удостоверением с идентификатором; авторитет объявляет это
// удостоверение отозванным; поток обязан закрыться. Положительный контроль —
// рядом, в TestLiveCredentialLeavesTheStreamOpen: без него «закрыт» зеленело бы
// на устройстве, закрывающем всё подряд.
func TestRevokedCredentialClosesTheOpenStreamEndToEnd(t *testing.T) {
	s := newStand(t, nil)

	done := s.openStream(t, map[string]string{
		principalmeta.HeaderPrincipalType: "user",
		principalmeta.HeaderPrincipalID:   "usr00000000000000001",
		principalmeta.HeaderTokenJti:      "jti-open-stream",
	})

	runCtx, stop := context.WithCancel(context.Background())
	defer stop()
	go s.sweeper.Run(runCtx)

	// Перепрос состоялся, отзыва ещё нет: поток обязан жить.
	select {
	case <-done:
		t.Fatal("поток закрыт до всякого отзыва — сметатель закрывает не по отзыву")
	case <-time.After(200 * time.Millisecond):
	}

	s.authority.revokeJTI("jti-open-stream")

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		jti, _ := s.authority.asked()
		t.Fatalf("поток пережил отзыв СВОЕГО удостоверения; у авторитета спрошено про %v — "+
			"перепрос на открытых потоках есть ЕДИНСТВЕННЫЙ путь отзыва удостоверения к длинным "+
			"соединениям, и не исполнив его, край не исполняет отзыв вовсе", jti)
	}
}

// itoa — секунды эпохи заголовком момента подтверждения.
func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// TestStreamArrivalIsSignalledPerStreamNotBroadcast — предмет задачи #1513:
// сигнал о приходе потока обязан быть ПЕР-ПОТОЧНЫМ, а не вещанием.
//
// Утверждение выбрано так, чтобы оно было НЕВЫПОЛНИМО при прежней форме и не
// зависело от загрузки машины: открыт РОВНО один поток, значит второго прибытия
// не случилось, и ожидание второго обязано не выполняться. Закрытие канала
// делает его выполнимым тождественно — то есть ожидание перестаёт быть условием
// и начинает быть тавтологией.
//
// Проверять это счётчиком обслуженных нельзя: «второй поток ещё не дошёл» и
// «сигнал отдан всем сразу» дали бы одно наблюдение, и вердикт стал бы функцией
// планировщика.
func TestStreamArrivalIsSignalledPerStreamNotBroadcast(t *testing.T) {
	s := newStand(t, nil)

	_ = s.openStream(t, map[string]string{
		principalmeta.HeaderPrincipalType: "user",
		principalmeta.HeaderPrincipalID:   "usr00000000000000001",
		principalmeta.HeaderTokenJti:      "jti-only-stream",
	})

	select {
	case <-s.owner.started:
		t.Fatalf("ожидание прихода потока выполнилось, когда открыт РОВНО один "+
			"и он уже дождан (обслужено всего %d) — сигнал подан вещанием, "+
			"поэтому ожидание n потоков истинно после первого; "+
			"условие, истинное независимо от предмета, условием не является",
			s.owner.servedStreams())
	default:
	}
}

// TestStandOpensTwoStreamsInSequence — ПРЕДИКАТ СНЯТИЯ задачи #1513 и
// положительный контроль к пробе выше.
//
// Помощник [stand.openStream] заведён для повторного вызова, и до сих пор им
// пользовались единожды. Здесь он зовётся дважды: второй поток обязан быть
// дождан по-настоящему, а не «уже готов». Без этого контроля утверждение об
// отсутствии лишнего сигнала зеленело бы и на стенде, который не сигналит вовсе.
func TestStandOpensTwoStreamsInSequence(t *testing.T) {
	s := newStand(t, nil)

	first := s.openStream(t, map[string]string{
		principalmeta.HeaderPrincipalType: "user",
		principalmeta.HeaderPrincipalID:   "usr00000000000000001",
		principalmeta.HeaderTokenJti:      "jti-first-stream",
	})
	second := s.openStream(t, map[string]string{
		principalmeta.HeaderPrincipalType: "user",
		principalmeta.HeaderPrincipalID:   "usr00000000000000002",
		principalmeta.HeaderTokenJti:      "jti-second-stream",
	})

	if got := s.owner.servedStreams(); got != 2 {
		t.Fatalf("стенд обслужил потоков %d, ожидалось 2 — возврат из ожидания "+
			"не означает прихода потока", got)
	}

	for name, done := range map[string]<-chan struct{}{"первый": first, "второй": second} {
		select {
		case <-done:
			t.Fatalf("%s поток закрылся сам, без отзыва — предъявлять отзыву нечего", name)
		default:
		}
	}
}
