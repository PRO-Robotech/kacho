// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package send_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/corelib/operations"
	"github.com/PRO-Robotech/corelib/pgtest"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"
	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/journal"
	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/send"
)

// source — источник ленты пробы с флагом on.
func source(t *testing.T, on bool) *feed.Source {
	t.Helper()
	flag := "false"
	if on {
		flag = "true"
	}
	enabled, err := feed.ParseEnabled("KACHO_NOTIFYPROBE_NOTIFICATIONS_ENABLED",
		func(string) (string, bool) { return flag, true })
	if err != nil {
		t.Fatal(err)
	}
	signal, err := feed.JournalSignal(journal.Journal(), journal.Module, journal.ChangeUpdated)
	if err != nil {
		t.Fatal(err)
	}
	src, err := feed.NewSource(feed.Config{
		Module: journal.Module, Service: journal.Service, Enabled: enabled,
		Signal: signal, Sealer: noSealer{}, Metrics: prometheus.NewRegistry(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return src
}

// noSealer — у probe-hello секретных атрибутов нет, запечатывать нечего.
type noSealer struct{}

func (noSealer) Seal(string, string, string, []byte) ([]byte, error) {
	return nil, errors.New("probe-hello секретов не несёт")
}

// untouchedDB — база, к которой отказ обязан НЕ обратиться.
type untouchedDB struct{ t *testing.T }

func (d untouchedDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	d.t.Fatal("отказ обратился к базе — проверка формы и флага обязана стоять до неё")
	return nil, nil
}

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func asUser(ctx context.Context) context.Context {
	return operations.WithPrincipal(ctx, operations.Principal{Type: "user", ID: ids.NewID(ids.PrefixUser)})
}

func codeAndMessage(t *testing.T, err error) (codes.Code, string) {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("ответ не gRPC-статус: %v", err)
	}
	return st.Code(), st.Message()
}

// Пустой адрес — INVALID_ARGUMENT `address: required`, до базы.
func TestSendEmptyAddressIsRequired(t *testing.T) {
	srv := send.New(untouchedDB{t}, source(t, true), discard)
	_, err := srv.Send(asUser(context.Background()), &notifyv1.SendRequest{})
	code, msg := codeAndMessage(t, err)
	if code != codes.InvalidArgument || msg != "address: required" {
		t.Fatalf("пустой адрес: %s %q, ожидалось InvalidArgument \"address: required\"", code, msg)
	}
}

// Адрес, который не нормализуется, — INVALID_ARGUMENT с именем поля, до базы.
func TestSendMalformedAddressNamesTheField(t *testing.T) {
	srv := send.New(untouchedDB{t}, source(t, true), discard)
	for _, a := range []string{"no-at-sign", "a@", "@b.example"} {
		_, err := srv.Send(asUser(context.Background()), &notifyv1.SendRequest{Address: a})
		code, msg := codeAndMessage(t, err)
		if code != codes.InvalidArgument || !strings.HasPrefix(msg, "address: ") {
			t.Fatalf("адрес %q: %s %q, ожидалось InvalidArgument с именем поля address", a, code, msg)
		}
		if strings.Contains(msg, a) && a != "" {
			t.Errorf("адрес %q: отказ повторяет значение адреса: %q", a, msg)
		}
	}
}

// Доставка выключена — единый отказ «доставка не настроена» (FAILED_PRECONDITION
// с ErrorInfo), до чтения адреса и до базы (NTF1-N06).
func TestSendWithDeliveryOffIsTheSingleRefusal(t *testing.T) {
	srv := send.New(untouchedDB{t}, source(t, false), discard)
	for _, a := range []string{"", "probe@example.com"} {
		_, err := srv.Send(asUser(context.Background()), &notifyv1.SendRequest{Address: a})
		st, _ := status.FromError(err)
		want := feed.DeliveryNotConfiguredStatus()
		if st.Code() != want.Code() || st.Message() != want.Message() {
			t.Fatalf("адрес %q при выключенной доставке: %s %q, ожидалось %s %q",
				a, st.Code(), st.Message(), want.Code(), want.Message())
		}
		found := false
		for _, d := range st.Details() {
			if info, ok := d.(*errdetails.ErrorInfo); ok && info.GetReason() == feed.DeliveryNotConfiguredReason {
				found = true
			}
		}
		if !found {
			t.Errorf("отказ без ErrorInfo %s", feed.DeliveryNotConfiguredReason)
		}
	}
}

// Положительный исход: ответ — id ровно той строки ленты, что закоммичена.
func TestSendReturnsTheIDOfTheCommittedRow(t *testing.T) {
	pool := pool(t)
	srv := send.New(pool, source(t, true), discard)
	resp, err := srv.Send(asUser(context.Background()), &notifyv1.SendRequest{Address: "Probe@Example.COM"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	id := resp.GetNotificationId()
	if !strings.HasPrefix(id, "ntf-") {
		t.Fatalf("notification_id %q не в форме ntf-…", id)
	}
	var template, state string
	if err := pool.QueryRow(context.Background(),
		`SELECT template, state FROM `+journal.FeedOutbox+` WHERE id = $1`, id).Scan(&template, &state); err != nil {
		t.Fatalf("строки с id %s в ленте нет: %v", id, err)
	}
	if template != "probe-hello" || state != "pending" {
		t.Fatalf("строка %s: шаблон %s, состояние %s", id, template, state)
	}
}

// Гонка: N параллельных Send — N разных id, и каждый — своя закоммиченная
// строка. Ответ, выведенный из «последней строки ленты», дал бы здесь чужой id.
//
// Гонка вероятностная: раундов parallelRounds по n вызовов на одной базе —
// инъекция READ COMMITTED краснит пробу на каждом прогоне замера (см. отчёт
// полосы), а не через раз.
func TestParallelSendsGetTheirOwnIDs(t *testing.T) {
	if testing.Short() {
		t.Skip("гонка идёт против живой базы (testcontainers): под кратким режимом пропускается, " +
			"гоняет цель test-pg-outside-selection")
	}
	for round := 0; round < parallelRounds; round++ {
		// Подпроба на раунд: база и пул раунда снимаются в его конце.
		t.Run(fmt.Sprintf("round-%d", round), parallelSendsRound)
	}
}

const parallelRounds = 4

func parallelSendsRound(t *testing.T) {
	t.Helper()
	pool := pool(t)
	srv := send.New(pool, source(t, true), discard)
	const n = 48
	ids := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			resp, err := srv.Send(asUser(context.Background()), &notifyv1.SendRequest{Address: "probe@example.com"})
			ids[i], errs[i] = resp.GetNotificationId(), err
		}(i)
	}
	close(start)
	wg.Wait()
	seen := map[string]bool{}
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("Send %d: %v", i, errs[i])
		}
		if seen[ids[i]] {
			t.Fatalf("id %s отдан двум вызовам", ids[i])
		}
		seen[ids[i]] = true
	}
	var rows int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM `+journal.FeedOutbox).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != n {
		t.Fatalf("строк ленты %d при %d вызовах", rows, n)
	}
	for id := range seen {
		var one int
		if err := pool.QueryRow(context.Background(),
			`SELECT count(*) FROM `+journal.FeedOutbox+` WHERE id = $1`, id).Scan(&one); err != nil || one != 1 {
			t.Fatalf("id %s: строк %d (%v)", id, one, err)
		}
	}
}

func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	p, err := pgxpool.New(context.Background(), pgtest.NewDB(t))
	if err != nil {
		t.Fatal(err)
	}
	pgtest.ClosePoolAtEnd(t, p)
	return p
}
