// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package idempotencypg — ОБЩЕЕ хранилище однократности края (Idempotency-Key),
// разделяемое всеми репликами флота.
//
// # Зачем оно есть (#694)
//
// Однократность — инвариант, и держать его обязан слой, охватывающий ВЕСЬ домен
// параллелизма, в котором её обходят (правило #10). У края этот домен — флот
// подов: посадка объявляет автомасштабирование, значит повтор, попавший в
// соседнюю реплику, записи в чужой памяти не находит и уходит к downstream.
// Хранилище в памяти процесса корректно ровно до второй реплики.
//
// # Чем держится однократность здесь
//
// Допуск — ОДИН оператор: `INSERT … ON CONFLICT (key) DO UPDATE … WHERE …
// RETURNING`. Уникальность ключа — первичный ключ таблицы, поэтому «проверить и
// погасить» неделимо: под конкуренцией строку создаёт ровно одна транзакция, а
// остальные попадают в ветку конфликта. Пара «SELECT, потом INSERT» здесь была
// бы ровно тем check-then-act, который правило #10 запрещает: два одновременных
// предъявления одного ключа промахнулись бы оба и оба мутировали (CWE-362).
//
// Ветка `DO UPDATE … WHERE` — не «перезапись чужого ответа», а ПЕРЕХВАТ брони,
// и он разрешён ровно в двух случаях: запись просрочена по TTL, либо держатель
// умер, не оставив исхода (его бронь просрочена). Оба условия проверяет сам
// оператор, по часам СЕРВЕРА — часы реплик между собой не сверены и сверять их
// нечем.
//
// # Погашение живёт до истечения ключа, потом убирается
//
// Строка живёт `TTL` и удаляется фоновым сборщиком партиями. Без него хранилище
// росло бы без границы: у ключа, предъявленного один раз, нет никого, кто
// пришёл бы его убрать.
package idempotencypg

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver для goose
	"github.com/pressly/goose/v3"

	"github.com/PRO-Robotech/kacho/gateway/internal/idempotencypg/migrations"
	"github.com/PRO-Robotech/kacho/gateway/internal/middleware"
	"github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/safeconv"
)

// schemaLockID — идентификатор advisory-блокировки, под которой накатывается
// схема. Одна константа на всё хранилище: несколько реплик, стартующих
// одновременно, накатят цепочку миграций ровно один раз, а не наперегонки.
const schemaLockID int64 = 694_0001

// Defaults — умолчания, если вызывающий их не задал.
const (
	DefaultPollInterval = 25 * time.Millisecond
	DefaultReapInterval = time.Hour
	// reapBatch — сколько просроченных строк сборщик уносит за один оператор.
	// Партия ограничена намеренно: одиночный DELETE по огромному хвосту держал
	// бы блокировки дольше, чем живёт запрос.
	reapBatch = 1000
)

// Config — параметры построения хранилища.
type Config struct {
	// DSN — адрес базы края. Пустой недопустим: производить его из чужого
	// адреса запрещено (security.md §9) — тогда хранилище выглядело бы
	// настроенным и вело в никуда.
	DSN string
	// TTL — сколько живёт погашение ключа.
	TTL time.Duration
	// LeaseTTL — срок брони держателя.
	LeaseTTL time.Duration
	// PollInterval — с каким шагом ждущий спрашивает исход держателя.
	PollInterval time.Duration
	// ReapInterval — с каким шагом сборщик уносит просроченные записи.
	ReapInterval time.Duration
	// Logger — куда писать о сборщике. nil → slog.Default().
	Logger *slog.Logger
}

func (c Config) withDefaults() Config {
	if c.TTL <= 0 {
		c.TTL = middleware.IdempotencyTTL
	}
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = middleware.IdempotencyLeaseTTL
	}
	if c.PollInterval <= 0 {
		c.PollInterval = DefaultPollInterval
	}
	if c.ReapInterval <= 0 {
		c.ReapInterval = DefaultReapInterval
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

// Store — общее хранилище однократности поверх Postgres.
type Store struct {
	pool     *pgxpool.Pool
	ownsPool bool
	cfg      Config
	// baseCtx живёт столько же, сколько хранилище, и НЕ наследует отмену
	// контекста построения: тот принадлежит старту процесса и вправе иметь срок,
	// а сборщик обязан работать всю жизнь хранилища. Значения (журнал, трассировка)
	// при этом сохраняются.
	baseCtx   context.Context
	cancel    context.CancelFunc
	stopped   chan struct{}
	closeOnce bool
}

// Убедиться при сборке, что хранилище удовлетворяет порту середины: интерфейс
// объявлен потребителем, реализация обязана ему соответствовать.
var _ middleware.IdempotencyStore = (*Store)(nil)

// New строит хранилище: поднимает пул, накатывает схему, запускает сборщик.
func New(ctx context.Context, cfg Config) (*Store, error) {
	cfg = cfg.withDefaults()
	if cfg.DSN == "" {
		return nil, errors.New("idempotencypg: DSN is empty")
	}
	pool, err := db.NewPool(ctx, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("idempotencypg: pool: %w", err)
	}
	s, err := newWithPool(ctx, pool, cfg)
	if err != nil {
		pool.Close()
		return nil, err
	}
	s.ownsPool = true
	return s, nil
}

// NewWithPool строит хранилище поверх уже поднятого пула. Пул остаётся за
// вызывающим — Close его не трогает.
func NewWithPool(ctx context.Context, pool *pgxpool.Pool, cfg Config) (*Store, error) {
	return newWithPool(ctx, pool, cfg.withDefaults())
}

func newWithPool(ctx context.Context, pool *pgxpool.Pool, cfg Config) (*Store, error) {
	if err := ensureSchema(ctx, pool.Config().ConnString()); err != nil {
		return nil, err
	}
	base, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s := &Store{
		pool:    pool,
		cfg:     cfg,
		baseCtx: base,
		cancel:  cancel,
		stopped: make(chan struct{}),
	}
	go s.reapLoop()
	return s, nil
}

// ensureSchema накатывает цепочку миграций под advisory-блокировкой, поэтому
// одновременно стартующие реплики не накатывают её наперегонки.
func ensureSchema(ctx context.Context, dsn string) error {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("idempotencypg: open for migrate: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("idempotencypg: migrate conn: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", schemaLockID); err != nil {
		return fmt.Errorf("idempotencypg: schema lock: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", schemaLockID)
	}()

	goose.SetBaseFS(migrations.FS)
	goose.SetTableName("kacho_gateway_goose_db_version")
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("idempotencypg: goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		return fmt.Errorf("idempotencypg: migrate: %w", err)
	}
	return nil
}

// reserveSQL — ЕДИНСТВЕННАЯ точка допуска, и она один оператор.
//
// Ветка вставки выигрывает ключ; ветка конфликта перехватывает бронь ТОЛЬКО у
// просроченной записи либо у умершего держателя. Если ни то ни другое —
// оператор не меняет ничего, и второй половиной выражения читается тот, кому мы
// проиграли: законченный ответ (повтор) или живая бронь (ждать).
const reserveSQL = `
WITH claimed AS (
    INSERT INTO kacho_gateway.idempotency_records AS r
        (key, lease_owner, lease_expires_at, done, content_type, expires_at)
    VALUES ($1, $2, now() + make_interval(secs => $3::double precision), FALSE, '',
            now() + make_interval(secs => $4::double precision))
    ON CONFLICT (key) DO UPDATE
       SET lease_owner      = EXCLUDED.lease_owner,
           lease_expires_at = EXCLUDED.lease_expires_at,
           done             = FALSE,
           status_code      = NULL,
           content_type     = '',
           body             = NULL,
           expires_at       = EXCLUDED.expires_at
     WHERE r.expires_at <= now()
        OR (NOT r.done AND r.lease_expires_at <= now())
    RETURNING TRUE AS mine, done, status_code, content_type, body
)
SELECT mine, done, status_code, content_type, body FROM claimed
UNION ALL
SELECT FALSE, done, status_code, content_type, body
  FROM kacho_gateway.idempotency_records
 WHERE key = $1 AND NOT EXISTS (SELECT 1 FROM claimed)
`

// reserveAttempts — сколько раз повторить допуск, когда обе половины выражения
// вернули пусто.
//
// Это НЕ маскировка гонки, а её честное следствие: проверка уникальности идёт
// вне снимка транзакции, а чтение — внутри него, поэтому строка, закоммиченная
// соседом ПОСЛЕ нашего снимка, конфликт вызывает, а прочитаться не может.
// Следующая попытка идёт с новым снимком и видит её. Число попыток ограничено:
// бесконечный повтор превратил бы редкую гонку в вечное ожидание.
//
// Между попытками стоит ожидание, и оно не декорация: без него «бюджет в три
// попытки» покрывал бы доли миллисекунды, то есть исход «не сошлось» означал бы
// не отсутствие результата, а отсутствие ожидания. Шаг — тот же PollInterval,
// которым ждущий спрашивает исход держателя; на нормальном пути цикл проходит
// один раз и не ждёт вовсе.
const reserveAttempts = 3

// Reserve атомарно разрешает ключ ровно в один из трёх исходов.
//
// РЕПЛИКИ: запрос — петля принадлежит обслуживаемому запросу: она ждёт исход брони, взятой
// другим вызывающим, и живёт ровно столько, сколько живёт этот запрос.
func (s *Store) Reserve(ctx context.Context, key string) (middleware.IdempotencyReservation, error) {
	owner, err := newLeaseOwner()
	if err != nil {
		return middleware.IdempotencyReservation{}, err
	}
	for attempt := 0; attempt < reserveAttempts; attempt++ {
		var (
			mine        bool
			done        bool
			statusCode  *int32
			contentType string
			body        []byte
		)
		row := s.pool.QueryRow(ctx, reserveSQL, key, owner,
			s.cfg.LeaseTTL.Seconds(), s.cfg.TTL.Seconds())
		switch err := row.Scan(&mine, &done, &statusCode, &contentType, &body); {
		case errors.Is(err, pgx.ErrNoRows):
			// Сосед закоммитил вне нашего снимка — спросить заново, выждав шаг
			// опроса, чтобы бюджет попыток покрывал время, а не только счётчик.
			timer := time.NewTimer(s.cfg.PollInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return middleware.IdempotencyReservation{}, ctx.Err()
			case <-timer.C:
			}
			continue
		case err != nil:
			return middleware.IdempotencyReservation{}, fmt.Errorf("idempotencypg: reserve: %w", err)
		}
		switch {
		case mine:
			return middleware.IdempotencyReservation{
				Key: key, Outcome: middleware.IdempotencyOwn, Lease: owner,
			}, nil
		case done && statusCode != nil:
			return middleware.IdempotencyReservation{
				Key:     key,
				Outcome: middleware.IdempotencyReplay,
				Record: middleware.IdempotencyRecord{
					StatusCode: int(*statusCode), ContentType: contentType, Body: body,
				},
			}, nil
		default:
			return middleware.IdempotencyReservation{
				Key: key, Outcome: middleware.IdempotencyWait, Lease: owner,
			}, nil
		}
	}
	return middleware.IdempotencyReservation{},
		errors.New("idempotencypg: reserve did not settle within the attempt budget")
}

// Commit записывает исход держателя. Условие `lease_owner = $2` — CAS: держатель,
// чью бронь уже перехватили по истечении срока, чужой ответ не перезапишет.
func (s *Store) Commit(ctx context.Context, res middleware.IdempotencyReservation, rec middleware.IdempotencyRecord, keep bool) {
	owner, ok := res.Lease.(string)
	if !ok {
		return
	}
	if !keep {
		// Исход есть, но хранить его нельзя (5xx либо тело сверх потолка).
		// Снимаем бронь целиком: ждущий В СОСЕДНЕЙ реплике увидит, что ключ
		// свободен, и исполнит downstream сам.
		//
		// ЭТО ОСОЗНАННОЕ ОТЛИЧИЕ ОТ ХРАНИЛИЩА В ПАМЯТИ, и оно названо, а не
		// умолчано: там ждущий той же пачки повторяет захваченный исход, потому
		// что тот лежит в общей структуре процесса. Здесь исхода не остаётся
		// нигде, и притворяться, что он есть, нечем. Цена ограничена ровно теми
		// двумя случаями, в которых ответ и так НЕ дедуплицируется между
		// запросами: 5xx объявлен безопасным для повтора, а ответ сверх потолка
		// не кэшируется вовсе — то есть следующее предъявление того же ключа
		// исполняет downstream в обоих хранилищах одинаково.
		s.release(ctx, res.Key, owner)
		return
	}
	const q = `
UPDATE kacho_gateway.idempotency_records
   SET done = TRUE, lease_owner = '', status_code = $3, content_type = $4, body = $5,
       expires_at = now() + make_interval(secs => $6::double precision)
 WHERE key = $1 AND lease_owner = $2 AND NOT done`
	if _, err := s.pool.Exec(ctx, q, res.Key, owner,
		safeconv.IntToInt32(rec.StatusCode), rec.ContentType, rec.Body, s.cfg.TTL.Seconds()); err != nil {
		s.cfg.Logger.Warn("idempotency store: could not record the answer",
			"error", err)
	}
}

// Release снимает бронь, не оставив исхода.
func (s *Store) Release(ctx context.Context, res middleware.IdempotencyReservation) {
	owner, ok := res.Lease.(string)
	if !ok {
		return
	}
	s.release(ctx, res.Key, owner)
}

func (s *Store) release(ctx context.Context, key, owner string) {
	const q = `DELETE FROM kacho_gateway.idempotency_records
                WHERE key = $1 AND lease_owner = $2 AND NOT done`
	if _, err := s.pool.Exec(ctx, q, key, owner); err != nil {
		s.cfg.Logger.Warn("idempotency store: could not release the reservation",
			"error", err)
	}
}

// Await ждёт исхода держателя, спрашивая запись с шагом PollInterval.
//
// Ждём УСЛОВИЕ, а не время: шаг опроса — цена вопроса, а выход из ожидания
// определяют исход держателя, его смерть или бюджет вызывающего.
//
// РЕПЛИКИ: запрос — петля принадлежит обслуживаемому запросу и завершается по его исходу;
// у каждой реплики свои запросы, общего состояния петля не двигает.
func (s *Store) Await(ctx context.Context, res middleware.IdempotencyReservation) middleware.IdempotencyAwait {
	const q = `
SELECT done, status_code, content_type, body, lease_expires_at <= now() AS lease_dead
  FROM kacho_gateway.idempotency_records
 WHERE key = $1`
	t := time.NewTicker(s.cfg.PollInterval)
	defer t.Stop()
	for {
		var (
			done        bool
			statusCode  *int32
			contentType string
			body        []byte
			leaseDead   bool
		)
		err := s.pool.QueryRow(ctx, q, res.Key).
			Scan(&done, &statusCode, &contentType, &body, &leaseDead)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// Держатель снял бронь, не оставив исхода — ключ свободен.
			return middleware.IdempotencyAwait{Outcome: middleware.IdempotencyAwaitVacant}
		case err != nil:
			// Спросить не смогли. Исполнять downstream нельзя — это и было бы
			// вторым исполнением; отвечаем «в работе», повтор осмыслен.
			return middleware.IdempotencyAwait{Outcome: middleware.IdempotencyAwaitBusy}
		case done && statusCode != nil:
			return middleware.IdempotencyAwait{
				Outcome: middleware.IdempotencyAwaitReplay,
				Record: middleware.IdempotencyRecord{
					StatusCode: int(*statusCode), ContentType: contentType, Body: body,
				},
			}
		case leaseDead:
			// Держатель умер, не оставив исхода: ключ свободен.
			return middleware.IdempotencyAwait{Outcome: middleware.IdempotencyAwaitVacant}
		}
		select {
		case <-ctx.Done():
			return middleware.IdempotencyAwait{Outcome: middleware.IdempotencyAwaitBusy}
		case <-t.C:
		}
	}
}

// reapLoop уносит просроченные записи партиями. Без него хранилище росло бы без
// границы: у ключа, предъявленного один раз, нет никого, кто пришёл бы его убрать.
//
// РЕПЛИКИ: на-реплику — уборка — один условный оператор `DELETE … WHERE expires_at <= now()`
// с ограничением партии. Строки заперты самим оператором, поэтому вторая
// реплика уносит только то, что осталось, а на пустой выборке не делает
// ничего. Дубль стоит одного запроса к своей базе и ни одного к соседям.
func (s *Store) reapLoop() {
	defer close(s.stopped)
	t := time.NewTicker(s.cfg.ReapInterval)
	defer t.Stop()
	for {
		select {
		case <-s.baseCtx.Done():
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(s.baseCtx, 30*time.Second)
			if n, err := s.Reap(ctx); err != nil {
				s.cfg.Logger.Warn("idempotency store: reap failed", "error", err)
			} else if n > 0 {
				s.cfg.Logger.Info("idempotency store: expired records removed", "removed", n)
			}
			cancel()
		}
	}
}

// Reap уносит одну партию просроченных записей и возвращает их число. Отдельный
// экспортированный метод, потому что «сборщик работает» обязано быть проверяемо
// без ожидания тикера.
func (s *Store) Reap(ctx context.Context) (int64, error) {
	const q = `
DELETE FROM kacho_gateway.idempotency_records
 WHERE ctid IN (
     SELECT ctid FROM kacho_gateway.idempotency_records
      WHERE expires_at <= now()
      LIMIT $1
 )`
	tag, err := s.pool.Exec(ctx, q, reapBatch)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// Len возвращает число записей — перепись для наблюдаемости и проб.
func (s *Store) Len(ctx context.Context) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM kacho_gateway.idempotency_records`).Scan(&n)
	return n, err
}

// Close останавливает сборщик и, если пул поднят здесь, закрывает его.
func (s *Store) Close() error {
	if s.closeOnce {
		return nil
	}
	s.closeOnce = true
	s.cancel()
	<-s.stopped
	if s.ownsPool {
		s.pool.Close()
	}
	return nil
}

// newLeaseOwner выдаёт непрозрачный идентификатор брони. Он обязан быть
// неугадываемым и неповторяющимся между репликами: по нему CAS отличает
// «мой ответ» от «ответа того, кого я перехватил».
func newLeaseOwner() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("idempotencypg: lease owner: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
