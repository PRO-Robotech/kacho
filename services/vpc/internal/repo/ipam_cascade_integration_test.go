// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	addressapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/address"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/addresspool"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/cqrsadapter"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// TestIntegration_IPAM_Cascade поднимает реальный pgxpool + CQRS-repo и
// прогоняет резолв пула AddressPool end-to-end. Cascade: network_default →
// zone_default → global_default.
func TestIntegration_IPAM_Cascade(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	dsn := setupTestDB(t)

	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := kachopg.New(pool, nil)
	defer r.Close()

	withTx := func(t *testing.T, fn func(kacho.RepositoryWriter) error) error {
		t.Helper()
		w, err := r.Writer(ctx)
		require.NoError(t, err)
		if err := fn(w); err != nil {
			w.Abort()
			return err
		}
		return w.Commit()
	}

	const zone = "zone-a"

	mkPool := func(name, zoneID string, isDefault bool, cidr string) *domain.AddressPool {
		p := &domain.AddressPool{
			ID:           ids.NewID("apl"),
			Name:         domain.RcNameVPC(name),
			V4CIDRBlocks: []string{cidr},
			Kind:         domain.AddressPoolKindExternalPublic,
			ZoneID:       zoneID,
			IsDefault:    isDefault,
		}
		require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
			_, e := w.AddressPools().Insert(ctx, p)
			return e
		}))
		return p
	}

	globalPool := mkPool("global-default", "", true, "198.18.0.0/24")
	zonePool := mkPool("zone-default", zone, true, "198.18.1.0/24")
	networkBindingPool := mkPool("network-bound", zone, false, "198.18.3.0/24")

	for _, p := range []*domain.AddressPool{globalPool, zonePool, networkBindingPool} {
		pID := p.ID
		require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
			return w.AddressPools().PopulateFreelistForPool(ctx, pID)
		}))
	}

	net := &domain.Network{ID: ids.NewID(ids.PrefixNetwork), ProjectID: "project-netdef", Name: domain.RcNameVPC("net-netdef")}
	require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
		_, e := w.Networks().Insert(ctx, net)
		return e
	}))
	sub := &domain.Subnet{
		ID: ids.NewID(ids.PrefixSubnet), ProjectID: "project-netdef",
		Name: domain.RcNameVPC("sub-netdef"), NetworkID: net.ID, PlacementType: domain.PlacementZonal, ZoneID: zone, V4CidrBlocks: []string{"10.10.0.0/24"},
	}
	require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
		_, e := w.Subnets().Insert(ctx, sub)
		return e
	}))
	require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
		return w.AddressPoolBindings().SetNetworkDefault(ctx, net.ID, networkBindingPool.ID)
	}))

	// ResolverService + AllocateUseCase принимают `kacho.Repository` напрямую.
	subnetAdapter := cqrsadapter.NewSubnet(r)
	addrAdapter := cqrsadapter.NewAddress(r)
	apResolver := addresspool.NewResolverService(r, addrAdapter, subnetAdapter)
	addrSvc := addressapp.NewAllocateUseCase(r, apResolver)

	mkAddr := func(projectID, name string, typ domain.AddressType, ext *domain.ExternalIpv4Spec, intSpec *domain.InternalIpv4Spec) *domain.Address {
		return &domain.Address{
			ID: ids.NewID(ids.PrefixAddress), ProjectID: projectID, Name: domain.RcNameVPC(name),
			Type: typ, IpVersion: domain.IpVersionIPv4, ExternalIpv4: ext, InternalIpv4: intSpec,
		}
	}

	insertAddr := func(a *domain.Address) {
		require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
			_, e := w.Addresses().Insert(ctx, a)
			return e
		}))
	}

	// network_default — internal address в subnet'е этой сети.
	aNetDef := mkAddr("project-netdef", "a-netdef", domain.AddressTypeInternal, nil, &domain.InternalIpv4Spec{SubnetID: sub.ID})
	insertAddr(aNetDef)

	aZone := mkAddr("project-zone", "a-zone", domain.AddressTypeExternal, &domain.ExternalIpv4Spec{ZoneID: zone}, nil)
	insertAddr(aZone)

	aGlobal := mkAddr("project-global", "a-global", domain.AddressTypeExternal, &domain.ExternalIpv4Spec{ZoneID: ""}, nil)
	insertAddr(aGlobal)

	// --- resolve: ResolvePoolForAddressObjFamily выбирает ожидаемый pool и MatchedVia ---
	resolveCase := func(t *testing.T, addressID, wantPoolID, wantVia string) {
		t.Helper()
		rd, err := r.Reader(ctx)
		require.NoError(t, err)
		rec, err := rd.Addresses().Get(ctx, addressID)
		_ = rd.Close()
		require.NoError(t, err)
		res, rerr := apResolver.ResolvePoolForAddressObjFamily(ctx, rec, addresspool.FamilyV4)
		require.NoError(t, rerr)
		require.NotNil(t, res)
		assert.Equal(t, wantPoolID, res.Pool.ID, "wrong pool resolved")
		assert.Equal(t, wantVia, res.MatchedVia, "wrong cascade step matched")
	}
	t.Run("network_default", func(t *testing.T) { resolveCase(t, aNetDef.ID, networkBindingPool.ID, "network_default") })
	t.Run("zone_default", func(t *testing.T) { resolveCase(t, aZone.ID, zonePool.ID, "zone_default") })
	t.Run("global_default", func(t *testing.T) { resolveCase(t, aGlobal.ID, globalPool.ID, "global_default") })

	// --- allocate (external addresses) ---
	for _, tc := range []struct {
		name       string
		addressID  string
		wantPoolID string
	}{
		{"allocate_zone", aZone.ID, zonePool.ID},
		{"allocate_global", aGlobal.ID, globalPool.ID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, aerr := addrSvc.AllocateExternalIP(ctx, tc.addressID)
			require.NoError(t, aerr)
			require.NotNil(t, res)
			assert.NotEmpty(t, res.IP, "an IP must be allocated")
			assert.Equal(t, tc.wantPoolID, res.PoolID, "IP must come from the cascade-resolved pool")
			res2, aerr2 := addrSvc.AllocateExternalIP(ctx, tc.addressID)
			require.NoError(t, aerr2)
			assert.Equal(t, res.IP, res2.IP)
			assert.True(t, res2.AlreadyAllocated)
		})
	}

	// --- placement lanes: a ZONE-PINNED request never consumes the
	// zone-independent (anycast) pool -----------------------------------------
	//
	// `globalPool` above is v4-only and zone-less. A v6 request PINNED TO A ZONE
	// must NOT be served from it: an anycast prefix cannot yield an address that
	// claims to live in a zone (data-integrity.md §Placement-coherence — the
	// placement anchor is a mutually exclusive discriminator). Only the anycast
	// lane (empty zone) reaches it, which the `global_default` subtest above
	// covers.
	//
	// Concretely this is what makes the cluster-wide zone-independent default
	// (partial UNIQUE on `(COALESCE(zone_id, <empty>), kind) WHERE is_default`)
	// provisionable at all: while it doubled as a catch-all fallback, seeding it
	// silently changed the answer for every zone that deliberately does not serve
	// a family.
	t.Run("zonal_request_does_not_borrow_from_zone_independent_pool", func(t *testing.T) {
		anycastV6 := mkPool("anycast-v6", "", false, "198.18.9.0/24")
		anycastV6.V4CIDRBlocks = nil
		anycastV6.V6CIDRBlocks = []string{"2001:db8:ac::/64"}
		require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
			_, e := w.AddressPools().Update(ctx, anycastV6)
			return e
		}))
		// Promote it to the zone-independent default, replacing the v4-only one so
		// the ONLY v6-capable pool in the cluster is zone-independent.
		globalPool.IsDefault = false
		require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
			_, e := w.AddressPools().Update(ctx, globalPool)
			return e
		}))
		anycastV6.IsDefault = true
		require.NoError(t, withTx(t, func(w kacho.RepositoryWriter) error {
			_, e := w.AddressPools().Update(ctx, anycastV6)
			return e
		}))

		zonedV6 := &domain.Address{
			ID: ids.NewID(ids.PrefixAddress), ProjectID: "project-lane",
			Name: domain.RcNameVPC("a-lane-v6"), Type: domain.AddressTypeExternal,
			IpVersion:    domain.IpVersionIPv6,
			ExternalIpv6: &domain.ExternalIpv6Spec{ZoneID: zone},
		}
		insertAddr(zonedV6)

		rd, err := r.Reader(ctx)
		require.NoError(t, err)
		rec, err := rd.Addresses().Get(ctx, zonedV6.ID)
		_ = rd.Close()
		require.NoError(t, err)

		res, rerr := apResolver.ResolvePoolForAddressObjFamily(ctx, rec, addresspool.FamilyV6)
		require.Error(t, rerr,
			"a zone-pinned v6 request must not be served by the zone-independent pool")
		assert.Nil(t, res)

		anycastV6Addr := &domain.Address{
			ID: ids.NewID(ids.PrefixAddress), ProjectID: "project-lane",
			Name: domain.RcNameVPC("a-lane-v6-any"), Type: domain.AddressTypeExternal,
			IpVersion:    domain.IpVersionIPv6,
			ExternalIpv6: &domain.ExternalIpv6Spec{ZoneID: ""},
		}
		insertAddr(anycastV6Addr)
		rd2, err := r.Reader(ctx)
		require.NoError(t, err)
		rec2, err := rd2.Addresses().Get(ctx, anycastV6Addr.ID)
		_ = rd2.Close()
		require.NoError(t, err)

		resAny, rerr2 := apResolver.ResolvePoolForAddressObjFamily(ctx, rec2, addresspool.FamilyV6)
		require.NoError(t, rerr2, "the anycast lane resolves the zone-independent pool")
		require.NotNil(t, resAny)
		assert.Equal(t, anycastV6.ID, resAny.Pool.ID)
		assert.Equal(t, "global_default", resAny.MatchedVia)
	})
}
