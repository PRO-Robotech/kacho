// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package limits_test

// fence_integration_test.go — ограда ключа сетки (полоса N7, kacho#2915;
// замысел З24 «Ограда ключа сетки», §6; CX1-66 (б), CX1-68, CX1-69, CX1-70).
// Реплики — экземпляры испытуемого в процессе пробы над общей базой; «старт»
// реплики — её WriteFence.

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/corelib/pgtest"

	"github.com/PRO-Robotech/kacho/services/notify/internal/limits"
)

// fenceFingerprint — отпечаток в строке ограды.
func fenceFingerprint(t *testing.T, pool *pgxpool.Pool) []byte {
	t.Helper()
	var fp []byte
	if err := pool.QueryRow(context.Background(), `SELECT fingerprint FROM recipient_key_fence`).Scan(&fp); err != nil {
		t.Fatalf("ограда не прочитана: %v", err)
	}
	return fp
}

// CX1-66 (б) — две реплики с разными ключами: после старта второй резерв
// первой — ErrRecipientKeySuperseded (отличный от исчерпания сетки), сетка
// изменена только второй, ограда несёт отпечаток второго ключа. Близнец — один
// ключ у обеих: обе резервируют.
func TestLimits_CX166b_SecondKeySupersedesTheFirstReplica(t *testing.T) {
	for _, sameKey := range []bool{false, true} {
		name := map[bool]string{false: "разные ключи", true: "близнец: один ключ"}[sameKey]
		t.Run(name, func(t *testing.T) {
			pool := openPool(t, pgtest.NewDB(t))
			clk := newClock(noon)
			second := keyTwo
			if sameKey {
				second = keyOne
			}
			first := started(t, pool, keyOne, gridWide, clk)
			next := started(t, pool, second, gridWide, clk)
			to := addr(t, "fence.recipient@example.invalid")
			ctx := context.Background()

			_, err := first.lim.Reserve(ctx, row("probe", feed.ClassNotice, to))
			if sameKey {
				if err != nil {
					t.Fatalf("близнец: один ключ у обеих, а первая не резервирует: %v", err)
				}
			} else {
				if !errors.Is(err, limits.ErrRecipientKeySuperseded) {
					t.Fatalf("после старта реплики с другим ключом резерв первой: ожидалось "+
						"ErrRecipientKeySuperseded, получено %v", err)
				}
				if errors.Is(err, limits.ErrRecipientNetExhausted) {
					t.Fatalf("замена ключа неотличима от исчерпания сетки: %v", err)
				}
				if got := sumCount(t, pool, feed.ClassNotice); got != 0 {
					t.Fatalf("резерв под заменённым ключом оставил вклад: %d", got)
				}
			}
			if _, err := next.lim.Reserve(ctx, row("probe", feed.ClassNotice, to)); err != nil {
				t.Fatalf("реплика действующего ключа не резервирует: %v", err)
			}
			if got := fenceFingerprint(t, pool); !strings.EqualFold(hex.EncodeToString(got), hex.EncodeToString(fingerprint(second))) {
				t.Fatalf("ограда несёт отпечаток %x, ожидался отпечаток ключа последней стартовавшей реплики %x", got, fingerprint(second))
			}
			var foreign int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM recipient_net WHERE key <> $1`, netKey(t, second, to)).Scan(&foreign); err != nil {
				t.Fatal(err)
			}
			if foreign != 0 {
				t.Fatalf("строк сетки не под действующим ключом %d — первая реплика писала после замены", foreign)
			}
		})
	}
}

// CX1-68 (б), конкурентный близнец — резерв первой, начатый до старта второй,
// коммитится; запись ограды второй ждёт его (табличный замок), а следующий
// резерв первой — сторож. Резерв первой задержан посторонним держателем строки
// `global_daily` уже после чтения ограды.
func TestLimits_CX168b_ReserveStartedBeforeFenceWriteCommits(t *testing.T) {
	pool := openPool(t, pgtest.NewDB(t))
	clk := newClock(noon)
	first := started(t, pool, keyOne, gridWide, clk)
	ctx := context.Background()
	if _, err := first.lim.Reserve(ctx, row("probe", feed.ClassNotice, addr(t, "seed@example.invalid"))); err != nil {
		t.Fatalf("посев строки потолка: %v", err)
	}
	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := holder.Exec(ctx, `SELECT count FROM global_daily FOR UPDATE`); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: держатель строки потолка не взял замок: %v", err)
	}

	inFlight := make(chan error, 1)
	go func() {
		_, err := first.lim.Reserve(ctx, row("probe", feed.ClassNotice, addr(t, "inflight@example.invalid")))
		inFlight <- err
	}()
	waitLockWaiters(t, pool, 1, 5*time.Second)

	second := newReplica(t, pool, keyTwo, gridWide, clk)
	fenceDone := make(chan error, 1)
	go func() { fenceDone <- second.lim.WriteFence(ctx) }()
	waitLockWaiters(t, pool, 2, 5*time.Second)

	select {
	case err := <-fenceDone:
		t.Fatalf("запись ограды завершилась (%v), пока резерв под прежним ключом в полёте", err)
	default:
	}
	if err := holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-inFlight; err != nil {
		t.Fatalf("резерв, начатый до записи ограды, не закоммичен: %v", err)
	}
	if err := <-fenceDone; err != nil {
		t.Fatalf("запись ограды после резерва в полёте: %v", err)
	}
	if _, err := first.lim.Reserve(ctx, row("probe", feed.ClassNotice, addr(t, "after@example.invalid"))); !errors.Is(err, limits.ErrRecipientKeySuperseded) {
		t.Fatalf("следующий резерв первой после записи ограды: ожидался сторож, получено %v", err)
	}
}

// reserveFlow — непрекращающийся поток перекрывающихся резервов реплики r в
// workers параллельных исполнителей, транзакция резерва целиком (ограда,
// сетка, потолок). stop останавливает поток; superseded — сколько резервов
// дали сторож; running — поток ещё идёт.
type reserveFlow struct {
	stop       chan struct{}
	wg         sync.WaitGroup
	ok         atomic.Int64
	superseded atomic.Int64
	running    atomic.Bool
	mu         sync.Mutex
	errs       []error
}

func startFlow(t *testing.T, r *replica, workers int) *reserveFlow {
	t.Helper()
	f := &reserveFlow{stop: make(chan struct{})}
	f.running.Store(true)
	for w := range workers {
		to := addr(t, fmt.Sprintf("flow%02d@example.invalid", w))
		f.wg.Add(1)
		go func() {
			defer f.wg.Done()
			for {
				select {
				case <-f.stop:
					return
				default:
				}
				_, err := r.lim.Reserve(context.Background(), row("probe", feed.ClassNotice, to))
				switch {
				case err == nil:
					f.ok.Add(1)
				case errors.Is(err, limits.ErrRecipientKeySuperseded):
					f.superseded.Add(1)
				default:
					f.mu.Lock()
					f.errs = append(f.errs, err)
					f.mu.Unlock()
				}
			}
		}()
	}
	return f
}

func (f *reserveFlow) halt() {
	close(f.stop)
	f.wg.Wait()
	f.running.Store(false)
}

// waitFlowing ждёт условия «поток резервов идёт»: n закоммиченных резервов.
func waitFlowing(t *testing.T, f *reserveFlow, n int64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for f.ok.Load() < n {
		if time.Now().After(deadline) {
			t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: поток резервов не пошёл (закоммичено %d из %d)", f.ok.Load(), n)
		}
		pollPause()
	}
}

// CX1-69 (б) — запись ограды под потоком: первая реплика ведёт не меньше 4
// параллельных перекрывающихся резервов, стартует вторая → запись ограды
// завершается, пока поток идёт, и резервы первой после неё — сторож. Близнец —
// один резерв без перекрытия: запись завершается.
func TestLimits_CX169b_FenceWriteCompletesUnderReserveFlow(t *testing.T) {
	for _, workers := range []int{4, 1} {
		t.Run(map[int]string{4: "поток 4", 1: "близнец: один исполнитель"}[workers], func(t *testing.T) {
			pool := openPool(t, pgtest.NewDB(t))
			clk := newClock(noon)
			first := started(t, pool, keyOne, gridWide, clk)
			flow := startFlow(t, first, workers)
			waitFlowing(t, flow, 20)

			second := newReplica(t, pool, keyTwo, gridWide, clk)
			start := time.Now()
			err := second.lim.WriteFence(context.Background())
			took := time.Since(start)
			stillRunning := flow.running.Load()
			okAtFence := flow.ok.Load()
			waitSuperseded := time.Now().Add(5 * time.Second)
			for flow.superseded.Load() == 0 && time.Now().Before(waitSuperseded) {
				pollPause()
			}
			flow.halt()
			if err != nil {
				t.Fatalf("запись ограды под потоком из %d отвергнута за %s: %v", workers, took, err)
			}
			if !stillRunning {
				t.Fatal("НЕ ВЫПОЛНИЛОСЬ: поток остановился раньше конца записи ограды")
			}
			if took > limits.FenceLockTimeout {
				t.Fatalf("запись ограды под потоком заняла %s при пределе %s", took, limits.FenceLockTimeout)
			}
			if flow.superseded.Load() == 0 {
				t.Fatalf("после записи ограды резервы первой не дали сторожа (закоммичено %d, до ограды %d)", flow.ok.Load(), okAtFence)
			}
			if len(flow.errs) > 0 {
				t.Fatalf("поток резервов дал ошибки вне исходов сетки: %v", flow.errs[0])
			}
			t.Logf("запись ограды под потоком %d: %s", workers, took)
		})
	}
}

// CX1-70 — предел под законным потоком: не меньше 16 одновременных резервов
// первой, старт второй → запись ограды в пределе FenceLockTimeout, и журнал
// старта второй печатает время ожидания замка числом (атрибут `wait`).
func TestFenceWriteWithinLimitUnderFullReserveFlow(t *testing.T) {
	pool := openPool(t, pgtest.NewDB(t))
	clk := newClock(noon)
	first := started(t, pool, keyOne, gridWide, clk)
	flow := startFlow(t, first, 16)
	waitFlowing(t, flow, 64)
	second := newReplica(t, pool, keyTwo, gridWide, clk)
	start := time.Now()
	err := second.lim.WriteFence(context.Background())
	took := time.Since(start)
	flow.halt()
	if err != nil {
		t.Fatalf("запись ограды под потоком 16 отвергнута за %s: %v", took, err)
	}
	if took > limits.FenceLockTimeout {
		t.Fatalf("запись ограды заняла %s, предел %s", took, limits.FenceLockTimeout)
	}
	if !strings.Contains(second.log.String(), `"wait":`) {
		t.Fatalf("журнал старта не печатает время ожидания замка ограды (атрибут wait):\n%s", second.log.String())
	}
	t.Logf("поток 16: запись ограды %s; журнал: %s", took, strings.TrimSpace(second.log.String()))
}

// CX1-69 (в) — посторонняя транзакция держит замок ограды дольше
// FenceLockTimeout: старт второй отказывает ошибкой 55P03 не позже предела с
// запасом, журнал называет отказ и время ожидания, не неся ни ключа, ни
// отпечатка, и не утверждает причины; ограда прежняя; первая продолжает
// резервировать после ухода постороннего.
func TestLimits_CX169c_FenceLockTimeoutRefusesStart(t *testing.T) {
	pool := openPool(t, pgtest.NewDB(t))
	clk := newClock(noon)
	first := started(t, pool, keyOne, gridWide, clk)
	ctx := context.Background()
	foreign, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = foreign.Rollback(ctx) }()
	if _, err := foreign.Exec(ctx, `LOCK TABLE recipient_key_fence IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: посторонний не взял замок ограды: %v", err)
	}

	second := newReplica(t, pool, keyTwo, gridWide, clk)
	start := time.Now()
	err = second.lim.WriteFence(ctx)
	took := time.Since(start)
	if err == nil {
		t.Fatal("ограда записана при постороннем держателе замка")
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "55P03" {
		t.Fatalf("отказ записи ограды не 55P03 (lock_timeout): %v", err)
	}
	if took > limits.FenceLockTimeout+2*time.Second {
		t.Fatalf("отказ пришёл через %s при пределе %s — ожидание не ограничено", took, limits.FenceLockTimeout)
	}
	log := second.log.String()
	if !strings.Contains(log, "ограда ключа сетки не записана: замок не получен за предел") || !strings.Contains(log, `"wait":`) {
		t.Fatalf("журнал отказа старта не называет отказ и время ожидания:\n%s", log)
	}
	for _, leak := range []string{string(keyOne), string(keyTwo), hex.EncodeToString(fingerprint(keyOne)), hex.EncodeToString(fingerprint(keyTwo))} {
		if strings.Contains(strings.ToLower(log), strings.ToLower(leak)) {
			t.Fatalf("журнал отказа несёт ключ или отпечаток:\n%s", log)
		}
	}
	if err := foreign.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if got := fenceFingerprint(t, pool); hex.EncodeToString(got) != hex.EncodeToString(fingerprint(keyOne)) {
		t.Fatalf("ограда сменилась несмотря на отказ: %x", got)
	}
	if _, err := first.lim.Reserve(ctx, row("probe", feed.ClassNotice, addr(t, "after.timeout@example.invalid"))); err != nil {
		t.Fatalf("первая реплика после отказа старта второй не резервирует: %v", err)
	}
}
