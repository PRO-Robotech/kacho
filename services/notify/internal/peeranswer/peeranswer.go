// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package peeranswer — правило чужого ответа notify (З23 замысла NTF-1).
//
// Ответ чужой службы классифицируется ТИПОМ, без ветки «прочее» (CX1-07 (б)):
//
//   - «ответ получен» — код OK; что в ответе, решает читатель ответа;
//   - «недоступен» — ответа нет: UNAVAILABLE, DEADLINE_EXCEEDED и отмена вызова
//     своим контекстом (CANCELLED);
//   - «отказ» — любой иной код, ошибка без статуса gRPC и ответ, в котором
//     вариант не задан либо значение вне известного notify перечня
//     ([Malformed]; УК69, заказ NTF-2 Е8 (б)).
//
// «Отказ» — неисправность настройки (таблица SAN kaname не согласна с
// сертификатом notify, у notify нет права, расхождение версий контракта), а не
// решение о письме: читатель откладывает строку и выставляет сигнал
// misconfigured ([Signals]). У «недоступен» сигнала нет: это состояние
// соседа, а не настройки.
//
// Пакет общий для читателей чужих ответов notify: читатель справочника
// адресов kaname (NTF-3) строится на этом же классификаторе.
package peeranswer

import (
	"context"
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Genus — род ответа чужой службы. Нулевое значение родом не является:
// [Classify] и [Malformed] его не производят.
type Genus uint8

// Роды ответа.
const (
	_ Genus = iota // нулевое значение родом не является
	// Answered — ответ получен (код OK).
	Answered
	// Unavailable — ответа нет.
	Unavailable
	// Refused — отказ: иной код, ошибка без статуса либо ответ без решения.
	Refused
)

// Answer — классифицированный ответ: род и код gRPC (код вне контракта
// gRPC приведён к UNKNOWN).
type Answer struct {
	genus Genus
	code  codes.Code
}

// Genus — род ответа.
func (a Answer) Genus() Genus { return a.genus }

// Code — код gRPC ответа; имя кода — [CodeLabel].
func (a Answer) Code() codes.Code { return a.code }

// row — строка таблицы кодов: каноническое имя и род.
type row struct {
	label string
	genus Genus
}

// table — закрытый перечень кодов контракта gRPC, каждый своей строкой.
// Имена — канонические имена кодов контракта (google.rpc.Code), а не
// [codes.Code.String] (у того «Canceled» вместо «CANCELLED»): так их пишет
// контракт ошибок и так их видит оператор на панели.
var table = [...]row{
	codes.OK:                 {"OK", Answered},
	codes.Canceled:           {"CANCELLED", Unavailable},
	codes.Unknown:            {"UNKNOWN", Refused},
	codes.InvalidArgument:    {"INVALID_ARGUMENT", Refused},
	codes.DeadlineExceeded:   {"DEADLINE_EXCEEDED", Unavailable},
	codes.NotFound:           {"NOT_FOUND", Refused},
	codes.AlreadyExists:      {"ALREADY_EXISTS", Refused},
	codes.PermissionDenied:   {"PERMISSION_DENIED", Refused},
	codes.ResourceExhausted:  {"RESOURCE_EXHAUSTED", Refused},
	codes.FailedPrecondition: {"FAILED_PRECONDITION", Refused},
	codes.Aborted:            {"ABORTED", Refused},
	codes.OutOfRange:         {"OUT_OF_RANGE", Refused},
	codes.Unimplemented:      {"UNIMPLEMENTED", Refused},
	codes.Internal:           {"INTERNAL", Refused},
	codes.Unavailable:        {"UNAVAILABLE", Unavailable},
	codes.DataLoss:           {"DATA_LOSS", Refused},
	codes.Unauthenticated:    {"UNAUTHENTICATED", Refused},
}

// inContract — код входит в контракт gRPC (строка таблицы есть).
func inContract(c codes.Code) bool { return int(c) < len(table) }

// Codes — закрытый перечень кодов контракта gRPC в порядке номеров.
func Codes() []codes.Code {
	out := make([]codes.Code, len(table))
	for i := range table {
		out[i] = codes.Code(i)
	}
	return out
}

// CodeLabel — каноническое имя кода. Код вне контракта gRPC — «UNKNOWN»:
// числу с провода, которого контракт не знает, своего имени нет.
func CodeLabel(c codes.Code) string {
	if !inContract(c) {
		return table[codes.Unknown].label
	}
	return table[c].label
}

// GenusOf — род кода по таблице. Код вне контракта gRPC — «отказ».
func GenusOf(c codes.Code) Genus {
	if !inContract(c) {
		return Refused
	}
	return table[c].genus
}

// Classify — род ответа по ошибке вызова.
//
//   - nil — ответ получен;
//   - истечение и отмена своего контекста (в том числе обёрнутые) — ответа нет;
//   - статус gRPC — по таблице кодов; код вне контракта — отказ UNKNOWN;
//   - ошибка без статуса — отказ UNKNOWN. Клиент gRPC такой ошибки не
//     производит, значит её произвёл переходник notify: это дефект, и он
//     обязан звучать сигналом, а не тихо ждать под видом недоступности.
func Classify(err error) Answer {
	if err == nil {
		return Answer{genus: Answered, code: codes.OK}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Answer{genus: Unavailable, code: codes.DeadlineExceeded}
	}
	if errors.Is(err, context.Canceled) {
		return Answer{genus: Unavailable, code: codes.Canceled}
	}
	st, ok := status.FromError(err)
	if !ok || !inContract(st.Code()) {
		return Answer{genus: Refused, code: codes.Unknown}
	}
	return Answer{genus: GenusOf(st.Code()), code: st.Code()}
}

// Malformed — ответ получен, но решения в нём нет: вариант не задан либо его
// значение notify не знает. Род «отказ», код OK — ответ пришёл без ошибки.
func Malformed() Answer { return Answer{genus: Refused, code: codes.OK} }

// Cause — причина сигнала misconfigured: какой вызов и каким родом отказа.
// Закрытый перечень — [Causes]; читатель, заводящий новый вызов, дописывает
// свою причину сюда.
type Cause string

// Причины сигнала misconfigured.
const (
	// CauseResolveSendRefused — ResolveSend ответил кодом рода «отказ».
	CauseResolveSendRefused Cause = "resolve_send_refused"
	// CauseResolveSendProtocol — ResolveSend ответил без решения (вариант не
	// задан либо вне перечня).
	CauseResolveSendProtocol Cause = "resolve_send_protocol"
)

// Causes — закрытый перечень причин сигнала misconfigured.
func Causes() []Cause { return []Cause{CauseResolveSendRefused, CauseResolveSendProtocol} }

// Signals — счётчик неисправности настройки notify_misconfigured_total
// {source, cause} (З27). Семейство одно на процесс: его регистрирует
// [NewSignals], читатели чужих ответов получают его готовым.
type Signals struct {
	vec *prometheus.CounterVec
}

// NewSignals регистрирует семейство в reg.
func NewSignals(reg prometheus.Registerer) (*Signals, error) {
	if reg == nil {
		return nil, errors.New("peeranswer: реестр метрик не задан")
	}
	vec := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "notify_misconfigured_total",
		Help: "Неисправность настройки: чужая служба отказала либо ответила без решения; ноль за всю жизнь — норма.",
	}, []string{"source", "cause"})
	if err := reg.Register(vec); err != nil {
		return nil, fmt.Errorf("peeranswer: регистрация notify_misconfigured_total: %w", err)
	}
	return &Signals{vec: vec}, nil
}

// Declare заводит нулём серии источника по каждой причине: «ноль за всю
// жизнь» отличим от «серии нет», и тревога видит клетку до первого сигнала.
func (s *Signals) Declare(source string) {
	for _, c := range Causes() {
		s.vec.WithLabelValues(source, string(c))
	}
}

// Misconfigured — сигнал неисправности настройки: +1 в клетке (source, cause).
func (s *Signals) Misconfigured(source string, c Cause) {
	s.vec.WithLabelValues(source, string(c)).Inc()
}
