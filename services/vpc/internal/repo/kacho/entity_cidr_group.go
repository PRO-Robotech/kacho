// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package kacho

import (
	"time"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

// CidrGroupRecord — repo-entity для CidrGroup: domain.CidrGroup плюс DB-managed
// CreatedAt и выведенный на чтении перечень потребителей.
//
// `UsedBy` держится ЗДЕСЬ, а не в domain, по той же причине, что и `CreatedAt`:
// это не свойство набора, а факт о других строках базы, собранный на чтении из
// проекции ссылок правил. В domain он завёл бы поле, которое нечем заполнить
// изнутри самого набора.
type CidrGroupRecord struct {
	domain.CidrGroup
	CreatedAt time.Time
	// UsedBy — группы правил, чьи правила ссылаются на этот набор. Output-only,
	// выводится на чтении; пусто, когда на набор никто не ссылается.
	UsedBy []CidrGroupReferrer
}

// CidrGroupReferrer — одна ссылка на набор со стороны группы правил.
//
// Имя группы — зеркало на момент чтения (best-effort): оно косметическое и может
// измениться, поэтому решение по нему не принимается — оно только показывается.
type CidrGroupReferrer struct {
	SecurityGroupID   string
	SecurityGroupName string
	// Rules — сколько правил ЭТОЙ группы ссылаются на набор. Число, а не перечень
	// идентификаторов правил: перечень чужих идентификаторов — координата, число
	// координатой не является.
	Rules int
}
