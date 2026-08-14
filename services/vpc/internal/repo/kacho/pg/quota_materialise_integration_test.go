// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vpcrepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
)

// Материализация строк учёта против НАСТОЯЩЕЙ базы.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), V2-1, V2-4, DoD S2 п.5 и п.10.
//
// Unit-пробы полосы (`apps/kacho/shared/quota`) утверждают ПОРЯДОК: спросить →
// на промахе материализовать → спросить снова. Здесь утверждается то, что
// порядком не проверяется и живёт только в схеме: повторная материализация не
// затирает потребление, а строка без зеркала аккаунта не вставляется вовсе.
// Дублёр про это молчит by construction — он не база.

func quotaRowsFor(project, account string, kinds map[string]int64) []kacho.QuotaRow {
	out := make([]kacho.QuotaRow, 0, len(kinds))
	for k, v := range kinds {
		out = append(out, kacho.QuotaRow{
			CarrierType:   vpcrepo.QuotaCarrierProject,
			CarrierID:     project,
			Kind:          k,
			Limit:         v,
			SourceScope:   "DEFAULT",
			SourceScopeID: "",
			LimitRevision: 0,
			AccountID:     account,
		})
	}
	return out
}

// TestQuotaMaterialise_SecondPassDoesNotResetUsage — повторная материализация не
// обнуляет потребление.
//
// Это и есть довод в пользу `ON CONFLICT DO NOTHING` против `UPSERT`: строка
// учёта несёт `used`, и перезапись снимка свежим резолвом вернула бы проекту,
// уже создавшему ресурсы, чужое «занято ноль» — то есть выдала бы место, которого
// нет. Материализация зовётся на КАЖДОМ промахе, а промах под конкуренцией
// случается у нескольких запросов сразу, поэтому «второй проход» — не редкость,
// а штатное течение.
func TestQuotaMaterialise_SecondPassDoesNotResetUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-quota-remat"
	rows := quotaRowsFor(project, "acc-remat", map[string]int64{"vpc.network": 4})

	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	n, err := w.Quotas().Materialize(ctx, rows)
	require.NoError(t, err)
	require.Equal(t, int64(1), n, "первый проход заводит строку")
	require.NoError(t, w.Commit())

	// Занимаем место штатным путём — вставкой строки ресурса.
	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	_, err = w2.Networks().Insert(ctx, newNetwork(project, "net-remat"))
	require.NoError(t, err)
	require.NoError(t, w2.Commit())
	require.Equal(t, int64(1), quotaUsed(t, ctx, pool, project, "vpc.network"))

	// Второй проход материализации — та же строка, другая величина.
	w3, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w3.Abort()
	n2, err := w3.Quotas().Materialize(ctx, quotaRowsFor(project, "acc-remat",
		map[string]int64{"vpc.network": 99}))
	require.NoError(t, err)
	require.NoError(t, w3.Commit())

	assert.Equal(t, int64(0), n2, "существующая строка не трогается: заведено ноль")
	assert.Equal(t, int64(1), quotaUsed(t, ctx, pool, project, "vpc.network"),
		"потребление пережило повторную материализацию — иначе проект получил бы место, которого нет")
}

// TestQuotaMaterialise_RowWithoutAccountMirrorIsRejected — строка без зеркала
// аккаунта невыразима.
//
// Такая строка НЕВИДИМА аккаунтной дельте: изменение аккаунтной области её не
// найдёт, и она проживёт со старой величиной, а снаружи это неотличимо от
// исправной работы — дельта отчитается успехом, просто не тронув её (V2-4).
// Закрыто ограничением СХЕМЫ, а не вниманием пишущего.
func TestQuotaMaterialise_RowWithoutAccountMirrorIsRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	_, err = w.Quotas().Materialize(ctx,
		quotaRowsFor("prj-quota-nomirror", "", map[string]int64{"vpc.network": 4}))

	require.Error(t, err, "строка без зеркала аккаунта не вставляется")

	// Положительный контроль: та же строка С зеркалом проходит. Без него отказ
	// выше был бы неотличим от «материализация не работает вовсе».
	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	n, err := w2.Quotas().Materialize(ctx,
		quotaRowsFor("prj-quota-nomirror", "acc-ok", map[string]int64{"vpc.network": 4}))
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)
	require.NoError(t, w2.Commit())
}

// TestQuotaMaterialise_MaterialisedProjectChargesAndRefuses — сквозная связка:
// заведённая строка делает вставку возможной, а исчерпание — наблюдаемым.
//
// Положительный контроль ко всему разделу: без него утверждения выше зеленели бы
// и на материализации, заводящей строки, по которым ничего не списывается.
func TestQuotaMaterialise_MaterialisedProjectChargesAndRefuses(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-quota-matcharge"
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	_, err = w.Quotas().Materialize(ctx, quotaRowsFor(project, "acc-mc",
		map[string]int64{"vpc.network": 1}))
	require.NoError(t, err)
	require.NoError(t, w.Commit())

	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	_, err = w2.Networks().Insert(ctx, newNetwork(project, "net-mc-1"))
	require.NoError(t, err, "по заведённой строке вставка проходит")
	require.NoError(t, w2.Commit())

	w3, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w3.Abort()
	_, err = w3.Networks().Insert(ctx, newNetwork(project, "net-mc-2"))
	require.Error(t, err, "предел в единицу обязан отвергнуть вторую вставку")
	assert.ErrorIs(t, err, vpcrepo.ErrQuotaExceeded,
		"исчерпание, а не «потолок не назван»: строка заведена")
}
