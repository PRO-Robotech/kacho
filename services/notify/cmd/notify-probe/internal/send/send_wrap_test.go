// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package send_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/grpc/codes"

	"github.com/PRO-Robotech/corelib/notify/address"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"
	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/send"
)

// errBegin / errPut — отказы базы, которые фальшивка подаёт на своём шаге.
var (
	errBegin = errors.New("отказ открытия транзакции фальшивки")
	errPut   = errors.New("отказ оператора постановки фальшивки")
)

// failingDB — база, отказывающая на одном шаге постановки: на открытии
// транзакции (begin) либо на первом операторе после установок помощника
// журнала (put).
type failingDB struct{ step string }

func (d failingDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	if d.step == "begin" {
		return nil, errBegin
	}
	return &failingTx{}, nil
}

// failingTx пропускает установки помощника журнала (первый Exec) и отказывает
// на всём, что делает постановка дальше.
type failingTx struct {
	pgx.Tx
	execs int
}

func (t *failingTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	t.execs++
	if t.execs == 1 {
		return pgconn.CommandTag{}, nil
	}
	return pgconn.CommandTag{}, errPut
}

func (t *failingTx) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, errPut }
func (t *failingTx) QueryRow(context.Context, string, ...any) pgx.Row        { return errRow{} }
func (t *failingTx) Begin(context.Context) (pgx.Tx, error)                   { return nil, errPut }
func (t *failingTx) Rollback(context.Context) error                          { return nil }
func (t *failingTx) Commit(context.Context) error                            { return errPut }

type errRow struct{}

func (errRow) Scan(...any) error { return errPut }

// GS-I2 — отказ постановки уходит в журнал с именем ШАГА: журнал различает
// «транзакция не открылась» и «постановка не прошла»; наружу — по-прежнему
// "internal error", исходная ошибка сохраняется для errors.Is (%w).
func TestSendInternalRefusalNamesTheStepInTheLog(t *testing.T) {
	cases := []struct {
		step, want string
		cause      error
	}{
		{"begin", "начало транзакции постановки: ", errBegin},
		{"put", "постановка probe-hello: ", errPut},
	}
	for _, c := range cases {
		t.Run(c.step, func(t *testing.T) {
			var buf bytes.Buffer
			log := slog.New(slog.NewTextHandler(&buf, nil))
			srv := send.New(failingDB{step: c.step}, source(t, true), log)
			_, err := srv.Send(asUser(context.Background()), &notifyv1.SendRequest{Address: "probe@example.com"})
			code, msg := codeAndMessage(t, err)
			if code != codes.Internal || msg != "internal error" {
				t.Fatalf("шаг %s: %s %q, ожидалось Internal \"internal error\"", c.step, code, msg)
			}
			if !strings.Contains(buf.String(), `err="`+c.want) || !strings.Contains(buf.String(), c.cause.Error()) {
				t.Fatalf("шаг %s: журнал не начинает ошибку шагом %q либо потерял причину %q:\n%s",
					c.step, c.want, c.cause, buf.String())
			}
		})
	}
}

// GS-I3 — текст отказа на ненормализуемом адресе равен тексту фундамента
// побайтно: префикс `address: ` фундамент ставит сам, второго нет.
func TestSendMalformedAddressCarriesTheFoundationTextOnce(t *testing.T) {
	srv := send.New(untouchedDB{t}, source(t, true), discard)
	for _, a := range []string{"no-at-sign", "a@", "@b.example"} {
		_, nerr := address.Normalize(a)
		if nerr == nil {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: фундамент принял %q — отрицанию не на чем стоять", a)
		}
		_, err := srv.Send(asUser(context.Background()), &notifyv1.SendRequest{Address: a})
		code, msg := codeAndMessage(t, err)
		if code != codes.InvalidArgument || msg != nerr.Error() {
			t.Fatalf("адрес %q: %s %q, ожидалось InvalidArgument %q", a, code, msg, nerr.Error())
		}
		if strings.Count(msg, "address: ") != 1 {
			t.Fatalf("адрес %q: префикс поля не ровно один: %q", a, msg)
		}
	}
}
