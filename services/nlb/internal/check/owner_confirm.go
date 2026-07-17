// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
	"context"
	"errors"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	iamclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/iam"
)

// OwnerConfirmer — read-after-register проба owner-tuple для confirm-gate Create-op
// (owner-tuple opgate). После того как Create-worker закоммитил ресурс и эмитнул
// FGA-register-intent (fga_register_outbox), NOTIFY-driven register-drainer
// применяет его через kacho-iam InternalIAMService.RegisterResource, а iam-реконсайлер
// материализует owner-tuple в FGA. Worker прогоняет эту пробу, пока авторизационный
// резолв `subject #v_update <object>` (тот же, что gateway scope_extractor выполнит на
// немедленной мутации созданного ресурса) не начнёт возвращать ALLOW. Так закрывается
// окно 403 «no direct relations granted» между op.done(success) и первой мутацией
// создателя.
//
// Проба идёт по ТОМУ ЖЕ ребру `InternalIAMService.Check` (:9091, reuse
// iamclient.CheckClient), что и per-RPC authz-gate — один read-path, один store
// OpenFGA. Нового cross-service ребра не добавляется. Read-only и идемпотентна.
type OwnerConfirmer struct {
	cc         iamclient.CheckClient
	objectType string
}

// NewLoadBalancerOwnerConfirmer / NewListenerOwnerConfirmer / NewTargetGroupOwnerConfirmer —
// confirmer'ы для соответствующих nlb-ресурсов. objectType зафиксирован внутренними
// FGA-константами (lb_network_load_balancer / lb_listener / lb_target_group), поэтому
// caller не может передать несогласованный тип. cc — тот же iamclient.CheckClient,
// что обслуживает per-RPC authz-gate.
func NewLoadBalancerOwnerConfirmer(cc iamclient.CheckClient) *OwnerConfirmer {
	return &OwnerConfirmer{cc: cc, objectType: objectTypeLoadBalancer}
}

// NewListenerOwnerConfirmer — см. NewLoadBalancerOwnerConfirmer (object lb_listener).
func NewListenerOwnerConfirmer(cc iamclient.CheckClient) *OwnerConfirmer {
	return &OwnerConfirmer{cc: cc, objectType: objectTypeListener}
}

// NewTargetGroupOwnerConfirmer — см. NewLoadBalancerOwnerConfirmer (object lb_target_group).
func NewTargetGroupOwnerConfirmer(cc iamclient.CheckClient) *OwnerConfirmer {
	return &OwnerConfirmer{cc: cc, objectType: objectTypeTargetGroup}
}

// Confirm — read-after-register проба под контракт operations.ConfirmFunc.
// creator — принципал op'а (= создатель ресурса); resourceID — id только что
// созданного ресурса.
//
// Relation зафиксирован `v_update` — канонический mutate-relation. Под flat
// verb-bearing моделью доступ создателя на ресурс — per-object v_* tuple'ы
// (v_get/v_list/v_update/v_delete + tier), которые iam-реконсайлер материализует
// АТОМАРНО per-object (один OpenFGA Write — все tuple'и объекта либо видны, либо
// нет). Поэтому подтверждение `v_update` гарантирует, что материализация легла
// целиком (в т.ч. v_delete/v_get → немедленный Delete/Get создателя тоже не 403/404).
//
// Семантика возврата (под operations.ConfirmFunc — confirmed=true ⇒ MarkDone,
// иначе worker ретраит в пределах confirmation-deadline):
//   - allowed=true                                → confirmed=true;
//   - allowed=false / ErrNoPath / ErrHideExistence → confirmed=false, err=nil (pending);
//   - транспорт/Unavailable (прочий err)          → confirmed=false, err!=nil (transient).
func (c *OwnerConfirmer) Confirm(ctx context.Context, creator operations.Principal, resourceID string) (bool, error) {
	subject := authz.FormatSubject(creator.Type, creator.ID)
	object, err := authz.FormatObject(c.objectType, resourceID)
	if err != nil {
		// Вырожденный object (пустой id / reserved-char) — не транзиент и не
		// «pending»: owner-tuple для такого object не подтвердится никогда → err
		// (worker fail-closed по deadline, а не ложный success).
		return false, err
	}
	allowed, cerr := c.checkConsistent(ctx, subject, relationVUpdate, object)
	if cerr != nil {
		// ErrNoPath / ErrHideExistence — owner-tuple ещё не виден в FGA (нет пути к
		// объекту) → pending, НЕ transient-сбой: confirmed=false без err.
		if errors.Is(cerr, authz.ErrNoPath) || errors.Is(cerr, authz.ErrHideExistence) {
			return false, nil
		}
		return false, cerr
	}
	return allowed, nil
}

// consistentChecker — опциональная capability CheckClient'а: Check с
// HIGHER_CONSISTENCY (read-after-own-write). Реализуется `*checkClient`.
type consistentChecker interface {
	CheckConsistent(ctx context.Context, subjectID, relation, object string) (allowed bool, err error)
}

// checkConsistent прогоняет confirm-пробу с HIGHER_CONSISTENCY, когда клиент это
// умеет (owner-tuple материализуется в тот же store — read-after-register ОБЯЗАН
// быть consistent). Fallback на default Check — только когда client не реализует
// consistentChecker (напр. тестовый stub).
func (c *OwnerConfirmer) checkConsistent(ctx context.Context, subject, relation, object string) (bool, error) {
	if cc, ok := c.cc.(consistentChecker); ok {
		return cc.CheckConsistent(ctx, subject, relation, object)
	}
	return c.cc.Check(ctx, subject, relation, object)
}
