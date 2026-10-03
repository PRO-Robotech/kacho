// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// m08_integration_test.go — NTF1-M08: звено прав kacho видит служебный
// принципал до ветки ScopeFiltered, одинаково на обоих слушателях.
//
// Проба поднимает НАСТОЯЩИЙ корень пробы (assemble + describe) носителем
// (`servicehost.Serve`) на двух живых слушателях с mTLS тестового УЦ. Владелец
// модели прав посеян: право `reader` на `notification_feed:notify-probe` есть
// у `service:notify` и больше ни у кого; он же записывает каждый заданный
// вопрос — звена решения (Check) и сужателя потока (BatchCheck).
//
// # Почему ленту служат ОБА слушателя в этой пробе
//
// В бою публичный слушатель пробы пуст: обе её службы — Internal* (ban #6).
// Но предмет M08 — что извлекатель субъекта отдаёт `service:notify` на ОБОИХ
// слушателях, а цепочку звеньев носитель собирает одну на пару (serverPair).
// Извлекатель спрашивается тем методом, который его зовёт, поэтому проба
// монтирует ПРОД-регистратор внутреннего слушателя (registerInternal) на оба
// и зовёт Claim и Subscribe через каждый.
//
// Близнец — то же звено с пустым перечнем и пустой таблицей (изъятие оси
// идентичности): субъекта нет, оба вызова — PERMISSION_DENIED на обоих
// слушателях, как до Р2, и владелец модели не спрошен о `service:notify` ни
// разу.

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	notifyv1 "github.com/PRO-Robotech/corelib/api/corelib/notify"
	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"
	"github.com/PRO-Robotech/corelib/grpcsrv"
	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/journaltx"
	"github.com/PRO-Robotech/corelib/operations"
	"github.com/PRO-Robotech/corelib/pgtest"
	"github.com/PRO-Robotech/corelib/servicecontract"
	"github.com/PRO-Robotech/corelib/servicehost"

	notify "github.com/PRO-Robotech/kacho/services/notify"
	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/config"
)

// liveProbe — проба на живых слушателях.
type liveProbe struct {
	public, internal string
	pool             *pgxpool.Pool
	parts            feedParts
}

// raise поднимает пробу носителем. identity, если задан, замещает ось звена
// идентичности, собранную корнем (близнец M08).
func raise(t *testing.T, ca *testCA, m *model, identity *servicecontract.Axis[grpcsrv.ServiceIdentity]) liveProbe {
	t.Helper()
	env := standEnv(t, pgtest.NewDB(t), serveModel(t, m), "true", ca)
	setEnv(t, env)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("конфигурация стенда отвергнута: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), cfg.DSN())
	if err != nil {
		t.Fatal(err)
	}
	pgtest.ClosePoolAtEnd(t, pool)

	ctx, cancel := context.WithCancel(context.Background())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p, err := assemble(ctx, cfg, logger, pool, prometheus.NewRegistry())
	if err != nil {
		cancel()
		t.Fatalf("корень не собрал пробу: %v", err)
	}
	if identity != nil {
		p.ports.identity = *identity
	}
	desc, err := describe(cfg, logger, p.ports)
	if err != nil {
		cancel()
		t.Fatalf("дескриптор отвергнут: %v", err)
	}
	both := func(r grpc.ServiceRegistrar) { registerInternal(r, p.ports) }
	var serveErr error
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		serveErr = servicehost.Serve(ctx, desc, both, both)
	}()
	t.Cleanup(func() {
		cancel()
		<-stopped
		if serveErr != nil {
			t.Errorf("носитель вернул ошибку: %v", serveErr)
		}
		p.close()
	})

	lp := liveProbe{
		public:   "127.0.0.1:" + env["KACHO_NOTIFYPROBE_GRPC_PORT"],
		internal: "127.0.0.1:" + env["KACHO_NOTIFYPROBE_INTERNAL_PORT"],
		pool:     pool, parts: p.ports.parts,
	}
	for _, addr := range []string{lp.public, lp.internal} {
		waitListening(t, addr, stopped, func() error { return serveErr })
	}
	return lp
}

// waitListening ждёт, пока слушатель примет соединение. Носитель, вернувшийся
// раньше, — отказ с его текстом; канал завершения не потребляется, чтобы
// уборка пробы дождалась носителя тем же каналом.
func waitListening(t *testing.T, addr string, stopped <-chan struct{}, result func() error) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		select {
		case <-stopped:
			t.Fatalf("носитель завершился до подъёма слушателя %s: %v", addr, result())
		default:
		}
		if c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
			_ = c.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("слушатель %s не поднялся за 30 с", addr)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// seed ставит n писем probe-hello порождённым SendProbeHello в транзакции
// помощника журнала — тем путём, которым ставит их глагол пробы.
func (lp liveProbe) seed(t *testing.T, n int) {
	t.Helper()
	ctx := operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: ids.NewID(ids.PrefixUser)})
	ctx = lp.parts.source.Bind(ctx)
	for i := 0; i < n; i++ {
		tx, err := journaltx.Begin(ctx, lp.pool, journaltx.NewOptions(lp.parts.source.Enabled()))
		if err != nil {
			t.Fatal(err)
		}
		if err := notify.SendProbeHello(ctx, tx, notify.ProbeHelloAttrs{To: "probe@example.com", Target: "/"}); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("постановка probe-hello: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

func dialAs(t *testing.T, ca *testCA, addr, name, san string) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(ca.clientCreds(t, name, san)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func claim(ctx context.Context, conn *grpc.ClientConn) (*notifyv1.ClaimResponse, error) {
	return notifyv1.NewInternalNotificationFeedServiceClient(conn).Claim(ctx, &notifyv1.ClaimRequest{
		Max: 1, Classes: []notifyv1.NotificationClass{notifyv1.NotificationClass_NOTICE},
	})
}

func subscribe(ctx context.Context, conn *grpc.ClientConn) (grpc.ServerStreamingClient[subscriptionv1.SubscriptionMessage], error) {
	return subscriptionv1.NewInternalSubscriptionServiceClient(conn).Subscribe(ctx, &subscriptionv1.SubscriptionRequest{
		Start: &subscriptionv1.SubscriptionRequest_Anchor{Anchor: subscriptionv1.SubscriptionAnchor_BEGINNING},
	})
}

// NTF1-M08 — notify с сертификатом из таблицы: на каждом слушателе Claim
// проходит Check(service:notify, reader, notification_feed:notify-probe) и
// забирает строку, Subscribe открыт и приносит сигнал ленты.
func TestNTF1M08_ServicePrincipalIsSeenOnBothListeners(t *testing.T) {
	ca := newTestCA(t)
	m := &model{allow: map[string]bool{readerOnFeed: true}}
	lp := raise(t, ca, m, nil)
	lp.seed(t, 2)

	for _, l := range []struct{ name, addr string }{{"public", lp.public}, {"internal", lp.internal}} {
		t.Run(l.name, func(t *testing.T) {
			conn := dialAs(t, ca, l.addr, "notify-"+l.name, notifySAN)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			before := len(m.questions())
			resp, err := claim(ctx, conn)
			if err != nil {
				t.Fatalf("Claim от notify на слушателе %s: %v", l.name, err)
			}
			if n := len(resp.GetNotifications()); n != 1 {
				t.Fatalf("Claim от notify на слушателе %s выдал %d строк, ожидалась 1", l.name, n)
			}
			// Окно положительных вердиктов одно на процесс (звено решения
			// одно на пару слушателей), поэтому второй слушатель может
			// ответить из окна, не спрашивая модель: ключ окна — тот же
			// субъект service:notify. Вопрос, если задан, — ровно этот.
			for _, q := range m.questions()[before:] {
				if q != readerOnFeed {
					t.Fatalf("Claim на слушателе %s спросил модель %q, ожидался %q", l.name, q, readerOnFeed)
				}
			}

			strm, err := subscribe(ctx, conn)
			if err != nil {
				t.Fatalf("Subscribe от notify на слушателе %s: %v", l.name, err)
			}
			first, err := strm.Recv()
			if err != nil {
				t.Fatalf("Subscribe от notify на слушателе %s не открыт: %v", l.name, err)
			}
			if first.GetOpened() == nil {
				t.Fatalf("первым кадром пришло %T, ожидалось сообщение открытия", first.GetMessage())
			}
			ev := firstEvent(t, strm)
			if ev.GetKind() != "notification_feed" || ev.GetResourceId() != "notify-probe" {
				t.Fatalf("сигнал ленты: вид %q, объект %q", ev.GetKind(), ev.GetResourceId())
			}
		})
	}

	asked := m.questions()
	claimAsked := 0
	for _, q := range asked {
		if !strings.HasPrefix(q, "service:notify ") {
			t.Fatalf("владельца модели спросили не от service:notify: %q", q)
		}
		if q == readerOnFeed {
			claimAsked++
		}
	}
	if claimAsked == 0 {
		t.Fatalf("Claim ни разу не спросил модель о %q — проверка прав не исполнялась: %q", readerOnFeed, asked)
	}
}

func firstEvent(t *testing.T, strm grpc.ServerStreamingClient[subscriptionv1.SubscriptionMessage]) *subscriptionv1.SubscriptionEvent {
	t.Helper()
	for {
		msg, err := strm.Recv()
		if err != nil {
			t.Fatalf("поток закрыт до сигнала ленты: %v", err)
		}
		if ev := msg.GetEvent(); ev != nil {
			return ev
		}
	}
}

// Близнец NTF1-M08 — пустой перечень и пустая таблица: субъекта нет, Claim и
// Subscribe — PERMISSION_DENIED на обоих слушателях, модель о service:notify
// не спрошена.
func TestNTF1M08_WithoutTheIdentityLinkBothCallsAreDenied(t *testing.T) {
	ca := newTestCA(t)
	m := &model{allow: map[string]bool{readerOnFeed: true}}
	absent := servicecontract.NotApplicable[grpcsrv.ServiceIdentity]("близнец M08: звено без перечня и таблицы")
	lp := raise(t, ca, m, &absent)
	lp.seed(t, 1)

	for _, l := range []struct{ name, addr string }{{"public", lp.public}, {"internal", lp.internal}} {
		t.Run(l.name, func(t *testing.T) {
			conn := dialAs(t, ca, l.addr, "notify-"+l.name, notifySAN)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			_, err := claim(ctx, conn)
			if status.Code(err) != codes.PermissionDenied {
				t.Fatalf("Claim без звена на слушателе %s: %v, ожидался PERMISSION_DENIED", l.name, err)
			}
			strm, err := subscribe(ctx, conn)
			if err == nil {
				_, err = strm.Recv()
			}
			if status.Code(err) != codes.PermissionDenied {
				t.Fatalf("Subscribe без звена на слушателе %s: %v, ожидался PERMISSION_DENIED", l.name, err)
			}
		})
	}
	for _, q := range m.questions() {
		if strings.HasPrefix(q, "service:notify ") {
			t.Fatalf("без звена владельца модели спросили от service:notify: %q", q)
		}
	}
}
