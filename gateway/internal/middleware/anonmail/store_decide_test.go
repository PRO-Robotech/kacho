// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// faultBackend — заглушка хранилища: шаг fail отказывает ошибкой err, прочие
// шаги отвечают «пусто»; rollback запоминает контекст, на котором его позвали.
type faultBackend struct {
	fail string
	err  error
	// hard — счёт ключа в окнах выше жёсткого порога (чтобы дойти до retryAfter).
	hard int

	mu         sync.Mutex
	rollbackOn context.Context
}

func (b *faultBackend) begin(context.Context) (decisionTx, error) {
	if b.fail == stepBegin {
		return nil, b.err
	}
	return &faultTx{b: b}, nil
}
func (b *faultBackend) close() error { return nil }

func (b *faultBackend) at(step string) error {
	if b.fail == step {
		return b.err
	}
	return nil
}

type faultTx struct{ b *faultBackend }

func (x *faultTx) lock(context.Context, []lockPair) error { return x.b.at(stepLock) }
func (x *faultTx) markSpent(context.Context, Proof) (bool, error) {
	return true, x.b.at(stepMarkSpent)
}
func (x *faultTx) bucket(context.Context) (float64, time.Time, error) {
	return 100, time.Time{}, x.b.at(stepBucket)
}
func (x *faultTx) counts(_ context.Context, key string, _ time.Time, w []time.Duration) ([]int, error) {
	step := stepCountsSubnet
	if strings.HasPrefix(key, "src/") {
		step = stepCountsSource
	}
	out := make([]int, len(w))
	if step == stepCountsSource {
		for i := range out {
			out[i] = x.b.hard
		}
	}
	return out, x.b.at(step)
}
func (x *faultTx) nthMoment(context.Context, string, time.Time, time.Duration, int) (time.Time, error) {
	return time.Time{}, x.b.at(stepRetryAfter)
}
func (x *faultTx) recordPass(context.Context, Keys, time.Time, *Proof, *bucketWrite) error {
	return x.b.at(stepRecordPass)
}
func (x *faultTx) commit(context.Context) Outcome { return Pass }
func (x *faultTx) rollback(ctx context.Context) {
	x.b.mu.Lock()
	x.b.rollbackOn = ctx
	x.b.mu.Unlock()
}

type probeCtxKey struct{}

// faultDecide — одно решение над faultBackend с журналом в буфер.
func faultDecide(t *testing.T, b *faultBackend, src string, proof bool) (Verdict, string) {
	t.Helper()
	l := testLimits()
	logs := &syncBuffer{}
	s := newStore(b, l, slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	keys, err := KeysFor(src)
	if err != nil {
		t.Fatal(err)
	}
	r := Request{Keys: keys, Now: time.Unix(1_900_000_000, 0)}
	if proof {
		r.Proof = &Proof{Bits: l.PoWBits.High}
	}
	ctx := context.WithValue(context.Background(), probeCtxKey{}, "request-scope")
	return s.Decide(ctx, r), logs.String()
}

// TestStore_GSE2_1_EveryStepFailureIsLoggedByStepAndClass — отказ любого из
// восьми шагов решения звучит в журнале строкой, называющей шаг; неправильная
// настройка (SQLSTATE 42P01 — таблицы нет) — уровнем ERROR, сбой (55P03 —
// блокировка не взята за lock_timeout) — уровнем WARN. Ключей запроса и
// клиентского адреса в журнале нет (hard-no-pii-in-logs).
func TestStore_GSE2_1_EveryStepFailureIsLoggedByStepAndClass(t *testing.T) {
	const src = "198.51.100.77"
	misconfig := &pgconn.PgError{Code: "42P01", Message: `relation "kacho_gateway.anon_mail_passes" does not exist`}
	lockTimeout := &pgconn.PgError{Code: "55P03", Message: "canceling statement due to lock timeout"}
	steps := []string{stepBegin, stepLock, stepMarkSpent, stepBucket, stepCountsSource, stepCountsSubnet, stepRetryAfter, stepRecordPass}
	for _, step := range steps {
		for _, c := range []struct {
			name  string
			err   error
			level string
		}{
			{"настройка 42P01", misconfig, "level=ERROR"},
			{"сбой 55P03", lockTimeout, "level=WARN"},
		} {
			t.Run(step+"/"+c.name, func(t *testing.T) {
				b := &faultBackend{fail: step, err: c.err}
				if step == stepRetryAfter {
					b.hard = testLimits().Source.Hard
				}
				v, logs := faultDecide(t, b, src, step == stepMarkSpent)
				if v.Outcome != StoreUnavailable {
					t.Fatalf("отказ шага %s: исход %s, ожидался store_unavailable", step, v.Outcome)
				}
				line := ""
				for _, ln := range strings.Split(logs, "\n") {
					if strings.Contains(ln, "step="+step) {
						line = ln
					}
				}
				if line == "" {
					t.Fatalf("журнал не называет шаг %s: %q", step, logs)
				}
				if !strings.Contains(line, c.level) {
					t.Errorf("шаг %s, %s: строка %q, ожидался %s", step, c.name, line, c.level)
				}
				if !strings.Contains(line, "err=") || !strings.Contains(line, "anonmail: "+step+":") {
					t.Errorf("шаг %s: ошибка не обёрнута именем шага: %q", step, line)
				}
				if strings.Contains(logs, "198.51.100") {
					t.Errorf("журнал несёт клиентский адрес: %q", logs)
				}
			})
		}
	}
}

// TestStore_GSE2_1_FailureIsNeverSilent — близнец: исправное хранилище не
// пишет в журнал ни строки уровня WARN и выше — проба отличает отказ от
// исправного решения, а не любой вывод от пустого.
func TestStore_GSE2_1_FailureIsNeverSilent(t *testing.T) {
	v, logs := faultDecide(t, &faultBackend{}, "198.51.100.78", false)
	if v.Outcome != Pass {
		t.Fatalf("исправное хранилище: исход %s", v.Outcome)
	}
	if strings.Contains(logs, "level=WARN") || strings.Contains(logs, "level=ERROR") {
		t.Errorf("исправное решение журналирует отказ: %q", logs)
	}
}

// TestStore_D66_SaturationIsTellableFromOutage — насыщение (ожидание
// хранилища сверх предела звена: блокировка, строка ведра, захват соединения,
// срок решения) помечено в вердикте; отказ иного рода — нет. Счётчик
// насыщения звена (Д66) судит эту пометку.
func TestStore_D66_SaturationIsTellableFromOutage(t *testing.T) {
	cases := []struct {
		name      string
		step      string
		err       error
		saturated bool
	}{
		{"строка ведра: lock_timeout", stepBucket, &pgconn.PgError{Code: "55P03"}, true},
		{"ключ: lock_timeout", stepLock, &pgconn.PgError{Code: "55P03"}, true},
		{"срок оператора: 57014", stepBucket, &pgconn.PgError{Code: "57014"}, true},
		{"захват соединения: срок", stepBegin, context.DeadlineExceeded, true},
		{"memory: ожидание сверх предела", stepBucket, errMemWait, true},
		{"таблицы нет: 42P01", stepCountsSource, &pgconn.PgError{Code: "42P01"}, false},
		{"нет прав: 42501", stepLock, &pgconn.PgError{Code: "42501"}, false},
		{"соединение отвергнуто", stepBegin, &pgconn.ConnectError{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, _ := faultDecide(t, &faultBackend{fail: c.step, err: c.err}, "198.51.100.79", false)
			if v.Outcome != StoreUnavailable || v.Saturated != c.saturated {
				t.Errorf("исход %s, насыщение %v; ожидалось store_unavailable, %v", v.Outcome, v.Saturated, c.saturated)
			}
		})
	}
}

// TestStore_GSE2_2_RollbackKeepsTheRequestContext — откат идёт на контексте
// запроса без отмены: значения контекста (трассировка, корреляция) доходят до
// отката, а отмена запроса его не обрывает.
func TestStore_GSE2_2_RollbackKeepsTheRequestContext(t *testing.T) {
	b := &faultBackend{fail: stepBucket, err: errMemWait}
	if v, _ := faultDecide(t, b, "198.51.100.80", false); v.Outcome != StoreUnavailable {
		t.Fatalf("исход %s", v.Outcome)
	}
	b.mu.Lock()
	ctx := b.rollbackOn
	b.mu.Unlock()
	if ctx == nil {
		t.Fatal("откат не позван")
	}
	if got := ctx.Value(probeCtxKey{}); got != "request-scope" {
		t.Errorf("значение контекста запроса до отката не дошло: %v", got)
	}
}

// TestStore_GSE2_5_ConstructorsRefuseUnresolvedLimits — пределы проверяет
// конструктор хранилища, по пределам которого идёт решение: второго источника
// пределов у звена нет. Близнец — разрешённые пределы приняты.
func TestStore_GSE2_5_ConstructorsRefuseUnresolvedLimits(t *testing.T) {
	log := discardLogger()
	if _, err := NewMemoryStore(config.AnonMailLimits{}, time.Now, log); err == nil {
		t.Error("memory: неразрешённые пределы приняты")
	}
	if _, err := NewPostgresStore(context.Background(), "postgres://127.0.0.1:1/none", config.AnonMailLimits{}, log); err == nil ||
		!strings.Contains(err.Error(), "limits are not resolved") {
		t.Errorf("postgres: неразрешённые пределы — %v, ожидался отказ по пределам до пула", err)
	}
	m, err := NewMemoryStore(testLimits(), time.Now, log)
	if err != nil {
		t.Fatalf("близнец: разрешённые пределы отвергнуты: %v", err)
	}
	_ = m.Close()
	if _, err := NewMemoryStore(testLimits(), time.Now, nil); err == nil {
		t.Error("memory: хранилище без журнала принято")
	}
}

// orderedDecisions — n решений одного источника подряд, с моментами base+i с
// по возрастанию либо по убыванию; FREE пропусков без вызова — дальше вызов.
// Возвращает число пропусков.
func orderedDecisions(t *testing.T, s Store, src string, n int, descending bool) int {
	t.Helper()
	keys, err := KeysFor(src)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Unix(1_900_000_000, 0).UTC()
	passed := 0
	for i := 0; i < n; i++ {
		k := i
		if descending {
			k = n - 1 - i
		}
		if v := s.Decide(context.Background(), Request{Keys: keys, Now: base.Add(time.Duration(k) * time.Second)}); v.Outcome == Pass {
			passed++
		}
	}
	return passed
}

// orderLimits — пределы проб порядка моментов: ведро общего потока не
// вмешивается (BURST выше числа решений).
func orderLimits() config.AnonMailLimits {
	l := testLimits()
	l.Global.Burst = 1000
	return l
}

// TestMemoryStore_SECE2_1_CountDoesNotDependOnTheOrderOfMoments — счёт ключа
// не зависит от порядка, в котором моменты решений дошли до сериализации
// (SEC-E2-1): FREE + 3 решения одного источника пропускают ровно FREE и при
// моментах по возрастанию, и при моментах по убыванию. Близнец — моменты по
// возрастанию — держит счёт и при окне, закрытом сверху; по убыванию такое
// окно не видит соседних пропусков с более поздним моментом.
func TestMemoryStore_SECE2_1_CountDoesNotDependOnTheOrderOfMoments(t *testing.T) {
	l := orderLimits()
	for _, desc := range []bool{false, true} {
		m, err := NewMemoryStore(l, time.Now, discardLogger())
		if err != nil {
			t.Fatal(err)
		}
		got := orderedDecisions(t, m, "198.51.100.81", l.Source.Free+3, desc)
		_ = m.Close()
		if got != l.Source.Free {
			t.Errorf("моменты по убыванию=%v: пропущено %d, ожидалось FREE=%d", desc, got, l.Source.Free)
		}
	}
}

// TestMemoryStore_SECE2_1_RaceOnDifferentMomentsPassesExactlyOne — N
// одновременных решений нового ключа при FREE = 1, у каждого СВОЙ момент:
// пропущено ровно одно, в каком бы порядке решения ни взяли блокировки.
func TestMemoryStore_SECE2_1_RaceOnDifferentMomentsPassesExactlyOne(t *testing.T) {
	l := orderLimits()
	l.Source.Free = 1
	m, err := NewMemoryStore(l, time.Now, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Close() }()
	if got := raceOnDifferentMoments(t, []Store{m}, "198.51.100.82", 32); got != 1 {
		t.Fatalf("пропущено %d из 32, ожидался ровно один", got)
	}
}

// raceOnDifferentMoments — n одновременных решений источника src на
// хранилищах stores (по кругу), моменты base+i мс; возвращает число пропусков.
func raceOnDifferentMoments(t *testing.T, stores []Store, src string, n int) int {
	t.Helper()
	keys, err := KeysFor(src)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Unix(1_900_000_000, 0).UTC()
	var ok, other atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		s := stores[i%len(stores)]
		now := base.Add(time.Duration(n-i) * time.Millisecond)
		go func() {
			defer wg.Done()
			<-start
			switch s.Decide(context.Background(), Request{Keys: keys, Now: now}).Outcome {
			case Pass:
				ok.Add(1)
			case Challenge:
			default:
				other.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if other.Load() != 0 {
		t.Errorf("решений вне {pass, challenge}: %d", other.Load())
	}
	return int(ok.Load())
}

// TestGate_D66_SaturationCounter — счётчик насыщения звена считает только
// отказы с пометкой насыщения; 503 по иной причине его не двигает, а общий
// счётчик недоступности считает оба.
func TestGate_D66_SaturationCounter(t *testing.T) {
	l := testLimits()
	sat := newRig(t, l, func(*testClock) Store { return stubStore{Verdict{Outcome: StoreUnavailable, Saturated: true}} }, 0)
	out := newRig(t, l, func(*testClock) Store { return stubStore{Verdict{Outcome: StoreUnavailable}} }, 0)
	for i := 0; i < 2; i++ {
		if rec := sat.send(pathRecovery, "198.51.100.83", "", ""); rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("насыщение: %d", rec.Code)
		}
	}
	if rec := out.send(pathRecovery, "198.51.100.83", "", ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("сбой: %d", rec.Code)
	}
	if s := sat.gate.Stats(); s.StoreUnavailable != 2 || s.StoreSaturated != 2 {
		t.Errorf("насыщение: %+v, ожидалось 2 и 2", s)
	}
	if s := out.gate.Stats(); s.StoreUnavailable != 1 || s.StoreSaturated != 0 {
		t.Errorf("сбой: %+v, ожидалось 1 и 0", s)
	}
}
