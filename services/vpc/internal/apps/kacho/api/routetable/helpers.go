// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package routetable

import (
	"fmt"
	"net/netip"

	"google.golang.org/protobuf/types/known/anypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
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
// целиком, аддитивного глагола у маршрутов нет (см. Handler.AddRoutes: отказ по
// имени). DB-CHECK route_tables_static_routes_cardinality (миграция 0028) —
// атомарный backstop на саму строку, независимо от writer'а.
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
		nhField := fmt.Sprintf("static_routes[%d].next_hop_address", i)
		if r.NextHopAddress == "" {
			return serviceerr.InvalidArg(nhField, nhField+" is required")
		}
		if _, err := netip.ParseAddr(r.NextHopAddress); err != nil {
			return serviceerr.InvalidArg(nhField, nhField+" must be a valid IP address (IPv4 or IPv6)")
		}
	}
	return nil
}

// staticRoutesFromProto — proto-маршруты в domain, с ЯВНЫМ отказом по ветке
// следующего перехода, которой у сервиса нет.
//
// `StaticRoute.next_hop` — oneof из двух ветвей: адрес и `gateway_id`. Реализован
// адрес. Пока разбор читал только его, выбранный вызывающим `gateway_id` не читал
// НИКТО (api-conventions.md, «Принято-и-проигнорировано — ЗАПРЕЩЕНО»): маршрут
// доезжал до валидации с пустым следующим переходом и получал отказ по имени
// СОСЕДНЕЙ ветки, которую вызывающий не посылал. Теперь ветка отвергается своим
// именем и синхронно — исход №2 правила.
//
// Поле оставлено на контракте намеренно: край REST молча выбрасывает неизвестные
// ключи тела, поэтому его удаление вернуло бы именованный отказ обратно в
// невнятный «next_hop_address is required». Тот же довод записан у compute над
// `CreateInstanceRequest.ssh_public_keys`.
func staticRoutesFromProto(in []*vpcv1.StaticRoute) ([]domain.StaticRoute, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]domain.StaticRoute, 0, len(in))
	for i, sr := range in {
		if _, ok := sr.GetNextHop().(*vpcv1.StaticRoute_GatewayId); ok {
			field := fmt.Sprintf("static_routes[%d].gateway_id", i)
			return nil, serviceerr.InvalidArg(field,
				field+" is not supported: route the next hop by IP address "+
					"(static_routes[].next_hop_address)")
		}
		out = append(out, domain.StaticRoute{
			Labels:            sr.Labels,
			DestinationPrefix: sr.GetDestinationPrefix(),
			NextHopAddress:    sr.GetNextHopAddress(),
		})
	}
	return out, nil
}
