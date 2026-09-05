// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

// Ссылка статического маршрута на шлюз РЕЗОЛВИТСЯ, и решает это оператор записи,
// а не проверка перед ним.
//
// Проверяется против настоящего Postgres, потому что предмет — поведение
// операторов и внешних ключей: подставной репозиторий утверждал бы о собственной
// копии правил, а не о тех, что исполняются.
//
// Четыре полосы и положительный контроль к каждой:
//   - шлюза нет → NOT_FOUND контрактным тоном (own-owned id, direct-read);
//   - шлюз в другой сети → FAILED_PRECONDITION;
//   - зона якоря шлюза не совпадает с зоной подсети, пользующейся таблицей →
//     FAILED_PRECONDITION; РЕГИОНАЛЬНАЯ (anycast) подсеть из зональной сверки
//     исключена by construction — отдельная проба;
//   - вид шлюза не обслуживает семейство назначения → FAILED_PRECONDITION;
//   - шлюз, названный живым маршрутом, не удаляется (обратное направление FK).

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// gwRefFixture — минимальная сеть с зональной подсетью-якорем и таблицей
// маршрутизации, к которой подсеть привязана.
type gwRefFixture struct {
	repo      kacho.Repository
	projectID string
	networkID string
	subnetID  string
	rtID      string
	addrSeq   int
}

func newGwRefFixture(ctx context.Context, t *testing.T) *gwRefFixture {
	t.Helper()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)
	t.Cleanup(r.Close)

	f := &gwRefFixture{repo: r, projectID: "prj-gwref"}
	f.networkID = ids.NewID(ids.PrefixNetwork)
	f.rtID = ids.NewID(ids.PrefixRouteTable)
	f.subnetID = ids.NewID(ids.PrefixSubnet)

	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		if _, e := w.Networks().Insert(ctx, &domain.Network{
			ID: f.networkID, ProjectID: f.projectID, Name: domain.RcNameVPC("net-gwref"),
		}); e != nil {
			return e
		}
		if _, e := w.RouteTables().Insert(ctx, &domain.RouteTable{
			ID: f.rtID, ProjectID: f.projectID, Name: domain.RcNameVPC("rt-gwref"),
			NetworkID: f.networkID,
		}); e != nil {
			return e
		}
		_, e := w.Subnets().Insert(ctx, &domain.Subnet{
			ID: f.subnetID, ProjectID: f.projectID, Name: domain.RcNameVPC("sub-gwref"),
			NetworkID: f.networkID, PlacementType: domain.PlacementZonal, ZoneID: "zone-a",
			V4CidrBlocks: []string{"10.80.0.0/24"}, RouteTableID: f.rtID,
		})
		return e
	}))
	return f
}

// anchorSubnet заводит ещё одну подсеть-якорь (в этой же или другой сети/зоне) и
// возвращает её id. `v4`/`v6` решают, какой вид шлюза она в состоянии удержать.
func (f *gwRefFixture) anchorSubnet(ctx context.Context, t *testing.T, networkID, name, zone string, v4, v6 []string) string {
	t.Helper()
	id := ids.NewID(ids.PrefixSubnet)
	require.NoError(t, legacyWithTx(t, ctx, f.repo, func(w kacho.RepositoryWriter) error {
		_, e := w.Subnets().Insert(ctx, &domain.Subnet{
			ID: id, ProjectID: f.projectID, Name: domain.RcNameVPC(name),
			NetworkID: networkID, PlacementType: domain.PlacementZonal, ZoneID: zone,
			V4CidrBlocks: v4, V6CidrBlocks: v6,
		})
		return e
	}))
	return id
}

// gateway заводит шлюз названного вида на названном якоре.
//
// Шлюзу ТРАНСЛЯЦИИ здесь же выделяется внешний адрес: без него строка не
// записывается вовсе (`gateways_nat_has_address_chk`, миграция 0038), и это
// не формальность фикстуры, а тот же инвариант, что держит продукт — вид и
// адрес связаны биусловием в обе стороны. Фикстура, обходящая его вставкой
// «попроще», была бы снисходительнее продукта и прятала бы ровно тот дефект,
// ради которого её подставляют. Адрес заводится напрямую, а не выделением из
// пула: предмет этих проб — резолв ссылки маршрута на шлюз, и заводить ради
// него пул зоны значило бы тянуть в пробу чужой механизм.
func (f *gwRefFixture) gateway(ctx context.Context, t *testing.T, name, subnetID string, kind domain.GatewayType) string {
	t.Helper()
	id := ids.NewID(ids.PrefixGateway)
	addrID := ""
	if kind == domain.GatewayTypeNat {
		addrID = f.externalAddress(ctx, t, name)
	}
	require.NoError(t, legacyWithTx(t, ctx, f.repo, func(w kacho.RepositoryWriter) error {
		_, e := w.Gateways().Insert(ctx, &domain.Gateway{
			ID: id, ProjectID: f.projectID, Name: domain.RcNameVPC(name),
			GatewayType: kind, SubnetID: subnetID, ExternalAddressID: addrID,
		})
		return e
	}))
	return id
}

// externalAddress — внешний адрес под шлюз трансляции. Каждый свой: один адрес
// два шлюза не обслуживает, и проба на это опирается.
func (f *gwRefFixture) externalAddress(ctx context.Context, t *testing.T, forGateway string) string {
	t.Helper()
	id := ids.NewID(ids.PrefixAddress)
	f.addrSeq++
	require.NoError(t, legacyWithTx(t, ctx, f.repo, func(w kacho.RepositoryWriter) error {
		_, e := w.Addresses().Insert(ctx, &domain.Address{
			ID: id, ProjectID: f.projectID,
			Name:      domain.RcNameVPC("addr-" + forGateway),
			Type:      domain.AddressTypeExternal,
			IpVersion: domain.IpVersionIPv4,
			ExternalIpv4: &domain.ExternalIpv4Spec{
				Address: fmt.Sprintf("203.0.113.%d", f.addrSeq),
				ZoneID:  "zone-a",
			},
		})
		return e
	}))
	return id
}

// writeRoutes переписывает набор маршрутов таблицы — тот же путь, которым идёт
// Update: набор несётся целиком, аддитивного глагола у маршрутов нет.
func (f *gwRefFixture) writeRoutes(ctx context.Context, t *testing.T, rtID string, routes []domain.StaticRoute) error {
	t.Helper()
	return legacyWithTx(t, ctx, f.repo, func(w kacho.RepositoryWriter) error {
		rec, e := w.RouteTables().GetForUpdate(ctx, rtID)
		if e != nil {
			return e
		}
		rec.RouteTable.StaticRoutes = routes
		_, e = w.RouteTables().Update(ctx, &rec.RouteTable)
		return e
	})
}

func viaGatewayRoute(prefix, gatewayID string) []domain.StaticRoute {
	return []domain.StaticRoute{{DestinationPrefix: prefix, GatewayID: gatewayID}}
}

// TestRouteTableGatewayRef_Resolves — положительный контроль всех отрицаний ниже:
// когерентная ссылка проходит и материализуется нормализованной строкой.
func TestRouteTableGatewayRef_Resolves(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	f := newGwRefFixture(ctx, t)
	// Якорь шлюза — та же сеть, ТА ЖЕ зона, что у подсети таблицы, IPv4-блок.
	anchor := f.anchorSubnet(ctx, t, f.networkID, "sub-anchor-ok", "zone-a", []string{"10.80.1.0/24"}, nil)
	gwID := f.gateway(ctx, t, "gw-ok", anchor, domain.GatewayTypeNat)

	require.NoError(t, f.writeRoutes(ctx, t, f.rtID, viaGatewayRoute("0.0.0.0/0", gwID)))

	// Ссылка нормализована: она и есть то, чем держится существование шлюза.
	rd, err := f.repo.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	got, err := rd.RouteTables().Get(ctx, f.rtID)
	require.NoError(t, err)
	require.Len(t, got.StaticRoutes, 1)
	assert.Equal(t, gwID, got.StaticRoutes[0].GatewayID)
	assert.Empty(t, got.StaticRoutes[0].NextHopAddress,
		"ветвь следующего узла ровно одна — адрес остаётся пустым")
}

// TestRouteTableGatewayRef_AbsentGatewayIsNotFound — шлюза нет: own-owned id,
// полоса direct-read, контрактный тон.
func TestRouteTableGatewayRef_AbsentGatewayIsNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	f := newGwRefFixture(ctx, t)
	absent := ids.NewID(ids.PrefixGateway)

	err := f.writeRoutes(ctx, t, f.rtID, viaGatewayRoute("0.0.0.0/0", absent))
	require.Error(t, err)
	assert.ErrorIs(t, err, helpers.ErrNotFound)
	assert.Contains(t, err.Error(), "Gateway "+absent+" not found")
}

// TestRouteTableGatewayRef_ForeignNetworkRefused — якорь шлюза в другой сети.
func TestRouteTableGatewayRef_ForeignNetworkRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	f := newGwRefFixture(ctx, t)

	otherNet := ids.NewID(ids.PrefixNetwork)
	require.NoError(t, legacyWithTx(t, ctx, f.repo, func(w kacho.RepositoryWriter) error {
		_, e := w.Networks().Insert(ctx, &domain.Network{
			ID: otherNet, ProjectID: f.projectID, Name: domain.RcNameVPC("net-other"),
		})
		return e
	}))
	anchor := f.anchorSubnet(ctx, t, otherNet, "sub-other", "zone-a", []string{"10.81.0.0/24"}, nil)
	gwID := f.gateway(ctx, t, "gw-other-net", anchor, domain.GatewayTypeNat)

	err := f.writeRoutes(ctx, t, f.rtID, viaGatewayRoute("0.0.0.0/0", gwID))
	require.Error(t, err)
	assert.ErrorIs(t, err, helpers.ErrFailedPrecondition)
	assert.Contains(t, err.Error(), "static_routes[0].gateway_id")
	assert.Contains(t, err.Error(), "is attached to another network")
}

// TestRouteTableGatewayRef_ZoneMismatchRefused — зональная сверка: и якорь шлюза,
// и подсеть таблицы зональны, зоны разные.
func TestRouteTableGatewayRef_ZoneMismatchRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	f := newGwRefFixture(ctx, t)
	anchor := f.anchorSubnet(ctx, t, f.networkID, "sub-anchor-zb", "zone-b", []string{"10.82.0.0/24"}, nil)
	gwID := f.gateway(ctx, t, "gw-zone-b", anchor, domain.GatewayTypeNat)

	err := f.writeRoutes(ctx, t, f.rtID, viaGatewayRoute("0.0.0.0/0", gwID))
	require.Error(t, err)
	assert.ErrorIs(t, err, helpers.ErrFailedPrecondition)
	assert.Contains(t, err.Error(), "Gateway is in zone zone-b, route table subnet zone is zone-a")
}

// TestRouteTableGatewayRef_RegionalTableSubnetOutOfZonalCheck — anycast-подсеть
// таблицы зоны не несёт, поэтому сравнивать не с чем: зональная полоса исключена
// BY CONSTRUCTION, и ссылка на шлюз из ЛЮБОЙ зоны той же сети проходит.
//
// Проба обязательна рядом с предыдущей: без неё «зоны не совпали» зеленело бы и
// на реализации, отвергающей вообще всякий зональный якорь.
func TestRouteTableGatewayRef_RegionalTableSubnetOutOfZonalCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	f := newGwRefFixture(ctx, t)

	// Отдельная таблица, к которой привязана ТОЛЬКО региональная подсеть.
	rtRegional := ids.NewID(ids.PrefixRouteTable)
	subRegional := ids.NewID(ids.PrefixSubnet)
	require.NoError(t, legacyWithTx(t, ctx, f.repo, func(w kacho.RepositoryWriter) error {
		if _, e := w.RouteTables().Insert(ctx, &domain.RouteTable{
			ID: rtRegional, ProjectID: f.projectID, Name: domain.RcNameVPC("rt-regional"),
			NetworkID: f.networkID,
		}); e != nil {
			return e
		}
		_, e := w.Subnets().Insert(ctx, &domain.Subnet{
			ID: subRegional, ProjectID: f.projectID, Name: domain.RcNameVPC("sub-regional"),
			NetworkID: f.networkID, PlacementType: domain.PlacementRegional, RegionID: "region-1",
			V4CidrBlocks: []string{"10.83.0.0/24"}, RouteTableID: rtRegional,
		})
		return e
	}))

	anchor := f.anchorSubnet(ctx, t, f.networkID, "sub-anchor-zc", "zone-c", []string{"10.84.0.0/24"}, nil)
	gwID := f.gateway(ctx, t, "gw-zone-c", anchor, domain.GatewayTypeNat)

	require.NoError(t, f.writeRoutes(ctx, t, rtRegional, viaGatewayRoute("0.0.0.0/0", gwID)),
		"anycast-подсеть зоны не несёт — зональная сверка не применяется")
}

// TestRouteTableGatewayRef_FamilyMismatchRefused — вид шлюза обслуживает одно
// семейство, назначение маршрута — другое.
func TestRouteTableGatewayRef_FamilyMismatchRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	f := newGwRefFixture(ctx, t)
	anchor := f.anchorSubnet(ctx, t, f.networkID, "sub-anchor-v6", "zone-a",
		[]string{"10.85.0.0/24"}, []string{"2001:db8:85::/64"})
	natID := f.gateway(ctx, t, "gw-nat", anchor, domain.GatewayTypeNat)
	eoID := f.gateway(ctx, t, "gw-eo", anchor, domain.GatewayTypeEgressOnly)

	// Положительные контроли: каждое семейство через свой вид проходит.
	require.NoError(t, f.writeRoutes(ctx, t, f.rtID, viaGatewayRoute("0.0.0.0/0", natID)))
	require.NoError(t, f.writeRoutes(ctx, t, f.rtID, viaGatewayRoute("::/0", eoID)))

	err := f.writeRoutes(ctx, t, f.rtID, viaGatewayRoute("::/0", natID))
	require.Error(t, err)
	assert.ErrorIs(t, err, helpers.ErrFailedPrecondition)
	assert.Contains(t, err.Error(), "does not serve IPv6 destinations")

	err = f.writeRoutes(ctx, t, f.rtID, viaGatewayRoute("0.0.0.0/0", eoID))
	require.Error(t, err)
	assert.ErrorIs(t, err, helpers.ErrFailedPrecondition)
	assert.Contains(t, err.Error(), "does not serve IPv4 destinations")
}

// TestRouteTableGatewayRef_NamedGatewayIsNotDeletable — обратное направление того
// же внешнего ключа. До миграции 0030 на `gateways` не ссылался НИ ОДИН внешний
// ключ, поэтому ветвь «gateway is in use» в `gatewayWriter.Delete` была
// недостижима и защищала от того, чего не бывает.
func TestRouteTableGatewayRef_NamedGatewayIsNotDeletable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	f := newGwRefFixture(ctx, t)
	anchor := f.anchorSubnet(ctx, t, f.networkID, "sub-anchor-del", "zone-a", []string{"10.86.0.0/24"}, nil)
	gwID := f.gateway(ctx, t, "gw-del", anchor, domain.GatewayTypeNat)

	// Положительный контроль: пока маршрут его не называет — шлюз удаляем.
	spare := f.gateway(ctx, t, "gw-spare", anchor, domain.GatewayTypeNat)
	require.NoError(t, legacyWithTx(t, ctx, f.repo, func(w kacho.RepositoryWriter) error {
		return w.Gateways().Delete(ctx, spare)
	}))

	require.NoError(t, f.writeRoutes(ctx, t, f.rtID, viaGatewayRoute("0.0.0.0/0", gwID)))

	err := legacyWithTx(t, ctx, f.repo, func(w kacho.RepositoryWriter) error {
		return w.Gateways().Delete(ctx, gwID)
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, helpers.ErrFailedPrecondition)
	assert.Contains(t, err.Error(), "gateway is in use")

	// Снятие маршрута снимает и ссылку — шлюз снова удаляем.
	require.NoError(t, f.writeRoutes(ctx, t, f.rtID, nil))
	require.NoError(t, legacyWithTx(t, ctx, f.repo, func(w kacho.RepositoryWriter) error {
		return w.Gateways().Delete(ctx, gwID)
	}))
}

// TestGatewayAnchorFamilyEnforcedOnInsert — вид шлюза сверяется с якорем ВНУТРИ
// вставки: NAT требует IPv4-блока у подсети, «только исход» — IPv6.
func TestGatewayAnchorFamilyEnforcedOnInsert(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	f := newGwRefFixture(ctx, t)
	v4only := f.anchorSubnet(ctx, t, f.networkID, "sub-v4", "zone-a", []string{"10.87.0.0/24"}, nil)

	// Положительный контроль: NAT в подсети с IPv4 создаётся.
	_ = f.gateway(ctx, t, "gw-v4-ok", v4only, domain.GatewayTypeNat)

	err := legacyWithTx(t, ctx, f.repo, func(w kacho.RepositoryWriter) error {
		_, e := w.Gateways().Insert(ctx, &domain.Gateway{
			ID: ids.NewID(ids.PrefixGateway), ProjectID: f.projectID,
			Name: domain.RcNameVPC("gw-eo-on-v4"), GatewayType: domain.GatewayTypeEgressOnly,
			SubnetID: v4only,
		})
		return e
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, helpers.ErrFailedPrecondition)
	assert.Contains(t, err.Error(), "has no IPv6 CIDR block")

	// Несуществующая подсеть — полоса direct-read своего id.
	absent := ids.NewID(ids.PrefixSubnet)
	err = legacyWithTx(t, ctx, f.repo, func(w kacho.RepositoryWriter) error {
		_, e := w.Gateways().Insert(ctx, &domain.Gateway{
			ID: ids.NewID(ids.PrefixGateway), ProjectID: f.projectID,
			Name: domain.RcNameVPC("gw-absent-anchor"), GatewayType: domain.GatewayTypeNat,
			// Адрес выдан НАМЕРЕННО: без него первым отвечает биусловие «вид ↔
			// адрес», и проба зеленела бы на чужой полосе, ничего не сказав о той,
			// которую называет её имя.
			ExternalAddressID: f.externalAddress(ctx, t, "gw-absent-anchor"),
			SubnetID:          absent,
		})
		return e
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, helpers.ErrNotFound)
	assert.Contains(t, err.Error(), "Subnet "+absent+" not found")

	// Подсеть ЧУЖОГО проекта отвечает ДОСЛОВНО тем же — иначе это оракул
	// существования чужого ресурса.
	foreignNet := ids.NewID(ids.PrefixNetwork)
	foreignSub := ids.NewID(ids.PrefixSubnet)
	require.NoError(t, legacyWithTx(t, ctx, f.repo, func(w kacho.RepositoryWriter) error {
		if _, e := w.Networks().Insert(ctx, &domain.Network{
			ID: foreignNet, ProjectID: "prj-foreign", Name: domain.RcNameVPC("net-foreign"),
		}); e != nil {
			return e
		}
		_, e := w.Subnets().Insert(ctx, &domain.Subnet{
			ID: foreignSub, ProjectID: "prj-foreign", Name: domain.RcNameVPC("sub-foreign"),
			NetworkID: foreignNet, PlacementType: domain.PlacementZonal, ZoneID: "zone-a",
			V4CidrBlocks: []string{"10.88.0.0/24"},
		})
		return e
	}))
	err = legacyWithTx(t, ctx, f.repo, func(w kacho.RepositoryWriter) error {
		_, e := w.Gateways().Insert(ctx, &domain.Gateway{
			ID: ids.NewID(ids.PrefixGateway), ProjectID: f.projectID,
			Name: domain.RcNameVPC("gw-foreign-anchor"), GatewayType: domain.GatewayTypeNat,
			// Адрес выдан НАМЕРЕННО: без него первым отвечает биусловие «вид ↔
			// адрес», и проба зеленела бы на чужой полосе, ничего не сказав о той,
			// которую называет её имя.
			ExternalAddressID: f.externalAddress(ctx, t, "gw-foreign-anchor"),
			SubnetID:          foreignSub,
		})
		return e
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, helpers.ErrNotFound)
	assert.Contains(t, err.Error(), "Subnet "+foreignSub+" not found")
}
