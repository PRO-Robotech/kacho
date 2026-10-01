// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package anonmail

import (
	"math"
	"time"

	"github.com/PRO-Robotech/kacho/gateway/internal/config"
)

// Rung — ступень лестницы (приёмка NTF-2, Р5).
type Rung int

// Ступени: открыто (без вызова), вызов базовой сложности, вызов повышенной
// сложности, жёсткий отказ. Нулевое значение — открыто: оно же наименьшая
// ступень, и максимум по осям его перебивает.
const (
	RungOpen Rung = iota
	RungBase
	RungHigh
	RungHard
)

func (r Rung) String() string {
	switch r {
	case RungOpen:
		return "open"
	case RungBase:
		return "base"
	case RungHigh:
		return "high"
	case RungHard:
		return "hard"
	}
	return "rung(?)"
}

// SourceCounts — счёт ключа источника в окне каждой ручки: у FREE, POW и HARD
// окна свои (Р8), поэтому и счёта три.
type SourceCounts struct {
	Free, PoW, Hard int
}

// SubnetCounts — счёт ключа подсети одной длины префикса в окнах `W_Ps` и
// `W_Hs`.
type SubnetCounts struct {
	Len       int
	PoW, Hard int
}

// Counts — счёт ключей запроса к его приходу: число запросов, которые звено
// ПРОПУСТИЛО к службе в окне ручки (Р5, «счёт ключа»).
type Counts struct {
	Source  SourceCounts
	Subnets []SubnetCounts
}

// Ladder — ступень запроса, чистая функция (З8). Ступени источника
// проверяются сверху вниз: HARD — отказ, POW — вызов повышенной сложности, FREE
// — вызов базовой. Ось подсети даёт отказ на Hs и вызов БАЗОВОЙ сложности на Ps:
// порог POW меняет только сложность вызова, и задаёт его ручка источника (Р5).
// Итог — наибольшая ступень по осям.
func Ladder(l config.AnonMailLimits, c Counts) Rung {
	r := RungOpen
	switch {
	case c.Source.Hard >= l.Source.Hard:
		return RungHard
	case c.Source.PoW >= l.Source.PoW:
		r = RungHigh
	case c.Source.Free >= l.Source.Free:
		r = RungBase
	}
	for _, sn := range c.Subnets {
		lim := subnetLimits(l, sn.Len)
		switch {
		case sn.Hard >= lim.Hard:
			return RungHard
		case sn.PoW >= lim.PoW:
			r = max(r, RungBase)
		}
	}
	return r
}

// subnetLimits — пределы оси подсети по длине префикса. Длина вне закрытого
// набора ключей (KeysFor) сюда не приходит; если придёт — пределы нулевые, то
// есть любой счёт ≥ 0 даёт отказ: ошибка построения ключа кончается отказом, а
// не пропуском.
func subnetLimits(l config.AnonMailLimits, prefixLen int) config.AnonMailSubnetLimits {
	switch prefixLen {
	case subnetLenV4:
		return l.SubnetV4Len24
	case subnetLen56:
		return l.SubnetV6Len56
	case subnetLen48:
		return l.SubnetV6Len48
	}
	return config.AnonMailSubnetLimits{}
}

// Outcome — исход решения звена. Набор закрыт: Pass · Challenge · Reject ·
// StoreUnavailable (З8). Нулевое значение исходом не является: незаполненное
// решение не проходит за одно из закрытых.
type Outcome int

const (
	// Pass — запрос пропущен к службе; момент пропуска и пометка вызова
	// записаны и зафиксированы.
	Pass Outcome = iota + 1
	// Challenge — ответ-вызов proof-of-work (429 PROOF_OF_WORK_REQUIRED).
	Challenge
	// Reject — жёсткий отказ (429 RATE_LIMITED с Retry-After).
	Reject
	// StoreUnavailable — хранилище не ответило в предел звена либо исход
	// фиксации не выяснен за срок разрешения: 503, до службы запрос не доходит.
	StoreUnavailable
)

// Outcomes — закрытый набор исходов в порядке объявления.
func Outcomes() []Outcome { return []Outcome{Pass, Challenge, Reject, StoreUnavailable} }

// Valid — принадлежит ли исход закрытому набору.
func (o Outcome) Valid() bool { return o >= Pass && o <= StoreUnavailable }

func (o Outcome) String() string {
	switch o {
	case Pass:
		return "pass"
	case Challenge:
		return "challenge"
	case Reject:
		return "reject"
	case StoreUnavailable:
		return "store_unavailable"
	}
	return "outcome(?)"
}

// proofState — что звено знает о доказательстве ДО хранилища.
type proofState int

const (
	// proofAbsent — заголовка доказательства нет.
	proofAbsent proofState = iota
	// proofRejected — заголовок есть, а чистая проверка (разбор, подпись,
	// срок, работа) его отвергла, либо пометка нашла вызов истраченным.
	proofRejected
	// proofFresh — чистая проверка прошла, и пометка вызова записана в
	// транзакции решения.
	proofFresh
)

// policy — исход чистой части решения.
type policy struct {
	outcome   Outcome
	bits      int
	takeToken bool
}

// decide — исход по ступени, доказательству и ведру общего потока (Р5):
//
//   - HARD — отказ; доказательство на этой ступени не принимается;
//   - свежее доказательство не ниже сложности ступени — пропуск без жетона
//     ведра: вызов, выданный общим потоком, решается доказательством (NTF2-74);
//   - доказательство, не прошедшее проверку, или ниже сложности ступени —
//     новый вызов, не пропуск (NTF2-62);
//   - ступень с вызовом — вызов её сложности;
//   - открытая ступень — жетон ведра; жетона нет — вызов базовой сложности
//     всем, включая источники ниже своего порога (NTF2-74).
func decide(l config.AnonMailLimits, r Rung, p proofState, proofBits int, tokens float64) policy {
	if r == RungHard {
		return policy{outcome: Reject}
	}
	required := 0
	switch r {
	case RungBase:
		required = l.PoWBits.Base
	case RungHigh:
		required = l.PoWBits.High
	}
	if p == proofFresh && proofBits >= max(required, l.PoWBits.Base) {
		return policy{outcome: Pass}
	}
	if p != proofAbsent || required > 0 {
		return policy{outcome: Challenge, bits: max(required, l.PoWBits.Base)}
	}
	if tokens >= 1 {
		return policy{outcome: Pass, takeToken: true}
	}
	return policy{outcome: Challenge, bits: l.PoWBits.Base}
}

// refill — ведро общего потока к моменту now: темп RATE в секунду до потолка
// BURST. Часы, ушедшие назад (реплики флота сверены не точнее своих часов), не
// отнимают жетонов и не сдвигают момент ведра назад.
func refill(tokens float64, at, now time.Time, g config.AnonMailGlobalFlow) (float64, time.Time) {
	if !now.After(at) {
		return math.Min(tokens, float64(g.Burst)), at
	}
	t := tokens + now.Sub(at).Seconds()*g.RatePerSecond
	return math.Min(t, float64(g.Burst)), now
}

// retryAfterFor — целые секунды (вверх, не меньше одной) до момента, когда
// засчитанный момент `at` выйдет из окна длины `window`: момент входит в счёт
// по `at + window − ε` и выходит в `at + window` (Р5, «окно ручки»).
func retryAfterFor(now, at time.Time, window time.Duration) time.Duration {
	left := at.Add(window).Sub(now)
	secs := int64(math.Ceil(left.Seconds()))
	if secs < 1 {
		secs = 1
	}
	return time.Duration(secs) * time.Second
}
