// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// vipSourceKind — тип источника VIP одного семейства (VipSource oneof).
type vipSourceKind int

const (
	srcSubnetAuto  vipSourceKind = iota // subnet_id → auto-аллокация internal Address
	srcPublicAuto                       // public {} → платформенный public Address
	srcAddressLink                      // address_id → линк существующего Address
)

// String — читаемая метка для server-логов (иначе в структурированном логе
// остаётся голое число, по которому полосу не восстановить).
func (k vipSourceKind) String() string {
	switch k {
	case srcSubnetAuto:
		return "subnet_auto"
	case srcPublicAuto:
		return "public_auto"
	case srcAddressLink:
		return "address_link"
	}
	return "unknown"
}

// familyVIPSpec — разобранный + резолвнутый per-family источник VIP.
type familyVIPSpec struct {
	family    domain.IPVersion
	kind      vipSourceKind
	subnetID  string // srcSubnetAuto — подсеть, из которой аллоцируется VIP
	addressID string // srcAddressLink — существующий Address
	// networkID — derived сеть семейства (INTERNAL): подсеть auto либо подсеть
	// linked-адреса. Пусто для EXTERNAL/public (сети нет). Используется для
	// dualstack same-network инварианта.
	networkID string
	// zoneID — derived зона семейства (INTERNAL ZONAL): zone_id резолвнутой подсети
	// (auto либо подсеть linked-адреса). Пусто для REGIONAL/anycast (подсеть
	// zone_id не несёт) и EXTERNAL/public. Используется для dualstack same-zone
	// инварианта (placement-coherence: обе VIP-семьи ZONAL LB в ОДНОЙ зоне).
	zoneID string
}

// crossZoneZonalMsg — verbatim contract text (NLB-1-16) when cross_zone_enabled is
// set true on a ZONAL placement. Part of the API contract (stable tone).
const crossZoneZonalMsg = "crossZoneEnabled is not applicable to ZONAL placement"

// resolvePlacementAuthoritative — NLB CONTRACT (F2 / NLB-1-08): `placement` is the
// SOLE authoritative mode input. type/placement_type are derived output-only —
// writing either in Create is an EXPLICIT reject (not silent-ignore), even when a
// legacy value would be consistent. placement is required; it drives the derived
// (type, placement_type) pair persisted for read. "external+zonal" is inexpressible
// by construction (no such placement enum value).
func resolvePlacementAuthoritative(
	req *lbv1.CreateNetworkLoadBalancerRequest,
) (domain.LBType, domain.PlacementType, domain.Placement, error) {
	if req.GetType() != lbv1.NetworkLoadBalancer_TYPE_UNSPECIFIED {
		return "", "", "", derivedModeInputErr("type")
	}
	if req.GetPlacementType() != lbv1.NetworkLoadBalancer_PLACEMENT_TYPE_UNSPECIFIED {
		return "", "", "", derivedModeInputErr("placement_type")
	}
	mode := placementModeFromPb(req.GetPlacement())
	if mode == "" {
		return "", "", "", errInvalidArg("placement",
			"is required — the load balancer mode is set solely by placement (EXTERNAL_REGIONAL, INTERNAL_REGIONAL, or INTERNAL_ZONAL)")
	}
	lbType, pt := domain.TypeAndPlacementTypeFromPlacement(mode)
	return lbType, pt, mode, nil
}

// derivedModeInputErr — NLB-1-08 reject: type/placement_type are derived output-only;
// the load balancer mode is set solely by the placement input. The message names
// placement as the single source of the mode discriminator.
func derivedModeInputErr(field string) error {
	return errInvalidArg(field,
		"is derived output-only; the load balancer mode is set solely by placement")
}

// foreignVipRef — проверка ФОРМЫ ЗАПРОСА для ссылки VIP-источника на ЧУЖОЙ
// (vpc-owned) ресурс. Существование, тип и placement решает ВЛАДЕЛЕЦ
// (peer-validate в resolveOneSource/resolveLinkedAddress) — здесь только два
// вопроса о самом запросе:
//
//  1. **ссылка названа.** Ветка oneof выбрана — значит caller обязался дать
//     ссылку; пустая строка означает, что спрашивать владельца не о чем. Без
//     этой проверки пустой id доезжал до peer-адаптера и возвращался как
//     `"subnet  not found"` — контракт-тон miss'а с вырезанным id, то есть
//     утверждение об отсутствии ресурса, который caller не называл.
//  2. **строка вообще является id Kachō.** `corevalidate.ResourceID`
//     **family-agnostic по контракту**: аргумент `expectedPrefix` НЕ сверяется
//     (см. его godoc) — проверяется лишь то, что первый сегмент входит в
//     ПЛАТФОРМЕННЫЙ каталог префиксов (`ids.KnownPrefixes()` /
//     `ids.KnownHyphenPrefixes()` + config-extras). Это НЕ приватный словарь
//     vpc: каталог принадлежит corelib и общий для всех сервисов, поэтому
//     дрейфа «vpc сменил префикс — nlb отвергает валидный id» здесь нет. Тип
//     ресурса локально НЕ утверждается: id с чужим для подсети префиксом
//     проходит и адресуется владельцу (B4 «чужой prefix — не наш словарь»).
//     `ids.PrefixSubnet`/`ids.PrefixAddress` остаются в вызове как документация
//     ожидаемого семейства — они ничего не гейтят.
//
// Осознанное отступление от буквы B4 («foreign id — existence-only») и его
// обоснование записаны в `docs/architecture/08-known-divergences.md`
// §«Формат чужого id (VIP-источники) проверяется синхронно — узкое, записанное
// отступление от B4»; carve-out — в самом B4 `api-conventions.md`. Коротко:
// быстрый терминальный 400 на явно
// не-id лучше служит вызывающему, чем поход к соседу — он остаётся терминальным
// и когда vpc недоступен (иначе caller получил бы retryable `UNAVAILABLE` на
// ввод, который не станет валидным никогда).
func foreignVipRef(field, resourceType, expectedFamilyPrefix, id string) error {
	if id == "" {
		return errInvalidArg(field, "required")
	}
	return corevalidate.ResourceID(resourceType, expectedFamilyPrefix, id)
}

// resolveVipSources — VipSource v4/v6 → упорядоченный набор familyVIPSpec. ≥1
// семейство обязательно; форма ссылок subnet_id/address_id проверяется
// синхронно (foreignVipRef), существование — владельцем.
func resolveVipSources(v4, v6 *lbv1.VipSource) ([]familyVIPSpec, error) {
	var out []familyVIPSpec
	add := func(family domain.IPVersion, src *lbv1.VipSource) error {
		if src == nil || src.GetSource() == nil {
			return nil
		}
		fs := familyVIPSpec{family: family}
		switch s := src.GetSource().(type) {
		case *lbv1.VipSource_SubnetId:
			if err := foreignVipRef(familyTag(family)+"_source.subnet_id",
				"subnet", ids.PrefixSubnet, s.SubnetId); err != nil {
				return err
			}
			fs.kind = srcSubnetAuto
			fs.subnetID = s.SubnetId
		case *lbv1.VipSource_AddressId:
			if err := foreignVipRef(familyTag(family)+"_source.address_id",
				"address", ids.PrefixAddress, s.AddressId); err != nil {
				return err
			}
			fs.kind = srcAddressLink
			fs.addressID = s.AddressId
		case *lbv1.VipSource_Public:
			fs.kind = srcPublicAuto
		default:
			return status.Errorf(codes.InvalidArgument,
				"%s_source has no vip source set", familyTag(family))
		}
		out = append(out, fs)
		return nil
	}
	if err := add(domain.IPVersionV4, v4); err != nil {
		return nil, err
	}
	if err := add(domain.IPVersionV6, v6); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, status.Error(codes.InvalidArgument,
			"load balancer must declare a vip source for at least one ip family")
	}
	return out, nil
}

// validateSourceTypeMatrix — subnet_id ⟹ INTERNAL; public {} ⟹ EXTERNAL.
// Несоответствие → каноничный field-текст (не generic — это форма запроса, не oracle).
func validateSourceTypeMatrix(specs []familyVIPSpec, lbType domain.LBType) error {
	for _, fs := range specs {
		switch fs.kind {
		case srcSubnetAuto:
			if lbType != domain.LBTypeInternal {
				return status.Error(codes.InvalidArgument,
					"subnet address source is only valid for INTERNAL load balancer")
			}
		case srcPublicAuto:
			if lbType != domain.LBTypeExternal {
				return status.Error(codes.InvalidArgument,
					"public address source is only valid for EXTERNAL load balancer")
			}
		}
	}
	return nil
}

// familiesFromSpecs — список заявленных семейств в порядке fan-out.
func familiesFromSpecs(specs []familyVIPSpec) []domain.IPVersion {
	out := make([]domain.IPVersion, 0, len(specs))
	for _, fs := range specs {
		out = append(out, fs.family)
	}
	return out
}

// familyTag — короткий тег семейства ("v4"/"v6").
func familyTag(family domain.IPVersion) string {
	if family == domain.IPVersionV6 {
		return "v6"
	}
	return "v4"
}
