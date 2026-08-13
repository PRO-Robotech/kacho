// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uc "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/dataplane"
	dataplanepg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/dataplane"
)

// dataplane_public_state_integration_test.go — проекция подтверждения, какой её
// видит АРЕНДАТОР, против настоящей базы.
//
// Подтверждение сюда попадает ТЕМ ЖЕ путём, которым его кладёт исполнитель
// (`store.Record`), а не INSERT'ом в таблицу: проба, сеющая состояние в обход
// приёмной стороны, утверждала бы о форме строки, а не о том, что арендатор
// увидит после реального доклада.

// reportApplied — доклад исполнителя о действующей ревизии объекта.
func reportApplied(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *dataplanepg.Store,
	id string, outcome uc.ApplyOutcome, reason uc.FailureReason,
) {
	t.Helper()
	got, err := store.Record(ctx, uc.ApplyReport{
		ResourceID: id,
		Revision:   revisionOf(t, ctx, pool, id),
		Outcome:    outcome,
		Reason:     reason,
	})
	require.NoError(t, err)
	require.True(t, got.Recorded, "доклад не записан — проба утверждала бы о несуществующем")
}

// Отказ исполнителя ВИДЕН арендатору вместе с классом причины — и это
// единственное, ради чего написана проекция.
//
// Рядом стоит положительный контроль: применённое намерение не несёт ничего
// сверх факта. Без него отрицание зеленело бы на проекции, которая на любом
// входе отвечает «не применено с классом».
func TestIntegration_DataplanePublicState_TenantSeesTheRefusalAndOnlyWhenThereIsOne(t *testing.T) {
	ctx, pool, store := dataplaneFixture(t)

	refused := insertNetwork(t, ctx, pool, "dp-public-refused")
	applied := insertNetwork(t, ctx, pool, "dp-public-applied")
	silent := insertNetwork(t, ctx, pool, "dp-public-silent")

	reportApplied(t, ctx, pool, store, refused, uc.OutcomeFailed, uc.ReasonCapacity)
	reportApplied(t, ctx, pool, store, applied, uc.OutcomeApplied, uc.ReasonNone)

	states, err := store.PublicApplyStates(ctx, []string{refused, applied, silent})
	require.NoError(t, err)

	assert.Equal(t, uc.PublicApplyState{Applied: false, Reason: uc.ReasonCapacity}, states[refused],
		"отказ исполнителя не доехал до арендатора")
	assert.Equal(t, uc.PublicApplyState{Applied: true, Reason: uc.ReasonNone}, states[applied],
		"подтверждённое применение прочитано неверно либо несёт лишнее")
	assert.Equal(t, uc.PublicApplyState{Applied: false, Reason: uc.ReasonNone}, states[silent],
		"объект, о котором исполнитель молчал, получил придуманное состояние")

	// Партия обслуживает КАЖДЫЙ названный объект: три разных ответа на один
	// вызов. Без этого одинаковый ответ на все три был бы неотличим от верного.
	assert.Len(t, states, 3, "не все названные объекты попали в ответ")
}

// Отказ по ПРЕЖНЕЙ ревизии не приписывается новому намерению.
//
// Арендатор поправил ресурс — исполнитель о новой ревизии ещё не отчитывался.
// Показать здесь прежний класс значило бы предложить чинить причину, которой у
// текущего изменения нет.
func TestIntegration_DataplanePublicState_RefusalDoesNotOutliveItsRevision(t *testing.T) {
	ctx, pool, store := dataplaneFixture(t)

	id := insertNetwork(t, ctx, pool, "dp-public-superseded")
	reportApplied(t, ctx, pool, store, id, uc.OutcomeFailed, uc.ReasonConflict)

	before, err := store.PublicApplyStates(ctx, []string{id})
	require.NoError(t, err)
	require.Equal(t, uc.ReasonConflict, before[id].Reason, "предпосылка пробы не выполнена: отказ не виден")

	_, err = pool.Exec(ctx, `UPDATE kacho_vpc.networks SET description = 'исправлено' WHERE id = $1`, id)
	require.NoError(t, err)

	after, err := store.PublicApplyStates(ctx, []string{id})
	require.NoError(t, err)
	assert.Equal(t, uc.PublicApplyState{Applied: false, Reason: uc.ReasonNone}, after[id],
		"класс отказа пережил ревизию, к которой относился")
}

// Успех по прежней ревизии тоже не выдаётся за состояние текущей: иначе правка
// арендатора выглядела бы применённой в тот же миг, когда он её сохранил.
func TestIntegration_DataplanePublicState_SuccessDoesNotOutliveItsRevision(t *testing.T) {
	ctx, pool, store := dataplaneFixture(t)

	id := insertNetwork(t, ctx, pool, "dp-public-stale-success")
	reportApplied(t, ctx, pool, store, id, uc.OutcomeApplied, uc.ReasonNone)

	before, err := store.PublicApplyStates(ctx, []string{id})
	require.NoError(t, err)
	require.True(t, before[id].Applied, "предпосылка пробы не выполнена: применение не видно")

	_, err = pool.Exec(ctx, `UPDATE kacho_vpc.networks SET description = 'снова изменено' WHERE id = $1`, id)
	require.NoError(t, err)

	after, err := store.PublicApplyStates(ctx, []string{id})
	require.NoError(t, err)
	assert.False(t, after[id].Applied, "неприменённая правка показана применённой")
	assert.Equal(t, uc.ReasonNone, after[id].Reason)
}

// Объект, о котором сказать нечего, ОТСУТСТВУЕТ в ответе — он не подменяется
// нулевым значением.
//
// Различать обязательно: «намерения нет» (объекта не существует либо он снят) и
// «намерение есть, исполнитель молчит» — разные вещи, и вызывающий отличает их
// наличием ключа, а не сравнением с нулём.
func TestIntegration_DataplanePublicState_NothingToSayIsAbsenceNotAZeroValue(t *testing.T) {
	ctx, pool, store := dataplaneFixture(t)

	live := insertNetwork(t, ctx, pool, "dp-public-live")
	gone := insertNetwork(t, ctx, pool, "dp-public-gone")
	_, err := pool.Exec(ctx, `DELETE FROM kacho_vpc.networks WHERE id = $1`, gone)
	require.NoError(t, err)

	states, err := store.PublicApplyStates(ctx, []string{live, gone, "net00000000000000000"})
	require.NoError(t, err)

	_, liveKnown := states[live]
	assert.True(t, liveKnown, "живой объект пропал из ответа — положительный контроль не сошёлся")

	_, goneKnown := states[gone]
	assert.False(t, goneKnown, "снятое намерение выдало состояние удалённого ресурса")

	_, unknownKnown := states["net00000000000000000"]
	assert.False(t, unknownKnown, "объект без намерения получил состояние")
}

// Пустой список — пустой ответ и НИ ОДНОГО обращения к базе: пустой вход не
// значит «все».
func TestIntegration_DataplanePublicState_EmptyRequestIsEmptyAnswer(t *testing.T) {
	ctx, pool, store := dataplaneFixture(t)
	insertNetwork(t, ctx, pool, "dp-public-not-asked")

	states, err := store.PublicApplyStates(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, states, "пустой запрос вернул чужие объекты")
}
