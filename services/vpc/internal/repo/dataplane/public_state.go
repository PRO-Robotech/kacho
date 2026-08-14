// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dataplane

import (
	"context"
	"fmt"

	uc "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/dataplane"
)

// public_state.go — чтение публичной проекции подтверждения применения.
//
// # Почему одним запросом и по партии идентификаторов
//
// Стоимость страницы принадлежит запросу: список из пятидесяти ресурсов не
// вправе превращаться в пятьдесят обращений к базе. Обе таблицы соединяются
// один раз, по первичным ключам обеих.
//
// # Почему обе величины читаются ОДНИМ стейтментом
//
// Ответ выводится СРАВНЕНИЕМ ревизии намерения с ревизией подтверждения.
// Прочитав их порознь, мы получили бы пару, которой ни в один момент не
// существовало: арендатор правит ресурс между двумя чтениями — и «применено»
// сложилось бы из старого подтверждения и нового намерения либо наоборот.
// Соединение внутри одного стейтмента снимает обе величины из одного снимка
// строк, и рассогласоваться внутри него нечему.
//
// # Почему снятое намерение исключается
//
// Строка со снятым намерением — надгробие удалённого ресурса, и её ревизия
// принадлежит самому снятию, а не последнему живому состоянию. Публично о таком
// объекте сказать нечего, поэтому он в ответ не попадает вовсе: отсутствие
// ключа и есть честный ответ «данных нет», отличимый от «есть данные, и они
// нулевые».

// publicApplyStatesSQL — состояние по названным объектам.
//
// Ведущая таблица — намерение: без него сказать нечего в принципе, а
// подтверждение без намерения невыразимо (внешний ключ с каскадом, миграция
// 0032). Поэтому соединение внешнее слева, и отсутствие подтверждения приезжает
// как NULL, а не как отсутствующая строка.
const publicApplyStatesSQL = `
SELECT i.resource_id,
       i.revision,
       a.applied_revision,
       a.outcome,
       a.reason
  FROM kacho_vpc.dataplane_intent i
  LEFT JOIN kacho_vpc.dataplane_apply a ON a.resource_id = i.resource_id
 WHERE i.resource_id = ANY($1)
   AND NOT i.withdrawn`

// PublicApplyStates возвращает состояние применения по названным объектам.
//
// Объект, о котором сказать нечего (намерения нет либо оно снято), в карте
// отсутствует. Строка, не сводимая к состоянию, — ОТКАЗ всего вызова, а не
// пропуск одной записи: пропустив её, мы отдали бы арендатору страницу, о
// полноте которой нечего сказать, и расхождение словаря базы с контрактом
// осталось бы незамеченным ровно до того дня, когда оно кому-нибудь навредит.
func (s *Store) PublicApplyStates(ctx context.Context, resourceIDs []string) (map[string]uc.PublicApplyState, error) {
	if len(resourceIDs) == 0 {
		// Пустой список — не «все», а «ни одного». Обращение к базе здесь дало бы
		// тот же пустой ответ дороже.
		return map[string]uc.PublicApplyState{}, nil
	}

	rows, err := s.pool.Query(ctx, publicApplyStatesSQL, resourceIDs)
	if err != nil {
		return nil, fmt.Errorf("dataplane: состояние применения: %w", err)
	}
	defer rows.Close()

	out := make(map[string]uc.PublicApplyState, len(resourceIDs))
	for rows.Next() {
		var (
			id      string
			row     uc.ApplyReportRow
			applied *int64
			outcome *string
			reason  *string
		)
		if err := rows.Scan(&id, &row.IntentRevision, &applied, &outcome, &reason); err != nil {
			return nil, fmt.Errorf("dataplane: разбор состояния применения: %w", err)
		}
		// Подтверждения нет — все три колонки NULL разом (левое соединение не даёт
		// частично заполненной строки). Признак ставится ОДИН, по ведущей колонке:
		// вывести его из «все три пусты» значило бы завести второй способ сказать
		// то же самое, и они разошлись бы при первом же nullable-столбце.
		if applied != nil {
			row.Reported = true
			row.AppliedRevision = *applied
			if outcome != nil {
				row.Outcome = uc.ApplyOutcome(*outcome)
			}
			if reason != nil {
				row.Reason = uc.FailureReason(*reason)
			}
		}
		state, serr := row.PublicState()
		if serr != nil {
			return nil, fmt.Errorf("dataplane: состояние применения объекта %s: %w", id, serr)
		}
		out[id] = state
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dataplane: обход состояния применения: %w", err)
	}
	return out, nil
}
