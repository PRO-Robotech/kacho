// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package smtp

import (
	"strings"

	"github.com/PRO-Robotech/corelib/notify/feed"
)

// Cell — клетка Р11: исход строки и два сигнала notify.
type Cell struct {
	// Name — имя строки таблицы; одно на строку, стабильное — значение метки
	// причины в счётчиках notify.
	Name    string
	Outcome feed.Outcome
	// Misconfigured — ретранслятор отвечает, но отвергает установку notify
	// (удостоверение, отправитель, письмо): сигнал `misconfigured`.
	Misconfigured bool
	// RelayUnavailable — вклад в размыкатель: узел недоступен или временно не
	// принимает (4xx, разрыв, нет TLS, недоверенный сертификат).
	RelayUnavailable bool
}

var (
	outcomeSent     = feed.Outcome{Kind: feed.KindSent, Reason: feed.ReasonNone}
	outcomeRejected = feed.Outcome{Kind: feed.KindRecipientRejected, Reason: feed.ReasonNone}
	outcomeDefer    = feed.Outcome{Kind: feed.KindDefer, Reason: feed.ReasonPlatformUnavailable}
)

// Строки таблицы (З26). Каждая — своя клетка с именем; две строки одной клетки
// не делят имя.
var (
	cellAccepted = Cell{Name: "accepted", Outcome: outcomeSent}

	// RCPT 5xx — три формы, три строки, один исход RECIPIENT_REJECTED (УК17).
	cellRcptPolicy   = Cell{Name: "rcpt_rejected_policy", Outcome: outcomeRejected}
	cellRcptEnhanced = Cell{Name: "rcpt_rejected_enhanced", Outcome: outcomeRejected}
	cellRcptBare     = Cell{Name: "rcpt_rejected_bare", Outcome: outcomeRejected}

	// 5xx вне RCPT — установка notify отвергнута: DEFER + misconfigured.
	cellGreetingRefused = Cell{Name: "greeting_refused", Outcome: outcomeDefer, Misconfigured: true}
	cellAuthRefused     = Cell{Name: "auth_refused", Outcome: outcomeDefer, Misconfigured: true}
	cellMailRefused     = Cell{Name: "mail_from_refused", Outcome: outcomeDefer, Misconfigured: true}
	cellDataRefused     = Cell{Name: "data_refused", Outcome: outcomeDefer, Misconfigured: true}

	// Недоступность узла — DEFER + размыкатель.
	cellTransient   = Cell{Name: "relay_transient", Outcome: outcomeDefer, RelayUnavailable: true}
	cellUnreachable = Cell{Name: "relay_unreachable", Outcome: outcomeDefer, RelayUnavailable: true}
	cellNoTLS       = Cell{Name: "relay_no_tls", Outcome: outcomeDefer, RelayUnavailable: true}
	cellUntrusted   = Cell{Name: "relay_untrusted_certificate", Outcome: outcomeDefer, RelayUnavailable: true}
	cellDisconnect  = Cell{Name: "relay_disconnect", Outcome: outcomeDefer, RelayUnavailable: true}

	// Ответ вне таблицы — своя строка: DEFER + misconfigured, без размыкателя.
	// Молчаливого «прочего» нет (NTF1-G14): строка видна по имени в счётчике.
	cellOutOfTable = Cell{Name: "code_outside_table", Outcome: outcomeDefer, Misconfigured: true}
)

// Classify — таблица классификатора ответа ретранслятора (З26, УК17). Ключ —
// (отказ без кода | стадия, класс кода, расширенный код). Ветки `default` нет ни
// здесь, ни в функциях, которые она зовёт: каждый класс назван своей ветвью, а то,
// что ни одна не назвала, — строка «код вне таблицы».
func Classify(a Attempt) Cell {
	if a.Failure != FailureNone {
		return failureCell(a.Failure)
	}
	if isClass(a.Code, 2) && a.Stage == StageDone {
		return cellAccepted
	}
	if isClass(a.Code, 4) {
		return cellTransient
	}
	if isClass(a.Code, 5) {
		return permanentCell(a.Stage, a.Enhanced)
	}
	return cellOutOfTable
}

// failureCell — отказ без кода ответа. Значение вне перечня Failure — строка «код
// вне таблицы», а не одна из недоступностей.
func failureCell(f Failure) Cell {
	switch f {
	case FailureUnreachable:
		return cellUnreachable
	case FailureNoTLS:
		return cellNoTLS
	case FailureUntrusted:
		return cellUntrusted
	case FailureDisconnect:
		return cellDisconnect
	case FailureNone:
		// Classify сюда FailureNone не передаёт; ветка названа, чтобы перечень был полон.
		return cellOutOfTable
	}
	return cellOutOfTable
}

// permanentCell — 5xx по стадии. На RCPT — отказ получателя; на приветствии,
// AUTH, MAIL FROM и DATA — отказ установки. 5xx на стадии, где таблица его не
// называет (TLS, после принятия письма), — «код вне таблицы».
func permanentCell(st Stage, enhanced string) Cell {
	switch st {
	case StageRcpt:
		return rcptRejectedCell(enhanced)
	case StageConnect:
		return cellGreetingRefused
	case StageAuth:
		return cellAuthRefused
	case StageMail:
		return cellMailRefused
	case StageData:
		return cellDataRefused
	case StageTLS, StageDone:
		return cellOutOfTable
	}
	return cellOutOfTable
}

// rcptRejectedCell — три формы 5xx на RCPT (УК17): `5.7.x` (политика), иной
// расширенный код, без расширенного кода. Исход один — RECIPIENT_REJECTED.
func rcptRejectedCell(enhanced string) Cell {
	if enhanced == "" {
		return cellRcptBare
	}
	if strings.HasPrefix(enhanced, "5.7.") {
		return cellRcptPolicy
	}
	return cellRcptEnhanced
}

// isClass — код ответа трёхзначен и его первая цифра — class.
func isClass(code, class int) bool {
	return code >= class*100 && code <= class*100+99
}
