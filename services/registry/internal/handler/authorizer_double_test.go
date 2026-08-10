// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// manyFromOne — вторая дверь порта прав, выведенная ИЗ ПЕРВОЙ.
//
// Дублёр обязан выполнять контракт настоящего, а не свою копию: страница здесь
// сужается ТЕМ ЖЕ вопросом, что задаёт `Check`, — только заданным по одному. Это и
// есть предикат приёмной стороны, поэтому проба, зелёная на таком дублёре, зелена и
// на боевом пакетном пути.
//
// Чем это НЕ является: производственным запасным путём. В бою пакетная дверь
// обязательна, и падение на неё поштучно вернуло бы ровно ту стоимость, ради снятия
// которой она заведена (страница каталога — до тысячи запросов вместо десяти). Здесь
// вывод допустим потому, что предмет пробы — решение, а не цена.
type manyFromOne struct {
	one func(ctx context.Context, subject, relation, object string) (bool, error)
}

func (m manyFromOne) checkMany(
	ctx context.Context, subject, relation, objectType string, objectIDs []string,
) ([]string, error) {
	out := make([]string, 0, len(objectIDs))
	for _, id := range objectIDs {
		allowed, err := m.one(ctx, subject, relation, domain.FGAObjectRef(objectType, id))
		if err != nil {
			return nil, err // fail-closed: частично сужённая страница не отдаётся
		}
		if allowed {
			out = append(out, id)
		}
	}
	return out, nil
}
