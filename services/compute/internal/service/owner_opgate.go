// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package service

import (
	"context"
	"log/slog"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/compute/internal/fgaintent"
)

// owner-tuple op-gating (P4) — общий для Instance/Disk Create хелпер. Гарантия
// (acceptance owner-tuple-opgate): Create-Operation достигает
// `done=true,result=response` ТОЛЬКО после read-after-register confirm owner-tuple
// в FGA; клиент, дождавшийся success-done, может НЕМЕДЛЕННО мутировать
// (`Update`/`Delete`) созданный ресурс без окна 403 «no direct relations granted».
// Fail-closed: confirm не достигнут за confirmation-deadline → op.error(Unavailable)
// (pkg/operations.RunWithConfirm; сам worker-механизм — фундамент P1).

// ownerConfirmRelation — FGA-relation, которым confirm-проба реплицирует gateway
// scope_extractor немедленной мутации. Gateway на `Update`/`Delete` резолвит
// `v_update`/`v_delete` на `compute_<kind>:<id>` (internal/check/permission_map.go).
// owner-tuple `project:<proj> #project @compute_<kind>:<id>` — единый parent-pointer,
// от которого КАСКАДИТ вся связка verb-relation'ов (`v_get`/`v_update`/`v_delete`…):
// он либо зарегистрирован (резолвятся ВСЕ), либо нет (не резолвится ни одна).
// Поэтому confirm `v_update` подтверждает эффективность owner-tuple и для Delete
// тоже — одна каноническая mutate-relation достаточна (D5). Строка — часть
// FGA-authorization-model контракта (совпадает с check.relationVUpdate).
const ownerConfirmRelation = "v_update"

// buildOwnerConfirm собирает operations.ConfirmFunc для свеже-создаваемого ресурса
// (kind ∈ {Instance,Disk}). Возвращает nil, когда opgate не сконфигурирован
// (confirmer==nil, dev/breakglass без authzConn) либо kind неизвестен — nil confirm
// эквивалентен сегодняшнему поведению (fn success → сразу MarkDone, back-compat).
//
// Subject фиксируется В МОМЕНТ Create из caller-ctx (auth-interceptor уже положил
// Principal), чтобы confirm-проба несла ТОГО ЖЕ subject'а, что gateway на немедленной
// мутации (FIX-2 consistency на уровне subject): `authz.FormatSubject(type,id)` даёт
// байт-в-байт тот же FGA-subject, что gateway defaultSubjectExtractor. Worker зовёт
// замыкание уже на своём (detached) ctx — subject захвачен строкой, не ctx.
func buildOwnerConfirm(ctx context.Context, confirmer OwnerConfirmer, kind, resourceID string) operations.ConfirmFunc {
	if confirmer == nil {
		return nil
	}
	objType := fgaintent.FGAType(kind)
	if objType == "" || resourceID == "" {
		return nil
	}
	object := objType + ":" + resourceID
	p := operations.PrincipalFromContext(ctx)
	subject := authz.FormatSubject(p.Type, p.ID)
	return func(cctx context.Context) (bool, error) {
		// HIGHER_CONSISTENCY: read-after-OWN-write — owner-tuple записан синхронно в
		// тот же store, default-read под multi-replica мог бы вернуть stale-negative
		// с другой реплики (хвост confirm-op, Koren-1).
		allowed, err := confirmer.CheckConsistent(cctx, subject, ownerConfirmRelation, object)
		if err != nil {
			// transient (peer Unavailable / probe error) → worker ретраит в пределах
			// confirmation-deadline; не считаем confirmed.
			return false, err
		}
		return allowed, nil
	}
}

// syncRegisterOwner синхронно (post-commit, best-effort) регистрирует owner-tuple
// свеже-созданного ресурса через registrar, чтобы confirm-gate happy-path'а
// резолвился НЕМЕДЛЕННО, не дожидаясь poll'а async register-drainer'а. Ошибка НЕ
// проваливает Create: durable outbox-intent (эмитится в writer-tx repo.Insert) +
// register-drainer остаются at-least-once backstop'ом (та же идемпотентная
// регистрация повторно безопасна, OTG-06/-07). registrar==nil → no-op (полагаемся
// на drainer).
func syncRegisterOwner(ctx context.Context, registrar OwnerRegistrar, kind, resourceID, projectID string, labels map[string]string) {
	if registrar == nil {
		return
	}
	if err := registrar.Register(ctx, kind, resourceID, projectID, labels); err != nil {
		slog.WarnContext(ctx, "owner-tuple sync register failed; register-drainer will backstop at-least-once",
			"err", err, "kind", kind, "resource", resourceID)
	}
}
