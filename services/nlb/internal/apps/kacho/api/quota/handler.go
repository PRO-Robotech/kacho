// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package quota — транспорт арендаторского чтения квот этого домена.
package quota

import (
	"context"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/quota/quotapb"

	quotaband "github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/quota"
)

// Handler — реализация lbv1.QuotaServiceServer. Тонкий транспорт: разбор
// запроса → полоса учёта → формат ответа.
//
// ТОЛЬКО ЧТЕНИЕ, и это граница прав, а не объём работы. Величины назначает
// администратор облака через `iam.v1.InternalLimitService` на внутреннем
// слушателе; здесь их нельзя ни завести, ни изменить, ни удалить. Арендатор,
// способный поднять свой потолок, потолка не имеет.
type Handler struct {
	lbv1.UnimplementedQuotaServiceServer

	band *quotaband.Guard
}

// NewHandler собирает обработчик поверх полосы учёта.
func NewHandler(band *quotaband.Guard) *Handler { return &Handler{band: band} }

// List отдаёт квоты проекта — предел, потребление и источник величины по
// каждому виду домена.
//
// Тело — ОБЩЕЕ (`quotapb.ListQuotas`): обязательность проекта, обращение к
// полосе и перевод в контракт одинаковы у всех владельцев, и пять копий этих
// решений разошлись бы текстом отказа. Своё здесь — только тип ответа.
func (h *Handler) List(ctx context.Context, req *lbv1.ListQuotasRequest) (*lbv1.ListQuotasResponse, error) {
	quotas, err := quotapb.ListQuotas(ctx, req.GetProjectId(), h.states())
	if err != nil {
		return nil, err
	}
	return &lbv1.ListQuotasResponse{Quotas: quotas}, nil
}

// states отдаёт глагол полосы ЛИБО настоящий nil.
//
// Метод типизированного nil-указателя вызвать можно, и он упал бы паникой уже
// внутри общего тела; здесь решение принимается там, где тип ещё конкретен, и
// непровязанная полоса отвечает названным отказом, а не падением.
func (h *Handler) states() quotapb.StatesFunc {
	if h == nil || h.band == nil {
		return nil
	}
	return h.band.States
}

// Гарантия соответствия контракту на этапе сборки.
var _ lbv1.QuotaServiceServer = (*Handler)(nil)
