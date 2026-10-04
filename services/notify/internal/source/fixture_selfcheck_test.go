// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package source

// fixture_selfcheck_test.go — положительный контроль фикстуры сырым клиентом
// gRPC, без испытуемого. Каждое поведение, которым пробы полосы N2 ставят
// испытуемому вопрос, здесь исполнено и утверждено: если фикстура сломана,
// краснеет она сама и своим текстом, а не выдаёт себя за отсутствующую
// возможность цикла.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	notifyv1 "github.com/PRO-Robotech/corelib/api/corelib/notify"
	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"
)

// rawConn — клиент с удостоверением notify; wantSAN, если задан, —
// требование точного URI-SAN сервера (так, как его обязан требовать цикл).
func rawConn(t *testing.T, ca *testCA, addr, wantSAN string) *grpc.ClientConn {
	t.Helper()
	cfg := ca.peerTLS(t)
	cfg.ServerName = "localhost"
	if wantSAN != "" {
		cfg.VerifyPeerCertificate = func(_ [][]byte, chains [][]*x509.Certificate) error {
			for _, ch := range chains {
				for _, u := range ch[0].URIs {
					if u.String() == wantSAN {
						return nil
					}
				}
			}
			return errors.New("server SAN is not " + wantSAN)
		}
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(credentials.NewTLS(cfg)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func rawClaim(ctx context.Context, conn *grpc.ClientConn, maxRows uint32) (*notifyv1.ClaimResponse, error) {
	return notifyv1.NewInternalNotificationFeedServiceClient(conn).Claim(ctx, &notifyv1.ClaimRequest{
		Max:     maxRows,
		Classes: []notifyv1.NotificationClass{notifyv1.NotificationClass_SECURITY, notifyv1.NotificationClass_NOTICE},
	})
}

func TestFixtureSourceAnswersClaimAsTheFeedContract(t *testing.T) {
	t.Parallel()
	ca := newTestCA(t)
	src := newFakeSource(t, ca, "probe", sourceOpts{})
	conn := rawConn(t, ca, src.addr, sanOf("probe"))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ids := src.put(3, false)
	resp, err := rawClaim(ctx, conn, 2)
	if err != nil {
		t.Fatalf("фикстура: Claim(max=2): %v", err)
	}
	if n := len(resp.GetNotifications()); n != 2 {
		t.Fatalf("фикстура: Claim(max=2) при 3 строках выдал %d", n)
	}
	for _, n := range resp.GetNotifications() {
		if n.GetLeaseRemaining().AsDuration() <= 0 || n.GetExpiresIn().AsDuration() <= 0 {
			t.Fatalf("фикстура: строка %s без остатка аренды или срока", n.GetId())
		}
	}
	resp, err = rawClaim(ctx, conn, 5)
	if err != nil || len(resp.GetNotifications()) != 1 || resp.GetNotifications()[0].GetId() != ids[2] {
		t.Fatalf("фикстура: второй Claim обязан выдать только незанятую %s: %v %v", ids[2], resp, err)
	}
	resp, err = rawClaim(ctx, conn, 5)
	if err != nil || len(resp.GetNotifications()) != 0 {
		t.Fatalf("фикстура: арендованные строки выданы повторно до конца аренды: %v %v", resp, err)
	}
	for _, bad := range []uint32{0, 501} {
		_, err := rawClaim(ctx, conn, bad)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("фикстура снисходительнее сервера ленты: Claim(max=%d) → %v, ожидался INVALID_ARGUMENT", bad, err)
		}
	}
	calls := src.claimCalls()
	if len(calls) != 5 || calls[0].max != 2 || !calls[0].ended || calls[0].leased != 2 {
		t.Fatalf("фикстура: запись вызовов неверна: %+v", calls)
	}
}

func TestFixtureSourceFreezeAndLateAnswer(t *testing.T) {
	t.Parallel()
	ca := newTestCA(t)
	src := newFakeSource(t, ca, "probe", sourceOpts{leaseTTL: 2 * time.Second})
	conn := rawConn(t, ca, src.addr, "")

	src.put(1, false)
	src.setFrozen(true)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	_, err := rawClaim(ctx, conn, 1)
	cancel()
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("фикстура: Claim к замороженному источнику → %v, ожидался DEADLINE_EXCEEDED", err)
	}
	if !waitFor(2*time.Second, func() bool { c := src.claimCalls(); return len(c) == 1 && c[0].ended && c[0].err != nil }) {
		t.Fatalf("фикстура: замороженный вызов не записан оконченным по контексту: %+v", src.claimCalls())
	}
	src.setFrozen(false)

	// Ответ позже срока ПОСЛЕ коммита аренды: строка занята до конца аренды.
	src.delayNext(time.Second)
	ctx, cancel = context.WithTimeout(context.Background(), 200*time.Millisecond)
	_, err = rawClaim(ctx, conn, 1)
	cancel()
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("фикстура: Claim с ответом через 1 с при сроке 200 мс → %v", err)
	}
	if !waitFor(2*time.Second, func() bool { c := src.claimCalls(); return len(c) == 2 && c[1].ended }) {
		t.Fatalf("фикстура: отменённый вызов не окончен: %+v", src.claimCalls())
	}
	if c := src.claimCalls()[1]; c.leased != 1 || c.err == nil {
		t.Fatalf("фикстура: отмена после коммита аренды не записана: %+v", c)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if resp, err := rawClaim(ctx, conn, 1); err != nil || len(resp.GetNotifications()) != 0 {
		t.Fatalf("фикстура: строка, чья аренда закоммичена отменённым вызовом, выдана до конца аренды: %v %v", resp, err)
	}
	if !waitFor(5*time.Second, func() bool {
		resp, err := rawClaim(ctx, conn, 1)
		return err == nil && len(resp.GetNotifications()) == 1
	}) {
		t.Fatal("фикстура: после конца аренды строка не выдана снова")
	}
}

func TestFixtureSourceSubscriptionModes(t *testing.T) {
	t.Parallel()
	ca := newTestCA(t)

	t.Run("open", func(t *testing.T) {
		src := newFakeSource(t, ca, "probe", sourceOpts{})
		conn := rawConn(t, ca, src.addr, "")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		strm, err := subscriptionv1.NewInternalSubscriptionServiceClient(conn).Subscribe(ctx,
			&subscriptionv1.SubscriptionRequest{Kinds: []string{feedKind}})
		if err != nil {
			t.Fatal(err)
		}
		first, err := strm.Recv()
		if err != nil || first.GetOpened() == nil {
			t.Fatalf("фикстура: первым кадром не SubscriptionOpened: %v %v", first, err)
		}
		src.put(1, true)
		ev, err := strm.Recv()
		if err != nil || ev.GetEvent().GetKind() != feedKind || ev.GetEvent().GetResourceId() != "probe" {
			t.Fatalf("фикстура: событие ленты не пришло: %v %v", ev, err)
		}
		if s := src.streamsSeen(); len(s) != 1 || len(s[0].kinds) != 1 || s[0].kinds[0] != feedKind {
			t.Fatalf("фикстура: поток записан неверно: %+v", s)
		}
	})

	t.Run("silent", func(t *testing.T) {
		src := newFakeSource(t, ca, "probe", sourceOpts{mode: subSilent})
		conn := rawConn(t, ca, src.addr, "")
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		strm, err := subscriptionv1.NewInternalSubscriptionServiceClient(conn).Subscribe(ctx,
			&subscriptionv1.SubscriptionRequest{Kinds: []string{feedKind}})
		if err == nil {
			_, err = strm.Recv()
		}
		if status.Code(err) != codes.DeadlineExceeded {
			t.Fatalf("фикстура: молчащий поток прислал кадр или отказ иной: %v", err)
		}
		if !waitFor(2*time.Second, func() bool { s := src.streamsSeen(); return len(s) == 1 && s[0].ended }) {
			t.Fatalf("фикстура: конец молчащего потока не записан: %+v", src.streamsSeen())
		}
	})

	t.Run("unavailable", func(t *testing.T) {
		src := newFakeSource(t, ca, "probe", sourceOpts{mode: subUnavailable})
		conn := rawConn(t, ca, src.addr, "")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		strm, err := subscriptionv1.NewInternalSubscriptionServiceClient(conn).Subscribe(ctx,
			&subscriptionv1.SubscriptionRequest{Kinds: []string{feedKind}})
		if err == nil {
			_, err = strm.Recv()
		}
		if status.Code(err) != codes.Unavailable {
			t.Fatalf("фикстура: остановленный сервер подписки ответил %v, ожидался UNAVAILABLE", err)
		}
		// Сервер ленты при этом доступен.
		src.put(1, false)
		if resp, err := rawClaim(ctx, conn, 1); err != nil || len(resp.GetNotifications()) != 1 {
			t.Fatalf("фикстура: сервер ленты недоступен вместе с подпиской: %v %v", resp, err)
		}
	})
}

// Два сервера различаются ровно URI-SAN: требование точного SAN отвергает
// чужой и принимает свой; без требования — оба приняты (имя узла у них общее).
func TestFixtureServerSANIsTheOnlyDifference(t *testing.T) {
	t.Parallel()
	ca := newTestCA(t)
	own := newFakeSource(t, ca, "kaname", sourceOpts{})
	foreign := newFakeSource(t, ca, "kaname", sourceOpts{serverSAN: sanOf("probe")})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := rawClaim(ctx, rawConn(t, ca, own.addr, sanOf("kaname")), 1); err != nil {
		t.Fatalf("фикстура: свой SAN отвергнут: %v", err)
	}
	if _, err := rawClaim(ctx, rawConn(t, ca, foreign.addr, sanOf("kaname")), 1); status.Code(err) != codes.Unavailable {
		t.Fatalf("фикстура: чужой SAN при требовании точного → %v, ожидался отказ подключения", err)
	}
	if n := len(foreign.claimCalls()); n != 0 {
		t.Fatalf("фикстура: вызов дошёл до сервера с чужим SAN: %d", n)
	}
	if _, err := rawClaim(ctx, rawConn(t, ca, foreign.addr, ""), 1); err != nil {
		t.Fatalf("фикстура: без требования SAN сервер отвергнут — различие не в одном SAN: %v", err)
	}
}

// Клиент предъявляет удостоверение notify; иной клиент сервером не принят.
func TestFixtureRequiresClientCertificate(t *testing.T) {
	t.Parallel()
	ca := newTestCA(t)
	src := newFakeSource(t, ca, "probe", sourceOpts{})
	conn, err := grpc.NewClient(src.addr, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		RootCAs: ca.pool, ServerName: "localhost", MinVersion: tls.VersionTLS12,
	})))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := rawClaim(ctx, conn, 1); err == nil {
		t.Fatal("фикстура: сервер принял клиента без сертификата")
	}
}
