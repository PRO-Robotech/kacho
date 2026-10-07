// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package send_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/corelib/journaltx"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"
	notify "github.com/PRO-Robotech/kacho/services/notify"
	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/journal"
	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/send"
)

// feedTable — таблица ленты пробы для ЧТЕНИЯ пробой (что закоммичено). Имя
// производит фундамент (`notifygen init` записал его в миграцию ленты); здесь
// оно выписано только потому, что проба читает базу снаружи. Продукт этого
// имени не строит и ленту не читает — это держат NTF1-B19 и
// TestSendIssuesOnlyTheFoundationStatements.
const feedTable = journal.Service + "_notification_outbox"

// recordingDB — база, чьи транзакции записывают текст каждого оператора и
// перед каждым из них (и перед коммитом) зовут before.
type recordingDB struct {
	pool   *pgxpool.Pool
	mu     *sync.Mutex
	stmts  *[]string
	before func(stmt string)
}

func (d recordingDB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := d.pool.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &recordingTx{Tx: tx, db: d}, nil
}

type recordingTx struct {
	pgx.Tx
	db recordingDB
}

func (t *recordingTx) note(stmt string) {
	t.db.mu.Lock()
	*t.db.stmts = append(*t.db.stmts, stmt)
	t.db.mu.Unlock()
	if t.db.before != nil {
		t.db.before(stmt)
	}
}

func (t *recordingTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.note(sql)
	return t.Tx.Exec(ctx, sql, args...)
}

func (t *recordingTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	t.note(sql)
	return t.Tx.Query(ctx, sql, args...)
}

func (t *recordingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	t.note(sql)
	return t.Tx.QueryRow(ctx, sql, args...)
}

func (t *recordingTx) Begin(ctx context.Context) (pgx.Tx, error) {
	t.note("SAVEPOINT")
	tx, err := t.Tx.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &recordingTx{Tx: tx, db: t.db}, nil
}

func (t *recordingTx) Commit(ctx context.Context) error {
	t.note("COMMIT")
	return t.Tx.Commit(ctx)
}

// Send — это постановка фундамента и ничего сверх неё: множество операторов
// транзакции Send равно множеству операторов законного близнеца — той же
// транзакции помощника журнала, в которой зовётся только порождённый
// SendProbeHello. Лишний оператор Send (чтение ленты в обход feed, подъём
// уровня изоляции) — красный с его текстом.
func TestSendIssuesOnlyTheFoundationStatements(t *testing.T) {
	pool := pool(t)
	src := source(t, true)
	ctx := src.Bind(asUser(context.Background()))

	var mu sync.Mutex
	var twin []string
	twinDB := recordingDB{pool: pool, mu: &mu, stmts: &twin}
	tx, err := journaltx.Begin(ctx, twinDB, journaltx.NewOptions(src.Enabled()))
	if err != nil {
		t.Fatalf("близнец: начало транзакции: %v", err)
	}
	if _, err := notify.SendProbeHello(ctx, tx, notify.ProbeHelloAttrs{To: "twin@example.com", Target: "/"}); err != nil {
		t.Fatalf("близнец: SendProbeHello: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("близнец: коммит: %v", err)
	}
	if len(twin) < 2 {
		t.Fatalf("близнец записал операторов %d — запись не исполнялась: %q", len(twin), twin)
	}

	var got []string
	srv := send.New(recordingDB{pool: pool, mu: &mu, stmts: &got}, src, discard)
	if _, err := srv.Send(asUser(context.Background()), &notifyv1.SendRequest{Address: "probe@example.com"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	t.Logf("операторов: близнец %d · Send %d", len(twin), len(got))
	if !slices.Equal(got, twin) {
		for _, s := range got {
			if !slices.Contains(twin, s) {
				t.Errorf("оператор Send сверх постановки фундамента: %q", s)
			}
		}
		t.Fatalf("операторы Send %q\nоператоры близнеца %q", got, twin)
	}
}

// Конкурирующая запись: пока транзакция Send открыта, перед каждым её
// оператором и перед коммитом другая транзакция ставит и коммитит письмо на
// свой адрес. Ответ Send — id строки, чей адресат — адрес Send; строк с этим
// адресатом ровно одна; конкурентов — столько, сколько их было, и ни один id
// не совпал с ответом. Законный близнец — конкурентов ноль: ответ тот же по
// форме, строк одна.
func TestSendReturnsItsOwnIDUnderACompetingWrite(t *testing.T) {
	for _, tc := range []struct {
		name    string
		compete bool
	}{{"twin-no-competitor", false}, {"competitor-at-every-statement", true}} {
		t.Run(tc.name, func(t *testing.T) {
			pool := pool(t)
			src := source(t, true)
			var mu sync.Mutex
			var stmts []string
			competitors := 0
			db := recordingDB{pool: pool, mu: &mu, stmts: &stmts}
			if tc.compete {
				db.before = func(string) {
					competitors++
					addr := fmt.Sprintf("competitor-%d@example.com", competitors)
					ctx := src.Bind(asUser(context.Background()))
					tx, err := journaltx.Begin(ctx, pool, journaltx.NewOptions(src.Enabled()))
					if err != nil {
						t.Errorf("конкурент %s: начало: %v", addr, err)
						return
					}
					defer func() { _ = tx.Rollback(context.Background()) }()
					if _, err := notify.SendProbeHello(ctx, tx, notify.ProbeHelloAttrs{To: addr, Target: "/"}); err != nil {
						t.Errorf("конкурент %s: постановка: %v", addr, err)
						return
					}
					if err := tx.Commit(ctx); err != nil {
						t.Errorf("конкурент %s: коммит: %v", addr, err)
					}
				}
			}
			srv := send.New(db, src, discard)
			const own = "own@example.com"
			resp, err := srv.Send(asUser(context.Background()), &notifyv1.SendRequest{Address: own})
			if err != nil {
				t.Fatalf("Send при конкурентах %d: %v", competitors, err)
			}
			id := resp.GetNotificationId()

			var recipient string
			if err := pool.QueryRow(context.Background(),
				`SELECT recipient_address FROM `+feedTable+` WHERE id = $1`, id).Scan(&recipient); err != nil {
				t.Fatalf("строки с id ответа %s нет: %v", id, err)
			}
			if recipient != own {
				t.Fatalf("id ответа %s — строка адресата %q, а не %q: Send ответил чужим id", id, recipient, own)
			}
			var mine, total int
			if err := pool.QueryRow(context.Background(),
				`SELECT count(*) FILTER (WHERE recipient_address = $1), count(*) FROM `+feedTable, own).
				Scan(&mine, &total); err != nil {
				t.Fatal(err)
			}
			t.Logf("операторов Send %d · конкурентов %d · строк ленты %d", len(stmts), competitors, total)
			if tc.compete && competitors == 0 {
				t.Fatal("условие не создано: конкурент не писал ни разу")
			}
			if mine != 1 || total != 1+competitors {
				t.Fatalf("строк адресата Send %d (ожидалась 1), всего %d (ожидалось %d)", mine, total, 1+competitors)
			}
		})
	}
}
