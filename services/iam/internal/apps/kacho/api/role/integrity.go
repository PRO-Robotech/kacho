// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package role

// integrity.go — целость роли заполняется ОДНИМ производителем на оба чтения.
//
// `Get` и `List` зовут `attachIntegrity` и отличаются только длиной входа: одна
// роль против страницы. Расхождение между поверхностями поэтому НЕПРЕДСТАВИМО,
// а не запрещено правилом — второй производитель был бы расхождением уже тем,
// что существует, даже пока совпадает.

import (
	"context"

	"github.com/PRO-Robotech/kacho/services/iam/internal/apps/kacho/shared"
	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/iam/internal/repo/kacho"
)

// attachIntegrity проставляет ролям состояние целости, читая проекцию В ТОЙ ЖЕ
// транзакции, в которой прочитаны сами роли: иначе объявленная сторона и
// спроецированная приехали бы из разных снимков и могли бы не сойтись.
//
// # Отказ роняет чтение, и это не «строгость», а единственный честный исход
//
// «Не смог посчитать» не есть «здорова». Молчаливая подстановка здорового
// состояния при недоступной проекции была бы fail-open ровно на признаке, ради
// которого признак заведён, — поэтому ошибка возвращается вызывающему, а не
// логируется. В дереве есть оба прецедента, и взят второй: у одного соседа
// такой помощник глотает отказ предупреждением, у другого возвращает ошибку.
//
// # Пустой странице вопрос не задаётся ВОВСЕ
//
// Спрашивать не о чем — значит не спрашивать: иначе каждая страница системного
// каталога платила бы за вопрос, ответ на который известен заранее.
func attachIntegrity(ctx context.Context, rd kachorepo.Reader, roles []domain.Role) error {
	if len(roles) == 0 {
		return nil
	}
	declared := make(map[domain.RoleID]int, len(roles))
	segments := make([]domain.RoleSegment, 0, len(roles))
	for _, r := range roles {
		segs := domain.SegmentsOf(r.ID, r.Rules)
		declared[r.ID] = len(segs)
		segments = append(segments, segs...)
	}

	unresolved := map[domain.RoleID]int{}
	if len(segments) > 0 {
		got, err := rd.Roles().UnresolvedSegments(ctx, segments)
		if err != nil {
			return shared.MapRepoErr(err)
		}
		unresolved = got
	}
	for i := range roles {
		id := roles[i].ID
		roles[i].Integrity = domain.HealthOf(declared[id], unresolved[id])
	}
	return nil
}
