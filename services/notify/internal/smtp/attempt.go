// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package smtp — отправитель письма на ретранслятор и таблица классификатора его
// ответа (З26 замысла kacho#2915).
//
// Пакет делит работу на три части, и каждая судится отдельно:
//
//   - [Sender] ведёт ОДНУ сессию SMTP на письмо и возвращает [Attempt] — чем она
//     закончилась: стадия, код ответа, расширенный код, отказ без кода. Решения об
//     исходе строки отправитель не принимает;
//   - [Classify] — таблица: [Attempt] → [Cell], клетка Р11 с исходом из словаря
//     фундамента (`feed.Outcome`), сигналом `misconfigured` и вкладом в размыкатель.
//     Ветки «прочее» нет: ответ вне таблицы — своя строка «код вне таблицы» (УК17);
//   - [Breaker] — размыкатель: после [BreakerThreshold] недоступностей подряд
//     notify не забирает строки [BreakerCooldown] (NTF1-G11).
//
// TLS обязателен: STARTTLS (`smtp`) либо неявный TLS (`smtps`), сертификат
// проверяется по доверенному набору [Relay.Roots] с именем узла [Relay.Host]. Поля
// «не проверять сертификат» нет (NTF1-G07, G08).
package smtp

import "fmt"

// Stage — стадия сессии, на которой она закончилась.
type Stage int

const (
	// StageConnect — соединение и приветствие (в том числе `EHLO`).
	StageConnect Stage = iota
	// StageTLS — установление TLS: `STARTTLS` и рукопожатие либо неявный TLS.
	StageTLS
	// StageAuth — `AUTH PLAIN`.
	StageAuth
	// StageMail — `MAIL FROM`.
	StageMail
	// StageRcpt — `RCPT TO`.
	StageRcpt
	// StageData — `DATA`, тело и ответ на его конец.
	StageData
	// StageDone — письмо принято ретранслятором.
	StageDone
)

var stageNames = [...]string{
	StageConnect: "connect",
	StageTLS:     "tls",
	StageAuth:    "auth",
	StageMail:    "mail",
	StageRcpt:    "rcpt",
	StageData:    "data",
	StageDone:    "done",
}

// String — имя стадии для журналов и сообщений проб.
func (s Stage) String() string {
	if s >= 0 && int(s) < len(stageNames) {
		return stageNames[s]
	}
	return fmt.Sprintf("stage(%d)", int(s))
}

// Failure — отказ сессии, у которого нет кода ответа ретранслятора.
type Failure int

const (
	// FailureNone — сессия закончилась ответом ретранслятора (Attempt.Code).
	FailureNone Failure = iota
	// FailureUnreachable — соединение с узлом не установлено.
	FailureUnreachable
	// FailureNoTLS — узел не предложил STARTTLS либо не говорит TLS
	// по адресу `smtps`: письмо открытым текстом не уходит (NTF1-G07).
	FailureNoTLS
	// FailureUntrusted — сертификат узла не проверен доверенным набором (NTF1-G08).
	FailureUntrusted
	// FailureDisconnect — соединение оборвано (или истёк срок сессии) до ответа.
	FailureDisconnect
)

var failureNames = [...]string{
	FailureNone:        "none",
	FailureUnreachable: "unreachable",
	FailureNoTLS:       "no_tls",
	FailureUntrusted:   "untrusted",
	FailureDisconnect:  "disconnect",
}

// String — имя отказа для журналов и сообщений проб.
func (f Failure) String() string {
	if f >= 0 && int(f) < len(failureNames) {
		return failureNames[f]
	}
	return fmt.Sprintf("failure(%d)", int(f))
}

// Attempt — чем закончилась одна сессия. Текста ответа ретранслятора в нём нет:
// текст может повторять адрес получателя, а попытка уходит в журналы и метки.
type Attempt struct {
	Stage Stage
	// Code — трёхзначный код последнего ответа; 0 — ответа не было либо он не
	// разобран (тогда при FailureNone клетка — «код вне таблицы»).
	Code int
	// Enhanced — расширенный код RFC 3463 («5.1.1») из начала текста ответа-отказа;
	// "" — отказ его не несёт либо письмо принято (StageDone).
	Enhanced string
	Failure  Failure
}

// Envelope — адреса конверта: отправитель установки и ровно один адресат.
type Envelope struct {
	From string
	To   string
}
