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

	// MoveProject — каскад от LB.MoveProject: переписывает `project_id` у ВСЕХ
	// слушателей балансировщика. Зовётся из `LoadBalancerWriterIface.MoveProject`
	// внутри той же TX.
	//
	// Возвращает ПЕРЕЕХАВШИЕ ЗАПИСИ, а не их число, и это не удобство вызывающего.
	// Каскад меняет якорь проекта у чужого вида, а вид `nlb_listener` объявлен
	// несущим ПОЛНОЕ состояние: строку журнала на каждый слушатель собрать не из
	// чего, если каскад отдаёт одно лишь количество. Прежде он отдавал `int64` —
	// и переезд не объявлял сделанного ни одной строкой, из-за чего подписчик
	// держал слушателей в СТАРОМ проекте бессрочно (#1549).
	//
	// Записи берутся `RETURNING` того же UPDATE: состояние на момент СОБЫТИЯ, а не
	// на момент чтения, и без второго запроса.
	MoveProject(ctx context.Context, lbID, newProjectID string) ([]*ListenerRecord, error)

	// Delete — DELETE listeners WHERE id=$1.
	Delete(ctx context.Context, id string) error
}
