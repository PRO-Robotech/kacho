// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/internal/pgtest"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	uc "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/dataplane"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/applystate"
	dataplanepg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/dataplane"
)

// dataplane_public_apply_state_integration_test.go — что видит АРЕНДАТОР о
// применении своего намерения (APPLY-06…09, 13, 14, 18, 19).
//
// # Почему интеграция, а не сквозная проба
//
// Предусловие каждого сценария ниже — ОТЧЁТ ИСПОЛНИТЕЛЯ, а компонента-исполнителя
// в дереве нет: отчёт производится только вызовом приёма на внутреннем
// слушателе, у которого нет ни одной HTTP-аннотации и который требует
// админского яруса. Арендатор такого состояния не создаст ничем. Сценарий,
// требующий состояния, которого в среде не построить, — негодный сценарий, а не
// повод ослабить утверждение; поэтому Given строится здесь настоящим приёмом
// отчёта поверх настоящей базы.

// applyStateEnv — база, приёмник отчётов и заполнитель поверх настоящего
// адаптера.
type applyStateEnv struct {
	ctx     context.Context
	pool    *pgxpool.Pool
	report  *uc.ReportAppliedUseCase
	filler  *applystate.Filler
	missing *int
}

func newApplyStateEnv(t *testing.T) *applyStateEnv {
	t.Helper()
	ctx, pool, store := dataplaneFixture(t)
	missing := 0
	return &applyStateEnv{
		ctx:     ctx,
		pool:    pool,
		report:  uc.NewReportAppliedUseCase(store, uc.NewObserver(nil)),
		filler:  applystate.NewFiller(store, func() { missing++ }),
		missing: &missing,
	}
}

// network заводит сеть и возвращает её идентификатор.
func (e *applyStateEnv) network(t *testing.T, name string) string {
	t.Helper()
	return insertNetwork(t, e.ctx, e.pool, name)
}

// state — публичная проекция ресурса, тем же путём, каким её читает обработчик.
func (e *applyStateEnv) state(t *testing.T, id string) *vpcv1.ApplyState {
	t.Helper()
	st, err := e.filler.One(e.ctx, id)
	require.NoError(t, err)
	return st
}

// record — отчёт исполнителя.
func (e *applyStateEnv) record(id string, rev int64, outcome uc.ApplyOutcome, reason uc.FailureReason) error {
	_, err := e.report.Record(e.ctx, uc.ApplyReport{
		ResourceID: id, Revision: rev, Outcome: outcome, Reason: reason,
	})
	return err
}

// TestIntegration_ApplyState_FreshIntentReadsAsInFlight — свежее намерение видно
// как «в работе»: не применено, класса нет.
//
// Положительный контроль ко всем отрицаниям ниже: без него «класса отказа не
// видно» было бы неотличимо от «поля нет».
func TestIntegration_ApplyState_FreshIntentReadsAsInFlight(t *testing.T) {
	e := newApplyStateEnv(t)
	id := e.network(t, "aps-fresh")

	st := e.state(t, id)
	require.NotNil(t, st, "о живом ресурсе платформа обязана делать утверждение")
	assert.False(t, st.GetApplied())
	assert.Equal(t, vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_UNSPECIFIED, st.GetReason(),
		"класса отказа не было — чинить арендатору нечего, надо ждать")
	assert.Zero(t, *e.missing)
}

// TestIntegration_ApplyState_ConfirmedApplyReachesTheTenant — APPLY-06.
func TestIntegration_ApplyState_ConfirmedApplyReachesTheTenant(t *testing.T) {
	e := newApplyStateEnv(t)
	id := e.network(t, "aps-applied")
	rev := revisionOf(t, e.ctx, e.pool, id)

	require.NoError(t, e.record(id, rev, uc.OutcomeApplied, uc.ReasonNone))

	st := e.state(t, id)
	require.NotNil(t, st)
	assert.True(t, st.GetApplied())
	assert.Equal(t, vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_UNSPECIFIED, st.GetReason(),
		"у успеха не бывает причины неуспеха")
}

// TestIntegration_ApplyState_EveryFailureClassReachesTheTenant — APPLY-07.
//
// Каждый класс словаря отдельно: «класс доезжает», доказанное на одном значении
// и распространённое на пять непроверенных, — это утверждение об одном значении.
func TestIntegration_ApplyState_EveryFailureClassReachesTheTenant(t *testing.T) {
	e := newApplyStateEnv(t)

	want := map[uc.FailureReason]vpcv1.ApplyFailureReason{
		uc.ReasonCapacity:           vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_CAPACITY,
		uc.ReasonConflict:           vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_CONFLICT,
		uc.ReasonUnsupported:        vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_UNSUPPORTED,
		uc.ReasonDependencyNotReady: vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_DEPENDENCY_NOT_READY,
		uc.ReasonTransient:          vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_TRANSIENT,
		uc.ReasonExecutorInternal:   vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_EXECUTOR_INTERNAL,
	}
	require.Len(t, want, len(uc.KnownFailureReasons),
		"перечень пробы разошёлся с закрытым словарём — проверяется не весь класс")

	for i, reason := range uc.KnownFailureReasons {
		t.Run(string(reason), func(t *testing.T) {
			id := e.network(t, fmt.Sprintf("aps-fail-%d", i))
			rev := revisionOf(t, e.ctx, e.pool, id)
			require.NoError(t, e.record(id, rev, uc.OutcomeFailed, reason))

			st := e.state(t, id)
			require.NotNil(t, st)
			assert.False(t, st.GetApplied())
			assert.Equal(t, want[reason], st.GetReason(),
				"класс отказа доехал до арендатора не тем, каким его назвал исполнитель")
		})
	}
}

// TestIntegration_ApplyState_StaleSuccessIsNotPassedOffAsTheNewIntent — APPLY-08.
func TestIntegration_ApplyState_StaleSuccessIsNotPassedOffAsTheNewIntent(t *testing.T) {
	e := newApplyStateEnv(t)
	id := e.network(t, "aps-stale-ok")
	rev := revisionOf(t, e.ctx, e.pool, id)
	require.NoError(t, e.record(id, rev, uc.OutcomeApplied, uc.ReasonNone))
	require.True(t, e.state(t, id).GetApplied(), "предпосылка сценария не построена")

	// Арендатор меняет ресурс — намерение получает следующую ревизию.
	_, err := e.pool.Exec(e.ctx,
		`UPDATE kacho_vpc.networks SET labels = '{"env":"prod"}'::jsonb WHERE id = $1`, id)
	require.NoError(t, err)
	require.Greater(t, revisionOf(t, e.ctx, e.pool, id), rev, "правка не подняла ревизию")

	st := e.state(t, id)
	require.NotNil(t, st)
	assert.False(t, st.GetApplied(),
		"устаревший успех выдан за состояние нового намерения — арендатор считал бы доехавшим то, чего исполнитель не видел")
	assert.Equal(t, vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_UNSPECIFIED, st.GetReason())
}

// TestIntegration_ApplyState_StaleFailureIsNotAttributedToTheNewIntent — APPLY-09.
//
// Отдельная проба, а не ветка предыдущей: там устаревшее утверждение доброе и
// его потеря безобидна, здесь — злое, и его перенос вреден. Приписав старый
// класс новому намерению, платформа заставила бы арендатора чинить причину,
// которой у его правки нет.
func TestIntegration_ApplyState_StaleFailureIsNotAttributedToTheNewIntent(t *testing.T) {
	e := newApplyStateEnv(t)
	id := e.network(t, "aps-stale-fail")
	rev := revisionOf(t, e.ctx, e.pool, id)
	require.NoError(t, e.record(id, rev, uc.OutcomeFailed, uc.ReasonConflict))
	require.Equal(t, vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_CONFLICT,
		e.state(t, id).GetReason(), "предпосылка сценария не построена")

	_, err := e.pool.Exec(e.ctx,
		`UPDATE kacho_vpc.networks SET description = 'правка арендатора' WHERE id = $1`, id)
	require.NoError(t, err)

	st := e.state(t, id)
	require.NotNil(t, st)
	assert.False(t, st.GetApplied())
	assert.Equal(t, vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_UNSPECIFIED, st.GetReason(),
		"класс отказа прошлой ревизии приписан новому намерению")
}

// TestIntegration_ApplyState_NoStatementIsDistinctFromNotApplied — APPLY-13.
func TestIntegration_ApplyState_NoStatementIsDistinctFromNotApplied(t *testing.T) {
	e := newApplyStateEnv(t)
	gone := e.network(t, "aps-gone")
	live := e.network(t, "aps-live")

	// Намерение снято — так выглядит удаление в полёте со стороны чтения.
	_, err := e.pool.Exec(e.ctx, `DELETE FROM kacho_vpc.networks WHERE id = $1`, gone)
	require.NoError(t, err)

	st, err := e.filler.One(e.ctx, gone)
	require.NoError(t, err, "снятое намерение — штатная гонка, а не отказ чтения")
	assert.Nil(t, st, "«утверждения нет» выражается отсутствием поля, а не правдоподобным «не применено»")
	assert.Equal(t, 1, *e.missing, "факт обязан быть наблюдаемым: иначе сломанная проекция неотличима от гонки")

	// ОТРИЦАТЕЛЬНАЯ ПОЛОВИНА: у живого ресурса того же прогона поле заполнено —
	// иначе «не заполнено» было бы верно всегда и сценарий не различал бы ничего.
	assert.NotNil(t, e.state(t, live))
	assert.Equal(t, 1, *e.missing)
}

// TestIntegration_ApplyState_RowOutsideTheDictionaryFailsTheReadOpaquely —
// APPLY-14.
//
// Значение вне закрытого словаря может появиться ровно одним способом: словарь
// базы разошёлся с контрактом. Свернув расхождение в «не применено», проекция
// сообщила бы арендатору правдоподобную неправду и пережила бы собственный
// предмет.
func TestIntegration_ApplyState_RowOutsideTheDictionaryFailsTheReadOpaquely(t *testing.T) {
	e := newApplyStateEnv(t)
	bad := e.network(t, "aps-bad-row")
	good := e.network(t, "aps-good-row")

	badRev := revisionOf(t, e.ctx, e.pool, bad)
	goodRev := revisionOf(t, e.ctx, e.pool, good)
	require.NoError(t, e.record(bad, badRev, uc.OutcomeFailed, uc.ReasonCapacity))
	require.NoError(t, e.record(good, goodRev, uc.OutcomeApplied, uc.ReasonNone))

	// Ограничение снимается ТОЛЬКО на время пробы: предмет утверждения — что
	// делает путь чтения, столкнувшись с расхождением, а не то, пускает ли база
	// такую строку (это утверждает своя проба ограничения).
	_, err := e.pool.Exec(e.ctx,
		`ALTER TABLE kacho_vpc.dataplane_apply DROP CONSTRAINT dataplane_apply_reason_matches_outcome`)
	require.NoError(t, err, "предпосылка пробы не построена: ограничение называется иначе")
	_, err = e.pool.Exec(e.ctx,
		`UPDATE kacho_vpc.dataplane_apply SET reason = 'МУСОР' WHERE resource_id = $1`, bad)
	require.NoError(t, err)

	_, err = e.filler.One(e.ctx, bad)
	require.Error(t, err, "негодная строка свёрнута в состояние — арендатор получил бы правдоподобную неправду")
	assert.Equal(t, codes.Internal, status.Code(err))
	msg := status.Convert(err).Message()
	assert.Equal(t, "internal database error", msg,
		"текст обязан быть фиксированным и непрозрачным")
	for _, leak := range []string{"dataplane_apply", "reason", "МУСОР", "kacho_vpc"} {
		assert.NotContains(t, msg, leak, "наружу уехала внутренность хранилища")
	}

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: строка годной формы в том же прогоне читается.
	st := e.state(t, good)
	require.NotNil(t, st)
	assert.True(t, st.GetApplied())
}

// TestIntegration_ApplyState_ReportForANeverIssuedRevisionIsRejected — APPLY-18.
func TestIntegration_ApplyState_ReportForANeverIssuedRevisionIsRejected(t *testing.T) {
	e := newApplyStateEnv(t)
	id := e.network(t, "aps-unknown-rev")
	rev := revisionOf(t, e.ctx, e.pool, id)

	err := e.record(id, rev+100, uc.OutcomeApplied, uc.ReasonNone)
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err),
		"объект есть, состояние не позволяет — это предусловие, а не отсутствие")

	st := e.state(t, id)
	require.NotNil(t, st)
	assert.False(t, st.GetApplied(), "отвергнутый отчёт изменил публичную проекцию")
	assert.Equal(t, vpcv1.ApplyFailureReason_APPLY_FAILURE_REASON_UNSPECIFIED, st.GetReason())

	// Отчёт про объект, которого нет: своя база, своя строка, строки нет.
	err = e.record(ids.NewID(ids.PrefixNetwork), 1, uc.OutcomeApplied, uc.ReasonNone)
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))

	// Отказ без класса причины невыразим: успех нёс бы причину неуспеха, а отказ
	// без причины не сообщал бы ничего, кроме самого факта.
	err = e.record(id, rev, uc.OutcomeFailed, uc.ReasonNone)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Contains(t, status.Convert(err).Message(), "reason",
		"отказ обязан называть поле — иначе исполнителю гадать, какое из четырёх он прислал не так")
}

// TestIntegration_ApplyState_PageCostsOneStatement — APPLY-19.
//
// Считаются НЕ вызовы порта, а исполнения стейтмента: вызов порта доказывал бы
// свойство заполнителя, а предмет здесь — стоимость, которую платит база.
func TestIntegration_ApplyState_PageCostsOneStatement(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx, pool, store, counter := tracedDataplaneFixture(t)
	filler := applystate.NewFiller(store, nil)

	const pageSize = 50
	ids50 := make([]string, 0, pageSize)
	for i := 0; i < pageSize; i++ {
		ids50 = append(ids50, insertNetwork(t, ctx, pool, fmt.Sprintf("aps-cost-%02d", i)))
	}

	counter.reset()
	states, err := filler.Page(ctx, ids50)
	require.NoError(t, err)
	require.Len(t, states, pageSize, "страница прочитана не целиком — стоимость меряется не на том")
	assert.Equal(t, 1, counter.value(),
		"страница из %d строк стоила больше одного обращения к проекции", pageSize)

	counter.reset()
	_, err = filler.Page(ctx, ids50[:1])
	require.NoError(t, err)
	assert.Equal(t, 1, counter.value(),
		"стоимость оказалась функцией размера страницы")

	counter.reset()
	_, err = filler.Page(ctx, nil)
	require.NoError(t, err)
	assert.Zero(t, counter.value(), "пустая страница обратилась к проекции")
}

// applyStateQueryCounter — счётчик исполнений стейтмента проекции.
type applyStateQueryCounter struct{ n int }

func (c *applyStateQueryCounter) reset()     { c.n = 0 }
func (c *applyStateQueryCounter) value() int { return c.n }

func (c *applyStateQueryCounter) TraceQueryStart(
	ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData,
) context.Context {
	// Признак — обе таблицы проекции в одном тексте: так стейтмент состояния
	// применения отличается от любого другого обращения к намерению.
	if strings.Contains(data.SQL, "dataplane_intent") && strings.Contains(data.SQL, "dataplane_apply") {
		c.n++
	}
	return ctx
}

func (c *applyStateQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// tracedDataplaneFixture — своя база и адаптер проекции с трассировщиком
// запросов на пуле.
//
// Отдельная сборка пула, а не общая: трассировщик ставится в конфигурацию
// соединения, и добавлять его в общий конструктор пула значило бы тащить
// диагностику пробы в боевой путь.
func tracedDataplaneFixture(t *testing.T) (context.Context, *pgxpool.Pool, *dataplanepg.Store, *applyStateQueryCounter) {
	t.Helper()
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(setupTestDB(t))
	require.NoError(t, err)
	counter := &applyStateQueryCounter{}
	cfg.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	return ctx, pool, dataplanepg.New(pool), counter
}
