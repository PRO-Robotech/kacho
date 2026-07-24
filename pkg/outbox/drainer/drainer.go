// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package drainer

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config — параметры конкретного экземпляра drainer-а.
type Config struct {
	// Table — полное имя outbox-таблицы (`<schema>.<table>`), e.g. "kacho_iam.fga_outbox".
	Table string
	// Channel — имя LISTEN-канала, e.g. "kacho_iam_fga_outbox".
	Channel string
	// BatchSize — сколько rows клейм'ить за один catch-up SELECT (default 32).
	BatchSize int
	// PollFallback — интервал poll'а на случай missed NOTIFY (default 30s).
	PollFallback time.Duration
	// MaxAttempts — отметка «poisoned», после которой drainer перестает ретраить
	//   (default 10). Permanent-error → force attempt_count = MaxAttempts, drainer пропускает.
	MaxAttempts int
	// BackoffMin/BackoffMax — exp-backoff bounds (default 1s..30s).
	BackoffMin time.Duration
	BackoffMax time.Duration
	// ApplyTimeout — таймаут на один Apply-вызов (default 5s). Используется
	//   также как graceful-grace при shutdown: in-flight Apply имеет
	//   non-cancellable inner ctx с этим deadline, чтобы row не осталась
	//   half-applied при ctx.Cancel parent-loop'а.
	ApplyTimeout time.Duration
	// ApplyConcurrency — сколько строк одного claim-батча применять ПАРАЛЛЕЛЬНО
	// (default 1 = последовательно, историческое поведение). >1 разворачивает
	// внешние Apply-вызовы батча по N горутинам, скрывая per-call latency пира:
	// одиночный drainer с последовательными ApplyTimeout-bounded apply'ями
	// упирается в ~1/apply_latency (при таймаутящем пире — ~1/ApplyTimeout, что
	// катастрофически мало под write-burst). Claim-батч при ApplyConcurrency>1
	// сайзится ровно в ApplyConcurrency, поэтому вся волна применяется за один
	// проход параллельно; mark'и остаются ПОСЛЕДОВАТЕЛЬНЫМИ на единственной
	// claim-транзакции. Exactly-once НЕ меняется: та же claim-tx держит
	// FOR UPDATE SKIP LOCKED lock КАЖДОЙ заклейменной строки до commit'а, а
	// Apply не трогает DB-состояние (только внешний вызов) → параллельные apply
	// не создают ни второй tx, ни лишних conn'ов пула. Требование: Applier
	// БЕЗОПАСЕН для конкурентного вызова (см. Applier godoc).
	//
	// ORDERING (важно): при ApplyConcurrency>1 порядок apply ВНУТРИ батча НЕ
	// сохраняется (строки применяются конкурентно), поэтому две intent-строки
	// ОДНОГО объекта (напр. register создания и unregister удаления, или два
	// label-register) могут закоммититься в target в обратном к id-порядке.
	// Включать ТОЛЬКО когда финальное состояние target'а СХОДИТСЯ независимо от
	// порядка: либо операции коммутативны, либо target идемпотентен И
	// монотонен/level-triggered (stale-apply — no-op). Канонические register-
	// appliers kacho (compute/vpc/…) безопасны НЕ из-за «независимости записей», а
	// потому что материализация в iam — source_version-LWW (resource_mirror UPSERT
	// с `WHERE source_version < EXCLUDED.source_version`) + level-triggered
	// reconciler (читает ТЕКУЩИЙ mirror), поэтому reordered stale register — no-op,
	// а enforcement сходится к mirror-состоянию при любом порядке. НЕ включать для
	// applier'а с order-sensitive target'ом без версионирования, ЕСЛИ порядок
	// внутри партиции не защищён PartitionColumn (см. ниже) — напр. iam
	// fga_outbox несёт СЫРОЙ, не-source_version-guarded owner-hierarchy tuple:
	// WRITE(grant) и DELETE(revoke) ОДНОГО (user,relation,object) НЕ коммутативны,
	// поэтому iam включает ApplyConcurrency>1 ТОЛЬКО вместе с PartitionColumn.
	ApplyConcurrency int

	// PartitionColumn — SQL-выражение над строкой outbox-таблицы, дающее ключ
	// ПАРТИЦИИ порядка (для iam fga_outbox = `payload->>'object'`). Пусто (default)
	// = фича выключена, claim-запрос БАЙТ-в-БАЙТ прежний (нулевое изменение
	// поведения для всех текущих consumer'ов, кроме iam).
	//
	// # Зачем (order-preserving concurrent drain без cross-batch reorder-leak)
	//
	// ApplyConcurrency>1 сам по себе НЕ сохраняет порядок apply НИ внутри батча
	// (горутины конкурентны), НИ МЕЖДУ батчами: claim `ORDER BY (attempt_count,id)`
	// разносит bumped-предшественника (attempt≥1 после transient) и fresh-преемника
	// (attempt=0) в РАЗНЫЕ claim-батчи → преемник может заклеймиться и примениться
	// РАНЬШЕ своего предшественника. Для order-sensitive target'а (iam raw-tuple:
	// WRITE потом DELETE того же tuple) это delete-before-write → tuple ВЫЖИВАЕТ →
	// authz over-grant / cross-account LEAK.
	//
	// PartitionColumn закрывает это на CLAIM-уровне (не на apply-re-sort, который
	// бессилен, если предшественник в другом батче): claim НЕ берёт строку t, если
	// в её партиции существует ДОСТАВЛЯЕМЫЙ (sent_at IS NULL AND
	// attempt_count < MaxAttempts) предшественник с меньшим id. Тогда:
	//   - преемник НИКОГДА не заклеймлен впереди unsent-предшественника (cross-batch
	//     И cross-replica: незакоммиченный claim соседа виден как sent_at IS NULL в
	//     снапшоте → его преемник остаётся заблокирован до commit'а) →
	//     per-partition FIFO держится by construction;
	//   - в любом снапшоте claimable ровно ОДНА строка партиции (её head), поэтому
	//     один claim-батч НИКОГДА не содержит две строки одной партиции → intra-batch
	//     конкурентный reorder двух строк одной партиции НЕВОЗМОЖЕН by construction
	//     (apply-level partition-grouping не нужен — было бы vestigial).
	// Строки РАЗНЫХ партиций по-прежнему клеймятся и применяются конкурентно
	// (пропускная способность ApplyConcurrency сохранена — предикат сериализует
	// только внутри одной партиции).
	//
	// # Head-of-line wedge (осознанный trade-off leak-safety > per-partition liveness)
	//
	// Пока head партиции доставляем-но-ещё-не-доставлен (transient-stuck: peer down,
	// attempt капнут на MaxAttempts-1, ретраится вечно с backoff) — его преемники
	// ЖДУТ. Это ВРЕМЕННЫЙ wedge: transient-строка НИКОГДА не отравляется (cap ниже
	// poison-gate) → применится, как только peer оживёт → wedge рассосётся. Границей
	// wedge является восстановление peer'а, не бесконечность. ОТРАВЛЕННЫЙ (poisoned,
	// attempt_count=MaxAttempts) предшественник из блокирующего набора ИСКЛЮЧЁН
	// (предикат `attempt_count < MaxAttempts`), т.к. он НИКОГДА не применится —
	// вечно wedge'ить партицию им нельзя; преемник «перепрыгивает» мёртвого
	// предшественника. Leak это НЕ реинтродуцирует: отравленный WRITE не создаёт
	// tuple → последующий DELETE (или отсутствие эффекта) не оставляет над-гранта;
	// порядок между ДОСТАВЛЯЕМЫМИ строками партиции по-прежнему строгий. Wedge
	// наблюдаем: (а) table-wide `outbox_oldest_pending_age_seconds{table}` (metrics
	// Collector) растёт, пока любой head застрял → alertable; (б) per-partition
	// атрибуция — опциональный WithWedgeObserver + WedgeWarnAfter (см. ниже).
	//
	// # Контракт выражения
	//
	// PartitionColumn — выражение над НЕквалифицированными колонками строки,
	// начинающееся с идентификатора колонки, так что префикс `<alias>.` даёт
	// валидный квалифицированный SQL (`payload->>'object'` → `t.payload->>'object'`,
	// парсится как `(t.payload)->>'object'`). Значение выражения используется в
	// equi-сравнении партиций в claim-предикате; в claim-пути оно не логируется, но
	// per-partition wedge-observer (WithWedgeObserver) НАМЕРЕННО surfaces ключ в WARN
	// для атрибуции застрявшей партиции — это id-based object-handle (напр.
	// `vpc_network:<id>`), не PII/инфра-чувствительное. Rows с NULL-ключом партиции трактуются как
	// независимые (NULL=NULL не true) — для iam невозможно (object всегда задан).
	// Для перф NOT EXISTS ОБЯЗАТЕЛЕН partial-index
	// `((<PartitionColumn>), id) WHERE sent_at IS NULL` (миграция сервиса).
	PartitionColumn string

	// WedgeWarnAfter — если >0 И задан PartitionColumn И зарегистрирован
	// WithWedgeObserver, drainer периодически (rate-limited) сообщает партиции, чей
	// самый старый unsent-row старше этого порога (head-of-line wedge, см. выше).
	// 0 (default) = per-partition wedge-репорт выключен (нулевая стоимость; table-wide
	// oldest-pending-age метрика всё равно покрывает «доставка застряла»). Опрос
	// использует тот же partial-index, что и claim.
	WedgeWarnAfter time.Duration
}

// withDefaults заполняет нулевые поля конфигом по умолчанию.
func (c Config) withDefaults() Config {
	if c.BatchSize <= 0 {
		c.BatchSize = 32
	}
	if c.PollFallback <= 0 {
		c.PollFallback = 30 * time.Second
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 10
	}
	if c.BackoffMin <= 0 {
		c.BackoffMin = 1 * time.Second
	}
	if c.BackoffMax <= 0 {
		c.BackoffMax = 30 * time.Second
	}
	if c.ApplyTimeout <= 0 {
		c.ApplyTimeout = 5 * time.Second
	}
	if c.ApplyConcurrency < 1 {
		c.ApplyConcurrency = 1
	}
	return c
}

// Decoder[T] — превращает payload JSONB в типизированный T.
// Ошибка decoder-а трактуется как permanent (poisoned row), drainer не вызывает
// applier и помечает row attempt_count = MaxAttempts + last_error = err.
type Decoder[T any] func(payload []byte) (T, error)

// Applier[T] — применяет T к target-системе.
// Возвращает nil → success (drainer mark'ит sent_at).
// Возвращает ErrAlreadyApplied → idempotent success (drainer mark'ит sent_at).
// Возвращает любую другую error → transient (retry с exp backoff)
//
//	ИЛИ permanent (если errors.Is(err, ErrPermanent)).
//
// CONCURRENCY: при Config.ApplyConcurrency>1 drainer вызывает Applier из
// НЕСКОЛЬКИХ горутин одновременно (разные строки одного claim-батча) — Applier
// ОБЯЗАН быть безопасен для конкурентного вызова. Канонические appliers это
// gRPC-клиенты поверх *grpc.ClientConn (конкурентно-безопасен) + чистое
// построение запроса — они уже удовлетворяют требованию. Applier, шарящий
// mutable-состояние без синхронизации, обязан либо синхронизироваться, либо
// оставить ApplyConcurrency=1.
type Applier[T any] func(ctx context.Context, eventType string, payload T) error

// ErrAlreadyApplied — applier возвращает, когда target-система сообщила «уже есть»
// (для OpenFGA: HTTP 409 на write existing tuple; HTTP 404 на delete missing tuple).
// Drainer трактует как success.
var ErrAlreadyApplied = errors.New("drainer: target reports already-applied (idempotent)")

// ErrPermanent — applier wrap'ит в это, если retry бессмыслен (HTTP 4xx не-409,
// malformed payload, etc). Drainer mark'ит last_error и пропускает row через
// force attempt_count = MaxAttempts.
var ErrPermanent = errors.New("drainer: permanent error, no retry")

// Drainer[T] — экземпляр drainer-а для одного outbox-table + один applier.
//
// Drainer работает по схеме:
//  1. Слушает LISTEN-канал `cfg.Channel` через dedicated pgx.Conn (hijacked
//     из pool).
//  2. На старте — catch-up: SELECT pending rows (sent_at IS NULL, attempt_count < MaxAttempts)
//     ORDER BY attempt_count, id LIMIT BatchSize → applies each (attempt_count-first
//     ordering предотвращает starvation свежего intent транзитно-залипшим backlog'ом).
//  3. Main loop: wake-up по NOTIFY (payload = row id) ИЛИ tick(PollFallback)
//     → claim → apply → mark.
//  4. Exactly-once: pre-claim атомарный UPDATE … RETURNING с CAS (sent_at IS NULL
//     AND attempt_count < MaxAttempts) — две реплики не возьмут одну row.
//  5. Graceful shutdown: при ctx.Done() — дозавершает текущий in-flight apply
//     (отдельный inner ctx с ApplyTimeout grace), exit.
type Drainer[T any] struct {
	cfg     Config
	pool    *pgxpool.Pool
	decoder Decoder[T]
	applier Applier[T]
	logger  *slog.Logger

	// onPoison, if set, is invoked once each time a row is poisoned (permanent
	// error / decode-fail). Used to drive the outbox_poisoned_total metric
	// without coupling the drainer to the metrics package.
	onPoison func()

	// onClaim, if set, is invoked once each time a claim query is issued against
	// the outbox table (every SELECT…FOR UPDATE SKIP LOCKED claim, including the
	// terminal empty claim that ends a drain loop). Enables deterministic,
	// in-process observation of claim frequency — e.g. a test can assert an idle
	// drainer issues ZERO claims across an observation window (busy-poll guard)
	// without depending on asynchronous pg_stat counters.
	onClaim func()

	// onWedge, if set (with Config.PartitionColumn and Config.WedgeWarnAfter>0),
	// is invoked once per partition whose oldest unsent row is older than
	// WedgeWarnAfter — the head-of-line wedge signal for a partition-ordered drain
	// (a stuck/transient partition head blocks its successors by design). Wire it
	// to a WARN log / metric for per-partition attribution beyond the table-wide
	// outbox_oldest_pending_age_seconds gauge. The scan is rate-limited to at most
	// once per WedgeWarnAfter and uses the same partial index as the claim.
	onWedge func(partition string, oldestUnsentAge time.Duration)
	// lastWedgeCheck throttles the wedge scan. Accessed only from the single
	// Run/drainBatch goroutine — no synchronisation needed.
	lastWedgeCheck time.Time
}

// Option customises a Drainer at construction (functional-options pattern).
type Option[T any] func(*Drainer[T])

// WithPoisonObserver registers a callback invoked once per poisoned row. Wire it
// to a metrics recorder's IncPoisoned to make poison events observable
// (outbox_poisoned_total). nil is ignored.
func WithPoisonObserver[T any](fn func()) Option[T] {
	return func(d *Drainer[T]) {
		if fn != nil {
			d.onPoison = fn
		}
	}
}

// WithClaimObserver registers a callback invoked once per claim query issued
// against the outbox table. Enables deterministic in-process observation of
// claim frequency (busy-poll guard) independent of pg_stat lag. nil is ignored.
func WithClaimObserver[T any](fn func()) Option[T] {
	return func(d *Drainer[T]) {
		if fn != nil {
			d.onClaim = fn
		}
	}
}

// WithWedgeObserver registers a callback invoked (rate-limited) once per
// partition whose oldest unsent row exceeds Config.WedgeWarnAfter — the
// head-of-line wedge signal for a partition-ordered drain. Requires
// Config.PartitionColumn set and Config.WedgeWarnAfter>0 (otherwise the scan is
// skipped and this is a no-op). Wire it to a WARN log / gauge for per-partition
// attribution. nil is ignored.
func WithWedgeObserver[T any](fn func(partition string, oldestUnsentAge time.Duration)) Option[T] {
	return func(d *Drainer[T]) {
		if fn != nil {
			d.onWedge = fn
		}
	}
}

// New создает Drainer; не запускает (вызывайте Run).
//
// pool — *pgxpool.Pool на БД сервиса (тот же pool, что используется для
// бизнес-логики; drainer Acquire().Hijack() один conn для LISTEN, остальные
// операции — через pool как обычно).
//
// decoder/applier — пользовательские функции, см. Decoder[T] / Applier[T].
//
// logger — slog.Logger; nil → slog.Default().
func New[T any](
	pool *pgxpool.Pool,
	cfg Config,
	decoder Decoder[T],
	applier Applier[T],
	logger *slog.Logger,
	opts ...Option[T],
) (*Drainer[T], error) {
	if pool == nil {
		return nil, errors.New("drainer.New: pool is nil")
	}
	if cfg.Table == "" {
		return nil, errors.New("drainer.New: Config.Table required")
	}
	if cfg.Channel == "" {
		return nil, errors.New("drainer.New: Config.Channel required")
	}
	if decoder == nil {
		return nil, errors.New("drainer.New: decoder is nil")
	}
	if applier == nil {
		return nil, errors.New("drainer.New: applier is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}
	cfg = cfg.withDefaults()
	logger = logger.With(
		slog.String("component", "outbox_drainer"),
		slog.String("table", cfg.Table),
		slog.String("channel", cfg.Channel),
	)
	d := &Drainer[T]{
		cfg:     cfg,
		pool:    pool,
		decoder: decoder,
		applier: applier,
		logger:  logger,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d, nil
}

// Run — основной loop drainer-а. Блокирует до ctx.Done().
//
// Поведение:
//  1. Запускает LISTEN-loop в goroutine (own conn, reconnect on drop).
//  2. Выполняет startup catch-up (drains all pending rows).
//  3. Основной select: NOTIFY-wake-up ИЛИ PollFallback-tick ИЛИ ctx.Done().
//     Каждое wake-up → drainBatch() (claim + apply + mark, в loop пока не пусто).
//  4. На ctx.Done() — дозавершает текущий drainBatch (с inner ApplyTimeout-grace
//     на in-flight apply), exits.
//
// Возвращает nil при clean shutdown.
func (d *Drainer[T]) Run(ctx context.Context) error {
	// Wake-up signal channel — listenLoop signals on NOTIFY, processLoop consumes.
	// Buffered: один сигнал «есть работа», даже если processLoop в данный
	// момент busy — он перепроверит после возврата.
	wakeup := make(chan struct{}, 1)

	// LISTEN-goroutine: own its own ctx tied to parent. Errors from LISTEN
	// subscription (conn drop, NOTIFY parse) are caught and re-tried with
	// exp-backoff inside listenLoop. Panics propagate to the runtime — drainer
	// is process-fatal on unhandled panic (correct: such panics indicate
	// programmer error, not transient infra failure).
	listenDone := make(chan struct{})
	go func() {
		defer close(listenDone)
		d.listenLoop(ctx, wakeup)
	}()

	// Стартовая попытка катч-апа — выгребаем все накопленное до начала LISTEN.
	// Если в этот момент LISTEN еще не подключился и кто-то INSERTит — мы либо
	// поймаем через NOTIFY (после connect), либо через PollFallback. Race ok.
	d.drainBatch(ctx)

	poll := time.NewTicker(d.cfg.PollFallback)
	defer poll.Stop()

	for {
		select {
		case <-ctx.Done():
			// Wait for listen-goroutine to exit (it will once ctx is done).
			<-listenDone
			return nil
		case <-wakeup:
			d.drainBatch(ctx)
		case <-poll.C:
			d.drainBatch(ctx)
		}
	}
}
