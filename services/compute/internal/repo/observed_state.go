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

// ErrUnknownDelivery — отчёт о намерении, которого мы не отправляли.
//
// Отдельная ошибка, а не общий отказ: устаревший отчёт и отчёт о несуществующем
// событии требуют разного. Первый — штатный исход переупорядочения в сети, на
// который агенту нечего ответить; второй означает расхождение между тем, что мы
// отправили, и тем, что нам сообщают, и молчаливо принять его нельзя.
var ErrUnknownDelivery = errors.New("compute: report references a delivery we never emitted")

// ObservedReport — один отчёт узла об одной машине.
type ObservedReport struct {
	InstanceID string
	State      string
	SequenceNo int64
	ObservedAt time.Time
	Reason     string
}

// ApplyObservedState записывает наблюдаемое узлом состояние машины.
//
// # Один стейтмент, а не «прочитал → сравнил → записал»
//
// Между чтением и записью помещается второй отчёт — от того же узла после
// перезапуска либо от узла, перехватившего машину. Оба прочитают одну и ту же
// свежесть, оба примут одно решение, и запишет последний, а не свежайший.
// Условие свежести стоит в самом обмене, поэтому проигравший отчёт не находит
// строки и отбрасывается — независимо от того, кто его прислал.
//
// # Три исхода, и они разные
//
//  1. применён — номер события строго больше действующего;
//  2. отброшен как устаревший — номер не больше действующего. Это НЕ отказ:
//     агент не обязан знать, что его обогнал более свежий отчёт;
//  3. отвергнут как неизвестный — номер больше всего, что мы вообще отправляли
//     про эту машину. Такого намерения не было, и принять отчёт о нём значило бы
//     записать наблюдение о событии, которого не происходило.
//
// Признак «неизвестного» взят как «больше максимума отправленного», а НЕ как
// «нет строки с таким номером»: поток событий когда-нибудь начнут подрезать по
// сроку, и тогда второй признак начал бы отвергать законные поздние отчёты о
// подрезанных событиях. Максимум монотонен и подрезкой не понижается — номер
// больше него означает ровно то, что заявлено: мы этого не отправляли.
func (r *InstanceRepo) ApplyObservedState(ctx context.Context, rep ObservedReport) (bool, int64, error) {
	const q = `
WITH target AS (
    SELECT id, COALESCE(observed_sequence_no, 0) AS current_seq
      FROM instances
     WHERE id = $1
), emitted AS (
    SELECT COALESCE(max(sequence_no), 0) AS max_seq
      FROM compute_outbox
     WHERE resource_id = $1
), applied AS (
    UPDATE instances
       SET observed_state       = $2,
           observed_sequence_no = $3,
           observed_at          = $4,
           observed_reason      = $5
     WHERE id = $1
       AND $3 > COALESCE(observed_sequence_no, 0)
       AND $3 <= (SELECT max_seq FROM emitted)
    RETURNING observed_sequence_no
)
-- COALESCE обязателен: машины может не быть вовсе, и тогда подзапрос отдаёт
-- пустоту, а не ноль. Без него разбор ответа падал бы на несуществующей машине
-- отказом хранилища — то есть отвечал бы «сломалось» на «нет такой».
SELECT COALESCE((SELECT current_seq FROM target), 0)         AS current_seq,
       (SELECT max_seq FROM emitted)                         AS max_seq,
       EXISTS (SELECT 1 FROM applied)                        AS applied,
       EXISTS (SELECT 1 FROM target)                         AS found`

	var currentSeq, maxSeq int64
	var applied, found bool
	if err := r.pool.QueryRow(ctx, q,
		rep.InstanceID, rep.State, rep.SequenceNo, rep.ObservedAt, rep.Reason,
	).Scan(&currentSeq, &maxSeq, &applied, &found); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, 0, ports.ErrNotFound
		}
		return false, 0, wrapPgErr(err, "Instance", rep.InstanceID)
	}
	if !found {
		return false, 0, ports.ErrNotFound
	}
	if applied {
		return true, rep.SequenceNo, nil
	}
	// Не применён — по одной из двух причин, и они не взаимозаменяемы.
	if rep.SequenceNo > maxSeq {
		return false, currentSeq, ErrUnknownDelivery
	}
	return false, currentSeq, nil
}
