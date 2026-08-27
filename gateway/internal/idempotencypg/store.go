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
	"sync/atomic"
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
	// ReapBatch — сколько просроченных строк уборка уносит ОДНИМ оператором.
	// Партия ограничена намеренно: одиночный DELETE по огромному хвосту держал
	// бы блокировки дольше, чем живёт запрос.
	//
	// Экспортирована, потому что предметом пробы является именно граница партии:
	// проба, ставящая свою, утверждала бы о числе, которого в проде нет.
	ReapBatch = 1000

	// DefaultReapMaxBatches — сторож от бесконечного цикла уборки записей
	// однократности, а НЕ ограничитель её темпа.
	//
	// Темп ограничивать этой величиной нельзя: строку пишет каждая мутация с
	// ключом однократности, то есть темп задаёт ВЫЗЫВАЮЩИЙ края, и всякая
	// постоянная граница ёмкости означала бы «догоняем до такого-то темпа,
	// дальше молча отстаём». Сверху уборку держит срок вызывающего; счётчик
	// партий нужен затем, чтобы цикл был конечен даже при часах базы, ушедших
	// назад. Тысяча партий — миллион строк за уборку.
	DefaultReapMaxBatches = 1000

	// reapStepsPerLifetime — сколько заходов уборки приходится на жизнь записи.
	// Обоснование величины — в godoc `ReapIntervalFor`.
	reapStepsPerLifetime = 24

	// minReapInterval — пол шага уборки.
	//
	// Не вкус, а два условия сразу: тикер с НЕПОЛОЖИТЕЛЬНЫМ шагом паникует
	// (жизнь записи в наносекунду законна и встречается в пробах), а шаг короче
	// одного оператора превратил бы уборку в непрерывный опрос базы.
	minReapInterval = time.Second

	// reapBudgetCap — потолок срока ОДНОГО захода уборки.
	//
	// Заход, переживший собственный период, накладывался бы на следующий,
	// поэтому срок не превышает шага; потолок нужен на случай крупного шага —
	// удерживать соединение дольше получаса нечем оправдать.
	reapBudgetCap = 30 * time.Second

	// DPoPPurgeBatch — сколько просроченных доказательств уборка уносит ОДНИМ
	// оператором. Та же величина и та же причина, что у `ReapBatch`: партия
	// ограничена ради блокировок, а не ради темпа.
	//
	// Экспортирована, потому что предметом пробы является именно граница партии:
	// проба, ставящая свою, утверждала бы о числе, которого в проде нет.
	DPoPPurgeBatch = 1000

	// DefaultDPoPPurgeInterval — запасной шаг уборки доказательств.
	//
	// # Откуда величина
	//
	// Нужное содержимое таблицы — доказательства за последнее окно свежести
	// (TTL строки, по умолчанию 120 с): всё, что старше, читателем уже не
	// принимается. Уборка с шагом P оставляет в таблице до (TTL+P) секунд
	// записей, то есть множитель над нужным равен (TTL+P)/TTL. При P=TTL это
	// 2×; при P=1 ч и TTL=120 с — 31×, и платится он ни за что.
	//
	// Поэтому шаг = TTL строки: самый крупный шаг, при котором множитель
	// остаётся малой константой. Значение здесь — ЗАПАСНОЕ: композиционный
	// корень выводит шаг из фактического TTL (`config.Config.DPoPReplayTTL`) и
	// задаёт его всегда, поэтому в проде это умолчание не исполняется.
	DefaultDPoPPurgeInterval = 2 * time.Minute

	// DefaultDPoPPurgeMaxBatches — сторож от бесконечного цикла уборки, а НЕ
	// ограничитель её темпа.
	//
	// Темп ограничивать этой величиной нельзя: он задан внешней стороной, и
	// всякая постоянная граница ёмкости означала бы «догоняем до такого-то
	// темпа, дальше молча отстаём». Сверху уборку держит срок вызывающего;
	// счётчик партий нужен затем, чтобы цикл был конечен даже при часах базы,
	// ушедших назад. Тысяча партий — миллион строк за уборку.
	DefaultDPoPPurgeMaxBatches = 1000
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
	// ReapInterval — с каким шагом сборщик уносит просроченные записи. Не задан
	// — ВЫВОДИТСЯ из `TTL` (`ReapIntervalFor`), а не берётся постоянной: шаг, не
	// связанный с жизнью записи, менял бы запас просроченного молча при всякой
	// смене TTL.
	ReapInterval time.Duration
	// ReapMaxBatches — сколько партий уборка записей однократности вправе унести
	// за один заход. Сторож от бесконечного цикла; см. DefaultReapMaxBatches.
	ReapMaxBatches int
	// DPoPPurgeInterval — с каким шагом сборщик уносит просроченные записи
	// однократности предъявления. Выводится из TTL этих записей, а не из
	// `ReapInterval`: у двух таблиц разная жизнь строки (сутки против двух
	// минут), и один шаг на обе означал бы тридцатикратный запас у одной.
	DPoPPurgeInterval time.Duration
	// DPoPPurgeMaxBatches — сколько партий уборка вправе унести за один заход.
	// Сторож от бесконечного цикла; см. DefaultDPoPPurgeMaxBatches.
	DPoPPurgeMaxBatches int
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
	// Шаг выводится ПОСЛЕ умолчания TTL: вывод из нуля дал бы нулевой шаг, а
	// тикер с ним паникует.
	if c.ReapInterval <= 0 {
		c.ReapInterval = ReapIntervalFor(c.TTL)
	}
	if c.ReapMaxBatches <= 0 {
		c.ReapMaxBatches = DefaultReapMaxBatches
	}
	if c.DPoPPurgeInterval <= 0 {
		c.DPoPPurgeInterval = DefaultDPoPPurgeInterval
	}
	if c.DPoPPurgeMaxBatches <= 0 {
		c.DPoPPurgeMaxBatches = DefaultDPoPPurgeMaxBatches
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

// ReapIntervalFor выводит шаг уборки из ЖИЗНИ УБИРАЕМОЙ ЗАПИСИ.
//
// # Почему шаг ВЫВОДИТСЯ, а не стоит постоянной
//
// Постоянная в один час стояла здесь рядом с жизнью записи в двадцать четыре
// часа, и связи между ними не было никакой: смени TTL — шаг остался бы прежним,
// а запас просроченного в таблице поменялся бы молча.
//
// # Откуда именно эта величина
//
// Уборка с шагом P оставляет в таблице, сверх нужного, до P × темп записи
// просроченных строк, то есть множитель над нужным содержимым равен (TTL+P)/TTL.
// Отсюда ДВЕ границы сверху, и обе названы:
//
//  1. МЁРТВЫЙ ВЕС. P ≤ TTL держит множитель ≤ 2×. Это потолок, а не выбор:
//     стопроцентный запас платится ни за что.
//  2. ЖИЗНЬ ПРОЦЕССА. Шаг, сравнимый с жизнью пода, означает, что тикер не
//     сработает НИ РАЗУ: под края перекатывают на каждой выкатке, то есть чаще
//     суток. Уборщик, объявленный и не исполняющийся, — ровно та находка,
//     которой закрыта #1293; перенести сюда её P = TTL значило бы завести её
//     второй раз, потому что здесь TTL — сутки.
//
// Связывает здесь ВТОРАЯ граница, и именно поэтому решение соседней уборки не
// копируется: у неё жизнь строки — окно свежести доказательства, и вторая
// граница у неё не связывает никогда.
//
// P = TTL/24 удовлетворяет обеим: запас мёртвого веса 1/24 ≈ 4 %, заходов
// уборки — двадцать четыре на жизнь записи, при сутках это час. Величина
// СЛЕДУЕТ за TTL: сменится жизнь записи — сменится и шаг.
//
// # Чего эта величина НЕ решает
//
// Догоняет ли уборка темп. Темп задаёт внешняя сторона, и ни одна постоянная
// величина ответить на это не может — отвечает ЁМКОСТЬ уборки, а она тянется за
// хвостом (см. `Reap`). Шаг остался бы верным даже при вдесятеро большем темпе;
// он про мёртвый вес, а не про то, кончится ли хвост.
func ReapIntervalFor(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		ttl = middleware.IdempotencyTTL
	}
	step := ttl / reapStepsPerLifetime
	if step < minReapInterval {
		step = minReapInterval
	}
	return step
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

	// Величины уборки доказательств. Атомарные, потому что пишет их сборщик, а
	// читает диагностическая поверхность, и замка между ними быть не должно:
	// сбор величин не имеет права ждать уборку.
	dpopSweeps    atomic.Uint64
	dpopRemoved   atomic.Uint64
	dpopLagMillis atomic.Int64
	dpopDrained   atomic.Bool

	// Величины уборки записей однократности. Те же четыре и по той же причине,
	// что у соседней уборки: пишет их сборщик, читает диагностическая
	// поверхность, и замка между ними быть не должно — сбор величин не имеет
	// права ждать уборку.
	reapSweeps    atomic.Uint64
	reapRemoved   atomic.Uint64
	reapLagMillis atomic.Int64
	reapDrained   atomic.Bool
}

// ReapSweepStats — снимок уборки записей однократности для диагностической
// поверхности.
//
// # Почему четыре величины, а не одно отставание
//
// Ноль отставания отвечает сразу на два вопроса и одинаково: «уборка догоняет»
// и «уборка не исполнялась ни разу». Различает их `Sweeps`, и без него нулевое
// отставание означало бы тишину, а не благополучие — то самое, что корпус велит
// делать заметным.
//
// `RemovedTotal` монотонен, поэтому по нему берётся производная — темп уборки;
// `Lag` колеблется и производной не имеет смысла. `Drained` — состояние
// последнего захода; на исправном пути оно избыточно к нулевому отставанию и
// расходится с ним ровно там, где отставание измерить не удалось: заход,
// оборванный отказом хранилища, вернёт ноль, не означающий благополучия.
type ReapSweepStats struct {
	// Sweeps — сколько уборок исполнено за жизнь процесса.
	Sweeps uint64
	// RemovedTotal — сколько записей унесено за жизнь процесса.
	RemovedTotal uint64
	// Lag — отставание, измеренное последней уборкой.
	Lag time.Duration
	// Drained — догнала ли последняя уборка хвост.
	Drained bool
}

// ReapSweepStats отдаёт снимок величин уборки записей однократности.
func (s *Store) ReapSweepStats() ReapSweepStats {
	return ReapSweepStats{
		Sweeps:       s.reapSweeps.Load(),
		RemovedTotal: s.reapRemoved.Load(),
		Lag:          time.Duration(s.reapLagMillis.Load()) * time.Millisecond,
		Drained:      s.reapDrained.Load(),
	}
}

// recordReap запоминает исход уборки. Зовётся и на отказе тоже: заход,
// оборвавшийся на середине, унёс сколько-то строк и хвоста не догнал — обе
// величины обязаны это сказать.
func (s *Store) recordReap(sw ReapSweep) {
	s.reapSweeps.Add(1)
	if sw.Removed > 0 {
		s.reapRemoved.Add(uint64(sw.Removed))
	}
	s.reapLagMillis.Store(sw.Lag.Milliseconds())
	s.reapDrained.Store(sw.Drained)
}

// DPoPSweepStats — снимок уборки доказательств для диагностической поверхности.
//
// # Почему четыре величины, а не одно отставание
//
// Ноль отставания отвечает сразу на два вопроса и одинаково: «уборка догоняет»
// и «уборка не исполнялась ни разу». Различает их `Sweeps`, и без него нулевое
// отставание означало бы тишину, а не благополучие — ровно тот класс, который
// корпус велит делать заметным.
//
// `RemovedTotal` монотонен, поэтому по нему берётся производная — темп уборки;
// `Lag` колеблется и производной не имеет смысла. `Drained` — состояние
// последнего захода; на исправном пути оно избыточно к нулевому отставанию и
// расходится с ним ровно там, где отставание измерить не удалось: заход,
// оборванный отказом хранилища, вернёт ноль, не означающий благополучия.
type DPoPSweepStats struct {
	// Sweeps — сколько уборок исполнено за жизнь процесса.
	Sweeps uint64
	// RemovedTotal — сколько строк унесено за жизнь процесса.
	RemovedTotal uint64
	// Lag — отставание, измеренное последней уборкой.
	Lag time.Duration
	// Drained — догнала ли последняя уборка хвост.
	Drained bool
}

// DPoPSweepStats отдаёт снимок величин уборки.
func (s *Store) DPoPSweepStats() DPoPSweepStats {
	return DPoPSweepStats{
		Sweeps:       s.dpopSweeps.Load(),
		RemovedTotal: s.dpopRemoved.Load(),
		Lag:          time.Duration(s.dpopLagMillis.Load()) * time.Millisecond,
		Drained:      s.dpopDrained.Load(),
	}
}

// recordDPoPSweep запоминает исход уборки. Зовётся и на отказе тоже: заход,
// оборвавшийся на середине, унёс сколько-то строк и хвоста не догнал — обе
// величины обязаны это сказать.
func (s *Store) recordDPoPSweep(sw DPoPSweep) {
	s.dpopSweeps.Add(1)
	if sw.Removed > 0 {
		s.dpopRemoved.Add(uint64(sw.Removed))
	}
	s.dpopLagMillis.Store(sw.Lag.Milliseconds())
	s.dpopDrained.Store(sw.Drained)
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
// РЕПЛИКИ: на-реплику — уборка идёт ограниченными ПАРТИЯМИ, пока хвост не
// кончится. Строки заперты самим оператором, поэтому вторая реплика уносит
// только то, что осталось, а на пустой выборке не делает ничего. Дубль стоит
// одного запроса к своей базе и ни одного к соседям.
func (s *Store) reapLoop() {
	defer close(s.stopped)
	t := time.NewTicker(s.cfg.ReapInterval)
	defer t.Stop()
	// ВТОРОЙ шаг, а не второй сборщик: таблицы две, и жизнь строки у них разная
	// (сутки против окна свежести доказательства), поэтому один шаг на обе
	// означал бы либо уборку раз в сутки, либо перебор запросов к первой.
	// Горутина при этом одна — «два расписания об одном предмете» так и не
	// заводится, предметов действительно два.
	dpop := time.NewTicker(s.cfg.DPoPPurgeInterval)
	defer dpop.Stop()
	for {
		select {
		case <-s.baseCtx.Done():
			return
		case <-t.C:
			s.reapOnce()
		case <-dpop.C:
			s.purgeDPoPOnce()
		}
	}
}

// reapOnce — один заход уборки записей однократности вместе с его отчётом.
//
// Отчёт разделён на три исхода намеренно, и молчаливый среди них один. Отказ
// хранилища — отказ. НЕ ДОГНАЛА — тоже находка, и она обязана прозвучать:
// уборка, унёсшая свою партию и смолчавшая, по журналу неотличима от
// догоняющей, а таблица при этом растёт. Молча проходит только третий исход —
// догнала и унесла ноль строк.
//
// Здесь стояла ветка, знавшая ДВА исхода: отказ и «унесено n». Про то,
// кончился ли хвост, она не спрашивала вовсе — а с постоянной ёмкостью в одну
// партию ответ «не кончился» был штатным при темпе выше 0.28 запроса в секунду.
func (s *Store) reapOnce() {
	// Останов — не находка. Тикер и отмена приходят в один `select`, и на
	// закрытии хранилища заход успел бы стартовать с уже отменённым контекстом:
	// уборка честно вернула бы отказ, а журнал назвал бы штатное завершение
	// неисправностью.
	if s.baseCtx.Err() != nil {
		return
	}

	// Срок захода не превышает его же шага: заход, переживший собственный
	// период, накладывался бы на следующий.
	budget := s.cfg.ReapInterval
	if budget > reapBudgetCap {
		budget = reapBudgetCap
	}
	ctx, cancel := context.WithTimeout(s.baseCtx, budget)
	defer cancel()

	sw, err := s.Reap(ctx)
	switch {
	case err != nil:
		s.cfg.Logger.Warn("idempotency store: reap failed",
			"error", err, "removed", sw.Removed)
	case !sw.Drained:
		s.cfg.Logger.Warn("idempotency store: reap did not keep up with the write rate",
			"removed", sw.Removed,
			"oldest_expired_age", sw.Lag,
			"interval", s.cfg.ReapInterval,
			"batch", ReapBatch)
	case sw.Removed > 0:
		s.cfg.Logger.Info("idempotency store: expired records removed", "removed", sw.Removed)
	}
}

// purgeDPoPOnce — один заход уборки доказательств вместе с его отчётом.
//
// Отчёт разделён на три исхода намеренно. Отказ хранилища — отказ. НЕ ДОГНАЛА —
// тоже находка, и она обязана прозвучать: уборка, унёсшая свою партию и
// смолчавшая, по журналу неотличима от догоняющей, а таблица при этом растёт.
// Молча проходит только третий исход — догнала и унесла ноль строк.
func (s *Store) purgeDPoPOnce() {
	// Останов — не находка. Тикер и отмена приходят в один `select`, и на
	// закрытии хранилища заход успел бы стартовать с уже отменённым контекстом:
	// уборка честно вернула бы отказ, а журнал назвал бы штатное завершение
	// неисправностью. «Не выполнилось» не зачитывается ни в успех, ни в отказ.
	if s.baseCtx.Err() != nil {
		return
	}

	// Срок захода не превышает его же шага: заход, переживший собственный
	// период, накладывался бы на следующий.
	budget := s.cfg.DPoPPurgeInterval
	if budget > 30*time.Second {
		budget = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(s.baseCtx, budget)
	defer cancel()

	sw, err := s.PurgeExpiredDPoPProofs(ctx)
	switch {
	case err != nil:
		s.cfg.Logger.Warn("dpop replay store: purge failed",
			"error", err, "removed", sw.Removed)
	case !sw.Drained:
		s.cfg.Logger.Warn("dpop replay store: purge did not keep up with the write rate",
			"removed", sw.Removed,
			"oldest_expired_age", sw.Lag,
			"interval", s.cfg.DPoPPurgeInterval,
			"batch", DPoPPurgeBatch)
	case sw.Removed > 0:
		s.cfg.Logger.Info("dpop replay store: expired proofs removed", "removed", sw.Removed)
	}
}

// reapExpiredPredicate — «запись отжила свой срок».
//
// # Почему ОДНА константа на уборку и на замер её отставания
//
// Разойдись эти два выражения — отставание считалось бы по строкам, которых
// уборка не уносит, и «не догоняем» звучало бы вечно при исправной уборке; либо
// наоборот, хвост рос бы при нулевом отставании. Согласованность держится не
// сверкой двух текстов, а тем, что текст ОДИН: оба оператора собираются из этой
// строки, и разойтись им нечем by construction.
//
// # Почему уборка НЕ открывает окна повторного допуска
//
// Держатель ключа (`Reserve`) перехватывает строку по УСЛОВИЮ ШИРЕ этого:
// истёк срок записи ЛИБО умерла бронь незаконченной. Уборка уносит строгое
// подмножество — то, что читатель и так перезаписал бы, — поэтому снять запись
// раньше, чем читатель перестал её honorировать, она не может.
//
// Часы — БАЗЫ, а не процесса, в обоих операторах: часы реплик между собой не
// сверены, и уборщик, судящий часами своего пода, снимал бы строку, которую
// держатель ещё считает живой.
const reapExpiredPredicate = `kacho_gateway.idempotency_records.expires_at <= now()`

// reapSQL — уборка ОДНОЙ ограниченной партии.
//
// Партия ограничена ради блокировок, а НЕ ради темпа: одиночный DELETE по
// хвосту произвольной длины держал бы их дольше, чем живёт запрос. Партий за
// одну уборку столько, сколько нужно, чтобы хвост кончился (см. Reap).
const reapSQL = `
DELETE FROM kacho_gateway.idempotency_records
 WHERE ctid IN (
     SELECT ctid FROM kacho_gateway.idempotency_records
      WHERE ` + reapExpiredPredicate + `
      LIMIT $1
 )`

// reapLagSQL — возраст САМОЙ СТАРОЙ просроченной записи.
//
// Это и есть отставание уборки от темпа записи, выраженное величиной, а не
// выведенное из спокойствия: пока уборка догоняет, просроченных строк после неё
// не остаётся вовсе; как только перестала — возраст растёт без границы.
//
// `min` по индексированному столбцу (`idempotency_records_expires_at_idx`)
// берёт первую строку индекса, поэтому запрос не дорожает вместе с хвостом — то
// есть остаётся дешёвым ровно тогда, когда хвост велик и величина нужнее всего.
const reapLagSQL = `
SELECT COALESCE(EXTRACT(EPOCH FROM (now() - min(expires_at))), 0)
  FROM kacho_gateway.idempotency_records
 WHERE ` + reapExpiredPredicate

// ReapSweep — исход ОДНОЙ уборки записей однократности.
//
// Три величины, и ни одна не выводится из остальных: `Removed` говорит, что
// уборка сделала, `Drained` — кончился ли хвост, `Lag` — насколько уборка от
// него отстала. Одного `Removed` мало: уборщик, унёсший свою партию и
// замолчавший, по нему неотличим от уборщика, который догоняет.
type ReapSweep struct {
	// Removed — сколько записей унесено этой уборкой.
	Removed int64
	// Drained — уборка остановилась потому, что просроченного не осталось, а не
	// потому, что упёрлась в свою границу.
	Drained bool
	// Lag — возраст самой старой просроченной записи, ОСТАВШЕЙСЯ после уборки.
	//
	// Ноль означает ДВА разных состояния, и различает их не он: догнавшей уборке
	// отставать не от чего (`Drained`), а уборке, оборвавшейся отказом
	// хранилища, отставание измерить нечем — соединение и есть то, что отказало.
	// Поэтому величину читают в паре с `Drained` и с исходом вызова.
	Lag time.Duration
}

// Reap уносит просроченные записи ограниченными партиями, пока хвост не
// кончится, и говорит, догнала ли она темп.
//
// Отдельный экспортированный метод, потому что «сборщик работает» обязано быть
// проверяемо без ожидания тикера.
//
// # Почему партии, а не одна партия за тик
//
// Строку пишет КАЖДАЯ мутация с ключом однократности, то есть темп задаёт
// вызывающий края: величина внешняя, границы у неё нет. Значит ни одна
// постоянная ёмкость уборки не может быть верной — уборка, уносящая B строк за
// период P, догоняет ровно пока темп ≤ B/P, а выше хвост растёт без границы,
// оставаясь ЗЕЛЁНЫМ по всякой проверке вида «сборщик вызвался». При прежних
// B=1000 и P=1 ч этот порог был 0.28 запроса в секунду (#1302).
//
// Поэтому ограничена ПАРТИЯ (блокировки не держатся дольше запроса), а ёмкость
// уборки — нет. Тогда «догоняет ли» перестаёт зависеть от знания темпа.
//
// # Что ограничивает уборку сверху
//
// Срок вызывающего (у сборщика он свой, см. `reapOnce`) и `ReapMaxBatches` —
// сторож от бесконечного цикла, а НЕ ограничитель темпа. Упёрлись в любую из
// границ — уборка возвращает `Drained=false` и измеренное отставание, и
// молчаливым это состояние быть не может.
func (s *Store) Reap(ctx context.Context) (ReapSweep, error) {
	var out ReapSweep
	maxBatches := s.cfg.ReapMaxBatches
	if maxBatches <= 0 {
		maxBatches = DefaultReapMaxBatches
	}
	for i := 0; i < maxBatches; i++ {
		if err := ctx.Err(); err != nil {
			break
		}
		tag, err := s.pool.Exec(ctx, reapSQL, ReapBatch)
		if err != nil {
			s.recordReap(out)
			return out, err
		}
		n := tag.RowsAffected()
		out.Removed += n
		if n < ReapBatch {
			// Неполная партия — просроченного больше нет. Это единственный
			// выход, означающий «догнали».
			out.Drained = true
			break
		}
	}
	if !out.Drained {
		lag, err := s.reapLag(ctx)
		if err != nil {
			s.recordReap(out)
			return out, err
		}
		out.Lag = lag
		// Граница исчерпана РОВНО на пустом хвосте: просроченного не осталось,
		// и объявлять отставание было бы тревогой без предмета. Проверка,
		// кричащая в исправном состоянии, перестаёт читаться, а вместе с ней —
		// и настоящая находка.
		out.Drained = lag == 0
	}
	s.recordReap(out)
	return out, nil
}

// reapLagTimeout — срок ОДНОГО замера отставания, когда срок захода уже вышел.
//
// Замер — не уборка: он читает первую строку индекса, и держать его дольше
// незачем.
const reapLagTimeout = 2 * time.Second

// reapLag спрашивает возраст самой старой просроченной записи.
//
// # Почему замер переживает срок захода
//
// Срок захода выходит ИМЕННО ПОТОМУ, что хвост велик, — то есть ровно в том
// случае, ради которого величина и заведена. Замер на исчерпанном контексте
// отказал бы, и отставание осталось бы неизмеренным именно тогда, когда оно
// максимально: наблюдение гасло бы при наступлении своего предмета.
//
// Свой срок при этом короткий и по-прежнему ограничен жизнью ХРАНИЛИЩА: на
// закрытии замер обрывается вместе со всем остальным.
func (s *Store) reapLag(ctx context.Context) (time.Duration, error) {
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(s.baseCtx, reapLagTimeout)
		defer cancel()
	}
	var secs float64
	if err := s.pool.QueryRow(ctx, reapLagSQL).Scan(&secs); err != nil {
		return 0, fmt.Errorf("idempotency store: отставание уборки: %w", err)
	}
	if secs <= 0 {
		return 0, nil
	}
	return time.Duration(secs * float64(time.Second)), nil
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
