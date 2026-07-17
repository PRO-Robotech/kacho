// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
	"context"
	"errors"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// OwnerConfirmer — read-after-register проба owner-tuple для confirm-gate Create-op
// (owner-tuple opgate). После того как Create-worker закоммитил ресурс и
// sync-registrar зарегистрировал owner-tuple в kacho-iam, worker прогоняет эту
// пробу до тех пор, пока owner-tuple не станет ЭФФЕКТИВЕН в FGA — т.е. пока
// авторизационный резолв `subject #v_update <object>`, который gateway
// scope_extractor выполнит на немедленной мутации созданного ресурса, не начнёт
// возвращать ALLOW. Так закрывается окно 403 «no direct relations granted» между
// op.done(success) и первой мутацией создателя (OTG-04).
//
// FIX-2 (consistency-совместимость): проба идёт по ТОМУ ЖЕ ребру
// `InternalIAMService.Check` (:9091, reuse authzConn), что и per-RPC authz-gate —
// один read-path, один store OpenFGA, а не независимый снапшот, поэтому
// подтверждение confirm-пробы влечёт ALLOW и на gateway scope_extractor Check.
// Нового cross-service ребра не добавляется (OTG-08): confirmer — тонкий адаптер
// поверх существующего authz.CheckClient.
//
// Read-only и идемпотентна: worker ретраит её в пределах confirmation-deadline;
// повторные пробы состояние FGA не мутируют.
type OwnerConfirmer struct {
	cc         authz.CheckClient
	objectType string
}

// NewNetworkOwnerConfirmer / NewSecurityGroupOwnerConfirmer / NewSubnetOwnerConfirmer —
// confirmer'ы для соответствующих vpc-ресурсов. objectType зафиксирован
// внутренними FGA-константами (vpc_network / vpc_security_group / vpc_subnet),
// поэтому caller не может передать несогласованный тип. cc — тот же
// authz.CheckClient (IAMCheckClient поверх authzConn), что обслуживает per-RPC
// authz-gate.
func NewNetworkOwnerConfirmer(cc authz.CheckClient) *OwnerConfirmer {
	return &OwnerConfirmer{cc: cc, objectType: objectTypeNetwork}
}

// NewSecurityGroupOwnerConfirmer — см. NewNetworkOwnerConfirmer (object vpc_security_group).
func NewSecurityGroupOwnerConfirmer(cc authz.CheckClient) *OwnerConfirmer {
	return &OwnerConfirmer{cc: cc, objectType: objectTypeSecurityGroup}
}

// NewSubnetOwnerConfirmer — см. NewNetworkOwnerConfirmer (object vpc_subnet).
func NewSubnetOwnerConfirmer(cc authz.CheckClient) *OwnerConfirmer {
	return &OwnerConfirmer{cc: cc, objectType: objectTypeSubnet}
}

// NewAddressOwnerConfirmer — см. NewNetworkOwnerConfirmer (object vpc_address).
func NewAddressOwnerConfirmer(cc authz.CheckClient) *OwnerConfirmer {
	return &OwnerConfirmer{cc: cc, objectType: objectTypeAddress}
}

// NewRouteTableOwnerConfirmer — см. NewNetworkOwnerConfirmer (object vpc_route_table).
func NewRouteTableOwnerConfirmer(cc authz.CheckClient) *OwnerConfirmer {
	return &OwnerConfirmer{cc: cc, objectType: objectTypeRouteTable}
}

// NewGatewayOwnerConfirmer — см. NewNetworkOwnerConfirmer (object vpc_gateway).
func NewGatewayOwnerConfirmer(cc authz.CheckClient) *OwnerConfirmer {
	return &OwnerConfirmer{cc: cc, objectType: objectTypeGateway}
}

// NewNetworkInterfaceOwnerConfirmer — см. NewNetworkOwnerConfirmer (object vpc_network_interface).
func NewNetworkInterfaceOwnerConfirmer(cc authz.CheckClient) *OwnerConfirmer {
	return &OwnerConfirmer{cc: cc, objectType: objectTypeNetworkInterface}
}

// Confirm — read-after-register проба под контракт operations.ConfirmFunc
// (адаптируется use-case'ом в замыкание). creator — принципал op'а (op.Principal,
// = создатель ресурса); resourceID — id только что созданного ресурса.
//
// Relation зафиксирован `v_update` — канонический mutate-relation. Под flat-моделью
// (Contract-A: `<rel> from project`-каскад удалён) доступ создателя на ресурс — это
// per-object v_* tuple'ы (v_get/v_list/v_update/v_delete + tier), которые
// iam-реконсайлер МАТЕРИАЛИЗУЕТ для owner-биндинга из события RegisterResource (а НЕ
// деривирует по project-parent-pointer'у). Реконсайлер эмитит весь v_*-набор per-object
// одним заходом, поэтому подтверждение `v_update` гарантирует, что материализация легла
// целиком (в т.ч. `v_delete` → немедленный Delete создателя тоже не 403, OTG-02/OTG-14).
//
// Семантика возврата (под operations.ConfirmFunc — confirmed=true ⇒ MarkDone,
// иначе worker ретраит в пределах confirmation-deadline):
//   - allowed=true                                → confirmed=true (owner-tuple эффективен);
//   - allowed=false / ErrNoPath / ErrHideExistence → confirmed=false, err=nil (owner-tuple
//     ещё не зарегистрирован/не виден — pending; ретрай, а не transient-сбой);
//   - транспорт/Unavailable (прочий err)          → confirmed=false, err!=nil (transient;
//     worker логирует и ретраит в пределах deadline, затем fail-closed Unavailable).
func (c *OwnerConfirmer) Confirm(ctx context.Context, creator operations.Principal, resourceID string) (bool, error) {
	subject := authz.FormatSubject(creator.Type, creator.ID)
	object, err := authz.FormatObject(c.objectType, resourceID)
	if err != nil {
		// Вырожденный object (пустой id / reserved-char) — не транзиент и не
		// «pending»: owner-tuple для такого object не подтвердится никогда. Возвращаем
		// err → worker ретраит и в итоге fail-closed по deadline (Unavailable), а не
		// ложный success.
		return false, err
	}
	allowed, cerr := c.checkConsistent(ctx, subject, relationVUpdate, object)
	if cerr != nil {
		// ErrNoPath / ErrHideExistence — owner-tuple ещё не виден в FGA (нет пути к
		// объекту) → pending, НЕ transient-сбой: возвращаем confirmed=false без err,
		// чтобы worker молча ретраил пробу до появления tuple (или deadline).
		if errors.Is(cerr, authz.ErrNoPath) || errors.Is(cerr, authz.ErrHideExistence) {
			return false, nil
		}
		return false, cerr
	}
	return allowed, nil
}

// consistentChecker — опциональная capability CheckClient'а: Check с
// HIGHER_CONSISTENCY (read-after-own-write). Реализуется `*IAMCheckClient`.
type consistentChecker interface {
	CheckConsistent(ctx context.Context, subjectID, relation, object string) (allowed bool, err error)
}

// checkConsistent прогоняет confirm-пробу с HIGHER_CONSISTENCY, когда клиент это
// умеет (owner-tuple записан синхронно в тот же store — read-after-own-write ОБЯЗАН
// быть consistent). Fallback на default Check — только когда client не реализует
// consistentChecker (напр. тестовый stub); в проде `*IAMCheckClient` реализует.
func (c *OwnerConfirmer) checkConsistent(ctx context.Context, subject, relation, object string) (bool, error) {
	if cc, ok := c.cc.(consistentChecker); ok {
		return cc.CheckConsistent(ctx, subject, relation, object)
	}
	return c.cc.Check(ctx, subject, relation, object)
}
