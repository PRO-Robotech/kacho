// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Placement-coherence of the PUBLIC VIP lane (data-integrity.md §Placement-
// coherence, module-nlb rules 5/10).
//
// `placement=EXTERNAL_REGIONAL` is the ONLY external placement — "external+zonal"
// is inexpressible by construction — so an external load balancer is ALWAYS
// REGIONAL/anycast, and a REGIONAL resource is zone-independent BY CONSTRUCTION
// (it carries no zone; it is excluded from the zonal check because there is
// nothing to compare). Its VIP must therefore be carved out of a ZONE-INDEPENDENT
// address pool.
//
// The use-case used to synthesise a zone for it (`ListZoneIDsInRegion` →
// `sort.Strings` → `zones[0]`) and hand that zone to vpc, which then resolved the
// ZONAL default pool of whichever zone sorted first. Two defects in one:
//
//   - the "anycast" VIP was pinned to a single zone's prefix, i.e. to a single
//     zone's failure domain — the exact property an anycast VIP exists to avoid;
//   - it only ever worked BY ACCIDENT, when the alphabetically-first zone happened
//     to own a default pool of the requested family. IPv6 had no such accident:
//     zero v6 VIPs were ever allocated (`resolve address pool: no address pool
//     resolved`).
//
// vpc already models the anycast lane natively: an external Address with an EMPTY
// zone resolves the zone-independent pool (`zone_id IS NULL`), and
// `address/create.go::validateExternalZone` documents an empty zone_id as valid
// ("anycast из global-пула, зоне-независим").
func TestCreate_ExternalRegional_PublicVIP_IsZoneIndependent(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	addr := &fakeAddressClient{}
	zc := &countingZoneClient{}
	uc := newCreateUC(repo, opsRepo, createDeps{addr: addr, zone: zc})
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_EXTERNAL_REGIONAL
	req.V4Source = vipPublic()
	req.V6Source = vipPublic()

	op, err := uc.Execute(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)

	rec := lbByName(t, repo, "lb-1")
	require.Equal(t, domain.PlacementUnspecified, rec.PlacementType,
		"EXTERNAL is regional/anycast — it carries no zonal placement")
	require.NotEmpty(t, string(rec.AddressIDV4))
	require.NotEmpty(t, string(rec.AddressIDV6), "the v6 anycast VIP must allocate too")

	require.Len(t, addr.extReqs, 2, "one public allocation per declared family")
	for _, r := range addr.extReqs {
		assert.Empty(t, r.ZoneID,
			"a REGIONAL/anycast VIP must be allocated zone-independently — "+
				"naming a zone pins the anycast address to one zone's pool and failure domain")
	}

	assert.Zero(t, zc.calls(),
		"nothing may derive a zone for a REGIONAL load balancer: the zone client is "+
			"only for disabled_announce_zones validation, which this request does not use")
}

// Single-family v4 mirror of the above — the v4 lane only ever worked because the
// first-sorted zone happened to carry a v4 pool, so it must be pinned as well.
func TestCreate_ExternalRegional_PublicVIP_V4_IsZoneIndependent(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	addr := &fakeAddressClient{}
	uc := newCreateUC(repo, opsRepo, createDeps{addr: addr})
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_EXTERNAL_REGIONAL
	req.V4Source = vipPublic()

	op, err := uc.Execute(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)

	require.Len(t, addr.extReqs, 1)
	assert.Empty(t, addr.extReqs[0].ZoneID)
}

// The zone client stays wired for what it is actually for — validating a
// caller-supplied `disabled_announce_zones` set against the region. Removing the
// zone DERIVATION must not remove that.
func TestCreate_Regional_DrainSet_StillValidatedAgainstRegion(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	zc := &countingZoneClient{}
	uc := newCreateUC(repo, opsRepo, createDeps{zone: zc})
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_REGIONAL
	req.V4Source = vipSubnet(lbTestSubnetRegional)
	req.DisabledAnnounceZones = []string{"region-1-zzz"}

	_, err := uc.Execute(context.Background(), req)
	require.Error(t, err, "a zone outside the region must still be rejected")
	assert.Contains(t, err.Error(), "is not in region region-1")
	assert.NotZero(t, zc.calls(), "the drain-set check is the zone client's remaining caller")
}

// Anti-oracle (security.md §инфра-чувствительные данные): when NO zone-independent
// pool exists the answer must stay the fixed capacity text and must not name the
// missing infra object — not the pool, and (now that there is none) certainly not
// a zone. The cause goes to the server log only.
func TestCreate_ExternalRegional_NoAnycastPool_AnswerNamesNoPlacement(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	addr := &fakeAddressClient{extAllocFn: func(_ context.Context, _ vpcclient.AllocateExternalIPRequest, _ string) (*vpcclient.AllocateResponse, error) {
		return nil, domain.ErrFailedPrecondition
	}}
	zc := &countingZoneClient{zones: []string{"region-1-a", "region-1-b"}}
	uc := newCreateUC(repo, opsRepo, createDeps{addr: addr, zone: zc})
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_EXTERNAL_REGIONAL
	req.V6Source = vipPublic()

	op, err := uc.Execute(context.Background(), req)
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.NotNil(t, final.Error)
	assert.Equal(t, int32(codes.FailedPrecondition), final.Error.GetCode())
	assert.Equal(t, "could not allocate load balancer address", final.Error.GetMessage())
	for _, leak := range []string{"region-1-a", "region-1-b", "pool", "zone"} {
		assert.False(t, strings.Contains(strings.ToLower(final.Error.GetMessage()), leak),
			"the tenant-facing answer must not disclose placement/infra detail %q", leak)
	}
	assert.Empty(t, repo.lbs, "the durable handle is compensated away")
}

// countingZoneClient — fakeZoneClient that records how many times the region's
// zone list was pulled, so a test can assert that NOTHING derives a zone.
type countingZoneClient struct {
	mu    sync.Mutex
	n     int
	zones []string
}

func (c *countingZoneClient) ListZoneIDsInRegion(_ context.Context, regionID string) ([]string, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	if c.zones != nil {
		return append([]string(nil), c.zones...), nil
	}
	return []string{regionID + "-a", regionID + "-b"}, nil
}

func (c *countingZoneClient) calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

var _ ZoneClient = (*countingZoneClient)(nil)
