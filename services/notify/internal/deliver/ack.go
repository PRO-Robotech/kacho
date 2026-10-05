// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deliver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	notifyv1 "github.com/PRO-Robotech/corelib/api/corelib/notify"
	"github.com/PRO-Robotech/corelib/notify/feed"
)

// Пауза между повторами `Ack` на транзиентном отказе: удваивается до
// ackRetryMax. Повторы ограничены концом аренды, а не числом: повтор
// идемпотентен по З9, и после конца аренды запись исхода невозможна.
const (
	ackRetryFirst = 100 * time.Millisecond
	ackRetryMax   = time.Second
)

// ack записывает исход строки (SDR-Н1). Контекст — собственный:
// `WithDeadline(WithoutCancel(<строка>), t_send + lease_remaining)`, срок —
// длительностью от внедряемых часов до конца аренды. Крайний момент строки
// ограничивает путь до ответа на `DATA`; под ним `Ack` упал бы сразу и строку
// выдал бы следующий `Claim` — второе письмо при живом процессе.
//
// Повторяются только `UNAVAILABLE` и `DEADLINE_EXCEEDED` ВЫЗОВА (собственный
// срок не истёк); прочие отказы — запись в журнал без повтора.
//
// РЕПЛИКИ: клейм — повторяется `Ack` строки, чью аренду выдал этой реплике
// `Claim` (`FOR UPDATE SKIP LOCKED`, З8): другая реплика ту же строку до конца
// аренды не держит, а повтор той же пары с тем же токеном оператор `Ack`
// принимает без изменения строки (З9). Повтор после конца аренды невозможен —
// срок контекста и есть конец аренды.
func (w *Worker) ack(rowCtx context.Context, j *job, st Step) {
	req, err := ackRequest(j.row, st)
	if err != nil {
		w.log.Error("исход строки не выражается на проводе: Ack не послан",
			"source", j.rt.src.Module, "id", j.row.GetId(), "err", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(rowCtx), w.clock.Until(j.leaseEnd))
	defer cancel()
	pause := ackRetryFirst
	for calls := 1; ; calls++ {
		_, err := j.rt.feed.Ack(ctx, req)
		if err == nil {
			return
		}
		code := status.Code(err)
		transient := code == codes.Unavailable || code == codes.DeadlineExceeded
		if !transient || ctx.Err() != nil {
			w.log.Warn("Ack не записан", "source", j.rt.src.Module, "id", j.row.GetId(),
				"kind", string(st.outcome.Kind), "reason", string(st.outcome.Reason),
				"code", code.String(), "calls", calls, "lease_over", ctx.Err() != nil)
			return
		}
		t := time.NewTimer(pause)
		select {
		case <-ctx.Done():
			t.Stop()
			w.log.Warn("Ack не записан до конца аренды", "source", j.rt.src.Module, "id", j.row.GetId(),
				"code", code.String(), "calls", calls)
			return
		case <-t.C:
		}
		pause = min(2*pause, ackRetryMax)
	}
}

// ackRequest — `Ack` строки: исход в словаре провода и defer_for только у DEFER.
func ackRequest(row *notifyv1.ClaimedNotification, st Step) (*notifyv1.AckRequest, error) {
	o, err := wireOutcome(st.outcome)
	if err != nil {
		return nil, err
	}
	req := &notifyv1.AckRequest{Id: row.GetId(), LeaseToken: row.GetLeaseToken(), Outcome: o}
	if st.outcome.Kind == feed.KindDefer {
		req.DeferFor = durationpb.New(st.deferFor)
	}
	return req, nil
}

// wireOutcome — исход ленты в значение провода. Обратное преобразованию
// сервера ленты (слово контракта в нижнем регистре): вид и причина — имена
// перечислений в верхнем регистре; причины нет — UNSPECIFIED. Вид или
// причина вне перечня провода — ошибка, а не «неуказанный».
func wireOutcome(o feed.Outcome) (*notifyv1.Outcome, error) {
	kind, ok := notifyv1.OutcomeKind_value[strings.ToUpper(string(o.Kind))]
	if !ok || notifyv1.OutcomeKind(kind) == notifyv1.OutcomeKind_OUTCOME_UNSPECIFIED {
		return nil, fmt.Errorf("вид исхода %q вне перечня провода", o.Kind)
	}
	out := &notifyv1.Outcome{Kind: notifyv1.OutcomeKind(kind)}
	if o.Reason == feed.ReasonNone {
		return out, nil
	}
	reason, ok := notifyv1.OutcomeReason_value[strings.ToUpper(string(o.Reason))]
	if !ok || notifyv1.OutcomeReason(reason) == notifyv1.OutcomeReason_OUTCOME_REASON_UNSPECIFIED {
		return nil, fmt.Errorf("причина исхода %q вне перечня провода", o.Reason)
	}
	out.Reason = notifyv1.OutcomeReason(reason)
	return out, nil
}
