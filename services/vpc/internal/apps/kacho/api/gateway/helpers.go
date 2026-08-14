// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package gateway

import (
	"fmt"

	"google.golang.org/protobuf/types/known/anypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"

	// Blank-import регистрирует трансферы Gateway/time через init().
	_ "github.com/PRO-Robotech/kacho/services/vpc/internal/dto/toproto"
)

// gatewayVariantMaskNat / gatewayVariantMaskEgressOnly — имена ветвей oneof
// `Gateway.gateway` В ТОМ ВИДЕ, В КАКОМ ИХ НАЗЫВАЕТ update_mask (имена полей
// контракта, не столбцов БД). Держатся константами ровно потому, что прежде на их
// месте в известном наборе маски стояло имя столбца.
const (
	gatewayVariantMaskNat        = "nat_gateway"
	gatewayVariantMaskEgressOnly = "egress_only_gateway"
)

// gatewayTypeFromCreateSpec — выбранная ветвь спецификации создания → доменный
// вид шлюза. Невыбранная ветвь даёт пустое значение, и это ОТДЕЛЬНЫЙ исход:
// required-проверку делает use-case и отвергает его именем поля. Подставлять здесь
// вид «по умолчанию» запрещено — это выбрало бы поведение шлюза за вызывающего.
//
// На входе весь запрос, а не сама ветвь: интерфейс oneof, который генератор
// заводит под неё, неэкспортируемый, и назвать его в подписи за пределами пакета
// стабов нельзя. Принимать `any` было бы хуже — подпись перестала бы говорить, что
// именно разбирают.
func gatewayTypeFromCreateSpec(req *vpcv1.CreateGatewayRequest) domain.GatewayType {
	switch req.GetGateway().(type) {
	case *vpcv1.CreateGatewayRequest_NatGatewaySpec:
		return domain.GatewayTypeNat
	case *vpcv1.CreateGatewayRequest_EgressOnlyGatewaySpec:
		return domain.GatewayTypeEgressOnly
	default:
		return ""
	}
}

// marshalGatewayRecord конвертирует repo-entity Gateway в *anypb.Any через
// DTO-реестр. Используется worker'ами для упаковки результата в Operation.response.
func marshalGatewayRecord(rec *kacho.GatewayRecord) (*anypb.Any, error) {
	var dst *vpcv1.Gateway
	if err := dto.Transfer(dto.FromTo(*rec, &dst)); err != nil {
		return nil, fmt.Errorf("dto.Transfer Gateway: %w", err)
	}
	return anypb.New(dst)
}
