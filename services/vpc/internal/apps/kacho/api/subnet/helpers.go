// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"

	// Blank-import регистрирует трансферы Subnet/Address/time через init().
	_ "github.com/PRO-Robotech/kacho/services/vpc/internal/dto/toproto"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// marshalSubnetRecord конвертирует repo-entity Subnet в *anypb.Any через
// DTO-реестр. Worker'ы Create/Update/AddCidrBlocks/RemoveCidrBlocks кладут этим
// результат в Operation.response.
func marshalSubnetRecord(rec *kachorepo.SubnetRecord) (*anypb.Any, error) {
	var dst *vpcv1.Subnet
	if err := dto.Transfer(dto.FromTo(*rec, &dst)); err != nil {
		return nil, fmt.Errorf("dto.Transfer Subnet: %w", err)
	}
	return anypb.New(dst)
}

// ---- CIDR helpers ----

// validateSubnetV4CIDR — host-bits=0 (canonical form) + размер подсети внутри
// диапазона, обещанного контрактом с ДВУХ сторон («Minimum /28, maximum /16»,
// см. `cidr_bounds.go`). Префикс вне диапазона — короче `subnetV4PrefixLenMin`
// или длиннее `subnetV4PrefixLenMax` — → InvalidArgument
// "Illegal argument Invalid network prefix /<N>" с именем поля в деталях.
//
// Прежде исполнялась только одна сторона: длинный префикс отвергался, короткий
// проходил, и `/8` забирал у сети адресное пространство, которого контракт
// подсети не обещал.
//
// Значение ЧУЖОГО семейства эта проверка не судит (как и не судила): семейство
// решает сверка с супернетом сети — `prefixWithinAny` сопоставляет семейства и
// возвращает контрактный отказ «not within any network CIDR block».
func validateSubnetV4CIDR(field, value string) error {
	if err := validateCIDRPrefix(field, value); err != nil {
		return err
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return serviceerr.InvalidArg(field, field+" must be a valid CIDR (e.g. 10.0.0.0/24)")
	}
	if !prefix.Addr().Is4() {
		return nil
	}
	if prefix.Bits() < subnetV4PrefixLenMin || prefix.Bits() > subnetV4PrefixLenMax {
		// Текст — часть контракта и утверждается дословно (newman SUB-CR-BVA-CIDR-*),
		// поэтому он тот же, что был у единственной прежней границы. Добавлено
		// только имя поля в деталях: без него вызывающий читает «что-то не так с
		// запросом» и правит наугад.
		return serviceerr.InvalidArg(field,
			fmt.Sprintf("Illegal argument Invalid network prefix /%d", prefix.Bits()))
	}
	return nil
}

// validateSubnetV6CIDR — host-bits=0 + проверка, что префикс реально IPv6.
//
// Диапазона размера здесь нет, и это не пропуск: контракт границ для IPv6 не
// называет — ни у `ipv6_cidr_primary`, ни у `ipv6_cidr_blocks`. Граница,
// которую никто не обещал, не выдумывается; появление обещания в контракте
// краснит пробу паритета `TestSubnetCidrBoundsMatchTheContract`, и тогда оно и
// исполняется.
func validateSubnetV6CIDR(field, value string) error {
	if err := validateCIDRPrefix(field, value); err != nil {
		return err
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return serviceerr.InvalidArg(field, field+" must be a valid IPv6 CIDR (e.g. 2001:db8::/64)")
	}
	if !prefix.Addr().Is6() || prefix.Addr().Is4In6() {
		return serviceerr.InvalidArg(field, field+" must be an IPv6 CIDR (e.g. 2001:db8::/64)")
	}
	return nil
}

// validateCIDRPrefix проверяет, что value — валидный CIDR-prefix и host-bits=0.
func validateCIDRPrefix(field, value string) error {
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return serviceerr.InvalidArg(field, field+" must be a valid CIDR (e.g. 10.0.0.0/24)")
	}
	if prefix.Masked() != prefix {
		// Подсказываем точный masked-адрес сети той же family (v4 → напр.
		// 10.0.0.0/24, v6 → напр. 2001:db8::/64), а не жестко зашитый v4-пример.
		return serviceerr.InvalidArg(field,
			field+" must have zero host-bits (use the network address "+prefix.Masked().String()+")")
	}
	return nil
}

// validateSubnetWithinSupernet проверяет, что каждый CIDR-блок подсети (v4 и v6)
// — подмножество одного из объявленных супернет-блоков сети соответствующего
// семейства (redesign VPC-1 F7: Subnet.ipv4CidrPrimary ⊆ network.ipv4CidrBlocks).
// Валидируется within-service против network-строки в той же БД.
//
// Ограничение БЕЗУСЛОВНО и ни при каком состоянии сети не пропускается. Сеть, не
// объявившая супернет этого семейства, подсеть семейства не принимает: нарезать не
// из чего, и отказ называет путь вперёд (`:add-cidr-blocks`). Сеть, чьи объявленные
// блоки не разбираются, от необъявившей ничем не отличается — плана у неё тоже нет.
// Нарушение вложенности → INVALID_ARGUMENT с редизайн-текстом
// "subnet CIDR %s is not within any network CIDR block".
//
// Здесь стояла оговорка про совместимость с сетями без объявленного адресного
// пространства. Она не называла ни того, чья это совместимость, ни предиката
// снятия, и пережила бы любой повод: поле супернета не обязательно на создании
// сети, поэтому «пустой набор» — не край, а штатное состояние целого класса сетей,
// на котором ограничение просто не действовало.
func validateSubnetWithinSupernet(netV4, netV6, subV4, subV6 []string) error {
	if err := eachWithinSupernet(netV4, subV4, "IPv4", "ipv4CidrBlocks"); err != nil {
		return err
	}
	return eachWithinSupernet(netV6, subV6, "IPv6", "ipv6CidrBlocks")
}

// eachWithinSupernet — общая проверка одного семейства: каждый блок из blocks
// обязан лежать внутри одного из supernet-блоков.
//
// Ранний выход здесь ровно один и он на ПРЕДМЕТЕ (`blocks`): подсеть этого
// семейства не просят — отвергать нечего. Раннего выхода по ИСТОЧНИКУ
// (`supernet`, разобранный `supers`) нет и заводить его нельзя: он делает проверку
// тождественно-истинной ровно тогда, когда ограничивать и надо. Различие ролей —
// не стилистическое, оно держится гейтом `TestCheckNeverAcceptsBecauseItsConstraintIsEmpty`
// в `internal/repohygiene`.
func eachWithinSupernet(supernet, blocks []string, family, field string) error {
	if len(blocks) == 0 {
		// Подсеть этого семейства не просит — отсутствие супернета её не касается,
		// и отказ был бы про то, чего не спрашивали.
		return nil
	}
	if len(supernet) == 0 {
		// Здесь стоял пропуск проверки со ссылкой на совместимость. Он не защитим:
		// поле супернета НЕ обязательно на создании сети, поэтому пустой супернет —
		// штатное состояние, а не редкость, и ограничение, ради которого поле
		// существует, не действовало вовсе. Оговорка при этом не называла ни того,
		// чья это совместимость, ни предиката снятия, — послабление, не истекающее
		// само.
		//
		// Отказ, а не ослабление текста контракта: нарезать не из чего. Без
		// объявленного блока у сети нет адресного плана, и подсеть перестаёт быть
		// частью чего-либо. Путь вперёд уже поставлен и назван в самом отказе.
		return status.Errorf(codes.InvalidArgument,
			"network declares no %s supernet: add blocks via :add-cidr-blocks (%s) "+
				"before creating an %s subnet", family, field, family)
	}
	supers := make([]netip.Prefix, 0, len(supernet))
	for _, s := range supernet {
		p, perr := netip.ParsePrefix(s)
		if perr != nil {
			continue // malformed supernet-блок сети (валидируется на Network.Create) — не учитываем
		}
		supers = append(supers, p.Masked())
	}
	// Здесь стоял второй ранний выход — по разобранному набору. Он повторял снятый
	// пропуск на шаг ниже и был невидим: список блоков непуст, а сравнивать не с
	// чем. Теперь нечитаемый план разбирается тем же циклом, что и невложенный
	// блок, и вызывающий получает тот же контрактный тон.
	for _, b := range blocks {
		inner, perr := netip.ParsePrefix(b)
		if perr != nil {
			continue // CIDR-формат блока подсети валидируется выше по стеку
		}
		if !prefixWithinAny(inner.Masked(), supers) {
			return status.Errorf(codes.InvalidArgument,
				"subnet CIDR %s is not within any network CIDR block", b)
		}
	}
	return nil
}

// prefixWithinAny — true, если inner ⊆ хотя бы одного outer того же семейства.
// inner ⊆ outer ⟺ outer не длиннее inner И outer содержит сетевой адрес inner.
func prefixWithinAny(inner netip.Prefix, supers []netip.Prefix) bool {
	for _, outer := range supers {
		if outer.Addr().Is4() != inner.Addr().Is4() {
			continue
		}
		if outer.Bits() <= inner.Bits() && outer.Contains(inner.Addr()) {
			return true
		}
	}
	return false
}

// prefixesOverlap возвращает true если два CIDR-блока пересекаются.
func prefixesOverlap(a, b netip.Prefix) bool {
	if a.Addr().Is4() != b.Addr().Is4() {
		return false
	}
	if a.Contains(b.Addr()) || b.Contains(a.Addr()) {
		return true
	}
	return false
}

// validateSubnetCidrCardinality — потолок числа диапазонов подсети НА СЕМЕЙСТВО.
// Стоит ПЕРЕД попарной проверкой пересечений, потому что та квадратична по
// величине, которую выбирает вызывающий, и не читает контекст (отмена запроса
// её не останавливает). Проверяется набор, который БУДЕТ записан, — иначе он
// растёт серией формально законных запросов. Сужение набора (:removeCidrBlocks)
// потолком не гейтится: оно обязано проходить всегда. DB-CHECK
// subnets_cidr_blocks_cardinality — атомарный backstop.
func validateSubnetCidrCardinality(field string, cidrs []string) error {
	if len(cidrs) > domain.MaxSubnetCidrBlocks {
		return serviceerr.InvalidArg(field,
			fmt.Sprintf("at most %d CIDR blocks per subnet per family", domain.MaxSubnetCidrBlocks))
	}
	return nil
}

// checkCIDRDisjoint — sync-проверка, что массив CIDR не содержит пересекающихся.
// fieldPrefix — имя поля КОНТРАКТА для error-сообщений; приезжает от глагола
// (`cidr_fields.go`), а не выписывается здесь: у `Create` это `ipv4CidrPrimary`,
// у `:addCidrBlocks` — `ipv4CidrBlocks`.
func checkCIDRDisjoint(fieldPrefix string, cidrs []string) error {
	prefixes := make([]netip.Prefix, 0, len(cidrs))
	for i, c := range cidrs {
		pr, err := netip.ParsePrefix(c)
		if err != nil {
			return serviceerr.InvalidArg(fmt.Sprintf("%s[%d]", fieldPrefix, i), "must be valid CIDR")
		}
		prefixes = append(prefixes, pr)
	}
	for i := 0; i < len(prefixes); i++ {
		for j := i + 1; j < len(prefixes); j++ {
			if prefixesOverlap(prefixes[i], prefixes[j]) {
				return status.Errorf(codes.FailedPrecondition, "Subnet CIDRs can not overlap")
			}
		}
	}
	return nil
}

// appendDedup добавляет элементы src в dst, пропуская уже присутствующие в dst.
func appendDedup(dst, src []string) []string {
	seen := make(map[string]struct{}, len(dst))
	for _, v := range dst {
		seen[v] = struct{}{}
	}
	for _, v := range src {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		dst = append(dst, v)
	}
	return dst
}

// subtractCIDRs возвращает existing без блоков из remove + сколько блоков было
// фактически удалено (для проверки "блок не найден").
func subtractCIDRs(existing, remove []string) ([]string, int) {
	toRemove := make(map[string]struct{}, len(remove))
	for _, c := range remove {
		toRemove[c] = struct{}{}
	}
	var remaining []string
	var removed int
	for _, e := range existing {
		if _, ok := toRemove[e]; ok {
			removed++
			continue
		}
		remaining = append(remaining, e)
	}
	return remaining, removed
}

// validateZoneID — sync-валидация zone_id: required + existence у владельца.
//
// Две РАЗНЫЕ полосы, и коды у них разные (api-conventions.md §By-lane):
//   - пустое/malformed значение — своя, синтаксическая: InvalidArgument с
//     FieldViolation;
//   - зона не резолвится у владельца — peer-validate: FailedPrecondition с
//     машинным признаком (serviceerr.UnknownZone), текст `unknown zone id '<id>'`.
//
// Любая другая ошибка → MapRepoErr (geo недоступен → Unavailable, fail-closed).
//
// `zr == nil` — безопасный fallback для тестов без zoneReg (existence не проверяем).
func validateZoneID(ctx context.Context, zr ZoneRegistry, field, zoneID string) error {
	if err := corevalidate.ZoneId(field, zoneID); err != nil {
		return err
	}
	if zr == nil {
		return nil
	}
	_, err := zr.Get(ctx, zoneID)
	if err == nil {
		return nil
	}
	if errors.Is(err, repo.ErrNotFound) {
		return serviceerr.UnknownZone(zoneID)
	}
	return serviceerr.MapRepoErr(err)
}

// validateRegionID — sync-валидация region_id REGIONAL-подсети: required +
// existence у owner-домена Geography (kacho-geo). Зеркало validateZoneID.
//
// Пустое значение → InvalidArgument `region_id is required` (своя полоса);
// несуществующий регион → FailedPrecondition `unknown region id '<regionId>'` с
// машинным признаком (peer-validate, serviceerr.UnknownRegion); geo недоступен →
// пробрасывается (Unavailable, fail-closed на мутации). `rr == nil` — fallback
// для тестов без regionReg (existence не проверяем).
func validateRegionID(ctx context.Context, rr RegionRegistry, field, regionID string) error {
	if regionID == "" {
		return serviceerr.InvalidArg(field, field+" is required")
	}
	if rr == nil {
		return nil
	}
	_, err := rr.Get(ctx, regionID)
	if err == nil {
		return nil
	}
	if errors.Is(err, repo.ErrNotFound) {
		return serviceerr.UnknownRegion(regionID)
	}
	return serviceerr.MapRepoErr(err)
}

// resolvePlacement выводит placementType° подсети (F6, redesign VPC-1): дискриминатор
// **server-derived, unwritable** из непустого zoneId XOR regionId. Возвращает выведенный
// дискриминатор (для записи в placement_type-колонку) либо sync-InvalidArgument.
//
//   - placementType задан клиентом (не UNSPECIFIED) → explicit reject (server-derived,
//     не silent-ignore — даже если значение «совпало бы» с выводимым);
//   - ровно один из zoneId/regionId непуст → derive ZONAL(zone)/REGIONAL(region) +
//     existence-валидация у owner-домена Geography (kacho-geo, fail-closed);
//   - оба заданы ИЛИ ни одного → InvalidArgument "exactly one of zone_id, region_id must be set".
//
// Та же биусловная форма закреплена DB-CHECK subnets_placement_payload_chk (backstop).
// Тексты — часть контракта (api-conventions §Error-format): field-refs в snake_case,
// как во всём vpc-сервисе (nlb/addresspool/routetable) — см. VPC-1 acceptance NB.
func resolvePlacement(ctx context.Context, zr ZoneRegistry, rr RegionRegistry, s domain.Subnet) (domain.SubnetPlacementType, error) {
	if s.PlacementType != domain.PlacementUnspecified {
		return "", status.Error(codes.InvalidArgument,
			"placement_type is server-derived; set zone_id or region_id instead")
	}
	hasZone := s.ZoneID != ""
	hasRegion := s.RegionID != ""
	if hasZone == hasRegion { // оба заданы ИЛИ ни одного
		return "", status.Error(codes.InvalidArgument, "exactly one of zone_id, region_id must be set")
	}
	if hasZone {
		if err := validateZoneID(ctx, zr, "zone_id", s.ZoneID); err != nil {
			return "", err
		}
		return domain.PlacementZonal, nil
	}
	if err := validateRegionID(ctx, rr, "region_id", s.RegionID); err != nil {
		return "", err
	}
	return domain.PlacementRegional, nil
}

// routeTableGetter — то, что нужно от репозитория для проверки ссылки на
// таблицу маршрутов: только чтение таблицы по id. Узкий порт вместо целого
// Writer/Reader — валидатор одинаково работает и на sync-пути (Reader), и в
// writer-TX (Writer видит свои записи).
type routeTableGetter interface {
	Get(ctx context.Context, id string) (*kachorepo.RouteTableRecord, error)
}

// validateSubnetRouteTableRef — существование И принадлежность таблицы
// маршрутов, названной вызывающим: таблица обязана лежать в ТОЙ ЖЕ сети, что и
// подсеть (а значит и в том же проекте — сеть у таблицы и у подсети одна).
// Внешний ключ этого выразить не может: он проверяет лишь наличие строки.
//
// Тон отказа для «нет такой» и «есть, но чужой сети» одинаков — иначе это
// оракул существования. Формат id проверяется тем же вызовом, что и у прочих
// ссылок, чтобы явный мусор получал терминальный отказ, а не «не найдено».
//
// Код и регистр — как у ссылки на значение поля (InvalidArgument, строчная
// буква), а не как у родительской сети подсети (NotFound, "Network %s not
// found"): предмет отказа — значение поля запроса, а не адресуемый ресурс.
// Прецеденты в сервисе разные; выбор осознанный, менять — через тикет.
func validateSubnetRouteTableRef(ctx context.Context, rtr routeTableGetter, routeTableID, networkID string) error {
	if routeTableID == "" {
		return nil
	}
	if err := corevalidate.ResourceID("route table", ids.PrefixRouteTable, routeTableID); err != nil {
		return err
	}
	rt, err := rtr.Get(ctx, routeTableID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return status.Errorf(codes.InvalidArgument, "route table %s not found", routeTableID)
		}
		return serviceerr.MapRepoErr(err)
	}
	if rt.NetworkID != networkID {
		return status.Errorf(codes.InvalidArgument, "route table %s not found", routeTableID)
	}
	return nil
}
