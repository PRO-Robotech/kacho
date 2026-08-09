// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	kerrors "github.com/PRO-Robotech/kacho/pkg/errors"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/shared"
	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// errSubnetRegionUnverifiable — регион подсети неустановим: denormalised mirror
// `Subnet.RegionID` пуст (zone→region резолвер не сконфигурирован или geo не
// ответил). Мутация с непроверяемым предусловием — **fail-closed** (UNAVAILABLE,
// retryable), НЕ «считаем совпавшим»: иначе отсутствие geo-ребра молча
// отключало бы region-coherence (data-integrity.md §Placement-coherence,
// "cross-service — peer-validate на request-path, fail-closed").
var errSubnetRegionUnverifiable = status.Error(codes.Unavailable, "subnet region lookup unavailable")

// subnetRegionCoherent — региональная проверка для caller-supplied subnet_id
// (descriptive текст — форма запроса, не oracle). Несовпадение → InvalidArgument
// со стабильным контракт-текстом (data-integrity.md §Placement-coherence).
func subnetRegionCoherent(sn *vpcclient.Subnet, lbRegion domain.RegionID) error {
	if sn == nil || sn.RegionID == "" {
		return errSubnetRegionUnverifiable
	}
	if sn.RegionID != string(lbRegion) {
		return status.Error(codes.InvalidArgument,
			"load balancer vip subnet must be in the same region as the load balancer")
	}
	return nil
}

// subnetRegionCoherentOpaque — та же региональная проверка на linked-address
// полосе: анти-oracle (не подтверждаем placement чужого адреса) → generic
// "Illegal argument addressId". Неустановимый регион — по-прежнему fail-closed.
func subnetRegionCoherentOpaque(sn *vpcclient.Subnet, lbRegion domain.RegionID) error {
	if sn == nil || sn.RegionID == "" {
		return errSubnetRegionUnverifiable
	}
	if sn.RegionID != string(lbRegion) {
		return status.Error(codes.InvalidArgument, "Illegal argument addressId")
	}
	return nil
}

// subnetPlacementMatches — placement подсети (vpc) == placement LB (domain).
func subnetPlacementMatches(subnetPlacement string, lbPlacement domain.PlacementType) bool {
	switch lbPlacement {
	case domain.PlacementRegional:
		return subnetPlacement == vpcclient.SubnetPlacementRegional
	case domain.PlacementZonal:
		return subnetPlacement != vpcclient.SubnetPlacementRegional && subnetPlacement != ""
	}
	return false
}

// allocAcquireErr — by-lane маппинг ошибок auto-аллокации VIP.
//
// `subnetRef` — caller-supplied `v4Source/v6Source.subnetId` (пусто на public-
// полосе, где ссылку выбирает платформа, а не tenant).
//
// Три полосы (api-conventions §By-lane code-split):
//
//   - peer недоступен            → UNAVAILABLE (fail-closed, retryable);
//   - ССЫЛКА не резолвится, и она caller-supplied → FAILED_PRECONDITION тем же
//     текстом `"subnet %s not found"`, что и SYNC-precheck этого же RPC
//     (`subnetPeerErr`). Раскрытия нет by construction: sync-полоса уже
//     отвечает ровно это на тот же id в том же запросе. Молчать здесь —
//     doc-untruth: клиент-исправимая проблема ссылки выдавалась за ёмкость
//     платформы (и была неотличима от неё под cross-service read-your-writes,
//     когда подсеть только что создана и ещё не видна address-create-пути);
//   - всё остальное (ёмкость пула/подсети И peer-miss инфра-объекта на public-
//     полосе) → фиксированный непрозрачный текст. Ёмкость и наличие
//     AddressPool в underlay-зоне — инфра-чувствительные данные
//     (security.md), их не раскрываем НИКОГДА.
//
// Вызывающий ОБЯЗАН залогировать проглоченную причину (CWE-778): наружу она не
// уходит, поэтому без лога отказ аллокации неатрибутируем в проде.
func allocAcquireErr(err error, subnetRef string) error {
	switch {
	case errors.Is(err, domain.ErrUnavailable):
		return status.Error(codes.Unavailable, "load balancer address allocation unavailable")
	case errors.Is(err, domain.ErrNotFound) && subnetRef != "":
		return status.Errorf(codes.FailedPrecondition, "subnet %s not found", subnetRef)
	}
	return status.Error(codes.FailedPrecondition, "could not allocate load balancer address")
}

// linkAcquireErr — анти-oracle маппинг ошибок link-CAS (адрес занят → generic).
func linkAcquireErr(err error) error {
	if errors.Is(err, domain.ErrUnavailable) {
		return status.Error(codes.Unavailable, "address lookup unavailable")
	}
	return status.Error(codes.FailedPrecondition, "Illegal argument addressId")
}

// zonePeerErr — маппер зон для validateDisabledAnnounceZones/resolveSources.
func zonePeerErr(err error) error {
	if errors.Is(err, domain.ErrUnavailable) {
		return status.Error(codes.Unavailable, "zone lookup unavailable")
	}
	return status.Error(codes.InvalidArgument, "Illegal argument disabledAnnounceZones")
}

// peerErrToStatus — тонкий делегатор к единому `shared.PeerErrToStatus`
// (один источник истины для project/region precheck).
func peerErrToStatus(err error, kind, id string) error {
	return shared.PeerErrToStatus(err, kind, id)
}

// subnetPeerErr — sync-precheck subnet_id через vpc.SubnetService.Get.
func subnetPeerErr(err error, id string) error {
	switch {
	case errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrInvalidArg):
		return status.Errorf(codes.InvalidArgument, "subnet %s not found", id)
	case errors.Is(err, domain.ErrUnavailable):
		return status.Errorf(codes.Unavailable, "subnet lookup unavailable")
	}
	return status.Errorf(codes.Internal, "subnet lookup failed")
}

// linkedAddressErr — анти-oracle маппинг AddressService.Get в link-precheck.
//
// Две РАЗНЫЕ полосы, которые прежде схлопывались в одну:
//
//   - ссылка не резолвится у владельца (`ErrNotFound` — hide-existence NOT_FOUND
//     либо PERMISSION_DENIED от vpc) → `FAILED_PRECONDITION` +
//     `ErrorInfo.reason = PEER_RESOURCE_MISSING` (api-conventions §By-lane
//     code-split). Это ПЕРЕХОДНОЕ состояние на own-lane: адрес, только что
//     созданный самим вызывающим, невидим, пока не материализовался пообъектный
//     owner-tuple. Вызывающий закрывает окно bounded-ретраем — и различает полосу
//     МАШИННО по reason-token, а не разбором прозы;
//   - всё, что повтором не лечится (malformed id, ownership/family/kind/placement
//     mismatch) → терминальный `INVALID_ARGUMENT` БЕЗ token'а.
//
// Проза в обоих случаях одна и та же генерическая — анти-oracle сохранён: ни
// placement, ни ownership чужого адреса мы не подтверждаем. Token существования
// оракулом не делает: vpc отвечает вызывающему ровно тем же hide-existence на
// прямой `AddressService.Get` под его же личностью, то есть бит «мне этот id не
// виден» ему доступен и без нас. Схлопывание отнимало у него не секрет, а
// возможность отличить «повтори» от «исправь ввод».
func linkedAddressErr(err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return peerResourceMissing("vpc.address", "Illegal argument addressId")
	case errors.Is(err, domain.ErrInvalidArg):
		return status.Error(codes.InvalidArgument, "Illegal argument addressId")
	case errors.Is(err, domain.ErrUnavailable):
		return status.Error(codes.Unavailable, "address lookup unavailable")
	}
	return status.Error(codes.Internal, "address lookup failed")
}

// peerResourceMissing — полоса peer-validate «чужого ресурса нет у владельца».
//
// Форма поднята в общий фундамент (pkg/errors): код полосы, машинный признак в
// details, проза — та, что передана вызывающим. Здесь она намеренно
// ГЕНЕРИЧЕСКАЯ и остаётся такой: на этой полосе мы не подтверждаем ни
// существование, ни placement, ни принадлежность чужого адреса. По той же
// причине идентификатор в details НЕ едет — фундамент опускает пустое поле, а не
// шлёт пустую строку.
//
// Детали не влияют на HTTP-статус края (grpc-gateway отображает по КОДУ), но
// именно по ним клиент отличает переходную полосу от терминальной, не парся
// сообщение (api-conventions.md §By-lane code-split).
func peerResourceMissing(resourceType, msg string) error {
	return kerrors.ReasonPeerResourceMissing.Errf(
		kerrors.PeerRef{Service: "nlb", ResourceType: resourceType},
		"%s", msg)
}
