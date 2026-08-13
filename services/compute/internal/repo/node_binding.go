// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// ErrHeldByAnotherNode — машину исполняет другой узел, и его аренда жива.
//
// Отдельная ошибка, а не общий отказ: вызывающий обязан отличить «занято» от
// «не найдено» и от «сломалось». Первое — штатный исход гонки, при котором
// проигравший узел просто ничего не делает; остальные требуют внимания.
var ErrHeldByAnotherNode = errors.New("instance is held by another node")

// NodeBinding — кто исполняет машину и до какого момента.
type NodeBinding struct {
	InstanceID   string
	NodeID       string
	ClaimedSeqNo int64
	LeaseUntil   time.Time
}

// ClaimInstance — атомарный обмен владением.
//
// # Почему это один стейтмент, а не «прочитал → решил → записал»
//
// Между чтением и записью помещается второй узел. Он прочитает ту же свободную
// привязку, примет то же решение и запишет следом — и оба будут считать, что
// машина их. Одна вставка с условием обмена этого не допускает by construction:
// строку держит блокировка, второй писатель ждёт коммита первого, видит его
// результат и не проходит условие.
//
// Свойство держит СТЕЙТМЕНТ, а не дисциплина вызывающего. Проверка в коде
// защищала бы ровно тот путь, который через неё проходит, — а восстановление
// оператором идёт мимо.
//
// # Три условия обмена, и каждое закрывает свой путь
//
//  1. привязки нет вовсе — машина свободна;
//  2. привязка НАША — повторный захват тем же узлом идемпотентен: агент,
//     перезапустившийся и не помнящий о себе, не должен терять свою же машину;
//  3. аренда истекла С ЗАПАСОМ — узел, не сумевший продлить, обязан был сам
//     остановить машину до истечения. Запас превышает срок самоостановки,
//     поэтому к моменту перехвата прежний узел уже отпустил том.
//
// Третье условие — единственное, где мы полагаемся на чужое поведение, и потому
// оно самое дорогое: если узел не остановился, ограждение остаётся единственной
// защитой. До его появления автоматический перенос не вводится.
func (r *InstanceRepo) ClaimInstance(ctx context.Context, b NodeBinding, graceAfterExpiry time.Duration) (*NodeBinding, error) {
	const q = `
		INSERT INTO instance_node_bindings
			(instance_id, node_id, claimed_seq_no, lease_until)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (instance_id) DO UPDATE
		   SET node_id        = EXCLUDED.node_id,
		       claimed_seq_no = EXCLUDED.claimed_seq_no,
		       lease_until    = EXCLUDED.lease_until,
		       renewed_at     = now(),
		       claimed_at     = CASE
		                          WHEN instance_node_bindings.node_id = EXCLUDED.node_id
		                          THEN instance_node_bindings.claimed_at
		                          ELSE now()
		                        END
		 WHERE instance_node_bindings.node_id = EXCLUDED.node_id
		    OR instance_node_bindings.lease_until < now() - $5::interval
		RETURNING instance_id, node_id, claimed_seq_no, lease_until`

	var got NodeBinding
	err := r.pool.QueryRow(ctx, q, b.InstanceID, b.NodeID, b.ClaimedSeqNo, b.LeaseUntil, graceAfterExpiry).
		Scan(&got.InstanceID, &got.NodeID, &got.ClaimedSeqNo, &got.LeaseUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		// Ноль строк из обмена означает ровно одно: условие не выполнено, то
		// есть машину держит другой узел и его аренда ещё жива. Это не сбой.
		return nil, ErrHeldByAnotherNode
	}
	if err != nil {
		return nil, wrapPgErr(err, "Instance", b.InstanceID)
	}
	return &got, nil
}

// ReleaseInstance — узел отпускает машину.
//
// Снимает привязку ТОЛЬКО свою: узел, отпускающий чужую машину, — это тот же
// перехват, только с другой стороны. Отсутствие строки не ошибка: отпустить
// то, чего не держишь, — законный исход повторной попытки.
func (r *InstanceRepo) ReleaseInstance(ctx context.Context, instanceID, nodeID string) error {
	const q = `DELETE FROM instance_node_bindings WHERE instance_id = $1 AND node_id = $2`
	_, err := r.pool.Exec(ctx, q, instanceID, nodeID)
	if err != nil {
		return wrapPgErr(err, "Instance", instanceID)
	}
	return nil
}

// BindingOf возвращает действующую привязку машины либо nil.
//
// Отдаётся вызывающему для внутренней проекции — на публичную идентификатор
// узла не выходит никогда.
func (r *InstanceRepo) BindingOf(ctx context.Context, instanceID string) (*NodeBinding, error) {
	const q = `SELECT instance_id, node_id, claimed_seq_no, lease_until
		         FROM instance_node_bindings WHERE instance_id = $1`
	var b NodeBinding
	err := r.pool.QueryRow(ctx, q, instanceID).
		Scan(&b.InstanceID, &b.NodeID, &b.ClaimedSeqNo, &b.LeaseUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, wrapPgErr(err, "Instance", instanceID)
	}
	return &b, nil
}

var _ = ports.ErrInternal
