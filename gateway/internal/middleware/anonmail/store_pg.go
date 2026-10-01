// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// Пределы звена (замысел З8, §8 «Константы кода»). Каждое число — одно место;
// второго литерала этих значений в пакете нет (УК53, гейт
// TestDecisionLimitLiteralsLiveOnlyInTheirDeclaration).
const (
	// anonMailStoreWait — предел ОДНОГО ожидания хранилища: блокировки
	// (`SET LOCAL lock_timeout`) и захвата соединения пула (срок контекста на
	// `pool.Acquire`) — одно число. Решение ключа — единицы операторов к базе,
	// и ожидание дольше означает очередь, а не конкуренцию.
	anonMailStoreWait = 250 * time.Millisecond
	// anonMailDecisionWaits — число ограниченных ожиданий на самом длинном
	// пути решения: захват соединения, три ключа IPv6 (/64, /56, /48), вставка
	// пометки, ждущая соседнюю незафиксированную вставку того же вызова, строка
	// ведра. У IPv4 их пять.
	anonMailDecisionWaits = 6
	// anonMailDecisionBudget — срок решения: захват соединения и все операторы
	// транзакции до COMMIT; тот же предел — `statement_timeout` и
	// `idle_in_transaction_session_timeout` сервера (decisionLimitsSQL).
	anonMailDecisionBudget = anonMailDecisionWaits * anonMailStoreWait
	// anonMailCommitWait — предел COMMIT решения, на контексте без отмены.
	anonMailCommitWait = anonMailStoreWait
	// anonMailCancelGrace — запасной дедлайн обработчика отмены пула
	// ограничителя: ответ на запрос отмены не пришёл за этот срок — сокет
	// получает дедлайн, соединение закрывается клиентом (З8 (3а)).
	anonMailCancelGrace = anonMailStoreWait
	// anonMailResolveBudget — срок разрешения исхода фиксации: исходная
	// транзакция при любом состоянии держателя кончается на сервере не позже
	// двух сроков решения от прихода её последнего оператора (И37).
	anonMailResolveBudget = 2 * anonMailDecisionBudget
	// anonMailPoolConns — соединений пула ограничителя на реплику: все решения
	// флота проходят строку ведра по одному, и соединений больше, чем «одно
	// держит ведро, три готовят свои блокировки», пропускной способности не
	// прибавляют.
	anonMailPoolConns = 4
	// anonMailConnLifetime и anonMailConnLifetimeJitter — срок жизни соединений
	// пула и его разброс (УК52 (2)): при разбросе 0 соединения одного возраста
	// истекали бы разом, и новое строилось бы внутри срока захвата.
	anonMailConnLifetime       = time.Hour
	anonMailConnLifetimeJitter = 10 * time.Minute
)

// limiterWarmUpBudget — срок прогрева пула при старте: ошибка либо срок —
// отказ старта, а не 503 первым клиентам.
const limiterWarmUpBudget = 30 * time.Second

// Пределы сервера в строке первого обращения транзакции решения.
const (
	limitStatement         = "statement_timeout"
	limitIdleInTransaction = "idle_in_transaction_session_timeout"
	limitLock              = "lock_timeout"
)

// decisionLimitsSQL — первое обращение транзакции решения (З8 (2), УК53):
// предел оператора и предел простоя в транзакции — срок решения, предел
// ожидания блокировки — anonMailStoreWait. Сессию, чья реплика исчезла посреди
// транзакции, сервер завершает сам (25P03) и снимает строку ведра и
// блокировки ключей. Параметров у строки нет: на Exec без аргументов pgx идёт
// простым протоколом, и три оператора одной строкой исполнимы.
var decisionLimitsSQL = decisionLimitsSQLFor(anonMailDecisionBudget, anonMailStoreWait)

// decisionLimitsSQLFor — ОДНО место перевода Duration в миллисекунды для строки
// пределов. omit — пределы, которых в строке нет (только для близнецов проб).
func decisionLimitsSQLFor(budget, wait time.Duration, omit ...string) string {
	parts := make([]string, 0, 3)
	for _, l := range []struct {
		name string
		d    time.Duration
	}{{limitStatement, budget}, {limitIdleInTransaction, budget}, {limitLock, wait}} {
		skip := false
		for _, o := range omit {
			skip = skip || o == l.name
		}
		if !skip {
			parts = append(parts, fmt.Sprintf("SET LOCAL %s = %d", l.name, l.d.Milliseconds()))
		}
	}
	return strings.Join(parts, "; ")
}

// Операторы транзакции решения.
const (
	// lockKeySQL — рекомендательная блокировка ключа формы с ДВУМЯ int4 (З8,
	// CX2-11). Форма с одним ключом в звене запрещена гейтом пакета.
	lockKeySQL = `SELECT pg_advisory_xact_lock($1::int4, $2::int4)`
	// markSpentSQL — пометка вызова использованным; 0 строк — уже истрачен.
	// Вставка ждёт соседнюю незафиксированную вставку того же вызова не дольше
	// lock_timeout.
	markSpentSQL = `INSERT INTO kacho_gateway.pow_spent (id, expires_at) VALUES ($1, $2)
ON CONFLICT (id) DO NOTHING RETURNING true`
	// bucketSQL — строка ведра общего потока, ПОСЛЕДНЕЙ после ключей.
	bucketSQL = `SELECT tokens, at FROM kacho_gateway.anon_mail_bucket WHERE id = 1 FOR UPDATE`
	// countsSQL — счёт ключа в трёх окнах (now − w, now] по индексу (key, at).
	countsSQL = `SELECT count(*) FILTER (WHERE at > $3),
       count(*) FILTER (WHERE at > $4),
       count(*) FILTER (WHERE at > $5)
  FROM kacho_gateway.anon_mail_passes
 WHERE key = $1 AND at <= $2 AND at > least($3, $4, $5)`
	// nthMomentSQL — момент номер $4 (с нуля, по возрастанию) в окне.
	nthMomentSQL = `SELECT at FROM kacho_gateway.anon_mail_passes
 WHERE key = $1 AND at > $2 AND at <= $3 ORDER BY at OFFSET $4 LIMIT 1`
	// recordPassSQL — моменты пропуска всех ключей запроса; RETURNING отдаёт
	// идентификатор транзакции решения — лишнего обращения нет.
	recordPassSQL = `INSERT INTO kacho_gateway.anon_mail_passes (key, at, decision_id)
SELECT k, $2, $3 FROM unnest($1::text[]) AS k RETURNING pg_current_xact_id()::text`
	// bucketWriteSQL — новое состояние ведра, когда решение взяло жетон.
	bucketWriteSQL = `UPDATE kacho_gateway.anon_mail_bucket SET tokens = $1, at = $2 WHERE id = 1`
)

// Операторы разрешения исхода фиксации — вне транзакции, на пуле ограничителя.
const (
	xactStatusSQL   = `SELECT pg_xact_status($1::xid8)`
	passRecordedSQL = `SELECT EXISTS (SELECT 1 FROM kacho_gateway.anon_mail_passes
 WHERE key = $1 AND at = $2 AND decision_id = $3)`
)

// PostgresStore — общее хранилище звена для флота: база хранилища
// однократности края, СВОЙ пул ограничителя (CX2-44 (б)).
type PostgresStore struct {
	*store
	b *pgBackend
}

// NewPostgresStore строит пул ограничителя по DSN хранилища однократности,
// прогревает его (anonMailPoolConns соединений, взятых разом) и отдаёт
// хранилище. Схему накатывает хранилище однократности при своём построении —
// поэтому пул строится после него (корень: buildAnonMailStore).
func NewPostgresStore(ctx context.Context, dsn string, l config.AnonMailLimits, log *slog.Logger) (*PostgresStore, error) {
	if log == nil {
		return nil, errors.New("anonmail: logger is required")
	}
	cfg, err := limiterPoolConfig(dsn)
	if err != nil {
		return nil, err
	}
	pool, err := newLimiterPool(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("anonmail: limiter pool: %w", err)
	}
	wctx, cancel := context.WithTimeout(ctx, limiterWarmUpBudget)
	defer cancel()
	if err := warmUp(wctx, pool, anonMailPoolConns); err != nil {
		pool.Close()
		return nil, fmt.Errorf("anonmail: limiter pool warm-up: %w", err)
	}
	return newPostgresStoreWithPool(pool, l, log, true), nil
}

func newPostgresStoreWithPool(pool *pgxpool.Pool, l config.AnonMailLimits, log *slog.Logger, owns bool) *PostgresStore {
	b := &pgBackend{pool: pool, owns: owns, log: log, limitsSQL: decisionLimitsSQL, lockSQL: lockKeySQL, classify: classifyPoll}
	b.onCommitError = func(ctx context.Context, pc pendingCommit, deadline time.Time) Outcome {
		return b.resolveCommit(ctx, pgResolveProbe{pool: b.pool}, pc, deadline)
	}
	return &PostgresStore{store: newStore(b, l, log), b: b}
}

// limiterPoolConfig — конфигурация пула ограничителя (З8 (3), (3а), (4)):
// пределы задаёт транзакция, а не пул, поэтому пул строится ParseConfig, а не
// общим конструктором; то, что общий конструктор ставит сверх пределов
// (обработчик отмены), задано явно.
func limiterPoolConfig(dsn string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("anonmail: limiter pool DSN: %w", err)
	}
	cfg.MaxConns = anonMailPoolConns
	cfg.MinConns = anonMailPoolConns
	cfg.MaxConnLifetime = anonMailConnLifetime
	cfg.MaxConnLifetimeJitter = anonMailConnLifetimeJitter
	// При срыве срока серверу уходит запрос отмены, сервер прерывает оператор и
	// отвечает; клиент читает НАСТОЯЩИЙ ответ, транзакция откатывается на том
	// же соединении, и прогретое соединение возвращается в пул. Ответа на
	// отмену нет за anonMailCancelGrace — сокет получает дедлайн.
	cfg.ConnConfig.BuildContextWatcherHandler = func(c *pgconn.PgConn) ctxwatch.Handler {
		return &pgconn.CancelRequestContextWatcherHandler{Conn: c, CancelRequestDelay: 0, DeadlineDelay: anonMailCancelGrace}
	}
	return cfg, validateLimiterPoolConfig(cfg)
}

// validateLimiterPoolConfig — отказ с именем поля, если конфигурация пула
// отступила от решения З8.
func validateLimiterPoolConfig(cfg *pgxpool.Config) error {
	switch {
	case cfg.MaxConns != anonMailPoolConns:
		return fmt.Errorf("anonmail: limiter pool MaxConns = %d, the decision is %d", cfg.MaxConns, anonMailPoolConns)
	case cfg.MinConns != anonMailPoolConns:
		return fmt.Errorf("anonmail: limiter pool MinConns = %d, the decision is %d (warm pool)", cfg.MinConns, anonMailPoolConns)
	case cfg.MaxConnLifetime <= 0:
		return errors.New("anonmail: limiter pool MaxConnLifetime is not set")
	case cfg.MaxConnLifetimeJitter <= 0:
		return errors.New("anonmail: limiter pool MaxConnLifetimeJitter must be > 0 — connections of one age would expire at once")
	case cfg.ConnConfig.BuildContextWatcherHandler == nil:
		return errors.New("anonmail: limiter pool BuildContextWatcherHandler is not set")
	}
	h, ok := cfg.ConnConfig.BuildContextWatcherHandler(nil).(*pgconn.CancelRequestContextWatcherHandler)
	if !ok || h.CancelRequestDelay != 0 || h.DeadlineDelay != anonMailCancelGrace {
		return errors.New("anonmail: limiter pool BuildContextWatcherHandler is not CancelRequest{0, anonMailCancelGrace}")
	}
	return nil
}

func newLimiterPool(ctx context.Context, cfg *pgxpool.Config) (*pgxpool.Pool, error) {
	return pgxpool.NewWithConfig(ctx, cfg)
}

// warmUp берёт n соединений ОДНОВРЕМЕННО — все, и только затем отпускает все
// (УК52 (1)): последовательное «взять — отпустить» прогрело бы одно.
func warmUp(ctx context.Context, pool *pgxpool.Pool, n int) error {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		held []*pgxpool.Conn
		errs []error
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := pool.Acquire(ctx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			held = append(held, c)
		}()
	}
	wg.Wait()
	for _, c := range held {
		c.Release()
	}
	return errors.Join(errs...)
}

// pendingCommit — что нужно, чтобы выяснить исход отправленной фиксации.
type pendingCommit struct {
	xid        string
	key        string
	at         time.Time
	decisionID [16]byte
}

// pgBackend — транзакции решения на пуле ограничителя.
type pgBackend struct {
	pool *pgxpool.Pool
	owns bool
	log  *slog.Logger
	// limitsSQL — первое обращение транзакции решения (decisionLimitsSQL).
	limitsSQL string
	// lockSQL — оператор блокировки ключа (lockKeySQL).
	lockSQL string
	// onCommitError — что делать с ошибкой COMMIT: разрешить исход
	// (resolveCommit). Ошибка COMMIT — НЕ StoreUnavailable: был ли COMMIT
	// исполнен сервером, клиенту неизвестно.
	onCommitError func(ctx context.Context, pc pendingCommit, deadline time.Time) Outcome
	// classify — класс ответа опроса pg_xact_status (classifyPoll).
	classify func(*string, error) pollClass
}

func (b *pgBackend) begin(ctx context.Context) (decisionTx, error) { return b.beginDecision(ctx) }

func (b *pgBackend) close() error {
	if b.owns {
		b.pool.Close()
	}
	return nil
}

// beginDecision — ОДНА дверь в транзакцию на пуле ограничителя (З8 (3)):
// захват соединения не дольше anonMailStoreWait, затем первое обращение
// транзакции — строка пределов сервера.
func (b *pgBackend) beginDecision(ctx context.Context) (*pgTx, error) {
	actx, cancel := context.WithTimeout(ctx, anonMailStoreWait)
	conn, err := b.pool.Acquire(actx)
	cancel()
	if err != nil {
		return nil, err
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		conn.Release()
		return nil, err
	}
	x := &pgTx{b: b, conn: conn, tx: tx}
	if _, err := tx.Exec(ctx, b.limitsSQL); err != nil {
		x.rollback()
		return nil, err
	}
	return x, nil
}

type pgTx struct {
	b       *pgBackend
	conn    *pgxpool.Conn
	tx      pgx.Tx
	pending pendingCommit
}

func (x *pgTx) lock(ctx context.Context, pairs []lockPair) error {
	for _, p := range pairs {
		if _, err := x.tx.Exec(ctx, x.b.lockSQL, p.class, p.obj); err != nil {
			return err
		}
	}
	return nil
}

func (x *pgTx) markSpent(ctx context.Context, p Proof) (bool, error) {
	var ok bool
	err := x.tx.QueryRow(ctx, markSpentSQL, p.ID[:], p.ExpiresAt).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (x *pgTx) bucket(ctx context.Context) (float64, time.Time, error) {
	var (
		tokens float64
		at     time.Time
	)
	err := x.tx.QueryRow(ctx, bucketSQL).Scan(&tokens, &at)
	return tokens, at, err
}

func (x *pgTx) counts(ctx context.Context, key string, now time.Time, windows []time.Duration) ([]int, error) {
	if len(windows) == 0 || len(windows) > 3 {
		return nil, fmt.Errorf("anonmail: %d windows, the statement reads 1..3", len(windows))
	}
	bounds := make([]time.Time, 3)
	for i := range bounds {
		w := windows[min(i, len(windows)-1)]
		bounds[i] = now.Add(-w)
	}
	var c [3]int
	if err := x.tx.QueryRow(ctx, countsSQL, key, now, bounds[0], bounds[1], bounds[2]).Scan(&c[0], &c[1], &c[2]); err != nil {
		return nil, err
	}
	return c[:len(windows)], nil
}

func (x *pgTx) nthMoment(ctx context.Context, key string, now time.Time, window time.Duration, offset int) (time.Time, error) {
	var at time.Time
	err := x.tx.QueryRow(ctx, nthMomentSQL, key, now.Add(-window), now, offset).Scan(&at)
	return at, err
}

func (x *pgTx) recordPass(ctx context.Context, keys Keys, now time.Time, _ *Proof, bw *bucketWrite) error {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	rows, err := x.tx.Query(ctx, recordPassSQL, keys.All(), now, id[:])
	if err != nil {
		return err
	}
	var xid string
	for rows.Next() {
		if xid != "" {
			continue
		}
		if err := rows.Scan(&xid); err != nil {
			rows.Close()
			return err
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if xid == "" {
		return errors.New("anonmail: pass moments were not recorded")
	}
	if bw != nil {
		if _, err := x.tx.Exec(ctx, bucketWriteSQL, bw.tokens, bw.at); err != nil {
			return err
		}
	}
	x.pending = pendingCommit{xid: xid, key: keys.Source, at: now, decisionID: id}
	return nil
}

// commit — COMMIT на контексте без отмены со сроком anonMailCommitWait (З8
// (1а)): ни отмена запроса клиентом, ни исчерпанный срок решения не обрывают
// уже отправленную фиксацию. Срок разрешения исхода отсчитывается от отправки
// COMMIT.
func (x *pgTx) commit(ctx context.Context) Outcome {
	deadline := time.Now().Add(anonMailResolveBudget)
	cctx, cancel := context.WithTimeout(ctx, anonMailCommitWait)
	err := x.tx.Commit(cctx)
	cancel()
	x.tx = nil
	x.conn.Release()
	x.conn = nil
	if err == nil {
		return Pass
	}
	return x.b.onCommitError(ctx, x.pending, deadline)
}

// rollback — откат на контексте без отмены со сроком anonMailStoreWait;
// соединение возвращается в пул (после отмены оператора — живым).
func (x *pgTx) rollback() {
	if x.tx != nil {
		rctx, cancel := context.WithTimeout(context.Background(), anonMailStoreWait)
		_ = x.tx.Rollback(rctx)
		cancel()
		x.tx = nil
	}
	if x.conn != nil {
		x.conn.Release()
		x.conn = nil
	}
}

// pollClass — класс ответа опроса pg_xact_status (УК57). Перечень закрыт, и
// каждый ответ попадает ровно в один класс.
type pollClass int

const (
	// pollInProgress — исходная транзакция на сервере не кончилась: ждать шаг.
	pollInProgress pollClass = iota + 1
	// pollEnded — committed, aborted либо NULL (старше усечения журнала
	// статусов): конец исходной транзакции — читать строку.
	pollEnded
	// pollFuture — 22023 «transaction ID … is in the future»: опрошена база с
	// меньшим счётчиком (после переключения), фиксация до неё не дошла —
	// читать строку сразу.
	pollFuture
	// pollTransient — ответа о транзакции нет: ошибка соединения, отмена,
	// 57014, любой иной отказ сервера — повторить опрос в пределах срока.
	pollTransient
)

// classifyPoll — класс ответа опроса.
func classifyPoll(status *string, err error) pollClass {
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "22023" {
			return pollFuture
		}
		return pollTransient
	}
	if status == nil {
		return pollEnded
	}
	switch *status {
	case "in progress":
		return pollInProgress
	case "committed", "aborted":
		return pollEnded
	}
	// Ответ вне контракта сервера — не ответ о транзакции.
	return pollTransient
}

// commitProbe — два оператора разрешения исхода фиксации.
type commitProbe interface {
	xactStatus(ctx context.Context, xid string) (*string, error)
	passRecorded(ctx context.Context, pc pendingCommit) (bool, error)
}

// resolveCommit — исход фиксации разрешается, а не угадывается (З8, CX2-46
// (б)). Сначала дождаться конца исходной транзакции (опрос pg_xact_status с
// шагом anonMailStoreWait), затем отдельным оператором — свой снимок после её
// конца — прочесть строку момента этого решения. Строка есть — Pass, строки
// нет — StoreUnavailable (вызов не истрачен). За весь срок разрешения строку
// прочесть не удалось — остаток: 503 и WARN «исход фиксации не разрешён» с
// decision_id; пропуска без подтверждённой записи нет.
func (b *pgBackend) resolveCommit(ctx context.Context, probe commitProbe, pc pendingCommit, deadline time.Time) Outcome {
	rctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	polls := 0
	for {
		polls++
		pctx, pcancel := context.WithTimeout(rctx, anonMailStoreWait)
		status, err := probe.xactStatus(pctx, pc.xid)
		pcancel()
		switch b.classify(status, err) {
		case pollEnded, pollFuture:
			rdctx, rcancel := context.WithTimeout(rctx, anonMailStoreWait)
			ok, rerr := probe.passRecorded(rdctx, pc)
			rcancel()
			if rerr == nil {
				if ok {
					return Pass
				}
				return StoreUnavailable
			}
		case pollInProgress, pollTransient:
		}
		t := time.NewTimer(anonMailStoreWait)
		select {
		case <-rctx.Done():
			t.Stop()
			b.log.Warn("anon mail limiter: commit outcome unresolved; answering 503",
				"decision_id", hex.EncodeToString(pc.decisionID[:]), "polls", polls)
			return StoreUnavailable
		case <-t.C:
		}
	}
}

// pgResolveProbe — операторы разрешения на пуле ограничителя, вне транзакции:
// их ровно два (гейт пакета).
type pgResolveProbe struct{ pool *pgxpool.Pool }

func (p pgResolveProbe) xactStatus(ctx context.Context, xid string) (*string, error) {
	var status *string
	err := p.pool.QueryRow(ctx, xactStatusSQL, xid).Scan(&status)
	return status, err
}

func (p pgResolveProbe) passRecorded(ctx context.Context, pc pendingCommit) (bool, error) {
	var ok bool
	err := p.pool.QueryRow(ctx, passRecordedSQL, pc.key, pc.at, pc.decisionID[:]).Scan(&ok)
	return ok, err
}
