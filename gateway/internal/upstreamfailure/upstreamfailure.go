// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package upstreamfailure — причина отказа вызова края своему соседу, на
// приборе оператора (kacho#3221).
//
// # Зачем
//
// Вызывающий видит у трёх разных неисправностей один ответ: `503` на запрос
// домена, чей адрес не разрешается, `503` на запрос домена, чья служба
// остановлена, и `503` «состояние удостоверения не установлено», когда вопрос
// службе доступа не уложился в бюджет. Чинить при этом надо разное: адрес
// (`backends.<сосед>` профиля), службу, бюджет (`callBudgets.identity` /
// `callBudgets.backend`). Ответ наружу намеренно один — он не называет
// внутренних адресов и не служит оракулом исправности соседа; различает
// причины ПРИБОР: клетка `kacho_api_gateway_upstream_call_failures_total`
// по соседу и причине.
//
// # Причины — закрытый словарь
//
//   - `unresolved` — адрес соседа не разрешился в набор адресов: имени нет
//     (разрешатель отдал пустой набор) либо сбой самого запроса имени. Чинят
//     адрес или сервер имён, а не службу.
//   - `unavailable` — адрес разрешился, но звено не поднялось (слушателя нет,
//     соединение отвергнуто) либо служба сама ответила UNAVAILABLE. Это и есть
//     «служба лежит».
//   - `deadline` — вызов не уложился в свой срок: бюджет края либо более
//     ранний срок вызывающего. Чинят бюджет или медленного соседа.
//
// Ответы службы по существу (нет, запрещено, неверно, не аутентифицирован) и
// отмена вызывающим причиной отказа соседа не являются и не учитываются.
//
// # Как различается `unresolved`
//
// Признак на проводе у неразрешённого адреса и у остановленной службы один —
// код UNAVAILABLE. Различает их ТЕКСТ, и порождает его не служба, а
// балансировщик и разрешатель библиотеки gRPC в процессе края: пустой набор
// адресов — «no children to pick from» (round_robin) и «produced zero
// addresses» (pick_first, с префиксом «name resolver error: »), сбой запроса
// имени — «dns: … record lookup error: …» разрешателя dns. Перечень форм выведен
// из исходников библиотеки и держится пробой на НАСТОЯЩЕМ клиенте обеими
// политиками (upstreamfailure_test.go): подъём библиотеки, сменивший текст,
// краснит пробу, а не переводит причину в `unavailable` молча.
//
// Граница: служба, которая сама пересказывает тот же текст своего соседа, будет
// учтена как `unresolved` у края. Это пересказ внутреннего отказа наружу, то
// есть отдельная неисправность самой службы.
package upstreamfailure

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Cause — причина отказа вызова соседу.
type Cause string

// Закрытый словарь причин.
const (
	CauseUnresolved  Cause = "unresolved"
	CauseUnavailable Cause = "unavailable"
	CauseDeadline    Cause = "deadline"
)

// Causes — причины в порядке объявления; по ним заводятся клетки.
func Causes() []Cause { return []Cause{CauseUnresolved, CauseUnavailable, CauseDeadline} }

// unresolvedMarkers — тексты балансировщика и разрешателя библиотеки gRPC о
// том, что набор адресов соседа пуст либо не получен (см. шапку пакета).
var unresolvedMarkers = []string{
	"no children to pick from",
	"produced zero addresses",
	"name resolver error",
}

// unresolvedPrefix — префикс отказа разрешателя dns библиотеки.
const unresolvedPrefix = "dns: "

// Classify называет причину отказа вызова соседу. ok=false — отказ не о соседе
// (ответ службы по существу, отмена вызывающим) либо отказа нет.
func Classify(err error) (cause Cause, ok bool) {
	if err == nil {
		return "", false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return CauseDeadline, true
	}
	st := status.Convert(err)
	switch st.Code() {
	case codes.DeadlineExceeded:
		return CauseDeadline, true
	case codes.Unavailable:
		msg := st.Message()
		if strings.HasPrefix(msg, unresolvedPrefix) {
			return CauseUnresolved, true
		}
		for _, m := range unresolvedMarkers {
			if strings.Contains(msg, m) {
				return CauseUnresolved, true
			}
		}
		return CauseUnavailable, true
	default:
		return "", false
	}
}

// Cell — одна клетка учёта: сосед, причина, число отказов с запуска.
type Cell struct {
	Upstream string
	Cause    Cause
	Count    uint64
}

// Tally — учёт отказов по соседу и причине. Соседи объявляются при сборке, и
// словарь меток на этом закрыт: сосед, не объявленный при сборке, клеткой не
// становится (число серий не растёт от данных).
//
// Нулевой указатель — несобранный учёт: записи и снимок ничего не делают.
type Tally struct {
	order []string
	cells map[string]*[3]atomic.Uint64
}

// NewTally собирает учёт с клетками всех объявленных соседей нулями.
func NewTally(upstreams []string) *Tally {
	t := &Tally{cells: make(map[string]*[3]atomic.Uint64, len(upstreams))}
	for _, u := range upstreams {
		if _, dup := t.cells[u]; dup || u == "" {
			continue
		}
		t.order = append(t.order, u)
		t.cells[u] = new([3]atomic.Uint64)
	}
	return t
}

func causeIndex(c Cause) int {
	switch c {
	case CauseUnresolved:
		return 0
	case CauseUnavailable:
		return 1
	default:
		return 2
	}
}

// Record учитывает отказ вызова соседу upstream; ответы не о соседе и
// незаявленные соседи не учитываются.
func (t *Tally) Record(upstream string, err error) {
	if t == nil {
		return
	}
	cause, ok := Classify(err)
	if !ok {
		return
	}
	cell, declared := t.cells[upstream]
	if !declared {
		return
	}
	cell[causeIndex(cause)].Add(1)
}

// Snapshot — все клетки в порядке объявления соседей и причин.
func (t *Tally) Snapshot() []Cell {
	if t == nil {
		return nil
	}
	out := make([]Cell, 0, len(t.order)*len(Causes()))
	for _, u := range t.order {
		cell := t.cells[u]
		for _, c := range Causes() {
			out = append(out, Cell{Upstream: u, Cause: c, Count: cell[causeIndex(c)].Load()})
		}
	}
	return out
}

// UnaryClientInterceptor учитывает отказ унарного вызова под соседом upstream.
// Ставится на соединение к соседу при сборке; ответ вызывающему не меняет.
func (t *Tally) UnaryClientInterceptor(upstream string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		err := invoker(ctx, method, req, reply, cc, opts...)
		if err != nil {
			t.Record(upstream, err)
		}
		return err
	}
}

// StreamClientInterceptor учитывает отказ ОТКРЫТИЯ потока под соседом upstream:
// неразрешённый адрес и остановленная служба отказывают именно здесь.
// Исход уже открытого потока — ответ службы, и он здесь не учитывается.
func (t *Tally) StreamClientInterceptor(upstream string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string,
		streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		cs, err := streamer(ctx, desc, cc, method, opts...)
		if err != nil {
			t.Record(upstream, err)
		}
		return cs, err
	}
}

// DialOptions — опции соединения к соседу upstream, несущие учёт обоих видов
// вызова. Несобранный учёт опций не даёт.
func (t *Tally) DialOptions(upstream string) []grpc.DialOption {
	if t == nil {
		return nil
	}
	return []grpc.DialOption{
		grpc.WithChainUnaryInterceptor(t.UnaryClientInterceptor(upstream)),
		grpc.WithChainStreamInterceptor(t.StreamClientInterceptor(upstream)),
	}
}
