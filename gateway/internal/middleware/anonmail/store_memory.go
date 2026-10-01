// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// memStripes — число полос мьютексов хранилища memory. Полоса выбирается той же
// свёрткой ключа, что пара рекомендательной блокировки postgres (keyLockPair).
const memStripes = 256

// memSweepEvery — шаг уборки хранилища memory: моменты старше срока хранения
// (З26) и пометки вызовов после их срока. Память процесса ограничена тем, что
// уборка идёт и по ключам, к которым больше никто не обращается.
const memSweepEvery = 5 * time.Minute

var errMemWait = errors.New("anonmail: memory store wait exceeded the limiter's store wait")

// MemoryStore — хранилище звена в памяти процесса. Законно ровно при флоте из
// одной реплики (пару «вид хранилища ↔ флот» судит гейт поставки, общий с
// однократностью). Неделимость та же, что у postgres: пометка, решение и
// момент исполняются под одной критической секцией полосатых мьютексов ключей
// и ведра, и пометка снимается при любом исходе, кроме Pass; исхода
// «фиксация неизвестна» у памяти нет.
type MemoryStore struct {
	*store
	*memBackend
}

// NewMemoryStore строит хранилище memory. now — часы уборки (часы решения
// приходят с запросом). Неразрешённые пределы и отсутствие журнала — отказ
// сборки.
func NewMemoryStore(l config.AnonMailLimits, now func() time.Time, log *slog.Logger) (*MemoryStore, error) {
	if err := validateStoreWiring(l, log); err != nil {
		return nil, err
	}
	b := &memBackend{
		bucketLock: make(chan struct{}, 1),
		moments:    map[string][]time.Time{},
		spent:      map[[challengeIDLen]byte]*spentEntry{},
		stop:       make(chan struct{}),
		stopped:    make(chan struct{}),
		tokens:     float64(l.Global.Burst),
	}
	for i := range b.stripes {
		b.stripes[i] = make(chan struct{}, 1)
	}
	go b.sweepLoop(now)
	return &MemoryStore{store: newStore(b, l, log), memBackend: b}, nil
}

// Close останавливает уборку.
func (m *MemoryStore) Close() error { return m.store.Close() }

type spentEntry struct {
	expires time.Time
	// pending — решение, пометившее вызов, ещё не зафиксировано; канал
	// закрывается фиксацией либо откатом. nil — пометка зафиксирована.
	pending chan struct{}
}

type memBackend struct {
	stripes    [memStripes]chan struct{}
	bucketLock chan struct{}
	// tokens и bucketAt принадлежат держателю bucketLock.
	tokens   float64
	bucketAt time.Time

	momentsMu sync.Mutex
	moments   map[string][]time.Time

	spentMu sync.Mutex
	spent   map[[challengeIDLen]byte]*spentEntry

	closeOnce sync.Once
	stop      chan struct{}
	stopped   chan struct{}
}

func (b *memBackend) begin(context.Context) (decisionTx, error) { return &memTx{b: b}, nil }

func (b *memBackend) close() error {
	b.closeOnce.Do(func() { close(b.stop) })
	<-b.stopped
	return nil
}

// sweepLoop — уборка хранилища memory.
//
// РЕПЛИКИ: на-реплику — хранилище memory живёт в памяти процесса и законно ровно
// при флоте из одной реплики; каждая реплика убирает только свою память.
func (b *memBackend) sweepLoop(now func() time.Time) {
	defer close(b.stopped)
	t := time.NewTicker(memSweepEvery)
	defer t.Stop()
	for {
		select {
		case <-b.stop:
			return
		case <-t.C:
			b.sweep(now())
		}
	}
}

// sweep уносит моменты старше срока хранения (функция З26 над таблицей границ)
// и зафиксированные пометки после срока вызова.
func (b *memBackend) sweep(now time.Time) {
	cut := now.Add(-PassRetention())
	b.momentsMu.Lock()
	for k, ms := range b.moments {
		i := sort.Search(len(ms), func(i int) bool { return ms[i].After(cut) })
		if i == len(ms) {
			delete(b.moments, k)
			continue
		}
		b.moments[k] = append([]time.Time(nil), ms[i:]...)
	}
	b.momentsMu.Unlock()
	b.spentMu.Lock()
	for id, e := range b.spent {
		if e.pending == nil && !now.Before(e.expires) {
			delete(b.spent, id)
		}
	}
	b.spentMu.Unlock()
}

type memSize struct{ moments, spent int }

func (b *memBackend) size() memSize {
	b.momentsMu.Lock()
	n := 0
	for _, ms := range b.moments {
		n += len(ms)
	}
	b.momentsMu.Unlock()
	b.spentMu.Lock()
	defer b.spentMu.Unlock()
	return memSize{moments: n, spent: len(b.spent)}
}

// markCommitted — зафиксированная пометка вызова (для построения проб).
func (b *memBackend) markCommitted(id [challengeIDLen]byte, expires time.Time) {
	b.spentMu.Lock()
	b.spent[id] = &spentEntry{expires: expires}
	b.spentMu.Unlock()
}

// acquire — взять полосу с ожиданием не дольше предела звена.
func acquire(ctx context.Context, ch chan struct{}) error {
	t := time.NewTimer(anonMailStoreWait)
	defer t.Stop()
	select {
	case ch <- struct{}{}:
		return nil
	case <-t.C:
		return errMemWait
	case <-ctx.Done():
		return ctx.Err()
	}
}

type memTx struct {
	b           *memBackend
	held        []chan struct{}
	holdsBucket bool
	reserved    *[challengeIDLen]byte
	pending     chan struct{}

	passKeys  []string
	passAt    time.Time
	passWrite bool
	bw        *bucketWrite
}

func (x *memTx) lock(ctx context.Context, pairs []lockPair) error {
	idx := make([]int, 0, len(pairs))
	seen := map[int]bool{}
	for _, p := range pairs {
		i := int(uint32(p.obj) % memStripes) // #nosec G115 -- номер полосы
		if !seen[i] {
			seen[i] = true
			idx = append(idx, i)
		}
	}
	// Порядок взятия — по номеру полосы: один для любых двух решений.
	sort.Ints(idx)
	for _, i := range idx {
		if err := acquire(ctx, x.b.stripes[i]); err != nil {
			return err
		}
		x.held = append(x.held, x.b.stripes[i])
	}
	return nil
}

// markSpent — пометка вызова в транзакции решения.
//
// РЕПЛИКИ: запрос — ожидание принадлежит обслуживаемому запросу: оно ждёт исход
// соседнего решения того же вызова не дольше anonMailStoreWait и живёт ровно
// столько, сколько этот запрос.
func (x *memTx) markSpent(ctx context.Context, p Proof) (bool, error) {
	deadline := time.NewTimer(anonMailStoreWait)
	defer deadline.Stop()
	for {
		x.b.spentMu.Lock()
		e, ok := x.b.spent[p.ID]
		if !ok {
			ch := make(chan struct{})
			x.b.spent[p.ID] = &spentEntry{expires: p.ExpiresAt, pending: ch}
			x.b.spentMu.Unlock()
			id := p.ID
			x.reserved, x.pending = &id, ch
			return true, nil
		}
		if e.pending == nil {
			x.b.spentMu.Unlock()
			return false, nil
		}
		wait := e.pending
		x.b.spentMu.Unlock()
		// Пометку держит незафиксированное решение соседа — ждать его исхода,
		// как вставка postgres ждёт соседнюю незафиксированную вставку.
		select {
		case <-wait:
		case <-deadline.C:
			return false, errMemWait
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}

func (x *memTx) bucket(ctx context.Context) (float64, time.Time, error) {
	if err := acquire(ctx, x.b.bucketLock); err != nil {
		return 0, time.Time{}, err
	}
	x.holdsBucket = true
	return x.b.tokens, x.b.bucketAt, nil
}

func (x *memTx) counts(_ context.Context, key string, now time.Time, windows []time.Duration) ([]int, error) {
	x.b.momentsMu.Lock()
	ms := x.b.moments[key]
	out := make([]int, len(windows))
	// Окно (now − w, +∞): сверху не закрыто (SEC-E2-1) — пропуск соседа с
	// более поздним моментом входит в счёт.
	for j, w := range windows {
		lo := sort.Search(len(ms), func(i int) bool { return ms[i].After(now.Add(-w)) })
		out[j] = len(ms) - lo
	}
	x.b.momentsMu.Unlock()
	return out, nil
}

func (x *memTx) nthMoment(_ context.Context, key string, now time.Time, window time.Duration, offset int) (time.Time, error) {
	x.b.momentsMu.Lock()
	defer x.b.momentsMu.Unlock()
	ms := x.b.moments[key]
	lo := sort.Search(len(ms), func(i int) bool { return ms[i].After(now.Add(-window)) })
	if lo+offset >= len(ms) || offset < 0 {
		return time.Time{}, errNotReached
	}
	return ms[lo+offset], nil
}

func (x *memTx) recordPass(_ context.Context, keys Keys, now time.Time, _ *Proof, bw *bucketWrite) error {
	x.passKeys, x.passAt, x.passWrite, x.bw = keys.All(), now, true, bw
	return nil
}

func (x *memTx) commit(context.Context) Outcome {
	if x.passWrite {
		x.b.momentsMu.Lock()
		for _, k := range x.passKeys {
			ms := x.b.moments[k]
			i := sort.Search(len(ms), func(i int) bool { return ms[i].After(x.passAt) })
			ms = append(ms, time.Time{})
			copy(ms[i+1:], ms[i:])
			ms[i] = x.passAt
			x.b.moments[k] = ms
		}
		x.b.momentsMu.Unlock()
		if x.bw != nil {
			x.b.tokens, x.b.bucketAt = x.bw.tokens, x.bw.at
		}
	}
	if x.reserved != nil {
		x.b.spentMu.Lock()
		if e := x.b.spent[*x.reserved]; e != nil && e.pending == x.pending {
			e.pending = nil
		}
		x.b.spentMu.Unlock()
		close(x.pending)
		x.reserved = nil
	}
	x.release()
	return Pass
}

func (x *memTx) rollback(context.Context) {
	if x.reserved != nil {
		x.b.spentMu.Lock()
		if e := x.b.spent[*x.reserved]; e != nil && e.pending == x.pending {
			delete(x.b.spent, *x.reserved)
		}
		x.b.spentMu.Unlock()
		close(x.pending)
		x.reserved = nil
	}
	x.release()
}

func (x *memTx) release() {
	if x.holdsBucket {
		<-x.b.bucketLock
		x.holdsBucket = false
	}
	for i := len(x.held) - 1; i >= 0; i-- {
		<-x.held[i]
	}
	x.held = nil
}
