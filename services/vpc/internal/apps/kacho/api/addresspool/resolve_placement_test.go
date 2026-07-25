// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package addresspool

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Placement-coherence of the pool cascade (data-integrity.md §Placement-coherence).
//
// `AddressPool.zone_id` declares WHERE the pool's prefixes live. A pool with an
// EMPTY zone is zone-independent — a REGIONAL/anycast prefix source; a pool with a
// zone is ZONAL. The placement anchor is a MUTUALLY EXCLUSIVE discriminator, so the
// two are different lanes, not a fallback chain:
//
//   - a ZONAL request (`external_ipv{4,6}.zone_id = <zone>`) must be served from
//     that zone — carving it out of an anycast prefix would mint an Address that
//     CLAIMS a zone its prefix does not have (a placement lie: the address is not
//     in, nor protected by, that zone's failure domain);
//   - an ANYCAST request (empty zone — a REGIONAL load balancer VIP, an anycast
//     address) is zone-independent BY CONSTRUCTION and is served by the
//     zone-independent pool.
//
// Practical consequence, and the reason this is not cosmetic: the zone-independent
// default is a CLUSTER-WIDE singleton (partial UNIQUE on
// `(COALESCE(zone_id, <empty>), kind) WHERE is_default`). While it doubled as a
// catch-all fallback it could not be provisioned at all without silently changing
// the answer for every zone that deliberately does not serve a family.
func TestCascade_ZonalRequest_DoesNotConsumeZoneIndependentPool(t *testing.T) {
	f := newUseCases(t)
	// The zone serves v4 only; the zone-independent (anycast) pool serves v6.
	f.seedPool(t, "zone-a-v4", true, "zone-a", []string{"198.51.100.0/24"}, nil, nil)
	f.seedPool(t, "anycast-dual", true, "",
		[]string{"203.0.113.0/24"}, []string{"2001:db8:ac::/64"}, nil)

	v6InZone := f.seedAddressV6Req(t, "f-lane", "zone-a")

	res, err := f.resolver.ResolvePoolForAddressObjFamily(context.Background(), v6InZone, FamilyV6)
	require.Error(t, err,
		"a zone-pinned v6 request must not be served from the zone-independent anycast pool")
	assert.True(t, errors.Is(err, ErrPoolNotResolved), "expected ErrPoolNotResolved, got %v", err)
	assert.Nil(t, res)
}

// Mirror for v4: a zone that deliberately serves v6 only must not silently borrow
// v4 from the anycast pool.
func TestCascade_ZonalV4Request_DoesNotConsumeZoneIndependentPool(t *testing.T) {
	f := newUseCases(t)
	f.seedPool(t, "zone-a-v6", true, "zone-a", nil, []string{"2001:db8:aa::/64"}, nil)
	f.seedPool(t, "anycast-dual", true, "",
		[]string{"203.0.113.0/24"}, []string{"2001:db8:ac::/64"}, nil)

	v4InZone := f.seedAddressV4Req(t, "f-lane", "zone-a")

	res, err := f.resolver.ResolvePoolForAddressObjFamily(context.Background(), v4InZone, FamilyV4)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPoolNotResolved))
	assert.Nil(t, res)
}

// The anycast lane itself: a zone-less external address resolves the
// zone-independent pool, for both families. This is the lane a REGIONAL
// (EXTERNAL_REGIONAL) load balancer VIP travels.
func TestCascade_AnycastRequest_ResolvesZoneIndependentPool(t *testing.T) {
	f := newUseCases(t)
	f.seedPool(t, "zone-a-v4", true, "zone-a", []string{"198.51.100.0/24"}, nil, nil)
	anycast := f.seedPool(t, "anycast-dual", true, "",
		[]string{"203.0.113.0/24"}, []string{"2001:db8:ac::/64"}, nil)

	v4 := f.seedAddressV4Req(t, "f-any", "")
	v6 := f.seedAddressV6Req(t, "f-any", "")

	resV4, err := f.resolver.ResolvePoolForAddressObjFamily(context.Background(), v4, FamilyV4)
	require.NoError(t, err)
	require.NotNil(t, resV4)
	assert.Equal(t, anycast.ID, resV4.Pool.ID)
	assert.Equal(t, "global_default", resV4.MatchedVia)

	resV6, err := f.resolver.ResolvePoolForAddressObjFamily(context.Background(), v6, FamilyV6)
	require.NoError(t, err)
	require.NotNil(t, resV6)
	assert.Equal(t, anycast.ID, resV6.Pool.ID)
	assert.Equal(t, "global_default", resV6.MatchedVia)
}

// The family filter still applies inside the anycast lane: a v4-only
// zone-independent pool does not serve a v6 anycast request.
func TestCascade_AnycastRequest_FamilyFilterStillApplies(t *testing.T) {
	f := newUseCases(t)
	f.seedPool(t, "anycast-v4", true, "", []string{"203.0.113.0/24"}, nil, nil)

	v6 := f.seedAddressV6Req(t, "f-any", "")

	res, err := f.resolver.ResolvePoolForAddressObjFamily(context.Background(), v6, FamilyV6)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPoolNotResolved))
	assert.Nil(t, res)
}
