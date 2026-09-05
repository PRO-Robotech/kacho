// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/pgtest"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
)

// Integration-тесты CQRS-impl `internal/repo/kacho/pg`. Покрывают:
//   - Reader видит Insert после Commit;
//   - Writer без Commit не виден параллельному Reader (read-committed);
//   - Abort() rollback'ит INSERT;
//   - outbox.Emit транзакционен с DML (Abort → outbox-row не вставлена).

// setupTestDB выдаёт тесту собственную базу на одном контейнере пакета: миграции
// уже применены в шаблоне, клон — отдельная база (свой каталог, свои строки, своё
// пространство advisory-lock), поэтому CAS / UNIQUE / EXCLUDE / SKIP LOCKED
// доказательства этого пакета видят ровно ту же изоляцию, что давал отдельный
// контейнер.
func setupTestDB(t testing.TB) string {
	t.Helper()
	dsn := pgtest.NewDB(t)

	// Учёт числа ресурсов: вставка строки ресурса СПИСЫВАЕТ место, и списать его
	// не с чего, пока у проекта нет строки учёта. На живом пути её заводит
	// материализация ПЕРЕД writer-транзакцией; проба идёт мимо use-case'а, прямо
	// в репозиторий, поэтому базу в то же состояние приводит фикстура. Разбор,
	// перечень идентичностей и что делать новой пробе — `quota_fixture_test.go`.
	seedFixtureQuotas(t, dsn)

	return dsn
}

func newNetwork(projectID, name string) *domain.Network {
	return &domain.Network{
		ID:          ids.NewID(ids.PrefixNetwork),
		ProjectID:   projectID,
		Name:        domain.RcNameVPC(name),
		Description: domain.RcDescription(""),
		Labels:      domain.LabelsFromMap(nil),
	}
}

// TestCQRS_Network_WriterCommit_ReaderSees — Writer.Insert + Commit; параллельный
// Reader видит запись.
func TestCQRS_Network_WriterCommit_ReaderSees(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := kachopg.New(pool, nil)

	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()

	n := newNetwork("project-1", "net-1")
	created, err := w.Networks().Insert(ctx, n)
	require.NoError(t, err)
	assert.Equal(t, n.ID, created.ID)
	// outbox emit в той же TX.
	require.NoError(t, w.Outbox().Emit(ctx, "Network", created.ID, created.ProjectID, "CREATED", map[string]any{"id": created.ID}))
	require.NoError(t, w.Commit())

	// Параллельный Reader видит committed запись.
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	got, err := rd.Networks().Get(ctx, n.ID)
	require.NoError(t, err)
	assert.Equal(t, n.ID, got.ID)
	assert.Equal(t, domain.RcNameVPC("net-1"), got.Name)
}

// TestCQRS_Network_WriterUncommitted_ReaderNotSees — Writer.Insert без Commit;
// параллельный Reader НЕ видит запись (read-committed).
func TestCQRS_Network_WriterUncommitted_ReaderNotSees(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := kachopg.New(pool, nil)

	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()

	n := newNetwork("project-1", "net-uncommitted")
	_, err = w.Networks().Insert(ctx, n)
	require.NoError(t, err)
	// Внутри writer'а — видно (writer видит свои writes).
	gotInWriter, err := w.Networks().Get(ctx, n.ID)
	require.NoError(t, err)
	assert.Equal(t, n.ID, gotInWriter.ID)

	// Снаружи — НЕ видно. Используем СВОЙ Reader, который открыт новой TX
	// на этом же pool (separate connection).
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	// Установим маленький deadline на Get — если уровень изоляции слабее,
	// мы бы получили запись; здесь должны получить NotFound.
	getCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, gerr := rd.Networks().Get(getCtx, n.ID)
	require.Error(t, gerr, "Reader should NOT see uncommitted writer's INSERT (read-committed)")
}

// TestCQRS_Network_WriterAbort_RollbacksInsert — Abort() rollback'ит INSERT;
// запись не появляется в БД.
func TestCQRS_Network_WriterAbort_RollbacksInsert(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := kachopg.New(pool, nil)

	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()

	n := newNetwork("project-1", "net-abort")
	_, err = w.Networks().Insert(ctx, n)
	require.NoError(t, err)
	w.Abort() // rollback

	// После Abort — Reader не видит запись.
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	_, gerr := rd.Networks().Get(ctx, n.ID)
	require.Error(t, gerr)
}

// TestCQRS_Network_OutboxAtomicityWithDML — Emit в той же TX, что и DML;
// Abort выкидывает и outbox-row.
func TestCQRS_Network_OutboxAtomicityWithDML(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := kachopg.New(pool, nil)

	// 1) Insert + Emit + Commit → outbox-row есть.
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	n := newNetwork("project-1", "net-outbox-commit")
	_, err = w.Networks().Insert(ctx, n)
	require.NoError(t, err)
	require.NoError(t, w.Outbox().Emit(ctx, "Network", n.ID, n.ProjectID, "CREATED", map[string]any{"id": n.ID}))
	require.NoError(t, w.Commit())

	// Проверяем outbox через прямой SQL — pkg pg/network.go не экспортирует
	// outbox read API (это домен Watch handler'а).
	var count1 int
	err = pool.QueryRow(ctx, "SELECT count(*) FROM vpc_outbox WHERE resource_id = $1", n.ID).Scan(&count1)
	require.NoError(t, err)
	assert.Equal(t, 1, count1, "committed Emit должен вставить outbox-row")

	// 2) Insert + Emit + Abort → НИ DML, НИ outbox-row не должны остаться.
	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	n2 := newNetwork("project-1", "net-outbox-abort")
	_, err = w2.Networks().Insert(ctx, n2)
	require.NoError(t, err)
	require.NoError(t, w2.Outbox().Emit(ctx, "Network", n2.ID, n2.ProjectID, "CREATED", map[string]any{"id": n2.ID}))
	w2.Abort()

	var count2 int
	err = pool.QueryRow(ctx, "SELECT count(*) FROM vpc_outbox WHERE resource_id = $1", n2.ID).Scan(&count2)
	require.NoError(t, err)
	assert.Equal(t, 0, count2, "aborted Emit не должен вставить outbox-row")

	// И запись network — тоже отсутствует.
	var nCount int
	err = pool.QueryRow(ctx, "SELECT count(*) FROM networks WHERE id = $1", n2.ID).Scan(&nCount)
	require.NoError(t, err)
	assert.Equal(t, 0, nCount, "aborted Insert не должен вставить network-row")
}

// TestCQRS_Network_UpdateDelete_FullCycle — Insert → Update → Delete, проверяем
// что каждый шаг через writer виден после Commit.
func TestCQRS_Network_UpdateDelete_FullCycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := kachopg.New(pool, nil)

	// Insert.
	w1, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w1.Abort()
	n := newNetwork("project-1", "net-cycle")
	created, err := w1.Networks().Insert(ctx, n)
	require.NoError(t, err)
	require.NoError(t, w1.Outbox().Emit(ctx, "Network", created.ID, created.ProjectID, "CREATED", map[string]any{"id": created.ID}))
	require.NoError(t, w1.Commit())

	// Update.
	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	created.Name = domain.RcNameVPC("net-cycle-updated")
	updated, err := w2.Networks().Update(ctx, &created.Network)
	require.NoError(t, err)
	assert.Equal(t, domain.RcNameVPC("net-cycle-updated"), updated.Name)
	require.NoError(t, w2.Outbox().Emit(ctx, "Network", updated.ID, updated.ProjectID, "UPDATED", map[string]any{"id": updated.ID}))
	require.NoError(t, w2.Commit())

	// Delete.
	w3, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w3.Abort()
	require.NoError(t, w3.Networks().Delete(ctx, n.ID))
	require.NoError(t, w3.Outbox().Emit(ctx, "Network", n.ID, n.ProjectID, "DELETED", map[string]any{"id": n.ID}))
	require.NoError(t, w3.Commit())

	// Reader не видит запись после Delete.
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	_, gerr := rd.Networks().Get(ctx, n.ID)
	require.Error(t, gerr)
}

// TestCQRS_Network_SetDefaultSGID_AtomicWithSG — узкий update-помощник
// SetDefaultSGID работает в той же writer-TX, в которой создан и сам SG (атомарное
// создание default-SG). Без SetDefaultSGID пришлось бы делать полноценный
// Networks().Update(...) — и риск перезаписать name/description/labels чем-то
// промежуточным.
func TestCQRS_Network_SetDefaultSGID_AtomicWithSG(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := kachopg.New(pool, nil)

	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()

	n := newNetwork("project-setdefault", "net-setdefault")
	created, err := w.Networks().Insert(ctx, n)
	require.NoError(t, err)
	originalName := created.Name

	// Insert default SG first (FK Network.default_security_group_id →
	// security_groups.id), затем SetDefaultSGID.
	sgDom := domain.NewDefaultSecurityGroup(ids.NewID(ids.PrefixSecurityGroup), created.Network)
	sgRec, err := w.SecurityGroups().Insert(ctx, &sgDom)
	require.NoError(t, err)

	upd, err := w.Networks().SetDefaultSGID(ctx, created.ID, sgRec.ID)
	require.NoError(t, err)
	assert.Equal(t, sgRec.ID, upd.DefaultSecurityGroupID)
	// SetDefaultSGID — узкий UPDATE; name/description не должны меняться.
	assert.Equal(t, originalName, upd.Name, "SetDefaultSGID не должен менять name")
	require.NoError(t, w.Commit())

	// Проверка committed-state.
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	got, err := rd.Networks().Get(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, sgRec.ID, got.DefaultSecurityGroupID)
	assert.Equal(t, originalName, got.Name)
}

// Assertion: реализация удовлетворяет интерфейсу (compile-time check).
var _ kacho.Repository = (*kachopg.Repository)(nil)
