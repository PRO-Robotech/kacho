// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// stream_credential_recheck_wiring_test.go — ПРОДОВАЯ точка сборки перепроса
// состояния удостоверения (kacho#1410) и величины, которыми она распоряжается.
//
// # Почему проба здесь, а не только у механизма
//
// Инъекция «передать ноль в точке сборки» оставляет ВЕСЬ корпус проб края
// зелёным и код собирающимся: сквозные пробы зовут конструктор напрямую и
// продовую точку сборки минуют. То есть несделанная провязка была бы
// НЕОТЛИЧИМА от сделанной — ровно тот класс, ради которого задача заведена.

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kaname/cloud/iam/v1"
	subscriptionv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/subscription"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
	"github.com/PRO-Robotech/kacho/gateway/internal/subscriptionstream"
)

// revokingAuthority — авторитет, объявляющий отозванным ВСЁ. Годится ровно для
// одного вопроса: доехало ли решение перепроса до реестра, который ему передала
// продовая точка сборки.
type revokingAuthority struct {
	iamv1.UnimplementedInternalSessionRevocationsServiceServer
}

func (revokingAuthority) IsRevoked(
	_ context.Context, _ *iamv1.IsRevokedRequest,
) (*iamv1.IsRevokedResponse, error) {
	return &iamv1.IsRevokedResponse{Revoked: true}, nil
}

// holdingOwner — владелец журнала: открывает поток и держит его.
type holdingOwner struct {
	subscriptionv1.UnimplementedInternalSubscriptionServiceServer

	mu sync.Mutex
	// served — сколько потоков дублёр обслужил. Отвечает на вопрос, на который
	// сигнал не отвечает: «дошло до дублёра» и «дошло до ждущего» — разные
	// факты, и [holdingOwner.awaitStreams] называет оба, когда истекает срок.
	served int

	// started получает по одному значению НА КАЖДЫЙ обслуженный поток.
	//
	// Прежняя редакция ЗАКРЫВАЛА канал БЕЗУСЛОВНО, и это давало сразу два
	// дефекта, из которых второй прятал первый (#1513).
	//
	// Первый: второе открытие роняло ПРОЦЕСС паникой двойного закрытия — то
	// есть отказ приходил не упавшим утверждением, а гибелью пакета, и читался
	// бы как дефект продукта, а не дублёра (#1482).
	//
	// Второй: закрытый канал отдаёт ВСЕМ и СРАЗУ, поэтому ожидание потока было
	// истинно после первого. Именно оно и прятало панику: проба возвращалась из
	// ожидания раньше, чем второй поток доходил до дублёра, и завершалась
	// зелёной ДО того, как он успевал упасть. Условие, истинное независимо от
	// предмета, условием не является.
	//
	// Форма повторена, а не изобретена: тот же приём и по тем же причинам снят
	// у двух стендов `gateway/internal/subscriptionstream` (#1485, #1482) и у
	// стенда `gateway/internal/streamrevocation` (#1513).
	//
	// Ждут через [holdingOwner.awaitStreams]; глубина буфера — [startedDepth].
	started chan struct{}
}

// startedDepth — глубина канала [holdingOwner.started].
//
// Отправка сигнала НЕБЛОКИРУЮЩАЯ: поток, которого проба не ждёт, обязан
// обслуживаться как обычно, а не замирать до чьего-то чтения — иначе дублёр
// стал бы снисходительнее продукта в одну сторону и строже в другую. Значит
// буфер обязан вмещать все потоки, которые дублёр обслужит за пробу;
// переполнение теряет сигнал, и [holdingOwner.awaitStreams] называет это
// числом, а не молчит. Восемь взято с запасом: самая многопоточная проба
// пакета держит два.
const startedDepth = 8

// servedStreams — сколько потоков дублёр обслужил на данный момент.
func (o *holdingOwner) servedStreams() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.served
}

// awaitStreams ждёт, пока дублёр не примет n потоков.
//
// Ждётся УСЛОВИЕ — приход n-го потока, — а не срок. Срок здесь страховочный, и
// его истечение — падение С ЧИСЛАМИ: «ноль сигналов» обязано быть отличимо от
// «ноль потоков», иначе разбор упавшей пробы начинается с догадки.
func (o *holdingOwner) awaitStreams(t *testing.T, n int) {
	t.Helper()
	for got := 0; got < n; got++ {
		select {
		case <-o.started:
		case <-time.After(10 * time.Second):
			t.Fatalf("дублёр принял потоков %d из ожидавшихся %d (обслужено всего %d) — "+
				"поток не открылся; если обслужено не меньше ожидаемого, мал буфер канала started",
				got, n, o.servedStreams())
		}
	}
}

func (o *holdingOwner) Subscribe(
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

func bufconnDial(t *testing.T, register func(*grpc.Server)) *grpc.ClientConn {
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

func recheckProbeConfig() config.Config {
	return config.Config{
		IntrospectionCacheTTLSeconds: 5,
		SubscriptionStreamBudget:     90 * time.Second,
	}
}

// TestCompositionRootWiresTheStreamRegistryIntoTheCredentialRecheck — несущее
// утверждение: перепрос, собранный ПРОДОВОЙ функцией, закрывает поток ИМЕННО
// той проекции, которую ему передали.
//
// Подделкой реестра здесь пользоваться нельзя: предмет — что край передаёт
// перепросу СВОЙ реестр открытых потоков, а не что-нибудь непустое.
func TestCompositionRootWiresTheStreamRegistryIntoTheCredentialRecheck(t *testing.T) {
	owner, projection := newHeldStreamStand(t)
	iamConn := bufconnDial(t, func(s *grpc.Server) {
		iamv1.RegisterInternalSessionRevocationsServiceServer(s, revokingAuthority{})
	})

	sweeper, err := buildStreamRevocationSweeper(recheckProbeConfig(), iamConn, projection, quietLog())
	if err != nil {
		t.Fatalf("сборка перепроса: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, subscriptionstream.Path+"?owner=probe", nil)
	r.Header.Set(principalmeta.HeaderPrincipalType, "user")
	r.Header.Set(principalmeta.HeaderPrincipalID, "usr00000000000000007")
	r.Header.Set(principalmeta.HeaderTokenJti, "jti-wiring")
	done := make(chan struct{})
	go func() {
		defer close(done)
		projection.ServeHTTP(httptest.NewRecorder(), r)
	}()
	owner.awaitStreams(t, 1)

	sweeper.Sweep(context.Background())
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("продовая точка сборки не связала перепрос с реестром открытых потоков — " +
			"отзыв удостоверения не доезжал бы до длинных соединений вовсе")
	}
}

// TestDeclaredRevocationWindowFollowsTheRequestPathAndIsNotTheStreamLifetime —
// ПРЕДИКАТ СНЯТИЯ задачи: окно названо числом и не равно сроку жизни соединения.
//
// Читается ОБЪЯВЛЕННАЯ посадка (`config.Load` на умолчаниях), а не выписанные
// здесь величины: проба о числах, которые поедут в бою, а не о тех, что удобно
// набрать в пробе.
func TestDeclaredRevocationWindowFollowsTheRequestPathAndIsNotTheStreamLifetime(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("объявленная посадка не читается: %v", err)
	}
	window := credentialRecheckInterval(cfg)
	declared := time.Duration(cfg.IntrospectionCacheTTLSeconds) * time.Second

	if window != declared {
		t.Fatalf("окно отзыва для открытого соединения %v, а объявленная граница на пути запроса %v — "+
			"у одного механизма две величины, и вторая разошлась бы с первой молча", window, declared)
	}
	if window <= 0 {
		t.Fatalf("окно отзыва %v — не окно", window)
	}
	if window == cfg.SubscriptionStreamBudget {
		t.Fatalf("окно отзыва совпало со сроком жизни соединения (%v) — это и есть предмет задачи: "+
			"для потока границей становится его бюджет, а не объявленный отзыв", window)
	}
	if window >= cfg.SubscriptionStreamBudget {
		t.Fatalf("окно отзыва %v не меньше срока жизни соединения %v — перепрос не состоялся бы "+
			"на потоке ни разу", window, cfg.SubscriptionStreamBudget)
	}
	t.Logf("перепись поставки: окно отзыва %v · срок жизни соединения %v · отношение %d",
		window, cfg.SubscriptionStreamBudget, int64(cfg.SubscriptionStreamBudget/window))
}

// TestCredentialRecheckStaleBudgetFollowsTheWindow — срок неподтверждённого
// перепроса ВЫВОДИТСЯ из окна, а не объявляется отдельно.
func TestCredentialRecheckStaleBudgetFollowsTheWindow(t *testing.T) {
	iamConn := bufconnDial(t, func(s *grpc.Server) {
		iamv1.RegisterInternalSessionRevocationsServiceServer(s, revokingAuthority{})
	})
	sweeper, err := buildStreamRevocationSweeper(recheckProbeConfig(), iamConn, probeProjection(t), quietLog())
	if err != nil {
		t.Fatalf("сборка перепроса: %v", err)
	}
	if got, want := sweeper.Interval(), 5*time.Second; got != want {
		t.Fatalf("окно перепроса %v, объявленная граница %v", got, want)
	}
	if got, want := sweeper.StaleAfter(), revocationStaleAfter(5*time.Second); got != want {
		t.Fatalf("срок неподтверждённого перепроса %v — точка сборки объявила не ту величину "+
			"(ожидалось %v)", got, want)
	}
}

// TestCredentialRecheckIsRefusedAtStartupWhenItCannotWork — страж старта.
//
// Каждый случай здесь — состояние, при котором контроль ПРИСУТСТВУЕТ и не
// отказывает ни разу за всю свою жизнь. Такое обнаруживают отказом старта, а не
// первым пережившим отзыв потоком в бою.
func TestCredentialRecheckIsRefusedAtStartupWhenItCannotWork(t *testing.T) {
	iamConn := bufconnDial(t, func(s *grpc.Server) {
		iamv1.RegisterInternalSessionRevocationsServiceServer(s, revokingAuthority{})
	})
	cases := []struct {
		name string
		cfg  config.Config
		conn *grpc.ClientConn
		proj *subscriptionstream.Handler
	}{
		{
			name: "проекция не собрана — закрывать нечего",
			cfg:  recheckProbeConfig(), conn: iamConn, proj: nil,
		},
		{
			name: "адреса авторитета нет — спросить не у кого",
			cfg:  recheckProbeConfig(), conn: nil, proj: probeProjection(t),
		},
		{
			name: "срок кэша интроспекции неположителен — окна нет вовсе",
			cfg: config.Config{
				IntrospectionCacheTTLSeconds: 0,
				SubscriptionStreamBudget:     90 * time.Second,
			},
			conn: iamConn, proj: probeProjection(t),
		},
		{
			name: "окно шире срока жизни потока — перепрос не состоится ни разу",
			cfg: config.Config{
				IntrospectionCacheTTLSeconds: 120,
				SubscriptionStreamBudget:     90 * time.Second,
			},
			conn: iamConn, proj: probeProjection(t),
		},
		{
			name: "срок неподтверждённого перепроса не меньше срока жизни потока — fail-closed не наступит",
			cfg: config.Config{
				IntrospectionCacheTTLSeconds: 5,
				SubscriptionStreamBudget:     20 * time.Second,
			},
			conn: iamConn, proj: probeProjection(t),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := buildStreamRevocationSweeper(c.cfg, c.conn, c.proj, quietLog()); err == nil {
				t.Fatal("точка сборки приняла посадку, при которой перепрос не отказал бы НИ РАЗУ")
			}
		})
	}
}

// TestShippedPostureAssemblesTheCredentialRecheck — ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ
// стража: объявленная посадка обязана собираться.
//
// Без него все отрицания выше зеленели бы на строителе, отвергающем ВСЁ.
func TestShippedPostureAssemblesTheCredentialRecheck(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("объявленная посадка не читается: %v", err)
	}
	iamConn := bufconnDial(t, func(s *grpc.Server) {
		iamv1.RegisterInternalSessionRevocationsServiceServer(s, revokingAuthority{})
	})
	if _, err := buildStreamRevocationSweeper(cfg, iamConn, probeProjection(t), quietLog()); err != nil {
		t.Fatalf("объявленная посадка не собирает перепрос: %v — тогда край не поднялся бы вовсе", err)
	}
}

// TestRecheckWindowHonoursEveryLaneItSpeaksFor — окно перепроса не шире
// объявленного окна ЛЮБОЙ полосы, за которую он отвечает (kacho#1450).
//
// # Предмет
//
// Перепрос один, а полос у него три, и у каждой ОБЪЯВЛЕНА своя граница отзыва.
// Для полосы подписанного удостоверения границей служит срок кэша интроспекции —
// из него окно и выводится. Полоса базового секрета объявляет свою границу
// константой (`middleware.BasicCredentialVerdictWindow`), и это не пожелание:
// «отозванное отвергается не позже N» — то, что продукт обещает.
//
// Разведи величины — и обещание полосы базового секрета тихо перестало бы
// действовать на открытых соединениях, оставаясь верным на пути запроса. Один
// механизм, две величины, и расхождение никто бы не решал.
//
// # Почему ОТКАЗ СБОРКИ, а не минимум из двух
//
// Минимум прошёл бы молча и сделал бы вторую величину невидимой: посадка,
// которой оператор объявил широкий срок кэша, работала бы не так, как объявлена,
// и узнать об этом было бы неоткуда. Отказ называет ОБЕ величины и требует
// решения.
func TestRecheckWindowHonoursEveryLaneItSpeaksFor(t *testing.T) {
	iamConn := bufconnDial(t, func(s *grpc.Server) {
		iamv1.RegisterInternalSessionRevocationsServiceServer(s, revokingAuthority{})
	})

	// Окно ШИРЕ объявленного полосой базового секрета; прочие стражи пройдены
	// заведомо — иначе отказ пришёл бы от соседа, и проба утверждала бы о нём.
	wide := config.Config{
		IntrospectionCacheTTLSeconds: int(middleware.BasicCredentialVerdictWindow/time.Second) + 5,
		SubscriptionStreamBudget:     10 * time.Minute,
	}
	if _, err := buildStreamRevocationSweeper(wide, iamConn, probeProjection(t), quietLog()); err == nil {
		t.Fatal("точка сборки приняла окно шире границы, объявленной полосой базового секрета: " +
			"обещание «отозванное отвергается не позже N» перестало бы действовать на открытых " +
			"соединениях, оставаясь верным на пути запроса")
	}

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ ТОЙ ЖЕ ОСИ: окно, равное объявленной границе,
	// проходит. Без него отрицание зеленело бы на строителе, отвергающем всё.
	exact := config.Config{
		IntrospectionCacheTTLSeconds: int(middleware.BasicCredentialVerdictWindow / time.Second),
		SubscriptionStreamBudget:     10 * time.Minute,
	}
	if _, err := buildStreamRevocationSweeper(exact, iamConn, probeProjection(t), quietLog()); err != nil {
		t.Fatalf("окно, РАВНОЕ объявленной границе, отвергнуто: %v — тогда объявленная "+
			"посадка не собралась бы вовсе", err)
	}
}

// newHeldStreamStand — дублёр владельца и проекция над ним. Величины те же, что
// у несущей пробы файла: предмет — поведение дублёра, и различие в посадке
// сделало бы пробы несравнимыми.
func newHeldStreamStand(t *testing.T) (*holdingOwner, *subscriptionstream.Handler) {
	t.Helper()
	owner := &holdingOwner{started: make(chan struct{}, startedDepth)}
	ownerConn := bufconnDial(t, func(s *grpc.Server) {
		subscriptionv1.RegisterInternalSubscriptionServiceServer(s, owner)
	})
	projection, err := subscriptionstream.NewHandler(subscriptionstream.Config{
		Owners: subscriptionstream.Owners{
			"probe": subscriptionv1.NewInternalSubscriptionServiceClient(ownerConn),
		},
		StreamBudget: 90 * time.Second, Heartbeat: 20 * time.Second,
		MaxStreams: 64, MaxStreamsPerSubject: 8, Logger: quietLog(),
	})
	if err != nil {
		t.Fatalf("сборка проекции: %v", err)
	}
	return owner, projection
}

// openHeldStream открывает поток названного предъявителя и ждёт, пока дублёр его
// примет. Возвращает канал, закрывающийся вместе с потоком.
func openHeldStream(
	t *testing.T, owner *holdingOwner, projection *subscriptionstream.Handler, id, jti string,
) <-chan struct{} {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, subscriptionstream.Path+"?owner=probe", nil)
	r.Header.Set(principalmeta.HeaderPrincipalType, "user")
	r.Header.Set(principalmeta.HeaderPrincipalID, id)
	r.Header.Set(principalmeta.HeaderTokenJti, jti)
	done := make(chan struct{})
	go func() {
		defer close(done)
		projection.ServeHTTP(httptest.NewRecorder(), r)
	}()
	owner.awaitStreams(t, 1)
	return done
}

// TestWiringOwnerServesASecondStream — ПРЕДИКАТ СНЯТИЯ задачи #1513: дублёр
// владельца обслуживает ВТОРОЕ открытие так же, как первое.
//
// Прежняя редакция роняла на нём ПРОЦЕСС паникой двойного закрытия, и увидеть
// это удавалось не всегда: ожидание было истинно после первого потока, поэтому
// проба успевала завершиться зелёной раньше, чем второй поток доходил до
// дублёра и падал. Отсюда утверждение о ЧИСЛЕ обслуженных, а не только об
// отсутствии паники: «дошло до дублёра» и «дошло до ждущего» — разные факты.
func TestWiringOwnerServesASecondStream(t *testing.T) {
	owner, projection := newHeldStreamStand(t)

	first := openHeldStream(t, owner, projection, "usr00000000000000001", "jti-first")
	second := openHeldStream(t, owner, projection, "usr00000000000000002", "jti-second")

	if got := owner.servedStreams(); got != 2 {
		t.Fatalf("дублёр обслужил потоков %d, ожидалось 2 — возврат из ожидания "+
			"не означает прихода потока", got)
	}
	for name, done := range map[string]<-chan struct{}{"первый": first, "второй": second} {
		select {
		case <-done:
			t.Fatalf("%s поток закрылся сам — дублёр обязан держать его открытым", name)
		default:
		}
	}
}

// TestWiringStreamArrivalIsSignalledPerStreamNotBroadcast — тот же предмет с
// другой стороны и БЕЗ зависимости от планировщика: открыт РОВНО один поток,
// значит второго прибытия не случилось, и ожидание второго обязано не
// выполняться. Закрытие канала делает его выполнимым тождественно.
func TestWiringStreamArrivalIsSignalledPerStreamNotBroadcast(t *testing.T) {
	owner, projection := newHeldStreamStand(t)

	_ = openHeldStream(t, owner, projection, "usr00000000000000001", "jti-only")

	select {
	case <-owner.started:
		t.Fatalf("ожидание прихода потока выполнилось, когда открыт РОВНО один "+
			"и он уже дождан (обслужено всего %d) — сигнал подан вещанием, "+
			"поэтому ожидание n потоков истинно после первого; "+
			"условие, истинное независимо от предмета, условием не является",
			owner.servedStreams())
	default:
	}
}
