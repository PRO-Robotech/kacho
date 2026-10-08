// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package limits

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/corelib/notify/address"
	"github.com/PRO-Robotech/corelib/notify/feed"
)

// Константы, не ручки (§8 замысла; довод чисел — там же и в З24).
const (
	// FenceLockTimeout — предел ожидания табличного замка записи ограды при
	// старте реплики (CX1-69 (в), CX1-70). Исход по нему — `55P03`, отказ старта.
	FenceLockTimeout = 5 * time.Second
	// TxStatementTimeout — серверный предел оператора транзакций резерва,
	// освобождения и записи ограды (SDR-К1), и срок собственного контекста
	// освобождения (SDR-Н1).
	TxStatementTimeout = 10 * time.Second
	// TxIdleTimeout — серверный предел простоя тех же транзакций (SDR-К1):
	// держатель, замолчавший посреди транзакции, снимается сервером, а не
	// TCP keepalive.
	TxIdleTimeout = 2 * time.Second
)

// KeyMinBytes — нижняя граница длины ключа сетки (Д89): ключ HMAC-SHA256 не
// короче выхода хеша.
const KeyMinBytes = 32

// fingerprintLabel — метка отпечатка ключа сетки (З24).
const fingerprintLabel = "kacho-notify/recipient-key-id"

// Сторожа резерва. Каждый отличим по errors.Is от прочих и от ошибки базы.
var (
	// ErrRecipientKeySuperseded — ограда несёт отпечаток другого ключа сетки:
	// стартовала реплика с другим ключом. Это не исход строки (клетка 9 не
	// выбирается): реплика перестаёт брать строки и выходит, когда
	// исполнители с закоммиченным резервом доведут `Ack` (З24, CX1-68 (в)).
	ErrRecipientKeySuperseded = errors.New("ключ сетки заменён: ограда несёт отпечаток другого ключа")
	// ErrRecipientNetExhausted — сетка на адресата исчерпана (клетка 9). Конкретное
	// окно и момент его освобождения несёт [*ExhaustedError].
	ErrRecipientNetExhausted = errors.New("сетка на адресата исчерпана")
	// ErrGlobalCeilingReached — суточный потолок потока достигнут: notice не
	// резервируется до конца суток UTC, security идёт (NTF1-H05).
	ErrGlobalCeilingReached = errors.New("суточный потолок потока достигнут")
)

// ExhaustedError — исчерпание окна сетки: класс и момент освобождения окна,
// по которому конвейер выбирает отсрочку или истечение строки.
type ExhaustedError struct {
	Class  feed.Class
	FreeAt time.Time
}

func (e *ExhaustedError) Error() string {
	return fmt.Sprintf("%v: класс %s, окно освобождается %s", ErrRecipientNetExhausted, e.Class, e.FreeAt.UTC().Format(time.RFC3339))
}

// Is — исчерпание окна есть исчерпание сетки.
func (e *ExhaustedError) Is(target error) bool { return target == ErrRecipientNetExhausted }

// Row — строка ленты глазами сетки: источник, класс, нормализованный адресат.
type Row struct {
	Source string
	Class  feed.Class
	To     address.Normalized
}

// Options — зависимости [Limiter].
type Options struct {
	// Pool — пул собственной базы kacho_notify.
	Pool *pgxpool.Pool
	// Key — ключ сетки (`notify.recipientKey`), не короче [KeyMinBytes].
	Key []byte
	// Grid — сетка и потолок установки.
	Grid Grid
	// Now — часы окон сетки и суток потолка.
	Now func() time.Time
	// Registerer — реестр метрик лимитов.
	Registerer prometheus.Registerer
	// Logger — журнал записи ограды и неудач освобождения.
	Logger *slog.Logger
}

// Limiter — сетка на адресата, потолок потока и ограда ключа сетки одной
// реплики над общей базой kacho_notify.
type Limiter struct {
	pool        *pgxpool.Pool
	key         []byte
	fingerprint []byte
	grid        Grid
	now         func() time.Time
	log         *slog.Logger

	// superseded — первый сторож ограды пойман: ключ этой реплики заменён, и
	// следующий резерв отвечает сторожем, не обращаясь к базе.
	superseded atomic.Bool

	netHits      *prometheus.CounterVec
	ceilingHits  prometheus.Counter
	reserveDBErr prometheus.Counter
}

// New собирает [Limiter]. Неполные опции, ключ короче [KeyMinBytes] и сетка
// вне границ — отказ.
func New(o Options) (*Limiter, error) {
	switch {
	case o.Pool == nil:
		return nil, errors.New("сетка лимитов: пул базы не задан")
	case len(o.Key) < KeyMinBytes:
		return nil, fmt.Errorf("сетка лимитов: ключ сетки короче %d байт", KeyMinBytes)
	case o.Now == nil:
		return nil, errors.New("сетка лимитов: часы не заданы")
	case o.Registerer == nil:
		return nil, errors.New("сетка лимитов: реестр метрик не задан")
	case o.Logger == nil:
		return nil, errors.New("сетка лимитов: журнал не задан")
	}
	if err := o.Grid.Validate(); err != nil {
		return nil, err
	}
	netHits, err := registerVec(o.Registerer, prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "notify_recipient_net_hits_total",
		Help: "Reservations refused because a recipient net window of the class is exhausted.",
	}, []string{"class"}))
	if err != nil {
		return nil, err
	}
	ceilingHits, err := registerVec(o.Registerer, prometheus.NewCounter(prometheus.CounterOpts{
		Name: "notify_global_ceiling_hits_total",
		Help: "Notice reservations refused because the installation daily ceiling is reached.",
	}))
	if err != nil {
		return nil, err
	}
	dbErr, err := registerVec(o.Registerer, prometheus.NewCounter(prometheus.CounterOpts{
		Name: "notify_reserve_db_errors_total",
		Help: "Reservation transactions failed by a database error (not a limit outcome): no reservation, no Ack.",
	}))
	if err != nil {
		return nil, err
	}
	// Ряды — классы сети: у класса только процесса окон сетки нет (windowsOf).
	for _, c := range NetworkClasses() {
		netHits.WithLabelValues(string(c))
	}
	key := append([]byte(nil), o.Key...)
	return &Limiter{
		pool:         o.Pool,
		key:          key,
		fingerprint:  fingerprintOf(key),
		grid:         o.Grid,
		now:          o.Now,
		log:          o.Logger,
		netHits:      netHits,
		ceilingHits:  ceilingHits,
		reserveDBErr: dbErr,
	}, nil
}

// fingerprintOf — отпечаток ключа сетки: первые 16 байт HMAC-SHA256 над меткой
// (З24). Ни ключа, ни его хеша без метки в базе и журнале нет.
func fingerprintOf(key []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(fingerprintLabel))
	return m.Sum(nil)[:16]
}

// period — окно сетки.
type period string

const (
	periodHour period = "hour"
	periodDay  period = "day"
)

// netWindow — ключ строки сетки и предел окна. Порядок полей — порядок
// первичного ключа `(key, class, period, window_start)`.
type netWindow struct {
	class  feed.Class
	period period
	start  time.Time
	limit  int
}

func (w netWindow) end() time.Time {
	if w.period == periodHour {
		return w.start.Add(time.Hour)
	}
	return w.start.AddDate(0, 0, 1)
}

// Reservation — запись резерва в обработке строки: ключи строк сетки и сутки
// потолка, под которыми резерв сделан, и признак «освобождён». Освобождение
// берёт ключи отсюда, а не вычисляет их заново (SDR-Н3).
type Reservation struct {
	netKey  []byte
	windows []netWindow
	day     time.Time

	mu       sync.Mutex
	released bool
}

// dayOf — сутки UTC момента.
func dayOf(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// windowsOf — окна класса в момент now, в порядке первичного ключа строк
// сетки ('day' < 'hour'): резерв и освобождение берут замки в этом порядке.
func (l *Limiter) windowsOf(class feed.Class, now time.Time) ([]netWindow, error) {
	now = now.UTC()
	switch class {
	case feed.ClassSecurity:
		return []netWindow{{class, periodDay, dayOf(now), l.grid.SecurityPerDay}}, nil
	case feed.ClassNotice:
		return []netWindow{
			{class, periodDay, dayOf(now), l.grid.NoticePerDay},
			{class, periodHour, now.Truncate(time.Hour), l.grid.NoticePerHour},
		}, nil
	}
	return nil, fmt.Errorf("класс строки %q вне перечня %v", class, NetworkClasses())
}

// netKeyOf — ключ строки сетки адресата: HMAC-SHA256(ключ сетки, адрес).
func (l *Limiter) netKeyOf(to address.Normalized) ([]byte, error) {
	v, err := to.Value()
	if err != nil {
		return nil, err
	}
	m := hmac.New(sha256.New, l.key)
	m.Write([]byte(v))
	return m.Sum(nil), nil
}

// txLimits — первые операторы транзакций резерва, освобождения и записи
// ограды: серверный предел оператора и простоя (SDR-К1). Замков не берут.
var txLimits = fmt.Sprintf("SET LOCAL statement_timeout = '%dms'; SET LOCAL idle_in_transaction_session_timeout = '%dms'",
	TxStatementTimeout.Milliseconds(), TxIdleTimeout.Milliseconds())

// begin открывает транзакцию с явным READ COMMITTED (CX1-68 (б)) и
// серверным пределом.
func (l *Limiter) begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := l.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, txLimits); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

const (
	readFenceSQL = `SELECT 1 FROM recipient_key_fence WHERE fingerprint = $1 FOR SHARE`

	reserveNetSQL = `INSERT INTO recipient_net (key, class, period, window_start, count)
VALUES ($1, $2, $3, $4, 1)
ON CONFLICT (key, class, period, window_start)
DO UPDATE SET count = recipient_net.count + 1 WHERE recipient_net.count < $5
RETURNING count`

	reserveCeilingSQL = `INSERT INTO global_daily (day, count) VALUES ($1, 1)
ON CONFLICT (day) DO UPDATE SET count = global_daily.count + 1 WHERE global_daily.count < $2
RETURNING count`

	// security идёт выше потолка (NTF1-H05), но в потоке суток учитывается.
	reserveSecurityFlowSQL = `INSERT INTO global_daily (day, count) VALUES ($1, 1)
ON CONFLICT (day) DO UPDATE SET count = global_daily.count + 1
RETURNING count`

	releaseNetSQL = `UPDATE recipient_net SET count = count - 1
WHERE key = $1 AND class = $2 AND period = $3 AND window_start = $4 AND count > 0`

	releaseCeilingSQL = `UPDATE global_daily SET count = count - 1 WHERE day = $1 AND count > 0`

	ceilingSQL = `SELECT coalesce((SELECT count FROM global_daily WHERE day = $1), 0)`
)

// guardOutcome — исход резерва, выбранный сторожем, а не ошибкой базы.
type guardOutcome struct{ err error }

func (g guardOutcome) Error() string { return g.err.Error() }
func (g guardOutcome) Unwrap() error { return g.err }

// Reserve — резерв строки до `MAIL FROM`: одна транзакция READ COMMITTED без
// сетевых вызовов — ограда (`FOR SHARE`), окна сетки класса по порядку
// первичного ключа, затем потолок потока. Ноль строк любого CAS — откат и
// сторож: [ErrRecipientKeySuperseded], [*ExhaustedError] либо
// [ErrGlobalCeilingReached]. Любая иная ошибка — ошибка базы, не сторож:
// резерва нет, `notify_reserve_db_errors_total` +1 (CX1-68 (б)).
func (l *Limiter) Reserve(ctx context.Context, row Row) (*Reservation, error) {
	if l.superseded.Load() {
		return nil, ErrRecipientKeySuperseded
	}
	key, err := l.netKeyOf(row.To)
	if err != nil {
		return nil, fmt.Errorf("резерв: адресат строки: %w", err)
	}
	now := l.now().UTC()
	windows, err := l.windowsOf(row.Class, now)
	if err != nil {
		return nil, fmt.Errorf("резерв: %w", err)
	}
	res := &Reservation{netKey: key, windows: windows, day: dayOf(now)}
	err = l.reserveTx(ctx, row.Class, res)
	var guard guardOutcome
	switch {
	case err == nil:
		return res, nil
	case errors.As(err, &guard):
		l.countGuard(row.Class, guard.err)
		return nil, guard.err
	default:
		l.reserveDBErr.Inc()
		return nil, fmt.Errorf("резерв: ошибка базы (не сторож сетки): %w", err)
	}
}

func (l *Limiter) countGuard(class feed.Class, err error) {
	switch {
	case errors.Is(err, ErrRecipientKeySuperseded):
		if !l.superseded.Swap(true) {
			l.log.Error("ключ сетки заменён: ограда несёт отпечаток ключа другой реплики; новых резервов нет")
		}
	case errors.Is(err, ErrRecipientNetExhausted):
		l.netHits.WithLabelValues(string(class)).Inc()
	case errors.Is(err, ErrGlobalCeilingReached):
		l.ceilingHits.Inc()
	}
}

// reserveTx — тело транзакции резерва. Сторож возвращается [guardOutcome],
// ошибка базы — как есть; откат — на любом исходе, кроме коммита.
func (l *Limiter) reserveTx(ctx context.Context, class feed.Class, res *Reservation) (err error) {
	tx, err := l.begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()
	var one int
	if err := tx.QueryRow(ctx, readFenceSQL, l.fingerprint).Scan(&one); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return guardOutcome{ErrRecipientKeySuperseded}
		}
		return err
	}
	for _, w := range res.windows {
		var n int
		err := tx.QueryRow(ctx, reserveNetSQL, res.netKey, string(w.class), string(w.period), w.start, w.limit).Scan(&n)
		if errors.Is(err, pgx.ErrNoRows) {
			return guardOutcome{&ExhaustedError{Class: w.class, FreeAt: w.end()}}
		}
		if err != nil {
			return err
		}
	}
	var n int
	if class == feed.ClassSecurity {
		err = tx.QueryRow(ctx, reserveSecurityFlowSQL, res.day).Scan(&n)
	} else {
		err = tx.QueryRow(ctx, reserveCeilingSQL, res.day, l.grid.GlobalPerDay).Scan(&n)
		if errors.Is(err, pgx.ErrNoRows) {
			return guardOutcome{ErrGlobalCeilingReached}
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Release — освобождение резерва на исходе строки, отличном от `SENT`.
// Однократно: повтор (повтор `Ack` после записанного исхода) вклада второй
// раз не снимает (УК30). Ключи — резерва (SDR-Н3), порядок замков — тот же,
// что у резерва. Контекст — собственный: крайний момент строки освобождения
// не отменяет (SDR-Н1). Неудача — запись в журнал и ошибка; вклад остаётся до
// конца окна, как резерв упавшей реплики.
func (l *Limiter) Release(ctx context.Context, res *Reservation) error {
	if res == nil {
		return errors.New("освобождение: резерва нет")
	}
	res.mu.Lock()
	defer res.mu.Unlock()
	if res.released {
		return nil
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), TxStatementTimeout)
	defer cancel()
	if err := l.releaseTx(rctx, res); err != nil {
		l.log.Warn("резерв сетки не освобождён: вклад остаётся до конца окна", "err", err.Error())
		return fmt.Errorf("освобождение резерва: %w", err)
	}
	res.released = true
	return nil
}

func (l *Limiter) releaseTx(ctx context.Context, res *Reservation) (err error) {
	tx, err := l.begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()
	for _, w := range res.windows {
		if _, err := tx.Exec(ctx, releaseNetSQL, res.netKey, string(w.class), string(w.period), w.start); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, releaseCeilingSQL, res.day); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CeilingReached — суточный потолок потока на сутки UTC текущего момента
// достигнут: `Claim` берёт только security (NTF1-H05).
func (l *Limiter) CeilingReached(ctx context.Context) (bool, error) {
	var n int
	if err := l.pool.QueryRow(ctx, ceilingSQL, dayOf(l.now())).Scan(&n); err != nil {
		return false, fmt.Errorf("потолок потока: %w", err)
	}
	return n >= l.grid.GlobalPerDay, nil
}
