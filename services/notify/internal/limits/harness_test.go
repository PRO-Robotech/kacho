// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package limits_test

// harness_test.go — общая оснастка проб полосы N7 (kacho#2915, замысел З24).
//
// # Контракт испытуемого, который утверждают пробы
//
// Пакет `services/notify/internal/limits` (строка N7 tasks.md) отдаёт:
//
//	limits.New(limits.Options{Pool, Key, Grid, Now, Registerer, Logger}) (*limits.Limiter, error)
//	(*Limiter).WriteFence(ctx) error                       — запись ограды при старте реплики
//	(*Limiter).Reserve(ctx, limits.Row) (*Reservation, error) — резерв до MAIL FROM, один CAS
//	(*Limiter).Release(ctx, *Reservation) error            — освобождение на не-SENT
//	(*Limiter).CeilingReached(ctx) (bool, error)           — потолок потока за сутки достигнут
//	limits.Row{Source, Class, To}                          — строка ленты глазами сетки
//	limits.Grid{SecurityPerDay, NoticePerHour, NoticePerDay, GlobalPerDay}
//	limits.ErrRecipientKeySuperseded · ErrRecipientNetExhausted · ErrGlobalCeilingReached
//	*limits.ExhaustedError{Class, FreeAt}                  — errors.Is(…, ErrRecipientNetExhausted)
//	limits.FenceLockTimeout · TxStatementTimeout · TxIdleTimeout (§8: 5s · 10s · 2s)
//	limits.NewSourceGate(module, limits.SourceLimits{Rate, Burst, Paused}, now, reg) (*SourceGate, error)
//	(*SourceGate).Classes(ceilingReached bool) []feed.Class · (*SourceGate).Take(n int) int
//	limits.SecurityNetCovers(perDay, bundleSecuritySum int) error
//
// Имена метрик — приёмки (NTF1-H09) и замысла (З27): notify_recipient_net_hits_total{class},
// notify_global_ceiling_hits_total, notify_source_throttled_total{source},
// notify_source_paused{source}, notify_reserve_db_errors_total.
//
// Часы испытуемого — Options.Now: окна сетки (час, сутки UTC) и сутки потолка
// пробы двигают сами (управляемые часы, NTF1-H02), а не ждут.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/PRO-Robotech/corelib/notify/address"
	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/corelib/pgtest"

	"github.com/PRO-Robotech/kacho/services/notify/internal/limits"
)

// Ключи сетки двух «установок» — 32 байта, отличимые от настоящих.
var (
	keyOne = []byte("n7-probe-recipient-key-one-000001")
	keyTwo = []byte("n7-probe-recipient-key-two-000002")
)

// fingerprintLabel — метка отпечатка ключа сетки (З24).
const fingerprintLabel = "kacho-notify/recipient-key-id"

// netKey — ключ строки сетки: HMAC-SHA256(ключ, Normalized.Value()) (З24, Р10).
func netKey(t *testing.T, key []byte, to address.Normalized) []byte {
	t.Helper()
	v, err := to.Value()
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: адрес фикстуры пуст: %v", err)
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte(v))
	return m.Sum(nil)
}

// fingerprint — отпечаток ключа сетки: первые 16 байт HMAC над меткой (З24).
func fingerprint(key []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(fingerprintLabel))
	return m.Sum(nil)[:16]
}

// addr — нормализованный адрес фикстуры; отказ разбора — отказ фикстуры.
func addr(t *testing.T, s string) address.Normalized {
	t.Helper()
	n, err := address.Normalize(s)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: адрес фикстуры %q не нормализован: %v", s, err)
	}
	return n
}

// clock — управляемые часы пробы.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock(at time.Time) *clock { return &clock{now: at.UTC()} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Set(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = at.UTC()
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// logBuf — журнал испытуемого: JSON-записи slog, читаемые пробой.
type logBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// replica — реплика notify в процессе пробы: свой пул, свой ключ, свой реестр
// метрик и журнал, общие с соседями база и часы.
type replica struct {
	lim *limits.Limiter
	reg *prometheus.Registry
	log *logBuf
	key []byte
}

// gridWide — сетка, которую пробы не исчерпывают, если предмет пробы не сетка.
var gridWide = limits.Grid{SecurityPerDay: 1000, NoticePerHour: 10000, NoticePerDay: 10000, GlobalPerDay: 10000000}

func openPool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: DSN pgtest не разобран: %v", err)
	}
	cfg.MaxConns = 40
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: пул не открыт: %v", err)
	}
	pgtest.ClosePoolAtEnd(t, pool)
	return pool
}

// newReplica собирает реплику испытуемого. Отказ сборки — отказ испытуемого,
// а не фикстуры: опции годны (ключ 32 байта, сетка в границах §8).
func newReplica(t *testing.T, pool *pgxpool.Pool, key []byte, grid limits.Grid, clk *clock) *replica {
	t.Helper()
	r := &replica{reg: prometheus.NewRegistry(), log: &logBuf{}, key: key}
	lim, err := limits.New(limits.Options{
		Pool:       pool,
		Key:        key,
		Grid:       grid,
		Now:        clk.Now,
		Registerer: r.reg,
		Logger:     slog.New(slog.NewJSONHandler(r.log, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatalf("limits.New отказал на годных опциях: %v", err)
	}
	r.lim = lim
	return r
}

// started — реплика после записи ограды, как после старта процесса.
func started(t *testing.T, pool *pgxpool.Pool, key []byte, grid limits.Grid, clk *clock) *replica {
	t.Helper()
	r := newReplica(t, pool, key, grid, clk)
	if err := r.lim.WriteFence(context.Background()); err != nil {
		t.Fatalf("запись ограды при старте отвергнута: %v", err)
	}
	return r
}

func row(source string, class feed.Class, to address.Normalized) limits.Row {
	return limits.Row{Source: source, Class: class, To: to}
}

// counter — сумма значений семейства метрик по совпадению меток.
func counter(t *testing.T, reg prometheus.Gatherer, name string, labels map[string]string) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("реестр метрик не собран: %v", err)
	}
	var sum float64
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
	metric:
		for _, m := range mf.GetMetric() {
			for k, v := range labels {
				found := false
				for _, lp := range m.GetLabel() {
					if lp.GetName() == k && lp.GetValue() == v {
						found = true
					}
				}
				if !found {
					continue metric
				}
			}
			switch {
			case m.GetCounter() != nil:
				sum += m.GetCounter().GetValue()
			case m.GetGauge() != nil:
				sum += m.GetGauge().GetValue()
			}
		}
	}
	return sum
}

// families — имена семейств реестра.
func families(t *testing.T, reg prometheus.Gatherer) map[string]bool {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("реестр метрик не собран: %v", err)
	}
	out := map[string]bool{}
	for _, mf := range mfs {
		out[mf.GetName()] = true
	}
	return out
}

// piiForms — формы посеянного адреса, которых не должно быть нигде вне
// процесса: адрес, локальная часть, hex и base64 адреса и ключа сетки.
func piiForms(t *testing.T, key []byte, raw string) []string {
	t.Helper()
	k := netKey(t, key, addr(t, raw))
	local, _, _ := strings.Cut(raw, "@")
	return []string{
		raw, local,
		hex.EncodeToString([]byte(raw)),
		hex.EncodeToString(k), strings.ToUpper(hex.EncodeToString(k)),
		base64.StdEncoding.EncodeToString(k), base64.RawURLEncoding.EncodeToString(k),
	}
}

// sumCount — сумма счётчиков строк сетки класса.
func sumCount(t *testing.T, pool *pgxpool.Pool, class feed.Class) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT coalesce(sum(count), 0) FROM recipient_net WHERE class = $1`, string(class)).Scan(&n); err != nil {
		t.Fatalf("сетка не прочитана: %v", err)
	}
	return n
}

// globalCount — счётчик потолка потока за сутки day.
func globalCount(t *testing.T, pool *pgxpool.Pool, day time.Time) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT coalesce((SELECT count FROM global_daily WHERE day = $1::date), 0)`, day.UTC().Format("2006-01-02")).Scan(&n)
	if err != nil {
		t.Fatalf("потолок потока не прочитан: %v", err)
	}
	return n
}

// lockWaiters — сколько сеансов базы ждут замка.
func lockWaiters(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&n); err != nil {
		t.Fatalf("pg_stat_activity не прочитан: %v", err)
	}
	return n
}

// waitLockWaiters ждёт УСЛОВИЯ «ждущих замка не меньше n», а не времени.
func waitLockWaiters(t *testing.T, pool *pgxpool.Pool, n int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if lockWaiters(t, pool) >= n {
			return
		}
		pollPause()
	}
	t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: за %s ждущих замка не стало %d (есть %d) — условие пробы не создано",
		within, n, lockWaiters(t, pool))
}

// pollPause — шаг опроса условия; ожидание пробы — условие, а не время.
func pollPause() { time.Sleep(5 * time.Millisecond) }

// commitSwallower — прокси TCP между репликой и базой, проглатывающий COMMIT:
// транзакция резерва исполнила все операторы и держит свои замки, а держатель
// молчит при открытом соединении (SDR-К1: реплика убита без RST, сеть
// отрезана). Сервер видит сеанс «idle in transaction».
type commitSwallower struct {
	ln       net.Listener
	upstream string
	swallow  bool
	mu       sync.Mutex
	conns    []net.Conn
}

func newCommitSwallower(t *testing.T, upstream string, swallow bool) *commitSwallower {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: прокси не слушает: %v", err)
	}
	p := &commitSwallower{ln: ln, upstream: upstream, swallow: swallow}
	go p.serve()
	t.Cleanup(p.close)
	return p
}

func (p *commitSwallower) serve() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		u, err := net.Dial("tcp", p.upstream)
		if err != nil {
			_ = c.Close()
			continue
		}
		p.mu.Lock()
		p.conns = append(p.conns, c, u)
		p.mu.Unlock()
		go func() { _, _ = io.Copy(c, u); _ = c.Close() }()
		go p.clientToServer(c, u)
	}
}

// commitQuery — сообщение Query протокола Postgres с текстом ровно `commit`
// (байт типа, длина 11, текст, нуль), в нижнем регистре. Признак — сообщение
// целиком, а не подстрока: `begin isolation level read committed`, которым
// pgx открывает транзакцию с явным READ COMMITTED (З24, CX1-68 (б)), несёт
// подстроку «commit», и прокси, судящий по подстроке, глотал бы BEGIN вместо
// COMMIT — держатель вовсе не начинал бы транзакцию.
var commitQuery = []byte{'q', 0, 0, 0, 11, 'c', 'o', 'm', 'm', 'i', 't', 0}

func (p *commitSwallower) clientToServer(c, u net.Conn) {
	buf := make([]byte, 64*1024)
	silent := false
	for {
		n, err := c.Read(buf)
		if n > 0 && !silent {
			if p.swallow && bytes.Contains(bytes.ToLower(buf[:n]), commitQuery) {
				silent = true
				continue
			}
			if _, werr := u.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			_ = u.Close()
			return
		}
	}
}

func (p *commitSwallower) close() {
	_ = p.ln.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.conns {
		_ = c.Close()
	}
}

// poolVia — пул, ходящий в базу dsn через прокси p.
func poolVia(t *testing.T, dsn string, p *commitSwallower) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: DSN pgtest не разобран: %v", err)
	}
	host, port, err := net.SplitHostPort(p.ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	portN, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Host = host
	cfg.ConnConfig.Port = uint16(portN)
	// Прокси читает протокол открытым текстом: TLS к контейнеру pgtest не
	// поднимается и так (sslmode=disable), здесь это закреплено явно.
	cfg.ConnConfig.TLSConfig = nil
	cfg.ConnConfig.Fallbacks = nil
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: пул через прокси не открыт: %v", err)
	}
	pgtest.ClosePoolAtEnd(t, pool)
	return pool
}

// upstreamOf — адрес базы из DSN pgtest.
func upstreamOf(t *testing.T, dsn string) string {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: DSN pgtest не разобран: %v", err)
	}
	return net.JoinHostPort(cfg.ConnConfig.Host, strconv.Itoa(int(cfg.ConnConfig.Port)))
}

// TestHarness_CommitSwallowerHoldsTheTransactionOpen — самопроверка фикстуры,
// а не испытуемого: прокси, проглатывающий COMMIT, оставляет сеанс «idle in
// transaction» с его замком (соседняя правка той же строки ждёт), а без
// проглатывания (близнец) транзакция коммитится. Таблица — своя, не схема
// шлюза: проба фикстуры не зависит от предмета полосы.
func TestHarness_CommitSwallowerHoldsTheTransactionOpen(t *testing.T) {
	for _, swallow := range []bool{true, false} {
		t.Run(map[bool]string{true: "проглатывает", false: "близнец: пропускает"}[swallow], func(t *testing.T) {
			dsn := pgtest.NewEmptyDB(t)
			direct := openPool(t, dsn)
			ctx := context.Background()
			if _, err := direct.Exec(ctx, `CREATE TABLE harness_row (id int PRIMARY KEY, n int NOT NULL);
				INSERT INTO harness_row VALUES (1, 0)`); err != nil {
				t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: таблица самопроверки не создана: %v", err)
			}
			proxied := poolVia(t, dsn, newCommitSwallower(t, upstreamOf(t, dsn), swallow))
			done := make(chan error, 1)
			go func() {
				tx, err := proxied.Begin(ctx)
				if err != nil {
					done <- err
					return
				}
				if _, err := tx.Exec(ctx, `UPDATE harness_row SET n = n + 1 WHERE id = 1`); err != nil {
					done <- err
					return
				}
				cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				done <- tx.Commit(cctx)
			}()
			if !swallow {
				if err := <-done; err != nil {
					t.Fatalf("близнец: COMMIT через прокси без проглатывания не прошёл: %v", err)
				}
				var n int
				if err := direct.QueryRow(ctx, `SELECT n FROM harness_row`).Scan(&n); err != nil || n != 1 {
					t.Fatalf("близнец: правка не закоммичена (n=%d, %v)", n, err)
				}
				return
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				var idle int
				if err := direct.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
					WHERE datname = current_database() AND state = 'idle in transaction'`).Scan(&idle); err != nil {
					t.Fatal(err)
				}
				if idle == 1 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("прокси не оставил сеанс «idle in transaction» — фикстура SDR-К1 не создаёт условия")
				}
				pollPause()
			}
			upd := make(chan error, 1)
			go func() {
				_, err := direct.Exec(ctx, `UPDATE harness_row SET n = n + 10 WHERE id = 1`)
				upd <- err
			}()
			waitLockWaiters(t, direct, 1, 5*time.Second)
			if _, err := direct.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity
				WHERE datname = current_database() AND state = 'idle in transaction'`); err != nil {
				t.Fatal(err)
			}
			if err := <-upd; err != nil {
				t.Fatalf("после снятия держателя соседняя правка не прошла: %v", err)
			}
			var n int
			if err := direct.QueryRow(ctx, `SELECT n FROM harness_row`).Scan(&n); err != nil || n != 10 {
				t.Fatalf("правка держателя не откатилась вместе с ним (n=%d, %v)", n, err)
			}
		})
	}
}
