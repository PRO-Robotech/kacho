// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// declared_but_absent.go — RPC, объявленные контрактом `InstanceService` и не
// несущие реализации, отказывают ЯВНО.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Семь методов не были переопределены над встроенной заглушкой. Отказ при этом
// приходил, но безымянный — `method AddOneToOneNat not implemented`, — из
// которого вызывающий не узнаёт ни причины, ни того, где возможность живёт на
// самом деле. Страница документации ресурса причину называет честно; клиент API
// её не читает и сталкивается с ограничением ровно в момент отказа. Отказ и
// есть то единственное место, где ему можно об этом сказать.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ЭТО НЕ «ДОДЕЛАТЬ ПОТОМ», А ГРАНИЦА ДОМЕНА
//
// Ни один из семи не является пробелом в compute — у каждого возможность
// принадлежит ДРУГОМУ владельцу, и правило одного владельца на тип ресурса
// запрещает дублировать её здесь:
//
//   - адресация и NAT — свойства `NetworkInterface`, владелец **vpc**; инстанс
//     их только зеркалит, поэтому править их через инстанс значило бы завести
//     второго писателя чужой строки;
//   - перенос машины в другую зону требует переноса её томов, а тома
//     принадлежат **storage**; кроме того, у продукта нет data plane, на который
//     переносить;
//   - привязки доступа — ресурс `AccessBinding` домена **iam**, с собственным
//     сервисом и закрытым словарём целей; поверхность на ресурсе задваивала бы
//     выдачу прав и разошлась бы с ней при первом же изменении модели.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО УДЕРЖИВАЕТ ТЕКСТ ОТКАЗА
//
// Тон и содержание — часть контракта: проба declared_but_absent_refusal_test.go
// утверждает СООБЩЕНИЕ, а не только код. Унаследованный отказ и названный дают
// один и тот же `UNIMPLEMENTED`, поэтому проба на коде зеленела бы ровно на
// возврате исходного состояния.
package handler

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	accesspb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/access"
	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
)

const (
	// refusalNICOwnedByVPC — адресация и NAT правятся у владельца интерфейса.
	refusalNICOwnedByVPC = "editing a network interface through Instance is not available: " +
		"address and one-to-one NAT are properties of the NetworkInterface resource owned by " +
		"vpc. Use vpc NetworkInterfaceService.Update on /vpc/v1/networkInterfaces/{id}; the " +
		"instance mirrors the result read-only"

	// refusalRelocateNeedsStorage — перенос машины тянет за собой её тома.
	refusalRelocateNeedsStorage = "relocating an instance is not available: moving a machine to " +
		"another zone requires moving its volumes, and volumes are owned by storage. There is no " +
		"cross-zone volume move in this API — create an instance in the target zone from a " +
		"snapshot instead"

	// refusalBindingsOwnedByIAM — выдача прав живёт в своём домене.
	refusalBindingsOwnedByIAM = "managing access bindings through Instance is not available: " +
		"AccessBinding is a resource of the iam domain. Use iam AccessBindingService with the " +
		"instance as the binding target"
)

func (h *InstanceHandler) AddOneToOneNat(
	_ context.Context, _ *computev1.AddInstanceOneToOneNatRequest,
) (*operationpb.Operation, error) {
	return nil, status.Error(codes.Unimplemented, refusalNICOwnedByVPC)
}

func (h *InstanceHandler) RemoveOneToOneNat(
	_ context.Context, _ *computev1.RemoveInstanceOneToOneNatRequest,
) (*operationpb.Operation, error) {
	return nil, status.Error(codes.Unimplemented, refusalNICOwnedByVPC)
}

func (h *InstanceHandler) UpdateNetworkInterface(
	_ context.Context, _ *computev1.UpdateInstanceNetworkInterfaceRequest,
) (*operationpb.Operation, error) {
	return nil, status.Error(codes.Unimplemented, refusalNICOwnedByVPC)
}

func (h *InstanceHandler) Relocate(
	_ context.Context, _ *computev1.RelocateInstanceRequest,
) (*operationpb.Operation, error) {
	return nil, status.Error(codes.Unimplemented, refusalRelocateNeedsStorage)
}

func (h *InstanceHandler) ListAccessBindings(
	_ context.Context, _ *accesspb.ListAccessBindingsRequest,
) (*accesspb.ListAccessBindingsResponse, error) {
	return nil, status.Error(codes.Unimplemented, refusalBindingsOwnedByIAM)
}

func (h *InstanceHandler) SetAccessBindings(
	_ context.Context, _ *accesspb.SetAccessBindingsRequest,
) (*operationpb.Operation, error) {
	return nil, status.Error(codes.Unimplemented, refusalBindingsOwnedByIAM)
}

func (h *InstanceHandler) UpdateAccessBindings(
	_ context.Context, _ *accesspb.UpdateAccessBindingsRequest,
) (*operationpb.Operation, error) {
	return nil, status.Error(codes.Unimplemented, refusalBindingsOwnedByIAM)
}
