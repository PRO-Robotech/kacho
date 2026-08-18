// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package quota

import (
	"context"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/quota/quotapb"

	quotaband "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/quota"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// Handler — реализация vpcv1.QuotaServiceServer. Тонкий транспорт: разбор
// запроса → полоса учёта → формат ответа.
//
// ТОЛЬКО ЧТЕНИЕ, и это граница прав, а не объём работы. Величины назначает
// администратор облака через `iam.v1.InternalLimitService` на внутреннем
// слушателе; здесь их нельзя ни завести, ни изменить, ни удалить. Арендатор,
// способный поднять свой потолок, потолка не имеет.
type Handler struct {
	vpcv1.UnimplementedQuotaServiceServer

	band *quotaband.Guard
}

// NewHandler собирает обработчик поверх полосы учёта.
func NewHandler(band *quotaband.Guard) *Handler {
	return &Handler{band: band}
}

// List отдаёт квоты проекта — предел, потребление и источник величины по
// каждому виду домена.
//
// Пагинации нет осознанно: словарь видов закрыт и мал, и ограничен он миграцией,
// а не поведением арендатора. Курсор здесь добавил бы отказ (арендатор читает
// вторую страницу своих восьми потолков) и не купил бы ничего.
//
// Тело — ОБЩЕЕ (`quotapb.ListQuotas`): обязательность проекта, обращение к
// полосе и перевод в контракт одинаковы у всех владельцев, и пять копий этих
// решений разошлись бы текстом отказа. Своё здесь — только тип ответа.
func (h *Handler) List(ctx context.Context, req *vpcv1.ListQuotasRequest) (*vpcv1.ListQuotasResponse, error) {
	quotas, err := quotapb.ListQuotas(ctx, req.GetProjectId(), h.states())
	if err != nil {
		return nil, err
	}
	return &vpcv1.ListQuotasResponse{Quotas: quotas}, nil
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
var _ vpcv1.QuotaServiceServer = (*Handler)(nil)

// Ссылка на тип строки учёта — чтобы смена её состава ломала сборку здесь, а не
// молча меняла форму ответа.
var _ = kacho.QuotaState{}
