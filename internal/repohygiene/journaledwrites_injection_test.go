// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// journaledwrites_injection_test.go — гейт УК3-27 доказан инъекцией в обе стороны
// на синтетическом дереве в t.TempDir(): законный стенд молчит и печатает
// непустую перепись; каждый дефект, по одному, краснеет своим правилом и
// называет координату; законный близнец той же формы молчит.
//
// Три инъекции, которых требует маршрут полосы S1-A2: автокоммитная запись в
// журналируемую таблицу, открытие транзакции мимо помощника, третий вызывающий
// `AsComponent` — в цикле `FinishStuckDeletes` compute. Сверх них — правила (в),
// (г) и пустой обход.
//
// Тексты SQL, которые правило (г) обязано найти, собираются в этом файле
// СЛОЖЕНИЕМ: гейт читает строковые литералы ствола, и литерал целиком нашёл бы
// сам этот файл.

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type jwStand struct{ root string }

func (s *jwStand) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(s.root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
}

func (s *jwStand) audit(t *testing.T) ([]JournaledWriteFinding, JournaledWriteCensus) {
	t.Helper()
	var log strings.Builder
	f, c, err := AuditJournaledWrites(JournaledWriteOptions{Root: s.root, TrunkRoots: []string{"services", "deploy"}}, &log)
	require.NoError(t, err)
	t.Log("\n" + log.String())
	return f, c
}

const jwStorageJournalMigration = `-- +goose Up
CREATE TABLE kacho_storage.storage_outbox (sequence_no bigserial, resource_kind text, resource_id text, event_type text);
CREATE TABLE kacho_storage.volumes (id text);
CREATE TABLE kacho_storage.disk_types (id text);
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kacho_storage.storage_outbox_emit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO kacho_storage.storage_outbox (resource_kind, resource_id, event_type)
    VALUES ('Volume', NEW.id, 'UPDATED');
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER volumes_storage_outbox_emit
    AFTER INSERT OR UPDATE OR DELETE ON kacho_storage.volumes
    FOR EACH ROW EXECUTE FUNCTION kacho_storage.storage_outbox_emit();
`

// Законный стенд: три модуля, у каждого журнал; запись — помощником; чтение —
// ReadOnly; точка сохранения — на транзакции; две пары AsComponent.
func newJWStand(t *testing.T) *jwStand {
	t.Helper()
	s := &jwStand{root: t.TempDir()}
	for _, m := range []struct{ mod, table string }{
		{"storage", "kacho_storage.storage_outbox"},
		{"nlb", "kacho_nlb.nlb_outbox"},
		{"compute", "compute_outbox"},
	} {
		s.write(t, "services/"+m.mod+"/internal/subscriptionjournal/journal.go",
			"package subscriptionjournal\n\nconst Table = \""+m.table+"\"\n")
	}
	s.write(t, "services/storage/internal/migrations/0001_journal.sql", jwStorageJournalMigration)

	s.write(t, "services/storage/internal/repo/volume.go", `package repo

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/corelib/journaltx"
)

type VolumeRepo struct {
	pool *pgxpool.Pool
	opts journaltx.Options
}

const qMark = "UPDATE volumes SET state = 'ERROR' WHERE id = $1"

func (r *VolumeRepo) MarkError(ctx context.Context, id string) error {
	tx, err := journaltx.Begin(ctx, r.pool, r.opts)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, qMark, id); err != nil {
		return err
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return err
	}
	_ = sp.Rollback(ctx)
	return tx.Commit(ctx)
}

func (r *VolumeRepo) Get(ctx context.Context, id string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var n int
	return r.pool.QueryRow(ctx, "SELECT count(*) FROM volumes WHERE id = $1 FOR UPDATE", id).Scan(&n)
}

func (r *VolumeRepo) TouchDiskType(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, "UPDATE disk_types SET id = id WHERE id = $1", id)
	return err
}
`)
	s.write(t, "services/storage/internal/reconciler/loop.go", `package reconciler

import (
	"context"

	"github.com/PRO-Robotech/corelib/journaltx"
)

const (
	componentService = "storage"
	componentRole    = "reconciler"
)

func Once(ctx context.Context) (context.Context, error) {
	return journaltx.AsComponent(ctx, componentService, componentRole)
}
`)
	s.write(t, "services/nlb/internal/jobs/free_ip_runner.go", `package jobs

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/corelib/journaltx"
)

type FreeIPRunner struct {
	pool *pgxpool.Pool
	opts journaltx.Options
}

func (r *FreeIPRunner) reconcileOne(ctx context.Context) error {
	ctx, err := journaltx.AsComponent(ctx, "nlb", "free-ip-runner")
	if err != nil {
		return err
	}
	tx, err := journaltx.Begin(ctx, r.pool, r.opts)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
`)
	s.write(t, "services/compute/internal/repo/instance.go", `package repo

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/corelib/journaltx"
)

type InstanceRepo struct {
	pool *pgxpool.Pool
	opts journaltx.Options
}

func (r *InstanceRepo) FinishStuckDeletes(ctx context.Context) error {
	for i := 0; i < 1; i++ {
		tx, err := journaltx.Begin(ctx, r.pool, r.opts)
		if err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
`)
	// Законная установка инициатора: локально к транзакции; и правило (г) не
	// путает её с сессионной.
	s.write(t, "deploy/seed/seed.sql", "BEGIN;\nSELECT set_"+"config('kacho_journal.initiator', 'system:stand-seed', true);\nSET LOCAL kacho_journal.initiator = 'system:stand-seed';\nCOMMIT;\n")
	return s
}

func jwOnly(findings []JournaledWriteFinding, rule string) []JournaledWriteFinding {
	var out []JournaledWriteFinding
	for _, f := range findings {
		if f.Rule == rule {
			out = append(out, f)
		}
	}
	return out
}

// TestJournaledWritesGateIsSilentOnTheLawfulStand — законный близнец всех
// инъекций: ни одной находки, перепись непуста по каждому модулю.
func TestJournaledWritesGateIsSilentOnTheLawfulStand(t *testing.T) {
	findings, census := newJWStand(t).audit(t)
	require.Empty(t, JournaledWritePremiseFailures(census))
	require.Empty(t, findings)
	st, ok := census.Module("storage")
	require.True(t, ok)
	require.Equal(t, []string{"storage_outbox", "volumes"}, st.JournaledTables, "журналируемые таблицы выведены из триггера")
	require.Equal(t, 1, st.BeginReadOnly)
	require.Equal(t, 1, st.BeginOnTx, "точка сохранения на транзакции")
	require.Equal(t, 2, st.PoolStatements, "QueryRow и Exec на пуле осмотрены")
	require.Len(t, st.AsComponent, 1)
	nlb, _ := census.Module("nlb")
	require.Len(t, nlb.AsComponent, 1)
	require.Equal(t, 1, census.SetConfigCalls, "локальный вызов set_config посева осмотрен и не находка")
}

// TestJournaledWritesGateCatchesAnAutocommitJournaledWrite — инъекция 1:
// автокоммитная запись в журналируемую таблицу на пуле (М1 сверщика storage).
func TestJournaledWritesGateCatchesAnAutocommitJournaledWrite(t *testing.T) {
	s := newJWStand(t)
	s.write(t, "services/storage/internal/reconciler/store.go", `package reconciler

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func (s *Store) MarkError(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, "UPDATE kacho_storage.volumes SET state = 'ERROR' WHERE id = $1", id)
	return err
}

func (s *Store) Forget(ctx context.Context, table, id string) error {
	q := fmt.Sprintf("DELETE FROM %s WHERE id = $1", table)
	_, err := s.pool.Exec(ctx, q, id)
	return err
}
`)
	findings, _ := s.audit(t)
	got := jwOnly(findings, JWRulePoolWrite)
	require.Len(t, got, 2, "%v", findings)
	require.Equal(t, "services/storage/internal/reconciler/store.go:13", got[0].Pos)
	require.Contains(t, got[0].Detail, "журналируемую таблицу volumes")
	require.Equal(t, "services/storage/internal/reconciler/store.go:19", got[1].Pos)
	require.Contains(t, got[1].Detail, "не установленную разбором")
	require.Len(t, findings, 2, "инъекция роняет только своё правило")
}

// TestJournaledWritesGateCatchesATransactionOpenedPastTheHelper — инъекция 2:
// открытие транзакции мимо помощника — методом пула и пакетной функцией pgx.
func TestJournaledWritesGateCatchesATransactionOpenedPastTheHelper(t *testing.T) {
	s := newJWStand(t)
	s.write(t, "services/storage/internal/repo/snapshot.go", `package repo

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SnapshotRepo struct{ pool *pgxpool.Pool }

func (r *SnapshotRepo) Insert(ctx context.Context) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *SnapshotRepo) Update(ctx context.Context) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error { return nil })
}
`)
	findings, _ := s.audit(t)
	got := jwOnly(findings, JWRuleBegin)
	require.Len(t, got, 2, "%v", findings)
	require.Equal(t, "services/storage/internal/repo/snapshot.go:13", got[0].Pos)
	require.Contains(t, got[0].Detail, "мимо journaltx.Begin")
	require.Equal(t, "services/storage/internal/repo/snapshot.go:21", got[1].Pos)
	require.Len(t, findings, 2)
}

// TestJournaledWritesGateCatchesAThirdComponentCallerInCompute — инъекция 3:
// вызов AsComponent в цикле FinishStuckDeletes compute (CX3H-02).
func TestJournaledWritesGateCatchesAThirdComponentCallerInCompute(t *testing.T) {
	s := newJWStand(t)
	s.write(t, "services/compute/internal/repo/instance.go", `package repo

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/corelib/journaltx"
)

type InstanceRepo struct {
	pool *pgxpool.Pool
	opts journaltx.Options
}

func (r *InstanceRepo) FinishStuckDeletes(ctx context.Context) error {
	for i := 0; i < 1; i++ {
		cctx, err := journaltx.AsComponent(ctx, "compute", "stuck-delete-finisher")
		if err != nil {
			return err
		}
		tx, err := journaltx.Begin(cctx, r.pool, r.opts)
		if err != nil {
			return err
		}
		if err := tx.Commit(cctx); err != nil {
			return err
		}
	}
	return nil
}
`)
	findings, census := s.audit(t)
	got := jwOnly(findings, JWRuleComponent)
	require.Len(t, got, 1, "%v", findings)
	require.Equal(t, "compute", got[0].Module)
	require.Equal(t, "services/compute/internal/repo/instance.go:18", got[0].Pos)
	require.Contains(t, got[0].Detail, "AsComponent(compute, stuck-delete-finisher) вне пар")
	c, _ := census.Module("compute")
	require.Equal(t, []string{"services/compute/internal/repo/instance.go:18 (compute, stuck-delete-finisher)"}, c.AsComponent,
		"перечень вызывающих печатается с парой")
	require.Len(t, findings, 1)
}

// TestJournaledWritesGateCatchesAMissingComponentPair — пара §8 без вызова:
// фоновый путь nlb остался без личности компонента.
func TestJournaledWritesGateCatchesAMissingComponentPair(t *testing.T) {
	s := newJWStand(t)
	s.write(t, "services/nlb/internal/jobs/free_ip_runner.go", "package jobs\n")
	findings, _ := s.audit(t)
	got := jwOnly(findings, JWRuleComponent)
	require.Len(t, got, 1, "%v", findings)
	require.Equal(t, "nlb", got[0].Module)
	require.Contains(t, got[0].Detail, "(nlb, free-ip-runner): 0, ожидается ровно 1")
}

// TestJournaledWritesGateCatchesAnInsertNamingTheInitiatorColumn — правило (в);
// близнец — та же вставка без колонки — в законном стенде (триггер).
func TestJournaledWritesGateCatchesAnInsertNamingTheInitiatorColumn(t *testing.T) {
	s := newJWStand(t)
	s.write(t, "services/nlb/internal/jobs/emit.go", "package jobs\n\nconst qEmit = `INSERT INTO kacho_nlb.nlb_outbox (resource_type, resource_id, action, initiator) VALUES ($1, $2, $3, $4)`\n\nconst qTwin = `INSERT INTO kacho_nlb.nlb_outbox (resource_type, resource_id, action) VALUES ($1, $2, $3)`\n")
	findings, _ := s.audit(t)
	got := jwOnly(findings, JWRuleNamesColumn)
	require.Len(t, got, 1, "%v", findings)
	require.Equal(t, "services/nlb/internal/jobs/emit.go:3", got[0].Pos)
	require.Len(t, findings, 1)
}

// TestJournaledWritesGateCatchesASessionLevelInitiator — правило (г): три формы
// сессионной установки; локальные близнецы — в законном стенде.
func TestJournaledWritesGateCatchesASessionLevelInitiator(t *testing.T) {
	s := newJWStand(t)
	s.write(t, "deploy/seed/bad.sql", "SELECT set_"+"config('kacho_journal.initiator', 'system:stand-seed', false);\nSET "+"kacho_journal.initiator = 'system:stand-seed';\n")
	s.write(t, "services/nlb/cmd/main.go", "package main\n\nconst dsnOpts = \"options=-c "+"kacho_journal.initiator=system:nlb-free-ip-runner\"\n")
	s.write(t, "services/nlb/cmd/main_test.go", "package main\n\nconst controlOpts = \"options=-c "+"kacho_journal.initiator=user:usr-a\"\n")
	findings, _ := s.audit(t)
	got := jwOnly(findings, JWRuleSessionSet)
	require.Len(t, got, 3, "%v", findings)
	var pos []string
	for _, f := range got {
		pos = append(pos, f.Pos)
	}
	require.ElementsMatch(t, []string{"deploy/seed/bad.sql", "deploy/seed/bad.sql", "services/nlb/cmd/main.go:3"}, pos,
		"параметр старта в тестовом файле — законный контроль, не находка")
}

// TestJournaledWritesGateResolvesAFieldPromotedFromAnEmbeddedStruct — получатель
// через поле встроенной структуры пакета (`w.tx`, где `tx` объявлено у встроенного
// читателя). Имя поля в пакете неоднозначно по типу (у писателя рядом —
// `*journaltx.Tx`), поэтому без продвижения полей разбор не установил бы тип и
// назвал бы точку сохранения неосмотренной.
//
// Обе стороны: законный близнец — точка сохранения на продвинутой транзакции —
// молчит и считается точкой сохранения; дефект той же формы — открытие на
// продвинутом ПУЛЕ — краснеет правилом (а) с типом получателя, а не «не
// установлен».
func TestJournaledWritesGateResolvesAFieldPromotedFromAnEmbeddedStruct(t *testing.T) {
	const embedded = `package pg

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/corelib/journaltx"
)

type writerImpl struct {
	tx   *journaltx.Tx
	pool *pgxpool.Conn
}

type groupReader struct {
	tx pgx.Tx
}

type groupWriter struct {
	groupReader
}

func (w *groupWriter) Move(ctx context.Context) error {
	sp, err := w.tx.Begin(ctx)
	if err != nil {
		return err
	}
	return sp.Commit(ctx)
}
`
	s := newJWStand(t)
	s.write(t, "services/nlb/internal/repo/pg/group.go", embedded)
	findings, census := s.audit(t)
	require.Empty(t, findings, "законный близнец: точка сохранения на продвинутой транзакции")
	nlb, _ := census.Module("nlb")
	require.Equal(t, 1, nlb.BeginOnTx, "точка сохранения осмотрена")
	require.Equal(t, 0, nlb.BeginUnresolved)

	s = newJWStand(t)
	s.write(t, "services/nlb/internal/repo/pg/group.go", embedded+`
type poolHolder struct {
	pool *pgxpool.Pool
}

type announceStore struct {
	poolHolder
}

func (a *announceStore) Report(ctx context.Context) error {
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
`)
	findings, _ = s.audit(t)
	got := jwOnly(findings, JWRuleBegin)
	require.Len(t, got, 1, "%v", findings)
	require.Equal(t, "services/nlb/internal/repo/pg/group.go:42", got[0].Pos)
	require.Contains(t, got[0].Detail, "Begin на *pgxpool.Pool")
	require.Len(t, findings, 1)
}

// TestJournaledWritesGateFailsOnAnEmptyWalk — пустое дерево — не вердикт.
func TestJournaledWritesGateFailsOnAnEmptyWalk(t *testing.T) {
	s := &jwStand{root: t.TempDir()}
	findings, census := s.audit(t)
	require.NotEmpty(t, JournaledWritePremiseFailures(census), "пустой обход обязан быть отказом предпосылки")
	require.Len(t, jwOnly(findings, JWRuleComponent), 2, "пары §8 без модулей — находки")
}
