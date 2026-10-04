// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package smtp_test

// classify_test.go — таблица классификатора ответа ретранслятора (З26, CX1-17,
// УК17; NTF1-G10, G14).
//
// # Контракт, который утверждают пробы этого пакета (полоса N6)
//
//	smtp.Stage    — стадия сессии: StageConnect, StageTLS, StageAuth, StageMail,
//	                StageRcpt, StageData, StageDone (письмо принято).
//	smtp.Failure  — отказ без кода ответа: FailureNone, FailureUnreachable,
//	                FailureNoTLS, FailureUntrusted, FailureDisconnect.
//	smtp.Attempt  — {Stage, Code int, Enhanced string, Failure}: чем закончилась
//	                одна сессия; Enhanced — расширенный код RFC 3463 («5.1.1») или "".
//	smtp.Classify(Attempt) Cell — ОДНА функция, таблица без `default` (УК17).
//	smtp.Cell     — {Name string, Outcome feed.Outcome, Misconfigured bool,
//	                RelayUnavailable bool}: клетка Р11; Name — имя строки таблицы;
//	                Misconfigured — сигнал `misconfigured`; RelayUnavailable — вклад
//	                в размыкатель.
//
// Исход клетки — словарь фундамента (feed.Outcome, feed.Kind*, feed.Reason*), а не
// свои строки: метка счётчика исходов notify берётся из того же закрытого перечня.

import (
	"testing"

	"github.com/PRO-Robotech/corelib/notify/feed"

	"github.com/PRO-Robotech/kacho/services/notify/internal/smtp"
)

var (
	sent      = feed.Outcome{Kind: feed.KindSent, Reason: feed.ReasonNone}
	rejected  = feed.Outcome{Kind: feed.KindRecipientRejected, Reason: feed.ReasonNone}
	deferUnav = feed.Outcome{Kind: feed.KindDefer, Reason: feed.ReasonPlatformUnavailable}
)

type cellWant struct {
	outcome       feed.Outcome
	misconfigured bool
	unavailable   bool
}

// Каждый ответ ретранслятора из NTF1-G14 — в названной клетке Р11.
func TestClassify_G14_EveryRelayAnswerHasANamedCell(t *testing.T) {
	cases := []struct {
		name string
		in   smtp.Attempt
		want cellWant
	}{
		{"250 — письмо принято", smtp.Attempt{Stage: smtp.StageDone, Code: 250}, cellWant{sent, false, false}},
		{"421 на приветствии", smtp.Attempt{Stage: smtp.StageConnect, Code: 421, Enhanced: "4.3.2"}, cellWant{deferUnav, false, true}},
		{"421 на RCPT", smtp.Attempt{Stage: smtp.StageRcpt, Code: 421}, cellWant{deferUnav, false, true}},
		{"451 на RCPT", smtp.Attempt{Stage: smtp.StageRcpt, Code: 451, Enhanced: "4.3.0"}, cellWant{deferUnav, false, true}},
		{"535 на AUTH", smtp.Attempt{Stage: smtp.StageAuth, Code: 535, Enhanced: "5.7.8"}, cellWant{deferUnav, true, false}},
		{"550 5.1.1 на RCPT", smtp.Attempt{Stage: smtp.StageRcpt, Code: 550, Enhanced: "5.1.1"}, cellWant{rejected, false, false}},
		{"550 5.7.1 на RCPT", smtp.Attempt{Stage: smtp.StageRcpt, Code: 550, Enhanced: "5.7.1"}, cellWant{rejected, false, false}},
		{"550 без расширенного кода на RCPT", smtp.Attempt{Stage: smtp.StageRcpt, Code: 550}, cellWant{rejected, false, false}},
		{"552 на DATA", smtp.Attempt{Stage: smtp.StageData, Code: 552, Enhanced: "5.3.4"}, cellWant{deferUnav, true, false}},
		{"554 на MAIL FROM", smtp.Attempt{Stage: smtp.StageMail, Code: 554, Enhanced: "5.7.1"}, cellWant{deferUnav, true, false}},
		{"разрыв посреди сессии", smtp.Attempt{Stage: smtp.StageData, Failure: smtp.FailureDisconnect}, cellWant{deferUnav, false, true}},
		{"узел без TLS", smtp.Attempt{Stage: smtp.StageTLS, Failure: smtp.FailureNoTLS}, cellWant{deferUnav, false, true}},
		{"недоверенный сертификат", smtp.Attempt{Stage: smtp.StageTLS, Failure: smtp.FailureUntrusted}, cellWant{deferUnav, false, true}},
		{"узел недоступен", smtp.Attempt{Stage: smtp.StageConnect, Failure: smtp.FailureUnreachable}, cellWant{deferUnav, false, true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := smtp.Classify(tc.in)
			if got.Name == "" {
				t.Fatalf("Classify(%+v): клетка без имени строки таблицы", tc.in)
			}
			if got.Outcome != tc.want.outcome || got.Misconfigured != tc.want.misconfigured || got.RelayUnavailable != tc.want.unavailable {
				t.Fatalf("Classify(%+v) = {%s %+v misconfigured=%v unavailable=%v}, ожидалось {%+v misconfigured=%v unavailable=%v}",
					tc.in, got.Name, got.Outcome, got.Misconfigured, got.RelayUnavailable,
					tc.want.outcome, tc.want.misconfigured, tc.want.unavailable)
			}
		})
	}
}

// УК17: три формы 5xx на RCPT — три строки таблицы с одним исходом
// RECIPIENT_REJECTED (ратификация Р11). Близнец — 550 5.1.1; единственный
// изменённый факт — расширенный код.
func TestClassify_UK17_ThreeRcpt5xxFormsAreThreeRows(t *testing.T) {
	forms := []smtp.Attempt{
		{Stage: smtp.StageRcpt, Code: 550, Enhanced: "5.1.1"},
		{Stage: smtp.StageRcpt, Code: 550, Enhanced: "5.7.1"},
		{Stage: smtp.StageRcpt, Code: 550},
	}
	names := map[string]smtp.Attempt{}
	for _, a := range forms {
		c := smtp.Classify(a)
		if c.Outcome != rejected || c.Misconfigured || c.RelayUnavailable {
			t.Errorf("RCPT %d %q: %+v, ожидался %+v без misconfigured и без размыкателя", a.Code, a.Enhanced, c, rejected)
		}
		if prev, dup := names[c.Name]; dup {
			t.Errorf("формы RCPT %q и %q легли в одну строку %q; УК17 требует строку на форму", prev.Enhanced, a.Enhanced, c.Name)
		}
		names[c.Name] = a
	}
	// 5.7.x — своей строкой и при ином подкоде.
	if c := smtp.Classify(smtp.Attempt{Stage: smtp.StageRcpt, Code: 554, Enhanced: "5.7.9"}); c.Outcome != rejected {
		t.Errorf("RCPT 554 5.7.9: %+v, ожидался %+v", c.Outcome, rejected)
	}
}

// 5xx вне RCPT — DEFER с misconfigured, а не терминальный исход (NTF1-G14 «5xx вне
// RCPT»). Близнец — тот же код на RCPT: исход RECIPIENT_REJECTED; изменённый факт —
// стадия.
func TestClassify_G14_5xxOutsideRcptIsMisconfiguredDefer(t *testing.T) {
	twin := smtp.Classify(smtp.Attempt{Stage: smtp.StageRcpt, Code: 550, Enhanced: "5.1.1"})
	if twin.Outcome != rejected {
		t.Fatalf("близнец RCPT 550 5.1.1: %+v, ожидался %+v", twin.Outcome, rejected)
	}
	for _, st := range []smtp.Stage{smtp.StageConnect, smtp.StageAuth, smtp.StageMail, smtp.StageData} {
		c := smtp.Classify(smtp.Attempt{Stage: st, Code: 550, Enhanced: "5.1.1"})
		if c.Outcome != deferUnav || !c.Misconfigured {
			t.Errorf("стадия %v, 550 5.1.1: %+v misconfigured=%v; ожидался %+v с misconfigured", st, c.Outcome, c.Misconfigured, deferUnav)
		}
	}
}

// Ответ вне таблицы — своя строка «код вне таблицы» с DEFER и misconfigured;
// молчаливого «прочего» нет (З26). Близнец — 250 на конце сессии.
func TestClassify_G14_CodeOutsideTheTableIsItsOwnRow(t *testing.T) {
	known := smtp.Classify(smtp.Attempt{Stage: smtp.StageDone, Code: 250})
	if known.Outcome != sent {
		t.Fatalf("близнец 250: %+v", known.Outcome)
	}
	outside := []smtp.Attempt{
		{Stage: smtp.StageRcpt, Code: 699},
		{Stage: smtp.StageMail, Code: 354},
		{Stage: smtp.StageDone, Code: 199},
	}
	var name string
	for _, a := range outside {
		c := smtp.Classify(a)
		if c.Outcome != deferUnav || !c.Misconfigured || c.RelayUnavailable {
			t.Errorf("код вне таблицы %+v: %+v misconfigured=%v unavailable=%v; ожидался %+v, misconfigured, без размыкателя",
				a, c.Outcome, c.Misconfigured, c.RelayUnavailable, deferUnav)
		}
		if name == "" {
			name = c.Name
		} else if c.Name != name {
			t.Errorf("коды вне таблицы легли в разные строки %q и %q; строка «код вне таблицы» одна", name, c.Name)
		}
		if c.Name == known.Name {
			t.Errorf("код вне таблицы %+v лёг в строку принятого письма %q", a, c.Name)
		}
	}
}

// Исход клетки — только из закрытого перечня, который отправитель вправе дать:
// sent, recipient_rejected, defer(platform_unavailable). Ни один вход не даёт
// пустой клетки.
func TestClassify_OutcomesStayInTheSenderVocabulary(t *testing.T) {
	stages := []smtp.Stage{smtp.StageConnect, smtp.StageTLS, smtp.StageAuth, smtp.StageMail, smtp.StageRcpt, smtp.StageData, smtp.StageDone}
	failures := []smtp.Failure{smtp.FailureNone, smtp.FailureUnreachable, smtp.FailureNoTLS, smtp.FailureUntrusted, smtp.FailureDisconnect}
	codes := []int{0, 199, 220, 250, 354, 421, 450, 451, 452, 500, 535, 550, 552, 553, 554, 699}
	enh := []string{"", "2.0.0", "4.3.0", "5.1.1", "5.7.1", "5.3.4"}
	checked := 0
	for _, st := range stages {
		for _, f := range failures {
			for _, code := range codes {
				for _, e := range enh {
					a := smtp.Attempt{Stage: st, Code: code, Enhanced: e, Failure: f}
					c := smtp.Classify(a)
					checked++
					if c.Name == "" {
						t.Fatalf("Classify(%+v): клетка без имени", a)
					}
					switch c.Outcome {
					case sent, rejected, deferUnav:
					default:
						t.Fatalf("Classify(%+v) = %+v — исход вне словаря отправителя", a, c.Outcome)
					}
					// «Отправлено» — только принятое письмо: конец сессии, 2xx, без отказа
					// соединения. Иначе строка закрывается как доставленная, не будучи ею.
					accepted := st == smtp.StageDone && code/100 == 2 && f == smtp.FailureNone
					if (c.Outcome == sent) != accepted {
						t.Fatalf("Classify(%+v) = %+v; «отправлено» обязано совпадать с принятым письмом (%v)", a, c.Outcome, accepted)
					}
				}
			}
		}
	}
	t.Logf("входов осмотрено: %d", checked)
	if checked == 0 {
		t.Fatal("пустой обход входов")
	}
}

// Узел предложил STARTTLS и отверг его: 4xx — временная недоступность, 5xx — нет
// TLS; оба — DEFER с вкладом в размыкатель и без misconfigured, письмо открытым
// текстом не уходит (З26 «нет TLS»). Близнец — 5xx той же формы на MAIL FROM:
// отказ установки; изменённый факт — стадия.
func TestClassify_StartTLSRefusalIsNoTLS(t *testing.T) {
	twin := smtp.Classify(smtp.Attempt{Stage: smtp.StageMail, Code: 554, Enhanced: "5.7.0"})
	if twin.Outcome != deferUnav || !twin.Misconfigured || twin.RelayUnavailable {
		t.Fatalf("близнец MAIL 554: %+v", twin)
	}
	noTLS := smtp.Classify(smtp.Attempt{Stage: smtp.StageTLS, Failure: smtp.FailureNoTLS})
	for _, a := range []smtp.Attempt{
		{Stage: smtp.StageTLS, Code: 554, Enhanced: "5.7.0"},
		{Stage: smtp.StageTLS, Code: 502},
	} {
		c := smtp.Classify(a)
		if c != noTLS {
			t.Errorf("STARTTLS отвергнут %+v: %+v, ожидалась клетка «нет TLS» %+v", a, c, noTLS)
		}
	}
	if c := smtp.Classify(smtp.Attempt{Stage: smtp.StageTLS, Code: 454, Enhanced: "4.7.0"}); c.Outcome != deferUnav || c.Misconfigured || !c.RelayUnavailable {
		t.Errorf("STARTTLS 454: %+v, ожидалась временная недоступность", c)
	}
}
