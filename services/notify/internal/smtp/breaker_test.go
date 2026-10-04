// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package smtp_test

// breaker_test.go — размыкатель ретранслятора (З26 «Размыкатель»; NTF1-G07
// «размыкатель открыт», NTF1-G11).
//
// # Контракт (полоса N6)
//
//	smtp.BreakerThreshold int           — N подряд недоступностей, константа notify;
//	smtp.BreakerCooldown  time.Duration — интервал охлаждения, константа notify;
//	smtp.NewBreaker(now func() time.Time) *Breaker — часы управляемые;
//	(*Breaker).Observe(Cell) — учитывает клетку исхода сессии;
//	(*Breaker).Open() bool   — true: notify не забирает строки (цикл Claim — N2).
//
// Вклад в размыкатель несёт только клетка с RelayUnavailable; принятое письмо
// обрывает серию. Часы — управляемые: проба не ждёт охлаждения настоящим временем.

import (
	"testing"
	"time"

	"github.com/PRO-Robotech/kacho/services/notify/internal/smtp"
)

type manualClock struct{ t time.Time }

func (c *manualClock) now() time.Time { return c.t }

var (
	unavailableCell = smtp.Classify(smtp.Attempt{Stage: smtp.StageConnect, Failure: smtp.FailureUnreachable})
	sentCell        = smtp.Classify(smtp.Attempt{Stage: smtp.StageDone, Code: 250})
)

func TestBreaker_ConstantsAreDeclared(t *testing.T) {
	if smtp.BreakerThreshold < 1 {
		t.Fatalf("BreakerThreshold = %d; порог обязан быть ≥ 1", smtp.BreakerThreshold)
	}
	if smtp.BreakerCooldown <= 0 {
		t.Fatalf("BreakerCooldown = %v; охлаждение обязано быть > 0", smtp.BreakerCooldown)
	}
}

// N подряд недоступностей размыкают; N−1 — нет (близнец; изменённый факт — число
// недоступностей подряд).
func TestBreaker_G11_OpensAfterThresholdConsecutiveUnavailabilities(t *testing.T) {
	if !unavailableCell.RelayUnavailable || sentCell.RelayUnavailable {
		t.Fatalf("клетки пробы: недоступность %+v, принятое %+v — вклад в размыкатель не тот", unavailableCell, sentCell)
	}
	clk := &manualClock{t: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)}
	b := smtp.NewBreaker(clk.now)
	for i := 1; i < smtp.BreakerThreshold; i++ {
		b.Observe(unavailableCell)
		if b.Open() {
			t.Fatalf("размыкатель открыт после %d недоступностей при пороге %d", i, smtp.BreakerThreshold)
		}
	}
	b.Observe(unavailableCell)
	if !b.Open() {
		t.Fatalf("размыкатель закрыт после %d недоступностей подряд", smtp.BreakerThreshold)
	}
}

// Принятое письмо обрывает серию: N−1 недоступностей, принятое, одна недоступность —
// размыкатель закрыт. Близнец — та же серия без принятого письма: открыт.
func TestBreaker_G11_SuccessBreaksTheSeries(t *testing.T) {
	clk := &manualClock{t: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)}

	twin := smtp.NewBreaker(clk.now)
	for range smtp.BreakerThreshold {
		twin.Observe(unavailableCell)
	}
	if !twin.Open() {
		t.Fatal("близнец: серия без принятого письма не разомкнула")
	}

	b := smtp.NewBreaker(clk.now)
	for i := 1; i < smtp.BreakerThreshold; i++ {
		b.Observe(unavailableCell)
	}
	b.Observe(sentCell)
	b.Observe(unavailableCell)
	if b.Open() {
		t.Fatal("размыкатель открыт, хотя принятое письмо оборвало серию")
	}
}

// Охлаждение: открыт до BreakerCooldown, закрыт по его истечении (управляемые часы);
// строки ждут, а не отравляются (NTF1-G11 «узел возвращается»).
func TestBreaker_G11_ClosesAfterCooldown(t *testing.T) {
	clk := &manualClock{t: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)}
	b := smtp.NewBreaker(clk.now)
	for range smtp.BreakerThreshold {
		b.Observe(unavailableCell)
	}
	if !b.Open() {
		t.Fatal("размыкатель не открылся")
	}
	clk.t = clk.t.Add(smtp.BreakerCooldown - time.Nanosecond)
	if !b.Open() {
		t.Fatalf("размыкатель закрылся раньше охлаждения %v", smtp.BreakerCooldown)
	}
	clk.t = clk.t.Add(time.Nanosecond)
	if b.Open() {
		t.Fatalf("размыкатель открыт по истечении охлаждения %v", smtp.BreakerCooldown)
	}
}

// Отказ настройки (535 на AUTH, 5xx вне RCPT) — misconfigured, а не недоступность
// узла: размыкатель от него не открывается (З26 — размыкатель только для 4xx,
// разрыва, нет TLS, недоверенного сертификата). Близнец — та же серия недоступностей.
func TestBreaker_MisconfigurationDoesNotOpenIt(t *testing.T) {
	authCell := smtp.Classify(smtp.Attempt{Stage: smtp.StageAuth, Code: 535, Enhanced: "5.7.8"})
	if authCell.RelayUnavailable {
		t.Fatalf("клетка 535 на AUTH несёт вклад в размыкатель: %+v", authCell)
	}
	clk := &manualClock{t: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)}
	twin := smtp.NewBreaker(clk.now)
	for range smtp.BreakerThreshold {
		twin.Observe(unavailableCell)
	}
	if !twin.Open() {
		t.Fatal("близнец: серия недоступностей той же длины не разомкнула — отрицанию нечем быть подтверждённым")
	}
	b := smtp.NewBreaker(clk.now)
	for range smtp.BreakerThreshold {
		b.Observe(authCell)
	}
	if b.Open() {
		t.Fatal("размыкатель открыт серией отказов AUTH")
	}
}
