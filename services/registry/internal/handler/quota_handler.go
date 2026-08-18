// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	"github.com/PRO-Robotech/kacho/pkg/quota/quotapb"

	quotaband "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/quota"
)

// QuotaHandler — реализация registryv1.QuotaServiceServer. Тонкий транспорт: разбор
// запроса → полоса учёта → формат ответа.
//
// ТОЛЬКО ЧТЕНИЕ, и это граница прав, а не объём работы. Величины назначает
// администратор облака через `iam.v1.InternalLimitService` на внутреннем
// слушателе; здесь их нельзя ни завести, ни изменить, ни удалить. Арендатор,
// способный поднять свой потолок, потолка не имеет.
//
// Живёт здесь, рядом с остальным транспортом этого сервиса, а не своим пакетом:
// перепись поверхности списков обходит ИМЕННО этот каталог, и обработчик,
// положенный в сторону, остался бы вне её поля зрения — то есть не был бы
// осуждён ни одной проверкой, оставаясь на вид таким же, как соседи.
type QuotaHandler struct {
	registryv1.UnimplementedQuotaServiceServer

	band *quotaband.Guard
}

// NewQuotaHandler собирает обработчик поверх полосы учёта.
func NewQuotaHandler(band *quotaband.Guard) *QuotaHandler { return &QuotaHandler{band: band} }

// List отдаёт квоты проекта — предел, потребление и источник величины по
// каждому виду домена.
//
// Пагинации нет осознанно: словарь видов закрыт и мал, и ограничен он миграцией,
// а не поведением арендатора. Курсор здесь добавил бы отказ (арендатор читает
// вторую страницу своих потолков) и не купил бы ничего.
//
// Сужать здесь НЕЧЕГО, и это не послабление: строка квоты — свойство проекта, а
// не объект с владельцем. Проект либо читаем этим вызывающим, либо нет — ровно
// один вопрос, и его решает `viewer` на проекте через извлечение области
// действия на крае.
//
// Тело — ОБЩЕЕ (`quotapb.ListQuotas`): обязательность проекта, обращение к
// полосе и перевод в контракт одинаковы у всех владельцев, и пять копий этих
// решений разошлись бы текстом отказа. Своё здесь — только тип ответа.
func (h *QuotaHandler) List(
	ctx context.Context, req *registryv1.ListQuotasRequest,
) (*registryv1.ListQuotasResponse, error) {
	quotas, err := quotapb.ListQuotas(ctx, req.GetProjectId(), h.states())
	if err != nil {
		return nil, err
	}
	return &registryv1.ListQuotasResponse{Quotas: quotas}, nil
}

// states отдаёт глагол полосы ЛИБО настоящий nil.
//
// Метод типизированного nil-указателя вызвать можно, и он упал бы паникой уже
// внутри общего тела; здесь решение принимается там, где тип ещё конкретен, и
// непровязанная полоса отвечает названным отказом, а не падением.
func (h *QuotaHandler) states() quotapb.StatesFunc {
	if h == nil || h.band == nil {
		return nil
	}
	return h.band.States
}

// Гарантия соответствия контракту на этапе сборки.
var _ registryv1.QuotaServiceServer = (*QuotaHandler)(nil)
