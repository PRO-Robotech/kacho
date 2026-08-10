// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzformbench

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
)

// Производители величины `StmtSQL` — по одному на КАЖДОЕ место снятия, а не один
// на форму.
//
// Мест здесь два: Postgres движка (там стейтменты порождает сам движок, и увидеть
// их можно только его же датастором) и Postgres формы E (там стейтменты порождаем
// мы, и считать их можно на своём соединении). Общего производителя у них быть не
// может: у первого нет хука трассировки, у второго нет расширения статистики —
// поэтому и заводятся два, и у каждого свой контроль.
//
// Ни один производитель не считается заведённым, пока не показан контроль в ОБЕ
// стороны: (а) счётчик не двигается, когда никто не спрашивает — иначе мерился бы
// фон; (б) счётчик двигается ровно на один на одном заведомом стейтменте — иначе
// мерится что-то другое. Обе половины проверяются исполнением и печатаются в
// провенансе: величина, у входа которой нет производителя, зеленеет молча.

// ── сторона движка: дельта pg_stat_statements ─────────────────────────────────

// pgStmtCounter снимает число стейтментов, исполненных В БАЗЕ ДВИЖКА за окно
// между Start и Stop.
//
// Собственные запросы счётчика из счёта исключены по тексту: иначе он мерил бы в
// том числе себя, и «ноль стейтментов» было бы недостижимо by construction.
type pgStmtCounter struct {
	db   *sql.DB
	base int64
}

const stmtCountQuery = `SELECT COALESCE(sum(s.calls), 0)
   FROM pg_stat_statements s
   JOIN pg_database d ON d.oid = s.dbid
  WHERE d.datname = current_database()
    AND s.query NOT LIKE '%pg_stat_statements%'`

func (c *pgStmtCounter) read(ctx context.Context) (int64, error) {
	var n int64
	if err := c.db.QueryRowContext(ctx, stmtCountQuery).Scan(&n); err != nil {
		return 0, fmt.Errorf("чтение счётчика стейтментов движка: %w", err)
	}
	return n, nil
}

func (c *pgStmtCounter) Start(ctx context.Context) {
	n, err := c.read(ctx)
	if err != nil {
		c.base = -1
		return
	}
	c.base = n
}

// Stop возвращает дельту окна. База -1 означает, что открыть окно не удалось:
// такая дельта не печатается нулём, вызывающий получает ошибку.
func (c *pgStmtCounter) Stop(ctx context.Context) (int, error) {
	if c.base < 0 {
		return 0, fmt.Errorf("окно счётчика не открылось")
	}
	n, err := c.read(ctx)
	if err != nil {
		return 0, err
	}
	return int(n - c.base), nil
}

// ensurePgStatStatements включает расширение в базе движка.
//
// Расширение требует, чтобы библиотека была предзагружена сервером; предзагрузку
// задаёт СВОЙ контейнер харнесса (`stack.go`), а не чужой прод-код.
func ensurePgStatStatements(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS pg_stat_statements`); err != nil {
		return fmt.Errorf("расширение статистики стейтментов недоступно: %w", err)
	}
	return nil
}

// VerifyEngineStmtProducer прогоняет ОБА контроля производителя со стороны движка.
//
// Исход печатается в провенансе целиком — и при успехе, и при провале: при
// провале колонка `StmtSQL` этого места не печатается вовсе (не ноль и не
// прочерк), а формулировка «на общем для форм уровне» из отчёта снимается.
func VerifyEngineStmtProducer(ctx context.Context, db *sql.DB) (*pgStmtCounter, ProducerStatus) {
	place := "движок (Postgres датастора OpenFGA)"
	producer := "дельта pg_stat_statements.calls"
	if err := ensurePgStatStatements(ctx, db); err != nil {
		return nil, ProducerStatus{Place: place, Producer: producer, Note: err.Error()}
	}
	c := &pgStmtCounter{db: db}

	// (а) никто не спрашивает — счётчик стоит.
	c.Start(ctx)
	idle, err := c.Stop(ctx)
	if err != nil {
		return nil, ProducerStatus{Place: place, Producer: producer, Note: err.Error()}
	}
	if idle != 0 {
		return nil, ProducerStatus{Place: place, Producer: producer,
			Note: fmt.Sprintf("холостая дельта %d вместо 0 — счётчик мерит фон, а не операцию", idle)}
	}

	// (б) один заведомый стейтмент — счётчик двигается ровно на один.
	c.Start(ctx)
	if _, err := db.ExecContext(ctx, `SELECT 1`); err != nil {
		return nil, ProducerStatus{Place: place, Producer: producer, Note: err.Error()}
	}
	one, err := c.Stop(ctx)
	if err != nil {
		return nil, ProducerStatus{Place: place, Producer: producer, Note: err.Error()}
	}
	if one != 1 {
		return nil, ProducerStatus{Place: place, Producer: producer,
			Note: fmt.Sprintf("один стейтмент дал дельту %d вместо 1 — счётчик мерит не стейтменты", one)}
	}
	return c, ProducerStatus{Place: place, Producer: producer, OK: true,
		Note: "холостая дельта 0, один стейтмент дал 1"}
}

// ── сторона формы E: трассировщик на СВОЁМ соединении ─────────────────────────

// stmtTracer считает стейтменты, пущенные пулом формы E.
//
// Готового счётчика для этого места не существует: соединение базы сравнения
// открыто `sql.Open`, у которого хука трассировки нет вовсе, — поэтому форма E
// берёт своё соединение и счётчик пишется здесь. Считается КАЖДЫЙ стейтмент,
// включая `begin`/`commit`: транзакция — такая же работа Postgres, и прятать её
// значило бы напечатать заниженную величину под именем измеренной.
type stmtTracer struct {
	n atomic.Int64
}

func (t *stmtTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	t.n.Add(1)
	return ctx
}

func (t *stmtTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (t *stmtTracer) count() int { return int(t.n.Load()) }

// window — окно измерения: разность показаний трассировщика.
type stmtWindow struct {
	t    *stmtTracer
	base int
}

func (t *stmtTracer) open() stmtWindow { return stmtWindow{t: t, base: t.count()} }

func (w stmtWindow) close() int { return w.t.count() - w.base }
