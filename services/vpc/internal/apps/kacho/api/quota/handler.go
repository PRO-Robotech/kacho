// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package quota

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	quotav1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/quota/v1"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

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
func (h *Handler) List(ctx context.Context, req *vpcv1.ListQuotasRequest) (*vpcv1.ListQuotasResponse, error) {
	// Обязательность проверяется ПЕРВЫМ стейтментом и своим отказом. Оставленная
	// краю, пустая строка дошла бы до извлечения области действия, не
	// разрешилась бы там и вернулась отказом в правах — то есть утверждением о
	// доступе на вопрос, который никогда не был корректным
	// (`api-conventions.md`: `corevalidate.ResourceID` пустую строку ПРОПУСКАЕТ,
	// required — отдельная ответственность вызывающего).
	if req.GetProjectId() == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id: required")
	}

	states, err := h.band.States(ctx, req.GetProjectId())
	if err != nil {
		return nil, err
	}

	out := make([]*quotav1.Quota, 0, len(states))
	for _, st := range states {
		out = append(out, &quotav1.Quota{
			Kind:          st.Kind,
			Limit:         st.Limit,
			Used:          st.Used,
			SourceScope:   scopeToProto(st.SourceScope),
			SourceScopeId: st.SourceScopeID,
			CarrierType:   st.CarrierType,
			CarrierId:     st.CarrierID,
		})
	}
	return &vpcv1.ListQuotasResponse{Quotas: out}, nil
}

// scopeToProto переводит область из строки хранения в перечисление контракта.
//
// Неопознанное значение отображается в `SCOPE_UNSPECIFIED`, а НЕ в `DEFAULT`:
// умолчание — это утверждение «величина платформенная», и делать его на строке,
// которую мы не смогли прочитать, значит выдавать незнание за факт. Пустой
// перечислитель виден арендатору как «источник не назван» и отличим от всех
// трёх законных областей.
func scopeToProto(s string) iamv1.Limit_Scope {
	switch s {
	case "DEFAULT":
		return iamv1.Limit_DEFAULT
	case "ACCOUNT":
		return iamv1.Limit_ACCOUNT
	case "PROJECT":
		return iamv1.Limit_PROJECT
	default:
		return iamv1.Limit_SCOPE_UNSPECIFIED
	}
}

// Гарантия соответствия контракту на этапе сборки.
var _ vpcv1.QuotaServiceServer = (*Handler)(nil)

// Ссылка на тип строки учёта — чтобы смена её состава ломала сборку здесь, а не
// молча меняла форму ответа.
var _ = kacho.QuotaState{}
