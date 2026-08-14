// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// ErrPlacementIncoherent — машину нельзя поставить в эту группу.
//
// # Почему исход ОДИН на четыре разные причины
//
// Группы нет · группа чужого проекта · группа зональная и зона другая · группа
// региональная и регион другой — четыре внутренних исхода, и вызывающий видит
// ОДИН и тот же ответ. Различимые ответы читались бы как справочник: по паре
// «нет группы» против «группа не той зоны» посторонний перечислил бы состав
// чужого проекта, задавая вопросы наугад.
//
// Это не потеря диагностики для законного вызывающего: он видит СВОИ группы
// списком и знает их якорь, поэтому ему достаточно знать, что связка
// невозможна.
var ErrPlacementIncoherent = fmt.Errorf(
	"%w: instance zone does not satisfy the placement group anchor", ports.ErrFailedPrecondition)

// checkPlacementCoherence проверяет, что машину можно поставить в названную
// группу, ВНУТРИ транзакции вставки/правки.
//
// # Почему регион приходит аргументом, а не читается здесь
//
// Связь «зона → её регион» принадлежит владельцу Geography, и выводить её
// разбором имени запрещено: имена зоны и региона произвольны, а строковая
// деривация молча возвращает пустоту на ресурсе без зоны и превращает проверку
// в тождественно-истинную. Регион резолвится у владельца на пути запроса и
// доезжает сюда уже авторитетным.
//
// # Почему это не «прочитал и сравнил»
//
// Условие стоит в WHERE самого чтения группы, и чтение идёт в ТОЙ ЖЕ
// транзакции, что вставка машины. Строка группы при этом заблокирована на
// чтение до конца транзакции, поэтому параллельное снятие группы не может
// проскользнуть между проверкой и вставкой: ссылочная целостность увидит
// живую строку.
func checkPlacementCoherence(ctx context.Context, tx pgx.Tx, in *domain.Instance) error {
	if in.PlacementGroupID == "" {
		return nil
	}

	// FOR SHARE — не оформление: без него группу можно снять между этой проверкой
	// и вставкой машины, и когерентность оказалась бы проверенной против строки,
	// которой больше нет.
	const q = `
		SELECT 1
		  FROM placement_groups
		 WHERE id = $1
		   AND project_id = $2
		   AND (
		         (placement_type = 'ZONAL'    AND zone_id   = $3)
		      OR (placement_type = 'REGIONAL' AND region_id = $4)
		       )
		   FOR SHARE`

	var ok int
	err := tx.QueryRow(ctx, q, in.PlacementGroupID, in.ProjectID, in.ZoneID, in.RegionID).Scan(&ok)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPlacementIncoherent
		}
		return wrapPgErr(err, "PlacementGroup", in.PlacementGroupID)
	}
	return nil
}
