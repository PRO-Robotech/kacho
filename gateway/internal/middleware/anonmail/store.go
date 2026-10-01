// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

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
	// Saturated — при исходе StoreUnavailable: хранилище не ответило потому,
	// что ожидание (захват соединения, блокировка ключа, строка ведра, срок
	// решения) превысило предел звена — пропускная способность решения
	// исчерпана (Д66). Отказ иного рода (настройка, соединение) — false.
	Saturated bool
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
	// counts — счёт ключа в окнах (now − w, +∞) по каждому окну. Сверху окно
	// не закрыто (SEC-E2-1): момент решения берётся до сериализации по ключу,
	// и пропуск соседа с более поздним моментом обязан войти в счёт.
	counts(ctx context.Context, key string, now time.Time, windows []time.Duration) ([]int, error)
	// nthMoment — момент номер offset (с нуля, по возрастанию) в окне
	// (now − w, +∞) — том же, что считает counts.
	nthMoment(ctx context.Context, key string, now time.Time, window time.Duration, offset int) (time.Time, error)
	// recordPass записывает моменты пропуска всех ключей запроса и, если
	// решение взяло жетон, новое состояние ведра.
	recordPass(ctx context.Context, keys Keys, now time.Time, proof *Proof, bw *bucketWrite) error
	// commit фиксирует решение. Исход — Pass либо StoreUnavailable; у postgres
	// исход неизвестной фиксации разрешается до возврата (resolveCommit).
	commit(ctx context.Context) Outcome
	// rollback откатывает всё записанное; безопасен после commit. ctx —
	// контекст запроса без отмены.
	rollback(ctx context.Context)
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

// validateStoreWiring — пределы и журнал хранилища. Пределы проверяет
// конструктор хранилища, по пределам которого идёт решение: второго источника
// пределов у звена нет.
func validateStoreWiring(l config.AnonMailLimits, log *slog.Logger) error {
	switch {
	case log == nil:
		return errors.New("anonmail: logger is required")
	case l.Source.Hard < 1 || l.PoWBits.Base < 1 || l.Global.Burst < 1:
		return errors.New("anonmail: limits are not resolved — build them with config.ResolveEdgeLimits")
	}
	return nil
}

// Шаги транзакции решения — имена, которыми отказ называется в журнале.
const (
	stepBegin        = "begin"
	stepLock         = "lock"
	stepMarkSpent    = "mark_spent"
	stepBucket       = "bucket"
	stepCountsSource = "counts_source"
	stepCountsSubnet = "counts_subnet"
	stepRetryAfter   = "retry_after"
	stepRecordPass   = "record_pass"
)

// storeFault — класс отказа хранилища. Набор закрыт, и каждый отказ попадает
// ровно в один класс (hard-misconfig-is-not-outage).
type storeFault int

const (
	// faultMisconfig — неправильная настройка: схемы или таблицы нет, нет прав,
	// неверная база или схема. Сама не пройдёт — звучит уровнем ERROR.
	faultMisconfig storeFault = iota + 1
	// faultSaturated — ожидание сверх предела звена: блокировка ключа или
	// строки ведра, захват соединения, срок оператора или решения. Пропускная
	// способность решения исчерпана (Д66) — WARN и счётчик насыщения.
	faultSaturated
	// faultOutage — прочий сбой: соединение, отказ сервера. WARN.
	faultOutage
)

// classifyStoreErr — класс отказа шага решения.
func classifyStoreErr(err error) storeFault {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch {
		case pe.Code == "55P03" || pe.Code == "57014":
			return faultSaturated
		case strings.HasPrefix(pe.Code, "42"), strings.HasPrefix(pe.Code, "28"),
			strings.HasPrefix(pe.Code, "3D"), strings.HasPrefix(pe.Code, "3F"):
			return faultMisconfig
		}
		return faultOutage
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errMemWait) {
		return faultSaturated
	}
	return faultOutage
}

// fail — отказ шага решения: ошибка обёрнута именем шага и записана в журнал
// уровнем своего класса; ключей запроса (в них клиентский адрес) в журнале нет.
func (s *store) fail(ctx context.Context, step string, err error) Verdict {
	err = fmt.Errorf("anonmail: %s: %w", step, err)
	f := classifyStoreErr(err)
	switch f {
	case faultMisconfig:
		s.log.ErrorContext(ctx, "anon mail limiter: store is misconfigured; answering 503", "step", step, "err", err)
	case faultSaturated:
		s.log.WarnContext(ctx, "anon mail limiter: store wait exceeded the limiter's wait; answering 503", "step", step, "err", err)
	case faultOutage:
		s.log.WarnContext(ctx, "anon mail limiter: store failed; answering 503", "step", step, "err", err)
	}
	return Verdict{Outcome: StoreUnavailable, Saturated: f == faultSaturated}
}

// Decide — одно решение (З8). Срок решения — anonMailDecisionBudget от
// контекста запроса: он покрывает захват соединения и все операторы до
// фиксации; сама фиксация в этот срок не входит (у неё свой предел).
func (s *store) Decide(ctx context.Context, r Request) Verdict {
	dctx, cancel := context.WithTimeout(ctx, anonMailDecisionBudget)
	defer cancel()
	tx, err := s.b.begin(dctx)
	if err != nil {
		return s.fail(ctx, stepBegin, err)
	}
	finished := false
	defer func() {
		if !finished {
			// Откат — на контексте запроса без отмены: исчерпанный срок
			// решения или ушедший клиент откат не обрывают (его предел — свой).
			tx.rollback(context.WithoutCancel(ctx))
		}
	}()
	if err := tx.lock(dctx, sortedLockPairs(r.Keys.All(), s.fold)); err != nil {
		return s.fail(ctx, stepLock, err)
	}
	state, proofBits := proofAbsent, 0
	switch {
	case r.Proof != nil:
		fresh, err := tx.markSpent(dctx, *r.Proof)
		if err != nil {
			return s.fail(ctx, stepMarkSpent, err)
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
		return s.fail(ctx, stepBucket, err)
	}
	l := s.limits
	src, err := tx.counts(dctx, r.Keys.Source, r.Now, []time.Duration{l.Source.FreeWindow, l.Source.PoWWindow, l.Source.HardWindow})
	if err != nil {
		return s.fail(ctx, stepCountsSource, err)
	}
	c := Counts{Source: SourceCounts{Free: src[0], PoW: src[1], Hard: src[2]}}
	for _, sn := range r.Keys.Subnets {
		n, err := tx.counts(dctx, sn.Key, r.Now, []time.Duration{l.SubnetPoWWindow, l.SubnetHardWindow})
		if err != nil {
			return s.fail(ctx, stepCountsSubnet, err)
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
			return s.fail(ctx, stepRetryAfter, err)
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
			return s.fail(ctx, stepRecordPass, err)
		}
		finished = true
		// Фиксация — на контексте запроса без отмены: исчерпанный срок
		// решения уже отправленную фиксацию не обрывает (её предел — свой).
		return Verdict{Outcome: tx.commit(context.WithoutCancel(ctx))}
	}
	return s.fail(ctx, "policy", fmt.Errorf("outcome %s outside the closed set", pol.outcome))
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
