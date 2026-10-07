// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package notice

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/PRO-Robotech/corelib/authz"
	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/operations"
)

// Caller — принципал вызывающего, извлечённый trust-aware парой носителя
// (З17): операции notify пишутся только с ним. Его отсутствие —
// UNAUTHENTICATED, а не запасной системный принципал.
func Caller(ctx context.Context) (operations.Principal, error) {
	p, forwarded := operations.PrincipalFromContextOK(ctx)
	if !forwarded || p.IsAnonymous() {
		return operations.Principal{}, status.Error(codes.Unauthenticated, "caller principal required")
	}
	return p, nil
}

// Subject — написание принципала в `createdBy` (`user:<id>`).
func Subject(p operations.Principal) string { return authz.FormatSubject(p.Type, p.ID) }

// NewOperation — операция notify: id с приставкой `nop` каталога операций
// фундамента (Х1, З17), моменты — часы notify, владелец — вызывающий.
func NewOperation(description string, metadata proto.Message, noticeID string, now time.Time,
	p operations.Principal) (operations.Operation, error) {
	meta, err := anypb.New(metadata)
	if err != nil {
		return operations.Operation{}, fmt.Errorf("operation metadata: %w", err)
	}
	return operations.Operation{
		ID:          ids.NewID(ids.PrefixOperationNotify),
		Description: description,
		CreatedAt:   now,
		CreatedBy:   p.ID,
		ModifiedAt:  now,
		Metadata:    meta,
		ResourceID:  noticeID,
		Principal:   p,
	}, nil
}

// DoneFinisher — Finisher перехода: операция, рождённая завершённой (Ф2), с
// ответом — внутренней проекцией извещения после записи (Р5).
func DoneFinisher(description string, metadata proto.Message, now time.Time, p operations.Principal) Finisher {
	return func(n Notice) (operations.Operation, *anypb.Any, error) {
		op, err := NewOperation(description, metadata, n.ID, now, p)
		if err != nil {
			return operations.Operation{}, nil, err
		}
		resp, err := anypb.New(Internal(n))
		if err != nil {
			return operations.Operation{}, nil, fmt.Errorf("operation response: %w", err)
		}
		op.Done = true
		op.Response = resp
		return op, resp, nil
	}
}
