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

// nicAddressRefField — имя поля запроса, которым вызывающий назвал ссылку. Нужно,
// чтобы отказ по форме называл ИМЕННО то поле, куда пришло значение: у интерфейса
// два семейства ссылок, и «v4_address_ids» на v6-входе отправило бы правящего
// смотреть не туда.
func nicAddressRefField(want domain.IpVersion) string {
	if want == domain.IpVersionIPv6 {
		return "v6_address_ids"
	}
	return "v4_address_ids"
}

// validateNICAddressRefID — ФОРМА одной ссылки на адрес: обязательность плюс
// формат идентификатора. Ни одного обращения к БД — величину задаёт вызывающий,
// поэтому проверка стоит первой и не оплачивается чтением.
//
// Обязательность — отдельная ответственность вызывающего: `corevalidate.ResourceID`
// пустую строку ПРОПУСКАЕТ по своему контракту. Без этой ветки пустая строка
// уезжала в чтение и возвращалась контракт-тоном промаха с вырезанным id
// («Address  not found») — утверждение об отсутствии ресурса, которого вызывающий
// не называл.
//
// Формат — канонический `corevalidate.ResourceID` («invalid address id '<X>'»),
// тот же, что у всех прочих путей адреса в сервисе. Отдельного текста здесь не
// заводится: два места об одной форме разъезжаются молча.
func validateNICAddressRefID(want domain.IpVersion, id string) error {
	if id == "" {
		return serviceerr.InvalidArg(nicAddressRefField(want), "address id must not be empty")
	}
	return corevalidate.ResourceID("address", ids.PrefixAddress, id)
}

// validateNICAddressRefIDs — та же проверка формы, но по обоим семействам сразу и
// СИНХРОННО, до создания Operation: мусорный идентификатор не стоит ни чтения, ни
// асинхронного хвоста.
//
// Предикат здесь ровно тот же (`validateNICAddressRefID`), что исполняется на пути
// чтения внутри `validateNICAddressRef`. Это не два разбора одного значения, а
// один разбор с двух мест вызова: синхронная проверка делает отказ дешёвым и
// детерминированным, а проверка внутри читающей функции защищает КАЖДОГО её
// вызывающего — в том числе того, кто придёт после нас и синхронной ветки не
// позовёт.
func validateNICAddressRefIDs(v4IDs, v6IDs []string) error {
	for _, id := range v4IDs {
		if err := validateNICAddressRefID(domain.IpVersionIPv4, id); err != nil {
			return err
		}
	}
	for _, id := range v6IDs {
		if err := validateNICAddressRefID(domain.IpVersionIPv6, id); err != nil {
			return err
		}
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
// Код и регистр здесь — «значение ПОЛЯ запроса» (InvalidArgument, "security group %s
// not found"), и этот выбор НЕ менялся. Менялось другое: прежняя редакция абзаца
// обосновывала его ссылкой на соседнюю ссылку ЭТОГО ЖЕ ресурса
// (`validateNICAddressRef` — «InvalidArgument, address %s not found»), а та переехала
// на полосу СОБСТВЕННОГО ресурса (NotFound, "Address %s not found"): адрес
// принадлежит vpc, и его четыре различимых исхода раскрывали чужой объект. То есть
// довод пережил то, на что опирался, и удалён вместе с ним, а не переписан на другого
// соседа — цитата чужой координаты стареет молча.
//
// Смена кода И ЗДЕСЬ — отдельный предмет: группа тоже собственный ресурс vpc, и по
// полосе direct-read ей полагался бы тот же NotFound. Тексты — часть контракта,
// поэтому это идёт своим изменением со своей приёмкой, а не побочным эффектом правки
// соседней ссылки.
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
