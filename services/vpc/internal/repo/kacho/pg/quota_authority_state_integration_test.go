// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// quota_authority_state_integration_test.go — состояние курсора синхронизации
// НАЗЫВАЕТ причину молчания, а не показывает застой.
//
// # Предмет
//
// Накопительные счётчики заведены затем, чтобы тянущий, не применивший ни
// строки, не выглядел здоровым. Этого довода хватает, пока авторитет величин
// существует. Уход модуля квотирования из службы доступа делает «ни одна строка
// не синхронизирована за всё время» ШТАТНЫМ и ВЕЧНЫМ состоянием: сигнал застоя,
// оставленный как есть, срабатывал бы всегда, а проверку, кричащую на нормальной
// работе, перестают читать вместе с настоящими находками.
//
// Приёмка ухода модуля квотирования из службы доступа, стадия S1: производитель
// П32, сценарии KAN-Q1-06 и KAN-Q4-13.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/quota"
)

// cursorRow — наблюдаемое состояние строки курсора целиком.
type cursorRow struct {
	cursor    string
	applied   int64
	pulls     int64
	authority string
}

func readCursor(t testing.TB, ctx context.Context, pool *pgxpool.Pool) cursorRow {
	t.Helper()
	const q = `SELECT cursor, applied_rows_total, pulls_total, authority_state
	             FROM kacho_vpc.quota_sync_cursor WHERE id = 'limits'`
	var r cursorRow
	require.NoError(t, pool.QueryRow(ctx, q).Scan(&r.cursor, &r.applied, &r.pulls, &r.authority))
	return r
}

// TestQuotaCursor_AuthorityStateStartsUnknown — до первого подъёма никто ничего
// не объявлял, и это ТРЕТЬЕ состояние, отличимое от обоих законных.
//
// Умолчание `deployed` записало бы за оператора выбор, которого он не делал.
func TestQuotaCursor_AuthorityStateStartsUnknown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)

	require.Equal(t, string(quota.AuthorityUnknown), readCursor(t, ctx, pool).authority,
		"миграция применена, процесс не поднимался — объявления ещё не было ни одного")
}

// TestQuotaCursor_KAN_Q1_06_AbsentIsNamedNotInferred — «домен величин не
// развёрнут» ОТЛИЧИМО от «ни одна строка не синхронизирована за всё время».
//
// Оба состояния дают нулевые накопительные счётчики; различает их только
// названная причина.
func TestQuotaCursor_KAN_Q1_06_AbsentIsNamedNotInferred(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	proj := quotaProjection(t, pool)

	require.NoError(t, proj.RecordAuthority(ctx, quota.AuthorityAbsent))

	got := readCursor(t, ctx, pool)
	require.Equal(t, string(quota.AuthorityAbsent), got.authority)
	require.Zero(t, got.applied, "тянущий не заводился — применённых строк нет")
	require.Zero(t, got.pulls, "тянущий не заводился — проходов нет")

	// Положительный близнец: развёрнутый домен даёт ДРУГОЕ наблюдаемое
	// состояние на тех же нулевых счётчиках. Без него утверждение выше зеленело
	// бы на реализации, пишущей одно и то же при любом объявлении.
	require.NoError(t, proj.RecordAuthority(ctx, quota.AuthorityPresent))
	require.Equal(t, string(quota.AuthorityPresent), readCursor(t, ctx, pool).authority)
}

// TestQuotaCursor_KAN_Q4_13_TransitionKeepsTheAccumulatedCounters — перевод
// объявления в «не развёрнут» НЕ теряет накопленного.
//
// Мир этой пробы отличается от предыдущей ровно ОДНИМ фактом: тянущий успел
// поработать. Предмет проверки другой — что свидетельство работы не стирается:
// обнулив счётчики, мы стёрли бы единственное доказательство того, что снимок
// когда-то догонял авторитет.
func TestQuotaCursor_KAN_Q4_13_TransitionKeepsTheAccumulatedCounters(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	proj := quotaProjection(t, pool)

	// Тянущий поработал: курсор сдвинут, обе накопительные величины ненулевые.
	require.NoError(t, proj.RecordAuthority(ctx, quota.AuthorityPresent))
	require.NoError(t, proj.SaveCursor(ctx, "rev-42", 7))
	require.NoError(t, proj.Heartbeat(ctx))
	before := readCursor(t, ctx, pool)
	require.Equal(t, "rev-42", before.cursor)
	require.EqualValues(t, 7, before.applied)
	require.EqualValues(t, 1, before.pulls)

	// Оператор перевёл объявление в «не развёрнут».
	require.NoError(t, proj.RecordAuthority(ctx, quota.AuthorityAbsent))

	after := readCursor(t, ctx, pool)
	require.Equal(t, string(quota.AuthorityAbsent), after.authority, "причина названа")
	require.Equal(t, before.cursor, after.cursor, "курсор не двигается")
	require.Equal(t, before.applied, after.applied, "накопленные строки не теряются")
	require.Equal(t, before.pulls, after.pulls, "накопленные проходы не теряются")
}

// TestQuotaCursor_VocabularyIsClosedAndMatchesTheDeclaredOne — словарь состояний
// ограничения таблицы совпадает с объявленным в фундаменте.
//
// Словарь живёт в двух местах by construction: в Go его объявляет тип, в схеме —
// ограничение, и SQL импортировать Go не может. Совпадение двух объявлений
// держит ЭТА проба, а не совпадение написания: разойдясь, они дали бы отказ
// вставки на значении, которое код считает законным.
func TestQuotaCursor_VocabularyIsClosedAndMatchesTheDeclaredOne(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	proj := quotaProjection(t, pool)

	for _, state := range quota.AuthorityStates() {
		require.NoError(t, proj.RecordAuthority(ctx, state),
			"состояние %q объявлено фундаментом законным — ограничение обязано его принять", state)
	}

	// Отрицательная половина: значение вне словаря отвергается БАЗОЙ, а не
	// договорённостью. Без неё «словарь закрыт» зеленело бы на таблице без
	// ограничения вовсе.
	require.Error(t, proj.RecordAuthority(ctx, quota.AuthorityState("whatever")),
		"значение вне закрытого словаря обязано отвергаться ограничением таблицы")
}
