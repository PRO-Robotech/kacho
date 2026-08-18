// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package routetable

import (
	"fmt"
	"net/netip"

	"google.golang.org/protobuf/types/known/anypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"

	// Blank-import регистрирует трансферы RouteTable/time через init().
	_ "github.com/PRO-Robotech/kacho/services/vpc/internal/dto/toproto"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// marshalRouteTableRecord конвертирует repo-entity RouteTable в *anypb.Any
// через DTO-реестр.
func marshalRouteTableRecord(rec *kacho.RouteTableRecord) (*anypb.Any, error) {
	var dst *vpcv1.RouteTable
	if err := dto.Transfer(dto.FromTo(*rec, &dst)); err != nil {
		return nil, fmt.Errorf("dto.Transfer RouteTable: %w", err)
	}
	return anypb.New(dst)
}

// validateStaticRoutesCardinality — потолок числа маршрутов в таблице
// (domain.MaxStaticRoutes). Стоит ПЕРВЫМ, до поэлементного разбора: длину
// набора выбирает вызывающий, и всё, что идёт дальше, — разбор каждой записи,
// сериализация набора в JSONB и его полная выдача в каждом ответе — линейно по
// этой длине. Проверка, ограничивающая стоимость, не может сама её платить.
//
// Проверяется набор, который БУДЕТ записан, — Create и Update несут итог
// целиком: аддитивного глагола у маршрутов нет вовсе. Здесь стояла ссылка на
// хендлер такого глагола («отказ по имени»), но глагол снят вместе со своим
// хендлером, и отказывать стало нечему — набор заменяется целиком. DB-CHECK
// route_tables_static_routes_cardinality (миграция 0028) — атомарный backstop
// на саму строку, независимо от writer'а.
func validateStaticRoutesCardinality(routes []domain.StaticRoute) error {
	if len(routes) > domain.MaxStaticRoutes {
		return serviceerr.InvalidArg("static_routes",
			fmt.Sprintf("at most %d static routes per route table", domain.MaxStaticRoutes))
	}
	return nil
}

// validateStaticRoutes проверяет набор маршрутов целиком:
//   - число записей ≤ domain.MaxStaticRoutes (первым делом, см. выше);
//   - destinationPrefix: валидный CIDR (IPv4 или IPv6) без host-bits;
//   - nextHopAddress: валидный IP-адрес (IPv4 или IPv6).
//
// Пустой массив — допустим (route table без статических маршрутов).
// При нарушении — InvalidArgument с FieldViolation `static_routes[<i>].<field>`
// для записи и `static_routes` для набора.
//
// Потолок стоит ЗДЕСЬ, а не у каждого вызывающего: через эту функцию проходит
// каждый путь записи набора (Create, Update по маске и full-object PATCH), и
// проверка, разложенная по вызывающим, закрывала бы ровно те из них, кто о ней
// помнит.
func validateStaticRoutes(routes []domain.StaticRoute) error {
	if err := validateStaticRoutesCardinality(routes); err != nil {
		return err
	}
	for i, r := range routes {
		dpField := fmt.Sprintf("static_routes[%d].destination_prefix", i)
		if r.DestinationPrefix == "" {
			return serviceerr.InvalidArg(dpField, dpField+" is required")
		}
		prefix, err := netip.ParsePrefix(r.DestinationPrefix)
		if err != nil {
			return serviceerr.InvalidArg(dpField, dpField+" must be a valid CIDR (e.g. 10.0.0.0/24)")
		}
		if prefix.Masked() != prefix {
			return serviceerr.InvalidArg(dpField,
				dpField+" must have zero host-bits (use the network address "+prefix.Masked().String()+")")
		}
		// Следующий узел — РОВНО ОДИН из двух: адрес либо шлюз. Ветвь выбирает
		// вызывающий (oneof `next_hop`), поэтому required-проверка обязана быть
		// на ОБЕИХ сторонах и на их сумме: «ни одного» и «оба» одинаково не имеют
		// смысла. Проверять только адрес — значит отвечать про соседнюю ветвь тому,
		// кто её не посылал (этот дефект здесь уже был).
		nhField := fmt.Sprintf("static_routes[%d].next_hop_address", i)
		gwField := fmt.Sprintf("static_routes[%d].gateway_id", i)
		switch {
		case r.NextHopAddress == "" && r.GatewayID == "":
			return serviceerr.InvalidArg(nhField,
				fmt.Sprintf("static_routes[%d]: next_hop_address or gateway_id is required", i))
		case r.NextHopAddress != "" && r.GatewayID != "":
			return serviceerr.InvalidArg(gwField,
				fmt.Sprintf("static_routes[%d]: next_hop_address and gateway_id are mutually exclusive", i))
		case r.GatewayID != "":
			// Формат СВОЕГО id проверяется синхронно и первым — до всякого обращения
			// к БД (api-conventions §malformed-id). Шлюз принадлежит vpc, значит id
			// own-owned: явный мусор обязан получить терминальный INVALID_ARGUMENT, а
			// не отказ полосы существования.
			if err := corevalidate.ResourceID("gateway", ids.PrefixGateway, r.GatewayID); err != nil {
				return err
			}
		default:
			if _, err := netip.ParseAddr(r.NextHopAddress); err != nil {
				return serviceerr.InvalidArg(nhField, nhField+" must be a valid IP address (IPv4 or IPv6)")
			}
		}
	}
	return nil
}

// staticRoutesFromProto — proto-маршруты в domain. ОБЕ ветви следующего узла
// читаются.
//
// `StaticRoute.next_hop` — oneof из двух ветвей: адрес и `gateway_id`. Прежде
// читался только адрес, поэтому выбранный вызывающим `gateway_id` не читал НИКТО:
// маршрут доезжал до валидации с пустым следующим узлом и получал отказ по имени
// СОСЕДНЕЙ ветви, которую вызывающий не посылал. Ветвь была вынужденно объявлена
// «не принимается» (исход №2 правила «принято-и-проигнорировано»), потому что
// резолвить шлюз было нечем: у шлюза не было ни якоря размещения, ни вида, с
// которым можно сверить маршрут.
//
// Теперь есть и то и другое (миграция 0030), и ветвь РЕАЛИЗОВАНА — исход №1:
// существование шлюза держит внешний ключ, а сеть, семейство и когерентность
// размещения проверяет сам оператор записи ссылки
// (`routeTableWriter.insertGatewayRef`).
//
// `GetGatewayId()`/`GetNextHopAddress()` возвращают пустую строку для невыбранной
// ветви — это и есть дискриминатор для доменной записи, поэтому отдельный флаг
// «какая ветвь выбрана» не нужен: взаимоисключение проверяет
// `validateStaticRoutes`, и «ни одной ветви» оно тоже отвергает.
func staticRoutesFromProto(in []*vpcv1.StaticRoute) ([]domain.StaticRoute, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]domain.StaticRoute, 0, len(in))
	for _, sr := range in {
		out = append(out, domain.StaticRoute{
			Labels:            sr.Labels,
			DestinationPrefix: sr.GetDestinationPrefix(),
			NextHopAddress:    sr.GetNextHopAddress(),
			GatewayID:         sr.GetGatewayId(),
		})
	}
	return out, nil
}
