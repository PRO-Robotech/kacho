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
	// Ведомость переселения читается ТЕМ ЖЕ вопросом на страницу и в ТОЙ ЖЕ
	// транзакции: она ОБЪЯСНЯЕТ состояние, поэтому приехать из другого снимка не
	// вправе — иначе объяснение относилось бы к другому состоянию, чем счётчики
	// рядом. Отказ роняет чтение по тому же доводу, что и у счётчиков: «не смог
	// прочитать» не есть «отобранного нет».
	ids := make([]domain.RoleID, 0, len(roles))
	for _, r := range roles {
		ids = append(ids, r.ID)
	}
	withdrawn, err := rd.Roles().WithdrawnGrants(ctx, ids)
	if err != nil {
		return shared.MapRepoErr(err)
	}
	// Ведомость ВЫРЕЗАНИЯ читается тем же порядком и в той же транзакции. Она
	// про ТРЕТЬЮ проекцию правила, у которой глагола нет вовсе, — поэтому она
	// отдельный вопрос, а не ветвь соседнего: сложи мы их, вырезанное поехало бы
	// с пустым глаголом, а пустой глагол у соседа есть ЯКОРЬ объявления правила,
	// то есть уже занятое значение.
	//
	// Вопросов на страницу становится три, и это названо, а не умолчано: каждый
	// ограничен страницей, ни один не растёт с популяцией ролей.
	pruned, err := rd.Roles().PrunedSelectorTypes(ctx, ids)
	if err != nil {
		return shared.MapRepoErr(err)
	}

	for i := range roles {
		id := roles[i].ID
		roles[i].Integrity = domain.HealthOf(declared[id], unresolved[id])
		roles[i].Withdrawn = withdrawn[id]
		roles[i].PrunedSelectorTypes = pruned[id]
	}
	return nil
}
