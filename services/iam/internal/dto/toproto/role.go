// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package toproto

// role.go — Transfer domain.Role → *iamv1.Role.

import (
	"fmt"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/pkg/safeconv"

	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
	"github.com/PRO-Robotech/kacho/services/iam/internal/dto"
)

type roleObj struct{}

func (roleObj) toPb(r domain.Role) (*iamv1.Role, error) {
	// Набор глаголов типа ОБЯЗАН быть провязан, и его отсутствие — отказ, а не
	// запасной путь (#1994).
	//
	// Тихий запасной путь и был снятым дефектом: превью, собранное словарём,
	// ПОРОЖДЁННЫМ СБОРКОЙ, выглядит как честное превью и расходится с эмиссией
	// ровно на типе, заведённом применением манифеста. Отказ виден сразу, называет
	// предмет и не может доехать до арендатора под видом ответа.
	if r.TypeVerbs == nil {
		return nil, fmt.Errorf("проекция роли %s: набор глаголов типа не провязан — "+
			"превью, собранное словарём сборки, разошлось бы с материализацией на типе, "+
			"заведённом применением манифеста (kacho#1994)", r.ID)
	}

	var createdAt *timestamppb.Timestamp
	if !r.CreatedAt.IsZero() {
		createdAt = timestamppb.New(r.CreatedAt.Truncate(tsTruncate))
	}
	var updatedAt *timestamppb.Timestamp
	if !r.UpdatedAt.IsZero() {
		updatedAt = timestamppb.New(r.UpdatedAt.Truncate(tsTruncate))
	}
	// RBAC rules-model 2026: rules[] is the PUBLIC API surface;
	// permissions[] is the INTERNAL compiled projection and is NOT populated in the
	// public Get/List response (left empty). For a legacy permissions-only role
	// (no rules) rules[] is empty and permissions still stays empty in the public
	// projection — clients render the role from rules[].
	rules := make([]*iamv1.Rule, 0, len(r.Rules))
	for _, rl := range r.Rules {
		rules = append(rules, &iamv1.Rule{
			Module:        rl.Module,
			Resources:     rl.Resources,
			Verbs:         rl.Verbs,
			ResourceNames: rl.ResourceNames,
			MatchLabels:   rl.MatchLabels,
		})
	}
	return &iamv1.Role{
		Id:          string(r.ID),
		AccountId:   string(r.AccountID),
		ProjectId:   string(r.ProjectID),
		ClusterId:   string(r.ClusterID),
		Name:        string(r.Name),
		Description: string(r.Description),
		Rules:       rules,
		// redesign-2026 F4: is_system is DERIVED from the definition tier
		// (tierType==iam.cluster ⇔ cluster_id set), not the stored flag.
		IsSystem: r.IsSystemDerived(),
		// redesign-2026 F4: definitionTier dotted projection over the typed scope
		// columns; the word "scope" is reserved for the AccessBinding anchor.
		DefinitionTier: &iamv1.DefinitionTier{
			TierType: r.DefinitionTierType(),
			TierId:   r.DefinitionTierID(),
		},
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
		CreatedByUserId: string(r.CreatedByUserID),
		Labels:          labelsToStringMap(r.Labels),
		// redesign-2026 F6: honest effective-verb preview + catalog metadata
		// (output-only derived; editor's effective set carries `delete*`).
		AuthoredVerbs:  r.AuthoredVerbs(r.TypeVerbs),
		EffectiveVerbs: r.EffectiveVerbs(r.TypeVerbs),
		VerbNotes:      r.VerbNotes(r.TypeVerbs),
		DisplayName:    r.DisplayName(),
		Purpose:        r.Purpose(),
		// Целость (#1035) — ТРИ величины вместе или ни одной. Считать здесь
		// `declared_segments` из `r.Rules` (они тут видны) в отрыве от остальных
		// двух запрещено: ответ операции получил бы числовой облик здоровья при
		// невычисленном состоянии, то есть `declared=2, unresolved=0` рядом с
		// UNSPECIFIED. Все три приходят с `domain.Role`, заполняет их ЧТЕНИЕ.
		Health:             roleHealthToPb(r.Integrity.Health),
		DeclaredSegments:   safeconv.ClampNonNegInt32(int64(r.Integrity.Declared)),
		UnresolvedSegments: safeconv.ClampNonNegInt32(int64(r.Integrity.Unresolved)),
		// Что отобрано и почему (#1992). ОБЪЯСНЯЕТ три величины выше и их не
		// определяет: у роли, пострадавшей вторым путём, переселения не было
		// вовсе, и список пуст при нездоровом состоянии.
		WithdrawnGrants: withdrawnGrantsToPb(r.Withdrawn),
		// Permissions intentionally omitted (internal compiled; not on the public
		// API surface — R-7/F5). Read compiled perms via InternalIAMService.GetRoleCompiled.
	}, nil
}

func init() {
	dto.RegTransfer(dto.Fn2Face(roleObj{}.toPb))
}

// ЗДЕСЬ СТОЯЛ `roleTypeVerbLookup` — резолв ПО СЛОВАРЮ, ПОРОЖДЁННОМУ СБОРКОЙ
//
// Он резолвил пару `(модуль, ресурс)` закрытой таблицей `authzmap.ObjectType`. На
// типе, заведённом применением манифеста в РАБОТАЮЩЕМ процессе, таблица отвечала
// «не знаю», и вызывающий брал запасной набор — глаголы ВСЕЙ платформы. Запасной
// набор заведён осознанно и для ДРУГОГО входа (форма `*.*`), но молча накрыл и
// второй, которого при его заведении не существовало (#1994).
//
// Сегодня набор приходит на самой роли (`domain.Role.TypeVerbs`), собранный из
// ЖИВЫХ строк каталога тем же фактом, которым идёт материализация
// (`catalog.Facts.RolePreviewLookup`), — поэтому превью и эмиссия не могут
// разойтись. Запасной набор для `*.*` не отнят, он переехал туда же и стал
// ОБЪЕДИНЕНИЕМ наборов живых типов.
//
// Этот пакет `authzmap` больше НЕ ИМПОРТИРУЕТ, и это не косметика: пока импорт
// есть, следующая правка достанет словарь сборки обратно двумя строками, и
// заметить это будет негде.

// roleHealthToPb — ПЕРЕВОД доменного состояния в контрактное, и только он.
// Решение о том, какое состояние несёт роль, принимает домен (`HealthOf`);
// здесь не судится ничего — иначе завелось бы второе место, знающее ответ.
//
// Неизвестному доменному значению отвечает UNSPECIFIED: «не вычислено» —
// единственный честный перевод того, чего перевести нельзя.
func roleHealthToPb(h domain.RoleHealth) iamv1.RoleHealth {
	switch h {
	case domain.RoleHealthHealthy:
		return iamv1.RoleHealth_ROLE_HEALTH_HEALTHY
	case domain.RoleHealthDegraded:
		return iamv1.RoleHealth_ROLE_HEALTH_DEGRADED
	case domain.RoleHealthEmpty:
		return iamv1.RoleHealth_ROLE_HEALTH_EMPTY
	default:
		return iamv1.RoleHealth_ROLE_HEALTH_UNSPECIFIED
	}
}

// withdrawnGrantsToPb переводит ведомость отобранного (#1992).
//
// Пустой вход даёт nil, а не пустой срез: на проводе это одно и то же, и
// заводить второе написание одного факта незачем.
//
// Отметка усечена до СЕКУНД — тем же правилом, что и все прочие отметки
// контракта: микросекунды базы на провод не текут.
func withdrawnGrantsToPb(in []domain.WithdrawnGrant) []*iamv1.WithdrawnGrant {
	if len(in) == 0 {
		return nil
	}
	out := make([]*iamv1.WithdrawnGrant, 0, len(in))
	for _, g := range in {
		out = append(out, &iamv1.WithdrawnGrant{
			ObjectType:  g.ObjectType,
			Verb:        g.Verb,
			Source:      withdrawnGrantSourceToPb(g.Source),
			Reason:      g.Reason,
			WithdrawnAt: timestamppb.New(g.WithdrawnAt.Truncate(time.Second)),
		})
	}
	return out
}

// withdrawnGrantSourceToPb — популяция ведомости.
//
// Неизвестному доменному значению отвечает UNSPECIFIED тем же доводом, что и у
// состояния целости: «не вычислено» — единственный честный перевод того, чего
// перевести нельзя. Прочитанную-но-непонятую строку сюда не доносит читатель:
// он отказывает раньше.
func withdrawnGrantSourceToPb(s domain.WithdrawnGrantSource) iamv1.WithdrawnGrantSource {
	switch s {
	case domain.WithdrawnGrantSourceGrant:
		return iamv1.WithdrawnGrantSource_WITHDRAWN_GRANT_SOURCE_GRANT
	case domain.WithdrawnGrantSourceRuleRef:
		return iamv1.WithdrawnGrantSource_WITHDRAWN_GRANT_SOURCE_RULE_REFERENCE
	default:
		return iamv1.WithdrawnGrantSource_WITHDRAWN_GRANT_SOURCE_UNSPECIFIED
	}
}
