// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package kacho

import (
	"context"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// ListenerReaderIface — read-операции Listener.
type ListenerReaderIface interface {
	Get(ctx context.Context, id string) (*ListenerRecord, error)
	List(ctx context.Context, f ListenerFilter, p Pagination) ([]*ListenerRecord, string, error)
	ListByLB(ctx context.Context, lbID string, p Pagination) ([]*ListenerRecord, string, error)
}

// ListenerWriterIface — write-операции + read.
type ListenerWriterIface interface {
	ListenerReaderIface

	// Insert — INSERT listeners RETURNING. UNIQUE-violation на
	// (load_balancer_id, port, protocol) WHERE status<>'DELETING' → ErrAlreadyExists.
	Insert(ctx context.Context, l *domain.Listener) (*ListenerRecord, error)

	// Update — UPDATE listeners SET mutable fields (name/description/labels/
	// default_target_group_id). Immutable lb_id/protocol/port обрабатываются в
	// use-case (rejected sync if в mask).
	// expectedXmin — OCC-snapshot (record.Xmin из Get); concurrent-modify → 0 rows
	// → ErrFailedPrecondition (защита от lost update на partial-mask Update).
	Update(ctx context.Context, l *domain.Listener, expectedXmin string) (*ListenerRecord, error)

	// SetStatusCAS — atomic CAS на status (CREATING → ACTIVE → DELETING).
	SetStatusCAS(ctx context.Context, id string, expected, newStatus domain.ListenerStatus) (*ListenerRecord, error)

	// MoveProject — каскад от LB.MoveProject; вызывается из
	// LoadBalancerWriterIface.MoveProject внутри той же TX.
	MoveProject(ctx context.Context, lbID, newProjectID string) (int64, error)

	// Delete — DELETE listeners WHERE id=$1.
	Delete(ctx context.Context, id string) error
}
