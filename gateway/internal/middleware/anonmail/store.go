// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// Request — вход решения: ключи, момент по часам звена и состояние
// доказательства после чистой проверки.
type Request struct {
	Keys Keys
	Now  time.Time
	// Proof — доказательство, прошедшее чистую проверку (разбор, подпись,
	// срок, работа); nil — заголовка нет либо проверка его отвергла.
	Proof *Proof
	// ProofRejected — заголовок был, и чистая проверка его отвергла: такой
	// запрос не пропускается — ответом будет новый вызов (NTF2-62).
	ProofRejected bool
}

// Verdict — исход решения.
type Verdict struct {
	Outcome Outcome
	// Bits — сложность вызова при исходе Challenge.
	Bits int
	// RetryAfter — при исходе Reject: время до выхода засчитанного момента из
	// окна HARD (источник или подсеть — что позже).
	RetryAfter time.Duration
}

// Store — хранилище решения звена. Decide — одна неделимая операция:
// блокировки ключей → пометка вызова → ведро → лестница → момент пропуска;
// записанное фиксируется только при Pass.
type Store interface {
	Decide(ctx context.Context, r Request) Verdict
	Close() error
}

// bucketWrite — новое состояние ведра общего потока, когда решение взяло
// жетон.
type bucketWrite struct {
	tokens float64
	at     time.Time
}

// decisionTx — одна транзакция решения в хранилище. Порядок вызовов задаёт
// store.Decide и только он: блокировки ключей → пометка → ведро (последней) →
// счёт → запись пропуска → фиксация. Ошибка любого шага до фиксации — отказ
// хранилища (StoreUnavailable) с откатом всего записанного.
type decisionTx interface {
	// lock берёт блокировки ключей в переданном (отсортированном) порядке.
	lock(ctx context.Context, pairs []lockPair) error
	// markSpent помечает вызов использованным; false — уже истрачен.
	markSpent(ctx context.Context, p Proof) (fresh bool, err error)
	// bucket берёт ведро общего потока — последней блокировкой решения.
	bucket(ctx context.Context) (tokens float64, at time.Time, err error)
	// counts — счёт ключа в окнах (now − w, now] по каждому окну.
	counts(ctx context.Context, key string, now time.Time, windows []time.Duration) ([]int, error)
	// nthMoment — момент номер offset (с нуля, по возрастанию) в окне.
	nthMoment(ctx context.Context, key string, now time.Time, window time.Duration, offset int) (time.Time, error)
	// recordPass записывает моменты пропуска всех ключей запроса и, если
	// решение взяло жетон, новое состояние ведра.
	recordPass(ctx context.Context, keys Keys, now time.Time, proof *Proof, bw *bucketWrite) error
	// commit фиксирует решение. Исход — Pass либо StoreUnavailable; у postgres
	// исход неизвестной фиксации разрешается до возврата (resolveCommit).
	commit(ctx context.Context) Outcome
	// rollback откатывает всё записанное; безопасен после commit.
	rollback()
}

// backend — хранилище, открывающее транзакции решения.
type backend interface {
	begin(ctx context.Context) (decisionTx, error)
	close() error
}

// errNotReached — шаг, которого решение не должно было достичь.
var errNotReached = errors.New("anonmail: decision step not reached")

// store — общий порядок решения поверх хранилища: один на оба вида, второго
// порядка шагов в звене нет.
type store struct {
	b      backend
	limits config.AnonMailLimits
	log    *slog.Logger
	// fold — свёртка ключа в пару рекомендательной блокировки.
	fold func(string) lockPair
}

func newStore(b backend, l config.AnonMailLimits, log *slog.Logger) *store {
	return &store{b: b, limits: l, log: log, fold: keyLockPair}
}

func unavailable() Verdict { return Verdict{Outcome: StoreUnavailable} }

// Decide — одно решение (З8). Срок решения — anonMailDecisionBudget от
// контекста запроса: он покрывает захват соединения и все операторы до
// фиксации; сама фиксация в этот срок не входит (у неё свой предел).
func (s *store) Decide(ctx context.Context, r Request) Verdict {
	dctx, cancel := context.WithTimeout(ctx, anonMailDecisionBudget)
	defer cancel()
	tx, err := s.b.begin(dctx)
	if err != nil {
		return unavailable()
	}
	finished := false
	defer func() {
		if !finished {
			tx.rollback()
		}
	}()
	if err := tx.lock(dctx, sortedLockPairs(r.Keys.All(), s.fold)); err != nil {
		return unavailable()
	}
	state, proofBits := proofAbsent, 0
	switch {
	case r.Proof != nil:
		fresh, err := tx.markSpent(dctx, *r.Proof)
		if err != nil {
			return unavailable()
		}
		if fresh {
			state, proofBits = proofFresh, r.Proof.Bits
		} else {
			state = proofRejected
		}
	case r.ProofRejected:
		state = proofRejected
	}
	tokens, at, err := tx.bucket(dctx)
	if err != nil {
		return unavailable()
	}
	l := s.limits
	src, err := tx.counts(dctx, r.Keys.Source, r.Now, []time.Duration{l.Source.FreeWindow, l.Source.PoWWindow, l.Source.HardWindow})
	if err != nil {
		return unavailable()
	}
	c := Counts{Source: SourceCounts{Free: src[0], PoW: src[1], Hard: src[2]}}
	for _, sn := range r.Keys.Subnets {
		n, err := tx.counts(dctx, sn.Key, r.Now, []time.Duration{l.SubnetPoWWindow, l.SubnetHardWindow})
		if err != nil {
			return unavailable()
		}
		c.Subnets = append(c.Subnets, SubnetCounts{Len: sn.Len, PoW: n[0], Hard: n[1]})
	}
	rung := Ladder(l, c)
	tok, bucketAt := refill(tokens, at, r.Now, l.Global)
	pol := decide(l, rung, state, proofBits, tok)
	switch pol.outcome {
	case Reject:
		ra, err := s.retryAfter(dctx, tx, r, c)
		if err != nil {
			return unavailable()
		}
		return Verdict{Outcome: Reject, RetryAfter: ra}
	case Challenge:
		return Verdict{Outcome: Challenge, Bits: pol.bits}
	case Pass:
		var bw *bucketWrite
		if pol.takeToken {
			bw = &bucketWrite{tokens: tok - 1, at: bucketAt}
		}
		var spent *Proof
		if state == proofFresh {
			spent = r.Proof
		}
		if err := tx.recordPass(dctx, r.Keys, r.Now, spent, bw); err != nil {
			return unavailable()
		}
		finished = true
		// Фиксация — на контексте запроса без отмены: исчерпанный срок
		// решения уже отправленную фиксацию не обрывает (её предел — свой).
		return Verdict{Outcome: tx.commit(context.WithoutCancel(ctx))}
	}
	return unavailable()
}

// retryAfter — секунды до выхода засчитанного момента из окна HARD на каждой
// оси, достигшей жёсткого порога; итог — наибольшее (что позже).
func (s *store) retryAfter(ctx context.Context, tx decisionTx, r Request, c Counts) (time.Duration, error) {
	l := s.limits
	var out time.Duration
	if c.Source.Hard >= l.Source.Hard {
		at, err := tx.nthMoment(ctx, r.Keys.Source, r.Now, l.Source.HardWindow, c.Source.Hard-l.Source.Hard)
		if err != nil {
			return 0, err
		}
		out = max(out, retryAfterFor(r.Now, at, l.Source.HardWindow))
	}
	for i, sn := range c.Subnets {
		lim := subnetLimits(l, sn.Len)
		if sn.Hard < lim.Hard {
			continue
		}
		at, err := tx.nthMoment(ctx, r.Keys.Subnets[i].Key, r.Now, l.SubnetHardWindow, sn.Hard-lim.Hard)
		if err != nil {
			return 0, err
		}
		out = max(out, retryAfterFor(r.Now, at, l.SubnetHardWindow))
	}
	return out, nil
}

// Close закрывает хранилище.
func (s *store) Close() error { return s.b.close() }

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }
