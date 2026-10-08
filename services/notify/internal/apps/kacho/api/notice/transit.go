// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package notice

import (
	"context"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	operationv1 "github.com/PRO-Robotech/corelib/api/corelib/operation"
	"github.com/PRO-Robotech/corelib/operations"
	"github.com/PRO-Robotech/corelib/operations/operationspb"

	"github.com/PRO-Robotech/kacho/services/notify/internal/notice/rules"
)

// Transit — общий ход глаголов Start, Complete, Cancel (Р5, З5 п.1–3): форма
// id первым оператором, принципал trust-aware пары, одна транзакция записи с
// условием на состояние и вид, операция done=true в ней же. Отказ перехода —
// синхронный статус, операция при отказе не создаётся.
func Transit(ctx context.Context, store Store, now Clock, verb rules.Verb, id, description string,
	metadata proto.Message) (*operationv1.Operation, error) {
	if err := ValidateID(id); err != nil {
		return nil, err
	}
	p, err := Caller(ctx)
	if err != nil {
		return nil, err
	}
	t := rules.TransitionOf(verb)
	at := Seconds(now())
	fin, done := Capture(DoneFinisher(description, metadata, at, p))
	if _, err := store.Transit(ctx, TransitInput{ID: id, Transition: t, Now: at}, p, fin); err != nil {
		return nil, Refusal(id, verb, t, err)
	}
	return operationspb.ToProto(done()), nil
}

// Capture оборачивает Finisher так, чтобы use-case получил записанную
// хранилищем операцию: ответ вызова — ровно та строка, что легла в транзакцию.
func Capture(f Finisher) (Finisher, func() *operations.Operation) {
	var op operations.Operation
	return func(n Notice) (operations.Operation, *anypb.Any, error) {
			o, resp, err := f(n)
			op = o
			return o, resp, err
		}, func() *operations.Operation {
			return &op
		}
}
