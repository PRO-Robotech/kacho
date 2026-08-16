// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Бэкстоп register-outbox поверх существующего fgaproxy-механизма: reconciler,
// метрики и fail-closed boot-gate. Co-commit-атомарность записи intent'а не
// меняется — бэкстоп лишь чинит застрявшие строки и не дает принимать мутации,
// пока drainer не подключен.
//
// Проверяемые сценарии:
//   - reconciler re-drive'ит «отравленную» строку обратно в claimable → она доставляется;
//   - fail-closed boot-gate: --require-iam без drainer → Create отклонен;
//   - длинная недоступность IAM (transient, > MaxAttempts) не отравляет intent —
//     он доставляется ровно один раз при восстановлении.
//
// Миграция, добавляющая resource_kind/resource_id, аддитивна и backfill-safe —
// ее column-present-проверка лежит тоже здесь (reconciler адресует intent'ы по
// resource_id).
//
// testcontainers Postgres 16; реальные corelib reconciler/drainer + fake IAM.
// Пропускается под -short.
package clients_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/outbox/bootgate"
	"github.com/PRO-Robotech/kacho/pkg/outbox/metrics"
	"github.com/PRO-Robotech/kacho/pkg/outbox/reconciler"
	"github.com/PRO-Robotech/kacho/pkg/servicehost"

	pgrepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
)

const vpcOutboxTable = "kacho_vpc.fga_register_outbox"

// newVPCReconciler собирает reconciler поверх vpc register-outbox и
// per-service-адаптера (FGAReconcileAdapter реализует оба порта).
func newVPCReconciler(t *testing.T, pool *pgxpool.Pool, grace time.Duration) *reconciler.Reconciler {
	t.Helper()
	ad := pgrepo.NewFGAReconcileAdapter(pool)
	rc, err := reconciler.New(pool, reconciler.Config{
		PartitionColumn: reconciler.RegisterOutboxPartition,
		Table:           vpcOutboxTable,
		Channel:         "kacho_vpc_fga_register_outbox",
		MaxAttempts:     10,
		GraceWindow:     grace,
	}, reconciler.Adapters{Enumerator: ad, Registry: ad}, nil)
	require.NoError(t, err)
	return rc
}

// Test_1_4_08A_Migration0008_ResourceColumns — миграция добавляет в
// kacho_vpc.fga_register_outbox колонки resource_kind/resource_id аддитивно и
// backfill-safe (NOT NULL DEFAULT пустой строкой): прежний column-minimal INSERT по-прежнему
// работает, а reconciler может адресовать intent'ы по ресурсу.
func Test_1_4_08A_Migration0008_ResourceColumns(t *testing.T) {
	pool := setupRegisterOutboxDB(t)
	ctx := context.Background()

	// Обе колонки есть, нужного типа + NOT NULL DEFAULT '' (backfill-safe).
	for _, col := range []string{"resource_kind", "resource_id"} {
		var dataType, isNullable string
		var def *string
		err := pool.QueryRow(ctx, `
			SELECT data_type, is_nullable, column_default
			  FROM information_schema.columns
			 WHERE table_schema='kacho_vpc' AND table_name='fga_register_outbox' AND column_name=$1`,
			col).Scan(&dataType, &isNullable, &def)
		require.NoError(t, err, "column %s must exist (migration 0008)", col)
		assert.Equal(t, "text", dataType, "%s is text", col)
		assert.Equal(t, "NO", isNullable, "%s is NOT NULL (backfill-safe with default)", col)
		require.NotNil(t, def, "%s has a default", col)
		assert.Contains(t, *def, "''", "%s defaults to empty string (backfill-safe)", col)
	}

	// Backfill-safe: INSERT без новых колонок проходит (прежний путь).
	_, err := pool.Exec(ctx,
		`INSERT INTO kacho_vpc.fga_register_outbox (event_type, payload)
		 VALUES ('fga.register', '{"subject_id":"project:p","relation":"project","object":"vpc_network:net-x"}'::jsonb)`)
	require.NoError(t, err, "legacy column-minimal INSERT still works (backfill-safe)")
	var kind, id string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT resource_kind, resource_id FROM kacho_vpc.fga_register_outbox LIMIT 1`).Scan(&kind, &id))
	assert.Equal(t, "", kind)
	assert.Equal(t, "", id)
}

// Test_1_4_30_ReconcilerRedrivesPoisoned — «отравленный» register-intent
// (attempt_count == MaxAttempts, sent_at NULL) reconciler возвращает в claimable,
// после чего drainer его доставляет (sent_at NOT NULL). Атомарность не затронута
// (resource-writer не меняется) — бэкстоп лишь чинит застрявшую строку.
func Test_1_4_30_ReconcilerRedrivesPoisoned(t *testing.T) {
	pool := setupRegisterOutboxDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// «Отравленный» intent: ранее applier отверг его как permanent; причина
	// устранена, и его нужно доставить заново.
	_, err := pool.Exec(ctx,
		`INSERT INTO kacho_vpc.fga_register_outbox
		   (event_type, resource_kind, resource_id, payload, attempt_count, last_error)
		 VALUES ('fga.register','vpc_network','net-redrive',
		         '{"subject_id":"project:p","relation":"project","object":"vpc_network:net-redrive"}'::jsonb,
		         10,'was permanent')`)
	require.NoError(t, err)

	rc := newVPCReconciler(t, pool, 0)
	n, err := rc.RedrivePoisoned(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "exactly one poisoned row re-driven")

	// Re-driven-строка снова claimable (attempt_count сброшен, last_error очищен).
	var attempt int
	var lastErr *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT attempt_count, last_error FROM kacho_vpc.fga_register_outbox WHERE resource_id='net-redrive'`).
		Scan(&attempt, &lastErr))
	assert.Less(t, attempt, 10, "attempt_count reset below MaxAttempts (claimable)")
	assert.Nil(t, lastErr, "last_error cleared")

	// Теперь drainer ее доставляет (IAM здоров).
	iam := newRecordingIAM()
	d := newRegisterDrainer(t, pool, iam, 10)
	go func() { _ = d.Run(ctx) }()
	require.Eventually(t, func() bool {
		return iam.count("vpc_network:net-redrive") == 1
	}, 5*time.Second, 50*time.Millisecond, "re-driven intent delivered exactly once")
}

// Test_1_4_31_FailClosedBootGate_RefusesCreate — при взведённом --require-iam и
// неподключённом дренаже регистраций загрузочный гейт отвергает мутацию, а после
// подключения (SetConnected(true)) снова её принимает. Postgres не нужен —
// проверяется чистое поведение гейта и КЛАССИФИКАЦИЯ методов vpc.
//
// # Почему здесь больше нет своего интерсептора
//
// До перевода vpc на носитель контура пакет `internal/fgaboot` держал СВОЮ копию
// связки «предикат гейтируемой мутации + unary-звено», дословно совпадавшую с
// такой же копией у соседей. Копия снята вместе с переводом: звено ставит
// носитель (`pkg/servicehost`), и его поведение — отказ на гейтируемой мутации,
// молчание на всём прочем — закреплено ЕГО пробами. Воспроизводить здесь ту же
// связку значило бы завести пятую копию предмета, который и убирали.
//
// Что осталось предметом ЭТОЙ пробы и чего носителева проба знать не может:
// классификация РЕАЛЬНЫХ имён методов vpc общим предикатом
// `servicehost.IsGatedMutation`. Тенантский Create гейтится, чтение — нет,
// админский Internal-Create — нет (у него нет владельца, и гейтить его значило бы
// закрыть админ-путь по причине, к нему не относящейся).
func Test_1_4_31_FailClosedBootGate_RefusesCreate(t *testing.T) {
	gate := bootgate.New(bootgate.Config{RequireIAM: true, Service: "kacho-vpc"})

	// Ещё не подключено → Ready() false, мутация отвергается fail-closed.
	assert.False(t, gate.Ready(), "require-iam + not connected → NotReady")
	err := gate.GuardMutation()
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err), "мутация отвергнута fail-closed (UNAVAILABLE)")

	// Классификация реальных методов vpc общим предикатом носителя.
	assert.True(t, servicehost.IsGatedMutation("/kacho.cloud.vpc.v1.NetworkService/Create"),
		"тенантский Create записывает намерение регистрации — он под гейтом")
	assert.False(t, servicehost.IsGatedMutation("/kacho.cloud.vpc.v1.NetworkService/Get"),
		"чтение не записывает намерения — гейтить его нечем и незачем")
	assert.False(t, servicehost.IsGatedMutation("/kacho.cloud.vpc.v1.InternalAddressPoolService/Create"),
		"админский Internal-Create владельца не заводит — под гейт не попадает")

	// Дренаж подключился → гейт открывается, мутация принимается.
	gate.SetConnected(true)
	assert.True(t, gate.Ready(), "connected → Ready")
	require.NoError(t, gate.GuardMutation())
}

// Test_1_4_31_RequireIAMOff_NoOp — контраст: --require-iam=false (dev) → гейт
// no-op, мутация всегда принимается, Ready() всегда true.
func Test_1_4_31_RequireIAMOff_NoOp(t *testing.T) {
	gate := bootgate.New(bootgate.Config{RequireIAM: false, Service: "kacho-vpc"})
	assert.True(t, gate.Ready(), "require-iam off → always Ready (dev)")
	require.NoError(t, gate.GuardMutation(), "мутация принимается в dev back-compat режиме")
}

// Test_1_4_32_LongOutageNoPoison_ThenMetricsSurface — IAM Unavailable дольше, чем
// MaxAttempts подряд (transient-класс) → intent НЕ отравляется → доставляется
// ровно один раз при восстановлении, а Collector метрик показывает backlog, пока
// строка pending. Контракт классификации corelib (Unavailable → transient, никогда
// не poison) прогоняется через реальный vpc-applier.
func Test_1_4_32_LongOutageNoPoison_ThenMetricsSurface(t *testing.T) {
	pool := setupRegisterOutboxDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const maxAttempts = 5
	// down остается true, пока тест его не переключит — это гарантирует, что drainer
	// сделает заметно БОЛЬШЕ maxAttempts подряд transient-попыток (Unavailable) до
	// любой возможности доставки. По правилу классификации corelib (Unavailable →
	// transient, никогда не poison) intent обязан пережить всю недоступность.
	var down atomic.Bool
	down.Store(true)
	var attempts atomic.Int32
	iam := newRecordingIAM()
	iam.errFn = func(_ int) error {
		if down.Load() {
			attempts.Add(1)
			return status.Error(codes.Unavailable, "iam down")
		}
		return nil
	}
	d := newRegisterDrainer(t, pool, iam, maxAttempts)
	go func() { _ = d.Run(ctx) }()

	insertRegisterIntent(t, ctx, pool, "fga.register", "project:p", "project", "vpc_network:net-long")

	// Пока IAM недоступен: drainer делает > maxAttempts попыток, но intent НЕ
	// отравлен (все еще pending, sent_at NULL) — и Collector метрик показывает
	// backlog + oldest-age. Это и есть гарантия no-poison для transient-ошибок.
	rec := metrics.NewMemRecorder()
	col := metrics.NewCollector(pool, rec, metrics.CollectorConfig{Table: vpcOutboxTable, MaxAttempts: maxAttempts})
	require.Eventually(t, func() bool {
		_ = col.Scan(ctx)
		return attempts.Load() > maxAttempts &&
			rec.BacklogDepth(vpcOutboxTable) >= 1 && rec.OldestPendingAgeSeconds(vpcOutboxTable) > 0
	}, 10*time.Second, 100*time.Millisecond, "> maxAttempts transient attempts, still pending (not poisoned), backlog surfaced")

	// Строка по-прежнему pending (sent_at NULL), несмотря на > maxAttempts transient-
	// сбоев — НЕ отравлена (transient-класс никогда не открывает poison-gate).
	var sentNullDuringOutage bool
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT sent_at IS NULL FROM kacho_vpc.fga_register_outbox WHERE payload->>'object'='vpc_network:net-long'`).
		Scan(&sentNullDuringOutage))
	assert.True(t, sentNullDuringOutage, "intent durable (pending) through a transient outage longer than MaxAttempts")

	// IAM восстановился → тот же durable-intent доставляется ровно один раз (не потерян).
	//
	// Ждём ОБА факта в одном условии: применитель вызван ровно один раз И строка
	// помечена отправленной. Ждать только вызова нельзя — drainer ставит sent_at
	// ОТДЕЛЬНЫМ стейтментом (markSuccess) уже ПОСЛЕ возврата применителя, поэтому
	// чтение строки сразу за ожиданием вызова попадает в окно между ними. Окно
	// узкое и на тихой машине почти всегда закрыто, но под конкуренцией за хост
	// (параллельные testcontainers-суиты) раскрывается — и проба краснеет на
	// здоровом продукте, то есть меряет загрузку машины, а не поведение дренажа.
	// Соседний iam_register_drainer_integration_test.go читает sent_at внутри
	// ожидания — здесь та же форма.
	//
	// Эта правка приехала сюда первой и БРАТЬЕВ не задела: те же пробы compute и
	// nlb прожили с зазором ещё волну, потому что разбор лежал только в этом
	// комментарии, а комментарий ничего не роняет. Форму держит гейт по дереву —
	// internal/repohygiene.TestDurableStateNeverAssertedAfterInProcessWait.
	down.Store(false)
	require.Eventually(t, func() bool {
		if iam.count("vpc_network:net-long") != 1 {
			return false
		}
		var sent bool
		if err := pool.QueryRow(ctx,
			`SELECT sent_at IS NOT NULL FROM kacho_vpc.fga_register_outbox WHERE payload->>'object'='vpc_network:net-long'`).
			Scan(&sent); err != nil {
			return false
		}
		return sent
	}, 10*time.Second, 100*time.Millisecond,
		"tuple delivered exactly once after long transient outage, and the intent marked sent (no poison, not lost)")

	// Счетчик poisoned остается 0 (permanent-ошибки не было).
	col2 := metrics.NewCollector(pool, rec, metrics.CollectorConfig{Table: vpcOutboxTable, MaxAttempts: maxAttempts})
	require.NoError(t, col2.Scan(ctx))
	assert.Equal(t, float64(0), rec.PoisonedCount(vpcOutboxTable),
		"a transient (Unavailable) outage must NOT poison — outbox_poisoned stays 0")
}
