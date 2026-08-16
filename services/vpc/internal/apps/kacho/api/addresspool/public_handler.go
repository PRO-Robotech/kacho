// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package addresspool

import (
	"context"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/pbconv"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

// PublicHandler — реализация `vpcv1.AddressPoolServiceServer`: та же
// административная поверхность на ПУБЛИЧНОМ слушателе, под правом
// `system_admin` @ `cluster` (под-фаза ADM-1 S1).
//
// # Тонкий транспорт, и «тонкий» здесь проверяемо
//
// Ни одной строки бизнес-логики: чтения зовут ТЕ ЖЕ use-case'ы, что внутренний
// handler, и ту же проекцию `poolToProto`; мутации уходят в `AsyncMutations`,
// которая зовёт те же use-case'ы внутри оболочки `Operation`. Composition root
// передаёт сюда указатель на уже собранный внутренний handler — не копию
// зависимостей, а его самого, — поэтому «оба пути делают одно» держится
// построением, а не совпадением сборки.
//
// # Чем публичный путь ОТЛИЧАЕТСЯ от внутреннего, и почему ровно этим
//
//	(1) форма ответа мутации — `Operation` (запрет 9): клиент публичного API
//	    вправе рассчитывать на один контракт у всех мутаций;
//	(2) форма идентификатора проверяется ПЕРВЫМ стейтментом — негодный по форме
//	    получает «исправь ввод», а не «его нет»;
//	(3) промах прямого чтения отвечает контрактным тоном и машинным признаком
//	    полосы.
//
// Ни одно из трёх не меняет СОСТОЯНИЕ: негодный по форме идентификатор не
// совпадает со строкой ни на одном из путей, а тон отказа строки не пишет.
// Поэтому два пути записи в одну таблицу остаются эквивалентными по предмету —
// это и утверждает сценарий 19 приёмки.
type PublicHandler struct {
	vpcv1.UnimplementedAddressPoolServiceServer

	admin *Handler
	async *AsyncMutations
}

// NewPublicHandler собирает публичный транспорт поверх уже собранного
// внутреннего handler'а и оболочки операций.
func NewPublicHandler(admin *Handler, async *AsyncMutations) *PublicHandler {
	return &PublicHandler{admin: admin, async: async}
}

// -- Чтение (sync) --

// Get — форма идентификатора первым стейтментом, затем тот же use-case чтения,
// что у внутреннего пути, и та же проекция.
func (h *PublicHandler) Get(ctx context.Context, req *vpcv1.GetAddressPoolRequest) (*vpcv1.AddressPool, error) {
	id := req.GetPoolId()
	if err := ValidatePoolID(id); err != nil {
		return nil, err
	}
	rec, err := h.admin.get.Execute(ctx, id)
	if err != nil {
		return nil, MapPublicErr(err, poolKind, poolDisplay, id)
	}
	return poolToProto(rec), nil
}

// List — межпроектный админский список, гейтится ОДНИМ вопросом `system_admin` @
// `cluster` на крае.
//
// Замыкания на пустом гранте здесь НЕТ и быть не может: у пула нет владельца, о
// котором можно было бы спросить пообъектно, — объект один и он кластерный.
// Поэтому правило «проверка формата страницы стоит до замыкания» выполняется
// вырожденно: замыкать нечего, а формат проверяет сам use-case, до обращения к
// репозиторию и одним кодеком с путём чтения.
func (h *PublicHandler) List(ctx context.Context, req *vpcv1.ListAddressPoolsRequest) (*vpcv1.ListAddressPoolsResponse, error) {
	pools, next, err := h.admin.list.Execute(ctx, AddressPoolFilter{
		Kind:   domain.AddressPoolKind(req.GetKind()), // #nosec G115 -- значение enum proto (ограниченный набор), не арифметическое переполнение.
		ZoneID: req.GetZoneId(),
	}, Pagination{
		PageToken: req.GetPageToken(),
		PageSize:  req.GetPageSize(),
	})
	if err != nil {
		return nil, MapPublicErr(err, poolKind, poolDisplay, "")
	}
	out := make([]*vpcv1.AddressPool, 0, len(pools))
	for _, p := range pools {
		out = append(out, poolToProto(p))
	}
	return &vpcv1.ListAddressPoolsResponse{Pools: out, NextPageToken: next}, nil
}

// ListAddresses — адреса всех проектов, получившие IP из этого пула.
// Межпроектность намеренна и есть предмет глагола.
func (h *PublicHandler) ListAddresses(ctx context.Context, req *vpcv1.ListAddressPoolAddressesRequest) (*vpcv1.ListAddressPoolAddressesResponse, error) {
	id := req.GetPoolId()
	if err := ValidatePoolID(id); err != nil {
		return nil, err
	}
	resp, err := h.admin.ListAddresses(ctx, req)
	if err != nil {
		return nil, MapPublicErr(err, poolKind, poolDisplay, id)
	}
	return resp, nil
}

// GetUtilization — статистика использования пула.
func (h *PublicHandler) GetUtilization(ctx context.Context, req *vpcv1.GetAddressPoolUtilizationRequest) (*vpcv1.AddressPoolUtilization, error) {
	id := req.GetPoolId()
	if err := ValidatePoolID(id); err != nil {
		return nil, err
	}
	resp, err := h.admin.GetUtilization(ctx, req)
	if err != nil {
		return nil, MapPublicErr(err, poolKind, poolDisplay, id)
	}
	return resp, nil
}

// -- Мутации (async, Operation) --

func (h *PublicHandler) Create(ctx context.Context, req *vpcv1.CreateAddressPoolRequest) (*operationpb.Operation, error) {
	op, err := h.async.Create(ctx, CreatePoolReq{
		Name:             req.GetName(),
		Description:      req.GetDescription(),
		Labels:           req.GetLabels(),
		V4CIDRBlocks:     req.GetV4CidrBlocks(),
		V6CIDRBlocks:     req.GetV6CidrBlocks(),
		Kind:             domain.AddressPoolKind(req.GetKind()), // #nosec G115 -- значение enum proto (ограниченный набор), не арифметическое переполнение.
		ZoneID:           req.GetZoneId(),
		IsDefault:        req.GetIsDefault(),
		SelectorLabels:   req.GetSelectorLabels(),
		SelectorPriority: req.GetSelectorPriority(),
	})
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// Update — разбор маски тот же, что у внутреннего пути (`normalizeMaskPaths`):
// REST шлёт camelCase, gRPC — snake_case, и приведение обязано быть одним, иначе
// одна и та же маска значила бы на двух путях разное.
func (h *PublicHandler) Update(ctx context.Context, req *vpcv1.UpdateAddressPoolRequest) (*operationpb.Operation, error) {
	op, err := h.async.Update(ctx, UpdatePoolReq{
		ID:               req.GetPoolId(),
		UpdateMask:       normalizeMaskPaths(req.GetUpdateMask().GetPaths()),
		Name:             req.GetName(),
		Description:      req.GetDescription(),
		Labels:           req.GetLabels(),
		IsDefault:        req.GetIsDefault(),
		SelectorLabels:   req.GetSelectorLabels(),
		SelectorPriority: req.GetSelectorPriority(),
	})
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

func (h *PublicHandler) Delete(ctx context.Context, req *vpcv1.DeleteAddressPoolRequest) (*operationpb.Operation, error) {
	op, err := h.async.Delete(ctx, req.GetPoolId())
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

func (h *PublicHandler) AddCidrBlocks(ctx context.Context, req *vpcv1.AddAddressPoolCidrBlocksRequest) (*operationpb.Operation, error) {
	op, err := h.async.AddCidrBlocks(ctx, req.GetAddressPoolId(), req.GetV4CidrBlocks(), req.GetV6CidrBlocks())
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

func (h *PublicHandler) RemoveCidrBlocks(ctx context.Context, req *vpcv1.RemoveAddressPoolCidrBlocksRequest) (*operationpb.Operation, error) {
	op, err := h.async.RemoveCidrBlocks(ctx, req.GetAddressPoolId(), req.GetV4CidrBlocks(), req.GetV6CidrBlocks())
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

func (h *PublicHandler) BindAsNetworkDefault(ctx context.Context, req *vpcv1.BindAsNetworkDefaultRequest) (*operationpb.Operation, error) {
	op, err := h.async.BindAsNetworkDefault(ctx, req.GetNetworkId(), req.GetPoolId())
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

func (h *PublicHandler) UnbindNetworkDefault(ctx context.Context, req *vpcv1.UnbindNetworkDefaultRequest) (*operationpb.Operation, error) {
	op, err := h.async.UnbindNetworkDefault(ctx, req.GetNetworkId())
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}
