// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package toproto

// role.go — Transfer domain.Role → *iamv1.Role.

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"

	"github.com/PRO-Robotech/kacho/pkg/safeconv"

	"github.com/PRO-Robotech/kacho/services/iam/internal/authzmap"
	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
	"github.com/PRO-Robotech/kacho/services/iam/internal/dto"
)

type roleObj struct{}

func (roleObj) toPb(r domain.Role) (*iamv1.Role, error) {
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
		AuthoredVerbs:  r.AuthoredVerbs(roleTypeVerbLookup),
		EffectiveVerbs: r.EffectiveVerbs(roleTypeVerbLookup),
		VerbNotes:      r.VerbNotes(roleTypeVerbLookup),
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
		// Что ВЫРЕЗАНО из отбора правил и почему (#1988). Отдельное поле, а не
		// ветвь соседнего: у отбора глагола нет вовсе, а пустой глагол у соседа
		// уже занят якорем объявления правила.
		PrunedSelectorTypes: prunedSelectorTypesToPb(r.PrunedSelectorTypes),
		// Permissions intentionally omitted (internal compiled; not on the public
		// API surface — R-7/F5). Read compiled perms via InternalIAMService.GetRoleCompiled.
	}, nil
}

func init() {
	dto.RegTransfer(dto.Fn2Face(roleObj{}.toPb))
}

// roleTypeVerbLookup — набор глаголов ТИПА, который адресует правило роли.
//
// Тот же источник, из которого читает материализация, поэтому превью и эмиссия не
// могут разойтись: пока превью держало собственный список, роль могла обещать не то,
// что исполняется, и сверяющего у этого списка не было ни одного во всём дереве.
//
// Правило, не резолвящееся ни в один известный тип (в том числе `*`-форма), берёт
// ВСЕ глаголы платформы: иначе такая роль показала бы пустой набор и выглядела бы
// ничего не дающей.
//
// ЗДЕСЬ СТОЯЛО ПЕРЕСЕЧЕНИЕ наборов всех типов, и это была подмена по совпадению:
// пока наборы типов совпадали, пересечение равнялось «всем глаголам». Пересечение
// объявлено СУЖАЮЩИМСЯ, поэтому снятие глагола у ОДНОГО типа укорачивало превью
// роли `*.*` — то есть роль-суперпользователь начинала обещать МЕНЬШЕ, чем даёт, от
// правки, к ней не относящейся, а превью объявлено ЧЕСТНЫМ показом. Наблюдалось при
// #1189: пересечение стало `[get list]`, и превью роли `admin` свелось к чтению.
// Держит `role_superuser_preview_test.go`.
var roleTypeVerbLookup = domain.WithCommonFallback(
	func(module, resource string) ([]string, bool) {
		fgaType, ok := authzmap.ObjectType(module, resource)
		if !ok {
			return nil, false
		}
		verbs := authzmap.VerbsOfType(fgaType)
		if len(verbs) == 0 {
			return nil, false
		}
		return verbs, true
	},
	authzmap.AllVerbVocabulary(),
)

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

// prunedSelectorTypesToPb — ведомость ВЫРЕЗАННОГО на провод.
//
// Пустой вход даёт nil, а не пустой срез: на проводе это одно и то же, и
// заводить второе написание одного факта незачем.
//
// Отметка усечена до СЕКУНД — тем же правилом, что и все прочие отметки
// контракта: микросекунды базы на провод не текут.
func prunedSelectorTypesToPb(in []domain.PrunedSelectorType) []*iamv1.PrunedSelectorType {
	if len(in) == 0 {
		return nil
	}
	out := make([]*iamv1.PrunedSelectorType, 0, len(in))
	for _, p := range in {
		out = append(out, &iamv1.PrunedSelectorType{
			ObjectType: p.ObjectType,
			Outcome:    selectorPruneOutcomeToPb(p.Outcome),
			Reason:     p.Reason,
			PrunedAt:   timestamppb.New(p.PrunedAt.Truncate(time.Second)),
		})
	}
	return out
}

// selectorPruneOutcomeToPb — исход строки отбора.
//
// Неизвестному доменному значению отвечает UNSPECIFIED тем же доводом, что и у
// соседа ниже: «не вычислено» — единственный честный перевод того, чего
// перевести нельзя. Прочитанную-но-непонятую строку сюда не доносит читатель:
// он отказывает раньше.
func selectorPruneOutcomeToPb(o domain.SelectorPruneOutcome) iamv1.SelectorPruneOutcome {
	switch o {
	case domain.SelectorPruneOutcomeShortened:
		return iamv1.SelectorPruneOutcome_SELECTOR_PRUNE_OUTCOME_SHORTENED
	case domain.SelectorPruneOutcomeDropped:
		return iamv1.SelectorPruneOutcome_SELECTOR_PRUNE_OUTCOME_DROPPED
	default:
		return iamv1.SelectorPruneOutcome_SELECTOR_PRUNE_OUTCOME_UNSPECIFIED
	}
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
