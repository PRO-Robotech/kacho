// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package networkinterface

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"

	// Blank-import регистрирует DTO-трансферы (включая NetworkInterface) через init().
	_ "github.com/PRO-Robotech/kacho/services/vpc/internal/dto/toproto"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// niResource — название ресурса для сообщений `corevalidate.ResourceID`.
const niResource = "network interface"

// niResourceID — sync-валидация формата NIC-id (3-char crockford-base32 prefix
// + 17-char base32). NIC имеет собственный prefix `nic`
// (`ids.PrefixNetworkInterface`). Проверка family-agnostic (corevalidate.ResourceID
// игнорирует expectedPrefix, сверяя лишь known-set), prefix передается для
// читаемости call-site.
func niResourceID(id string) error {
	return corevalidate.ResourceID(niResource, ids.PrefixNetworkInterface, id)
}

// validateNICAddressCardinality — fast-fail sync-валидация: на одной NetworkInterface
// разрешен максимум один IPv4 и максимум один IPv6. Совпадает с DB-уровнем
// `network_interfaces_v4_addr_max1` / `_v6_addr_max1` (DB-side — финальный backstop,
// эта функция дает понятный InvalidArgument до создания Operation). Multi-IP на VM
// реализуется через несколько NIC, а не через secondary-адреса в одном NIC.
func validateNICAddressCardinality(v4IDs, v6IDs []string) error {
	if len(v4IDs) > 1 {
		return serviceerr.InvalidArg("v4_address_ids", "at most one IPv4 address per network interface (use multiple NICs for multi-IP)")
	}
	if len(v6IDs) > 1 {
		return serviceerr.InvalidArg("v6_address_ids", "at most one IPv6 address per network interface (use multiple NICs for multi-IP)")
	}
	return nil
}

// validateNICSecurityGroupCardinality — потолок числа групп на интерфейсе.
// Стоит СИНХРОННО, первым делом: длину массива задаёт вызывающий, и она
// определяет стоимость запроса. DB-CHECK network_interfaces_sg_cardinality —
// атомарный backstop.
func validateNICSecurityGroupCardinality(ids []string) error {
	if len(ids) > domain.MaxNICSecurityGroups {
		return serviceerr.InvalidArg("security_group_ids",
			fmt.Sprintf("at most %d security groups per network interface", domain.MaxNICSecurityGroups))
	}
	return nil
}

// validateNICSecurityGroupRefs — существование И принадлежность каждой
// названной группы безопасности. Ссылка приходит массивом от вызывающего, у
// колонки нет внешнего ключа (jsonb), поэтому единственная защита — эта
// проверка; без неё интерфейс одного проекта ссылается на группу другого, и
// владелец группы теряет возможность её удалить (предусловие удаления
// спрашивает «ссылается ли кто-нибудь», а ссылающийся интерфейс лежит в чужом
// проекте и владельцу не виден).
//
// Резолв — ОДИН запрос на весь массив (`GetMany`), а не обращение к БД на
// элемент: длину массива задаёт вызывающий.
//
// Тон отказа для «нет такой» и «есть, но не твоя» одинаков — иначе это оракул
// существования. Группа без сети (project-level) принимается в любой подсети
// проекта; группа с сетью — только в подсети своей сети.
//
// Код и регистр выбраны по соседней ссылке ЭТОГО ЖЕ ресурса (validateNICAddressRef:
// InvalidArgument, "address %s not found"), а не по родительской подсети
// (NotFound, "Subnet %s not found"): здесь речь о значении ПОЛЯ запроса, а не о
// самом адресуемом ресурсе. Два соседних прецедента в сервисе действительно
// разные — выбор сделан осознанно, менять его — через тикет.
func validateNICSecurityGroupRefs(
	ctx context.Context,
	sgr kachorepo.SecurityGroupReaderIface,
	ids []string,
	projectID, subnetNetworkID string,
) error {
	if len(ids) == 0 {
		return nil
	}
	if err := validateNICSecurityGroupCardinality(ids); err != nil {
		return err
	}
	for _, id := range ids {
		if id == "" {
			return serviceerr.InvalidArg("security_group_ids", "security group id must not be empty")
		}
	}
	found, err := sgr.GetMany(ctx, ids)
	if err != nil {
		return serviceerr.MapRepoErr(err)
	}
	for _, id := range ids {
		sg, ok := found[id]
		if !ok || string(sg.ProjectID) != projectID ||
			(sg.NetworkID != "" && sg.NetworkID != subnetNetworkID) {
			return status.Errorf(codes.InvalidArgument, "security group %s not found", id)
		}
	}
	return nil
}

// marshalNetworkInterfaceRecord конвертирует repo-entity NIC в *anypb.Any через
// DTO-реестр. Используется worker'ами Create/Update для упаковки результата в
// Operation.response.
func marshalNetworkInterfaceRecord(rec *kachorepo.NetworkInterfaceRecord) (*anypb.Any, error) {
	var dst *vpcv1.NetworkInterface
	if err := dto.Transfer(dto.FromTo(*rec, &dst)); err != nil {
		return nil, fmt.Errorf("dto.Transfer NetworkInterface: %w", err)
	}
	return anypb.New(dst)
}
