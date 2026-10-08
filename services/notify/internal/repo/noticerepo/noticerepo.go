// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package noticerepo — хранилище извещений notify-api над базой kacho_notify
// (замысел issue-2924 З3 п.1, З5, З6, З7, З13; схема — миграция
// 20261006232858_operator_notices). Рукописный pgx, без ORM (ban #3).
//
// Каждая запись — одна транзакция READ COMMITTED, в которой лежит и строка
// операции: Ф1 (`CreatePendingTx`) у приёма, Ф2 (`CreateDoneTx`) у переходов и
// Update. Своего SQL к таблице операций здесь нет (Р23): строку пишет хранилище
// фундамента в транзакции вызывающего.
//
// Инварианты — у базы (ban #10): переход — одна запись с условием на состояние
// и вид, классификация нуля строк — чтением той же транзакции ПОСЛЕ оператора
// (З5 п.2); согласованность моментов и полей по виду — именованные `CHECK`,
// которые этот пакет переводит в отказ по имени ограничения (З5 п.5).
package noticerepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/corelib/db/pgfault"
	"github.com/PRO-Robotech/corelib/operations"

	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice"
	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/publicnotice"
	"github.com/PRO-Robotech/kacho/services/notify/internal/notice/rules"
)

// Repo — хранилище извещений.
type Repo struct {
	pool *pgxpool.Pool
	ops  operations.TxWriter
}

// New — хранилище над пулом kacho_notify и записью операций в транзакции
// вызывающего (тот же `operations.NewRepo`, что отдаётся обработчику операций).
func New(pool *pgxpool.Pool, ops operations.TxWriter) *Repo {
	return &Repo{pool: pool, ops: ops}
}

var (
	_ notice.Store        = (*Repo)(nil)
	_ publicnotice.Reader = (*Repo)(nil)
)

// querier — общий для пула и транзакции интерфейс чтения.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// txOptions — уровень транзакций записи (З3 п.1, З5 п.1).
var txOptions = pgx.TxOptions{IsoLevel: pgx.ReadCommitted}

// inTx исполняет fn в транзакции записи; ошибка fn — откат целиком.
func (r *Repo) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := r.pool.BeginTx(ctx, txOptions)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// nonNil — пустой список вместо nil: столбцы-массивы заявки NOT NULL.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Accept — заявка Create и операция done=false одной транзакцией (З3 п.1).
// Заявка вставляется без аренды: первое же взятие notify-sender её видит.
func (r *Repo) Accept(ctx context.Context, req notice.CreateRequest, op operations.Operation, p operations.Principal) error {
	types := make([]string, len(req.Affected))
	refIDs := make([]string, len(req.Affected))
	for i, a := range req.Affected {
		types[i], refIDs[i] = a.Type, a.ID
	}
	return r.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO notice_create_requests
			(notice_id, operation_id, kind, starts_at, ends_at, audience_all,
			 audience_account_ids, audience_project_ids, affected_types, affected_ids,
			 created_by, accepted_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			req.NoticeID, req.OperationID, string(req.Kind), req.StartsAt, req.EndsAt, req.AllAccounts,
			nonNil(req.AccountIDs), nonNil(req.ProjectIDs), nonNil(types), nonNil(refIDs),
			req.CreatedBy, req.AcceptedAt); err != nil {
			return fmt.Errorf("insert notice_create_requests: %w", err)
		}
		if err := r.ops.CreatePendingTx(ctx, tx, op, p); err != nil {
			return fmt.Errorf("create pending operation: %w", err)
		}
		return nil
	})
}

// noticeColumns — столбцы строки извещения в порядке [scanNotice].
const noticeColumns = `n.id, n.kind, n.state, n.starts_at, n.ends_at, n.audience_all, n.revision,
	n.created_by, n.created_at, n.updated_at, n.started_at, n.completed_at, n.cancelled_at`

func scanNotice(row pgx.Row) (notice.Notice, error) {
	var (
		n           notice.Notice
		kind, state string
	)
	if err := row.Scan(&n.ID, &kind, &state, &n.StartsAt, &n.EndsAt, &n.AllAccounts, &n.Revision,
		&n.CreatedBy, &n.CreatedAt, &n.UpdatedAt, &n.StartedAt, &n.CompletedAt, &n.CancelledAt); err != nil {
		return notice.Notice{}, err
	}
	n.Kind, n.State = rules.Kind(kind), rules.State(state)
	return n, nil
}

// one — извещение по id во внутренней проекции; ErrNotFound — строки нет.
func one(ctx context.Context, q querier, id string) (notice.Notice, error) {
	n, err := scanNotice(q.QueryRow(ctx, `SELECT `+noticeColumns+` FROM notices n WHERE n.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return notice.Notice{}, notice.ErrNotFound
	}
	if err != nil {
		return notice.Notice{}, fmt.Errorf("select notice: %w", err)
	}
	ns := []notice.Notice{n}
	if err := details(ctx, q, ns); err != nil {
		return notice.Notice{}, err
	}
	return ns[0], nil
}

// many — строки страницы и их подзаписи.
func many(ctx context.Context, q querier, sql string, args ...any) ([]notice.Notice, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("select notices: %w", err)
	}
	var out []notice.Notice
	for rows.Next() {
		n, err := scanNotice(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan notice: %w", err)
		}
		out = append(out, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("select notices: %w", err)
	}
	if err := details(ctx, q, out); err != nil {
		return nil, err
	}
	return out, nil
}

// details дочитывает аудиторию, затронутые ресурсы и напоминания страницы —
// по оператору на подзапись, в порядке, который объявляет контракт: аудитория
// и ссылки — порядок оператора (ordinal), напоминания — по возрастанию момента.
func details(ctx context.Context, q querier, ns []notice.Notice) error {
	if len(ns) == 0 {
		return nil
	}
	ids := make([]string, len(ns))
	at := make(map[string]int, len(ns))
	for i, n := range ns {
		ids[i] = n.ID
		at[n.ID] = i
	}
	if err := eachRow(ctx, q, `SELECT notice_id, scope_type, scope_id FROM notice_audience
		WHERE notice_id = ANY($1) ORDER BY notice_id, ordinal`, ids, func(row pgx.Rows) error {
		var id string
		var s notice.Scope
		if err := row.Scan(&id, &s.Type, &s.ID); err != nil {
			return err
		}
		ns[at[id]].Audience = append(ns[at[id]].Audience, s)
		return nil
	}); err != nil {
		return fmt.Errorf("select notice_audience: %w", err)
	}
	if err := eachRow(ctx, q, `SELECT notice_id, resource_type, resource_id FROM notice_affected_resources
		WHERE notice_id = ANY($1) ORDER BY notice_id, ordinal`, ids, func(row pgx.Rows) error {
		var id string
		var ref notice.Ref
		if err := row.Scan(&id, &ref.Type, &ref.ID); err != nil {
			return err
		}
		ns[at[id]].Affected = append(ns[at[id]].Affected, ref)
		return nil
	}); err != nil {
		return fmt.Errorf("select notice_affected_resources: %w", err)
	}
	if err := eachRow(ctx, q, `SELECT notice_id, at FROM notice_reminders
		WHERE notice_id = ANY($1) ORDER BY notice_id, at`, ids, func(row pgx.Rows) error {
		var id string
		var t time.Time
		if err := row.Scan(&id, &t); err != nil {
			return err
		}
		ns[at[id]].Reminders = append(ns[at[id]].Reminders, t)
		return nil
	}); err != nil {
		return fmt.Errorf("select notice_reminders: %w", err)
	}
	return nil
}

func eachRow(ctx context.Context, q querier, sql string, ids []string, fn func(pgx.Rows) error) error {
	rows, err := q.Query(ctx, sql, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// Get — извещение во внутренней проекции.
func (r *Repo) Get(ctx context.Context, id string) (notice.Notice, error) {
	return one(ctx, r.pool, id)
}

// List — страница всех извещений, keyset (created_at, id) по индексу
// notices_created_idx.
func (r *Repo) List(ctx context.Context, p notice.Page) ([]notice.Notice, error) {
	if p.AfterID == "" {
		return many(ctx, r.pool, `SELECT `+noticeColumns+` FROM notices n
			ORDER BY n.created_at, n.id LIMIT $1`, p.Limit)
	}
	return many(ctx, r.pool, `SELECT `+noticeColumns+` FROM notices n
		WHERE (n.created_at, n.id) > ($1, $2)
		ORDER BY n.created_at, n.id LIMIT $3`, p.AfterCreatedAt, p.AfterID, p.Limit)
}

// visibleFrom — предикат видимости Р16 для области: проект — аудитория P;
// аккаунт — аудитория A либо проект с записанным аккаунтом A (столбец
// account_id несёт аккаунт обеих форм); у обеих — «все аккаунты».
// $1 — id области. Поиск — по индексам notice_audience_scope_idx и
// notice_audience_account_idx.
func visibleFrom(s notice.Scope) (string, error) {
	switch s.Type {
	case notice.ScopeProject:
		return `(n.audience_all OR EXISTS (SELECT 1 FROM notice_audience a
			WHERE a.notice_id = n.id AND a.scope_type = 'project' AND a.scope_id = $1))`, nil
	case notice.ScopeAccount:
		return `(n.audience_all OR EXISTS (SELECT 1 FROM notice_audience a
			WHERE a.notice_id = n.id AND a.account_id = $1))`, nil
	}
	return "", fmt.Errorf("noticerepo: область вида %q вне перечня", s.Type)
}

// ListVisible — страница видимых из области извещений.
func (r *Repo) ListVisible(ctx context.Context, s notice.Scope, p notice.Page) ([]notice.Notice, error) {
	pred, err := visibleFrom(s)
	if err != nil {
		return nil, err
	}
	if p.AfterID == "" {
		return many(ctx, r.pool, `SELECT `+noticeColumns+` FROM notices n WHERE `+pred+`
			ORDER BY n.created_at, n.id LIMIT $2`, s.ID, p.Limit)
	}
	return many(ctx, r.pool, `SELECT `+noticeColumns+` FROM notices n WHERE `+pred+`
		AND (n.created_at, n.id) > ($2, $3)
		ORDER BY n.created_at, n.id LIMIT $4`, s.ID, p.AfterCreatedAt, p.AfterID, p.Limit)
}

// GetVisible — извещение, видимое из области, одной выборкой с предикатом в
// WHERE (З13 п.1): невидимое и несуществующее — один ErrNotFound.
func (r *Repo) GetVisible(ctx context.Context, s notice.Scope, id string) (notice.Notice, error) {
	pred, err := visibleFrom(s)
	if err != nil {
		return notice.Notice{}, err
	}
	ns, err := many(ctx, r.pool, `SELECT `+noticeColumns+` FROM notices n WHERE `+pred+` AND n.id = $2`, s.ID, id)
	if err != nil {
		return notice.Notice{}, err
	}
	if len(ns) == 0 {
		return notice.Notice{}, notice.ErrNotFound
	}
	return ns[0], nil
}

// transitSQL — запись перехода: одна строка с условием на прежнее состояние и
// вид (З5 п.1). Состояние до записи не читается. Столбец момента — по глаголу,
// текст запроса неизменен (закрытый switch, а не подстановка имени).
func transitSQL(v rules.Verb) (string, error) {
	switch v {
	case rules.VerbStart:
		return `UPDATE notices SET state = $2, started_at = $3, updated_at = $3
			WHERE id = $1 AND state = $4 AND kind = ANY($5) RETURNING kind, revision`, nil
	case rules.VerbComplete:
		return `UPDATE notices SET state = $2, completed_at = $3, updated_at = $3
			WHERE id = $1 AND state = $4 AND kind = ANY($5) RETURNING kind, revision`, nil
	case rules.VerbCancel:
		return `UPDATE notices SET state = $2, cancelled_at = $3, updated_at = $3
			WHERE id = $1 AND state = $4 AND kind = ANY($5) RETURNING kind, revision`, nil
	case rules.VerbUpdate:
	}
	return "", fmt.Errorf("noticerepo: глагол %s — не переход состояния", v)
}

// refused — классификация нуля строк записи чтением той же транзакции ПОСЛЕ
// оператора (З5 п.2): строки нет — ErrNotFound; иначе фактические вид и
// состояние, по которым use-case выбирает текст отказа.
func refused(ctx context.Context, tx pgx.Tx, id string) error {
	var kind, state string
	err := tx.QueryRow(ctx, `SELECT kind, state FROM notices WHERE id = $1`, id).Scan(&kind, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return notice.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("select refused notice: %w", err)
	}
	return &notice.TransitionRefusal{Kind: rules.Kind(kind), State: rules.State(state)}
}

// closeEvents закрывает незакрытые события этапов, которые переход делает
// неактуальными (Р5, З6 п.2): закрытое событие раскрытием больше не берётся.
// maxRevision > 0 — только события ревизий не новее (Update).
func closeEvents(ctx context.Context, tx pgx.Tx, id string, stages []rules.Stage, now time.Time, maxRevision int64) error {
	if len(stages) == 0 {
		return nil
	}
	var err error
	if maxRevision > 0 {
		_, err = tx.Exec(ctx, `UPDATE notice_stage_events SET closed_at = $2
			WHERE notice_id = $1 AND closed_at IS NULL AND stage = ANY($3) AND revision <= $4`,
			id, now, rules.StageNames(stages), maxRevision)
	} else {
		_, err = tx.Exec(ctx, `UPDATE notice_stage_events SET closed_at = $2
			WHERE notice_id = $1 AND closed_at IS NULL AND stage = ANY($3)`,
			id, now, rules.StageNames(stages))
	}
	if err != nil {
		return fmt.Errorf("close stage events: %w", err)
	}
	return nil
}

// stageEvent — событие этапа с письмом (З6 п.1): due_at — now этой транзакции.
func stageEvent(ctx context.Context, tx pgx.Tx, id string, stage rules.Stage, revision int64, now time.Time) error {
	if _, err := tx.Exec(ctx, `INSERT INTO notice_stage_events (notice_id, stage, revision, due_at)
		VALUES ($1, $2, $3, $4)`, id, string(stage), revision, now); err != nil {
		return fmt.Errorf("insert stage event: %w", err)
	}
	return nil
}

// finish пишет операцию, рождённую завершённой (Ф2), с ответом — извещением
// после записи, прочитанным той же транзакцией.
func (r *Repo) finish(ctx context.Context, tx pgx.Tx, id string, p operations.Principal, fin notice.Finisher) (notice.Notice, error) {
	n, err := one(ctx, tx, id)
	if err != nil {
		return notice.Notice{}, err
	}
	op, resp, err := fin(n)
	if err != nil {
		return notice.Notice{}, err
	}
	if err := r.ops.CreateDoneTx(ctx, tx, op, p, resp); err != nil {
		return notice.Notice{}, fmt.Errorf("create done operation: %w", err)
	}
	return n, nil
}

// Transit — переход Start/Complete/Cancel (З5 п.1–3).
func (r *Repo) Transit(ctx context.Context, in notice.TransitInput, p operations.Principal, fin notice.Finisher) (notice.Notice, error) {
	t := in.Transition
	sql, err := transitSQL(t.Verb)
	if err != nil {
		return notice.Notice{}, err
	}
	var out notice.Notice
	err = r.inTx(ctx, func(tx pgx.Tx) error {
		var (
			kind     string
			revision int64
		)
		err := tx.QueryRow(ctx, sql, in.ID, string(t.To), in.Now, string(t.From), t.KindNames()).Scan(&kind, &revision)
		if errors.Is(err, pgx.ErrNoRows) {
			return refused(ctx, tx, in.ID)
		}
		if err != nil {
			return r.constraint(ctx, in.ID, err)
		}
		k := rules.Kind(kind)
		if err := closeEvents(ctx, tx, in.ID, rules.Superseded(t.Verb, k), in.Now, 0); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM notice_reminders WHERE notice_id = $1`, in.ID); err != nil {
			return fmt.Errorf("delete reminders: %w", err)
		}
		if stage, letter := rules.Letter(t.Verb, k); letter {
			if err := stageEvent(ctx, tx, in.ID, stage, revision, in.Now); err != nil {
				return err
			}
		}
		out, err = r.finish(ctx, tx, in.ID, p, fin)
		return err
	})
	return out, err
}

// Update — перенос моментов (З5 п.4). Запись меняет строку, только если
// значения действительно меняются; ноль строк классифицируется чтением после
// оператора: годные состояние и вид при совпавших значениях — успех без
// ревизии и без события (Р5), операция пишется и тогда.
func (r *Repo) Update(ctx context.Context, in notice.UpdateInput, p operations.Principal, fin notice.Finisher) (notice.Notice, error) {
	t := rules.TransitionOf(rules.VerbUpdate)
	var out notice.Notice
	err := r.inTx(ctx, func(tx pgx.Tx) error {
		var (
			kind     string
			revision int64
			startsAt time.Time
		)
		err := tx.QueryRow(ctx, `UPDATE notices
			SET starts_at = COALESCE($2::timestamptz, starts_at),
			    ends_at = COALESCE($3::timestamptz, ends_at),
			    revision = revision + 1,
			    updated_at = $4
			WHERE id = $1 AND state = $5 AND kind = ANY($6)
			  AND (starts_at, ends_at) IS DISTINCT FROM
			      (COALESCE($2::timestamptz, starts_at), COALESCE($3::timestamptz, ends_at))
			RETURNING kind, revision, starts_at`,
			in.ID, in.StartsAt, in.EndsAt, in.Now, string(t.From), t.KindNames()).Scan(&kind, &revision, &startsAt)
		if errors.Is(err, pgx.ErrNoRows) {
			if rerr := refused(ctx, tx, in.ID); !unchanged(rerr, t) {
				return rerr
			}
			out, err = r.finish(ctx, tx, in.ID, p, fin)
			return err
		}
		if err != nil {
			return r.constraint(ctx, in.ID, err)
		}
		k := rules.Kind(kind)
		if err := closeEvents(ctx, tx, in.ID, rules.Superseded(rules.VerbUpdate, k), in.Now, revision-1); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM notice_reminders WHERE notice_id = $1`, in.ID); err != nil {
			return fmt.Errorf("delete reminders: %w", err)
		}
		for _, at := range rules.Reminders(k, startsAt, in.Now, in.Lead) {
			if _, err := tx.Exec(ctx, `INSERT INTO notice_reminders (notice_id, at, revision) VALUES ($1, $2, $3)`,
				in.ID, at, revision); err != nil {
				return fmt.Errorf("insert reminder: %w", err)
			}
		}
		if stage, letter := rules.Letter(rules.VerbUpdate, k); letter {
			if err := stageEvent(ctx, tx, in.ID, stage, revision, in.Now); err != nil {
				return err
			}
		}
		out, err = r.finish(ctx, tx, in.ID, p, fin)
		return err
	})
	return out, err
}

// unchanged — ноль строк Update при годных состоянии и виде: значения совпали
// с хранимыми (условие IS DISTINCT FROM).
func unchanged(err error, t rules.Transition) bool {
	var tr *notice.TransitionRefusal
	return errors.As(err, &tr) && t.Supports(tr.Kind) && tr.State == t.From
}

// constraint переводит отказ записи по именованному ограничению `notices` в
// ConstraintRefusal. Вид — для текста «не разрешено для вида» — читается
// отдельно: он неизменяем, а транзакция записи после отказа уже прервана.
func (r *Repo) constraint(ctx context.Context, id string, err error) error {
	f := pgfault.Classify(err)
	if !f.Is(pgfault.Check) || f.Table != "notices" {
		return fmt.Errorf("write notice: %w", err)
	}
	var kind string
	if rerr := r.pool.QueryRow(ctx, `SELECT kind FROM notices WHERE id = $1`, id).Scan(&kind); rerr != nil {
		return fmt.Errorf("write notice: %w", err)
	}
	return &notice.ConstraintRefusal{Constraint: f.Constraint, Kind: rules.Kind(kind)}
}
