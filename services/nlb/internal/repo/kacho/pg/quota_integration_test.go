// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho/pg"
)

// Учёт числа ресурсов у kacho-nlb: списание, возврат, два исхода отказа и ось
// вложенности «слушателей в одном балансировщике».
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), DoD S4 п.1, сценарии QV2-11/12/13/14/30/31/35/36.
//
// # Почему пробы этого файла заводят строки учёта САМИ
//
// Их предмет — поведение при заведённой и при ОТСУТСТВУЮЩЕЙ строке. Заведи им
// строку общая фикстура пакета (`quota_fixture_test.go`), они утверждали бы про
// состояние, которого не создавали, а исход «потолок не назван» стал бы
// невыразимым. Поэтому их идентичности (`prj-nlbq-*`) в перечень фикстуры не
// входят намеренно.

// seedQuota заводит одну строку учёта ТЕМ ЖЕ оператором, что и продукт.
//
// Своего INSERT здесь нет: копия оператора разошлась бы с настоящим молча, и
// разошлась бы именно на составе столбцов — там, где расхождение не видно
// глазом при чтении диффа.
func seedQuota(t testing.TB, dsn, carrierType, carrierID, kind string, limit int64) {
	t.Helper()
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()

	n, err := kachopg.MaterializeQuotas(ctx, conn, []kacho.QuotaRow{{
		CarrierType:   carrierType,
		CarrierID:     carrierID,
		Kind:          kind,
		Limit:         limit,
		SourceScope:   "DEFAULT",
		SourceScopeID: "",
		LimitRevision: 0,
		AccountID:     "acc-nlbq",
	}})
	require.NoError(t, err)
	require.Equal(t, int64(1), n, "перепись: заведена ровно одна строка учёта")
}

// seedNestedDefault заводит ПРОЕКТНЫЙ резолв вложенного вида — величину, из
// которой триггер берёт снимок, заводя строку учёта нового родителя.
//
// Отдельная таблица, а не строка учёта: величина резолвится по проекту, а
// считается по родителю, поэтому проектного потребления у неё нет — и столбца
// `used`, который бы его выдумывал, тоже.
func seedNestedDefault(t testing.TB, dsn, projectID, kind string, limit int64) {
	t.Helper()
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()

	tag, err := conn.Exec(ctx,
		`INSERT INTO kacho_nlb.nested_quota_defaults
		     (project_id, kind, limit_value, source_scope, source_scope_id,
		      limit_revision, account_id)
		 VALUES ($1, $2, $3, 'DEFAULT', '', 0, 'acc-nlbq')`,
		projectID, kind, limit)
	require.NoError(t, err)
	require.Equal(t, int64(1), tag.RowsAffected())
}

// setQuotaLimit понижает (или поднимает) предел уже заведённой строки —
// административное действие, которое обязано проходить ВСЕГДА, в том числе ниже
// текущего потребления. Именно поэтому потолок живёт в предикате списания, а не
// в `CHECK (used <= limit_value)`: с ограничением схемы этот вызов падал бы на
// 23514, и понижение предела стало бы невыразимым (§1.4 приёмки).
func setQuotaLimit(t testing.TB, dsn, carrierType, carrierID, kind string, limit int64) {
	t.Helper()
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()

	tag, err := conn.Exec(ctx,
		`UPDATE kacho_nlb.project_resource_quotas SET limit_value = $4
		  WHERE carrier_type = $1 AND carrier_id = $2 AND kind = $3`,
		carrierType, carrierID, kind, limit)
	require.NoError(t, err, "понижение предела — штатное административное действие")
	require.Equal(t, int64(1), tag.RowsAffected(), "перепись: правится ровно одна строка")
}

// quotaUsed читает потребление строки учёта. Отсутствие строки — отдельный
// исход, а не ноль: ноль означает «строка есть и пуста».
func quotaUsed(t testing.TB, dsn, carrierType, carrierID, kind string) (int64, bool) {
	t.Helper()
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()

	var used int64
	err = conn.QueryRow(ctx,
		`SELECT used FROM kacho_nlb.project_resource_quotas
		  WHERE carrier_type = $1 AND carrier_id = $2 AND kind = $3`,
		carrierType, carrierID, kind).Scan(&used)
	if err != nil {
		require.ErrorIs(t, err, pgx.ErrNoRows, "чтение строки учёта")
		return 0, false
	}
	return used, true
}

// TestQuota_NLB_NotProvisionedIsRefusal — «не сказано» = ОТКАЗ, а не «без предела».
//
// Это то самое утверждение, которое прецедент compute держал наоборот и которое
// приёмка переворачивает (V2-3). Положительный контроль рядом обязателен: без
// него отрицание зеленело бы и на сломанной вставке.
func TestQuota_NLB_NotProvisionedIsRefusal(t *testing.T) {
	dsn := setupTestDB(t)
	repo, cleanup := newRepo(t, dsn)
	defer cleanup()
	ctx := context.Background()

	const project = "prj-nlbq-noceiling"

	w, err := repo.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	_, err = w.LoadBalancers().Insert(ctx, newLB(project, "lb-no-ceiling"))
	require.Error(t, err, "вставка без строки учёта обязана быть отвергнута")
	assert.ErrorIs(t, err, kacho.ErrQuotaNotProvisioned,
		"исход обязан быть отличим от исчерпания: %v", err)

	// Положительный контроль: тот же путь при заведённой строке проходит.
	dsn2 := setupTestDB(t)
	repo2, cleanup2 := newRepo(t, dsn2)
	defer cleanup2()
	const okProject = "prj-nlbq-ceiling"
	seedQuota(t, dsn2, "project", okProject, "loadbalancer.networkLoadBalancers", 4)

	commitWriter(t, repo2, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, newLB(okProject, "lb-ok"))
		require.NoError(t, err)
	})
	used, ok := quotaUsed(t, dsn2, "project", okProject, "loadbalancer.networkLoadBalancers")
	require.True(t, ok)
	assert.Equal(t, int64(1), used, "вставка списывает ровно одно место")
}

// TestQuota_NLB_ExceededAndRefund — исчерпание отвергает, удаление возвращает.
func TestQuota_NLB_ExceededAndRefund(t *testing.T) {
	dsn := setupTestDB(t)
	repo, cleanup := newRepo(t, dsn)
	defer cleanup()
	ctx := context.Background()

	const project = "prj-nlbq-exhaust"
	const kind = "loadbalancer.networkLoadBalancers"
	seedQuota(t, dsn, "project", project, kind, 1)

	first := newLB(project, "lb-first")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, first)
		require.NoError(t, err)
	})

	w, err := repo.Writer(ctx)
	require.NoError(t, err)
	_, err = w.LoadBalancers().Insert(ctx, newLB(project, "lb-second"))
	require.Error(t, err, "второй балансировщик при пределе в один обязан быть отвергнут")
	assert.ErrorIs(t, err, kacho.ErrQuotaExceeded, "got %v", err)
	w.Abort()

	used, ok := quotaUsed(t, dsn, "project", project, kind)
	require.True(t, ok)
	assert.Equal(t, int64(1), used, "отвергнутая вставка места НЕ занимает")

	// Возврат на удалении — в той же транзакции, что снятие строки ресурса.
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		require.NoError(t, w.LoadBalancers().Delete(ctx, string(first.ID)))
	})
	used, ok = quotaUsed(t, dsn, "project", project, kind)
	require.True(t, ok)
	assert.Equal(t, int64(0), used, "удаление возвращает место")

	// Положительный контроль: освободившееся место снова занимается.
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, newLB(project, "lb-third"))
		require.NoError(t, err)
	})
}

// TestQuota_NLB_NestedCarrierIsTheParent — предел вложенности принадлежит
// РОДИТЕЛЮ, а не проекту (QV2-30).
//
// Положительный контроль здесь несущий: без него «отвергло» неотличимо от
// «отвергает всех», то есть от предела, ошибочно поставленного на проект.
func TestQuota_NLB_NestedCarrierIsTheParent(t *testing.T) {
	dsn := setupTestDB(t)
	repo, cleanup := newRepo(t, dsn)
	defer cleanup()
	ctx := context.Background()

	const project = "prj-nlbq-nested"
	const nested = "loadbalancer.networkLoadBalancers.listeners"
	seedQuota(t, dsn, "project", project, "loadbalancer.networkLoadBalancers", 8)
	seedQuota(t, dsn, "project", project, "loadbalancer.listeners", 32)
	// Проектный резолв вложенного вида: щедрый, чтобы предметом пробы осталась
	// ОСЬ, а не величина. Понижение до одного делается ниже и адресно — ровно
	// тому родителю, про которого проба утверждает.
	seedNestedDefault(t, dsn, project, nested, 8)

	lb1 := newLB(project, "lb-one")
	lb2 := newLB(project, "lb-two")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb1)
		require.NoError(t, err)
		_, err = w.LoadBalancers().Insert(ctx, lb2)
		require.NoError(t, err)
	})

	// Строка учёта родителя заводится ТОЙ ЖЕ транзакцией, что сам родитель, —
	// то есть производитель у неё есть by construction (V2-5).
	used, ok := quotaUsed(t, dsn, nested, string(lb1.ID), nested)
	require.True(t, ok, "строка учёта родителя обязана появиться вместе с родителем")
	assert.Equal(t, int64(0), used)

	// Понижаем предел ровно этого родителя до одного слушателя.
	setQuotaLimit(t, dsn, nested, string(lb1.ID), nested, 1)

	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.Listeners().Insert(ctx, newListener(lb1.ID, project, "lst-1", 80))
		require.NoError(t, err)
	})

	w, err := repo.Writer(ctx)
	require.NoError(t, err)
	_, err = w.Listeners().Insert(ctx, newListener(lb1.ID, project, "lst-2", 81))
	require.Error(t, err, "второй слушатель в исчерпанном балансировщике отвергается")
	assert.ErrorIs(t, err, kacho.ErrQuotaExceeded, "got %v", err)
	w.Abort()

	// Положительный контроль, доказывающий, что предел на РОДИТЕЛЯ: у соседнего
	// балансировщика того же проекта место есть.
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.Listeners().Insert(ctx, newListener(lb2.ID, project, "lst-3", 80))
		require.NoError(t, err)
	})

	// Второй положительный контроль: проектная ось считает обоих вместе.
	used, ok = quotaUsed(t, dsn, "project", project, "loadbalancer.listeners")
	require.True(t, ok)
	assert.Equal(t, int64(2), used, "проектная ось считает слушателей обоих балансировщиков")
}

// TestQuota_NLB_ParentRowsGoWithTheParent — удаление родителя не оставляет
// строк учёта (QV2-32).
func TestQuota_NLB_ParentRowsGoWithTheParent(t *testing.T) {
	dsn := setupTestDB(t)
	repo, cleanup := newRepo(t, dsn)
	defer cleanup()
	ctx := context.Background()

	const project = "prj-nlbq-teardown"
	const nested = "loadbalancer.networkLoadBalancers.listeners"
	seedQuota(t, dsn, "project", project, "loadbalancer.networkLoadBalancers", 4)
	seedNestedDefault(t, dsn, project, nested, 4)

	lb := newLB(project, "lb-teardown")
	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		_, err := w.LoadBalancers().Insert(ctx, lb)
		require.NoError(t, err)
	})
	_, ok := quotaUsed(t, dsn, nested, string(lb.ID), nested)
	require.True(t, ok, "предусловие: строка учёта родителя существует")

	commitWriter(t, repo, func(w kacho.RepositoryWriter) {
		require.NoError(t, w.LoadBalancers().Delete(ctx, string(lb.ID)))
	})

	_, ok = quotaUsed(t, dsn, nested, string(lb.ID), nested)
	assert.False(t, ok, "строк учёта снятого родителя не остаётся")
}
