// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package vpc

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// OWN-LANE read-your-writes on the auto-allocate flow.
//
// `allocFromCreate` is a two-step: vpc `AddressService.Create` commits the Address
// row, then `InternalAddressService.SetAddressReference` links it to the LB. Between
// those two calls the address is ALREADY nlb's own committed resource — so a NOT_FOUND
// (or PERMISSION_DENIED) from the link call CANNOT mean "that address id does not
// resolve". It is vpc's hide-existence answer while the address's per-object
// owner-tuple is still materialising (data-integrity.md: authz materialisation is
// eventually consistent — outbox → drainer → iam → FGA; the remedy is a bounded CLIENT
// retry, never a server confirm-barrier, ban #9).
//
// Ground truth — CI run 30135586348, kacho-vpc log, four occurrences that each killed
// an otherwise healthy LoadBalancer.Create:
//
//	{"time":"2026-07-25T00:30:52.489Z","level":"WARN","msg":"authz_hide_existence",
//	 "rpc":"/kacho.cloud.vpc.v1.InternalAddressService/SetAddressReference",
//	 "relation":"v_update","object":"vpc_address:adrj251yyhebawpehh6h"}
//
// The generic `SetReference` mapper turns that NOT_FOUND into `ErrInvalidArg`
// ("you passed a bad address id" — correct for the BYO lane, where the id IS
// caller-supplied), and `loadbalancer.allocAcquireErr` then has no lane left but the
// capacity-opaque "could not allocate load balancer address". That answer is factually
// WRONG: the address had just been allocated, capacity was never the constraint.

// setErrTimes on the shared fake makes the link call fail only for the first N
// attempts, modelling a materialisation window that closes on its own.
func TestAllocateInternalIP_OwnAddressNotYetVisible_RetriesUntilMaterialised(t *testing.T) {
	addrSvc := &fakeAddressForAlloc{
		createResp: &vpcpb.Address{
			Id: "adr-own-1",
			Address: &vpcpb.Address_InternalIpv4Address{
				InternalIpv4Address: &vpcpb.InternalIpv4Address{Address: "10.0.0.7"},
			},
		},
	}
	intAddrSvc := &fakeInternalAddressService{
		setErr:      status.Error(codes.NotFound, "Address adr-own-1 not found"),
		setErrTimes: 2, // window closes on the 3rd attempt
	}
	conn := startFakeVPC(t, nil, nil, addrSvc, intAddrSvc, &fakeOperationService{})

	c := newFastRetryInternalAddressClient(t, conn)
	resp, err := c.AllocateInternalIP(ctxBackground(), AllocateInternalIPRequest{
		ProjectID: "prj-1", Name: "vip", SubnetID: "sub-1",
		Owner: AddressOwner{Kind: "nlb_load_balancer", ID: "nlb-1"},
	})
	require.NoError(t, err,
		"a hide-existence NOT_FOUND on nlb's OWN fresh address is a read-your-writes lag "+
			"and must be retried over the materialisation window, not reported as a failure")
	require.NotNil(t, resp)
	assert.Equal(t, "adr-own-1", resp.AddressID)
	assert.Equal(t, "10.0.0.7", resp.Value)

	assert.Equal(t, 3, intAddrSvc.setCallCount(), "must retry the link until it is authorised")
	assert.Equal(t, 1, addrSvc.createCallCount(),
		"the address is allocated ONCE — the retry re-links, it must never re-allocate (pool leak)")
	assert.Zero(t, addrSvc.deleteCallCount(),
		"a transient invisibility must not trigger compensation: the address is ours and stays ours")
}

// The same discipline for PERMISSION_DENIED: vpc answers hide-existence NOT_FOUND on
// the resources that carry it, plain PERMISSION_DENIED on the ones that do not. Both
// mean the same thing on this lane — the owner-tuple has not materialised yet.
func TestAllocateExternalIP_OwnAddressPermissionDenied_RetriesUntilMaterialised(t *testing.T) {
	addrSvc := &fakeAddressForAlloc{
		createResp: &vpcpb.Address{
			Id: "adr-own-2",
			Address: &vpcpb.Address_ExternalIpv4Address{
				ExternalIpv4Address: &vpcpb.ExternalIpv4Address{Address: "198.51.100.9"},
			},
		},
	}
	intAddrSvc := &fakeInternalAddressService{
		setErr:      status.Error(codes.PermissionDenied, "permission denied"),
		setErrTimes: 1,
	}
	conn := startFakeVPC(t, nil, nil, addrSvc, intAddrSvc, &fakeOperationService{})

	c := newFastRetryInternalAddressClient(t, conn)
	resp, err := c.AllocateExternalIP(ctxBackground(), AllocateExternalIPRequest{
		ProjectID: "prj-1", Name: "vip", ZoneID: "zone-a",
		Owner: AddressOwner{Kind: "nlb_load_balancer", ID: "nlb-1"},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "198.51.100.9", resp.Value)
	assert.Equal(t, 2, intAddrSvc.setCallCount())
}

// Budget spent: the answer must stay HONEST. It is a transient peer condition
// (retryable) — NOT a capacity refusal, and NOT "you passed a bad address id".
// `loadbalancer.allocAcquireErr` maps ErrUnavailable to
// `UNAVAILABLE "load balancer address allocation unavailable"`, which is both truthful
// and leak-free (it discloses neither pool capacity nor the underlay zone).
func TestAllocateInternalIP_OwnAddressNeverVisible_IsUnavailableNotCapacity(t *testing.T) {
	addrSvc := &fakeAddressForAlloc{
		createResp: &vpcpb.Address{
			Id: "adr-own-3",
			Address: &vpcpb.Address_InternalIpv4Address{
				InternalIpv4Address: &vpcpb.InternalIpv4Address{Address: "10.0.0.8"},
			},
		},
	}
	intAddrSvc := &fakeInternalAddressService{
		setErr: status.Error(codes.NotFound, "Address adr-own-3 not found"), // never closes
	}
	conn := startFakeVPC(t, nil, nil, addrSvc, intAddrSvc, &fakeOperationService{})

	c := newFastRetryInternalAddressClient(t, conn)
	_, err := c.AllocateInternalIP(ctxBackground(), AllocateInternalIPRequest{
		ProjectID: "prj-1", Name: "vip", SubnetID: "sub-1",
		Owner: AddressOwner{Kind: "nlb_load_balancer", ID: "nlb-1"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrUnavailable),
		"a non-converging own-resource visibility window is a TRANSIENT peer condition, got %v", err)
	assert.False(t, errors.Is(err, domain.ErrInvalidArg),
		"nlb minted this address id itself — it can never be an invalid caller argument")

	// And the leased address must be handed back: the pool must not leak a lease
	// just because the link could not be authorised (data-integrity.md
	// «Lease-recycle-on-delete»).
	assert.Positive(t, addrSvc.deleteCallCount(),
		"the half-allocated address must be compensated, not leaked")
}

// The compensation is on the SAME lane, and in production it was denied by the SAME
// missing tuple — CI run 30135586348 shows the pair, ~10ms apart, on every occurrence:
//
//	{"msg":"authz_hide_existence","rpc":".../InternalAddressService/SetAddressReference",
//	 "relation":"v_update","object":"vpc_address:adrtdrpxy5b70zm6qk7w"}
//	{"msg":"authz_hide_existence","rpc":".../AddressService/Delete",
//	 "relation":"v_delete","object":"vpc_address:adrtdrpxy5b70zm6qk7w"}
//
// `FreeIP` reads a Delete NOT_FOUND as "idempotent: already deleted" and returns nil —
// true for a genuinely absent address, FALSE here: we created this address moments ago,
// so the NOT_FOUND is the hide-existence deny. Swallowing it turns a reclaimable lease
// into a SILENT pool leak (data-integrity.md «Lease-recycle-on-delete»), invisible even
// to the `address_compensation_free_failed` warning.
//
// The compensating free must therefore ride the same bounded window, and a leak that
// still does not converge must be LOUD.
func TestAllocateInternalIP_CompensationAlsoDenied_ReclaimsLeaseAndDoesNotLeakSilently(t *testing.T) {
	addrSvc := &fakeAddressForAlloc{
		createResp: &vpcpb.Address{
			Id: "adr-own-4",
			Address: &vpcpb.Address_InternalIpv4Address{
				InternalIpv4Address: &vpcpb.InternalIpv4Address{Address: "10.0.0.9"},
			},
		},
		// The compensating free runs on HALF the budget (visibilityBudget(2)) — it is
		// cleanup on an already-failing path and must not eat the client's poll budget.
		deleteErrTimes: 1, // the delete deny clears on the 2nd attempt
		deleteErr:      status.Error(codes.NotFound, "Address adr-own-4 not found"),
	}
	intAddrSvc := &fakeInternalAddressService{
		setErr: status.Error(codes.NotFound, "Address adr-own-4 not found"), // never closes
	}
	conn := startFakeVPC(t, nil, nil, addrSvc, intAddrSvc, &fakeOperationService{})

	c := newFastRetryInternalAddressClient(t, conn)
	_, err := c.AllocateInternalIP(ctxBackground(), AllocateInternalIPRequest{
		ProjectID: "prj-1", Name: "vip", SubnetID: "sub-1",
		Owner: AddressOwner{Kind: "nlb_load_balancer", ID: "nlb-1"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrUnavailable))

	// The lease must actually come back: a Delete denied by the not-yet-visible
	// tuple has to be retried, not accepted as "already gone".
	assert.GreaterOrEqual(t, addrSvc.deleteCallCount(), 2,
		"a hide-existence NOT_FOUND on the compensating Delete of our OWN address must be "+
			"retried over the materialisation window, not mistaken for an idempotent no-op")
}

// newFastRetryInternalAddressClient builds the real client with the own-lane retry
// cadence compressed, so the bounded-window behaviour is asserted deterministically
// without sleeping for the production interval.
func newFastRetryInternalAddressClient(t *testing.T, conn *grpc.ClientConn) InternalAddressClient {
	t.Helper()
	c, ok := NewInternalAddressClient(conn, conn).(*internalAddressClient)
	require.True(t, ok)
	c.visibilityRetries = 4
	c.visibilityInterval = time.Millisecond
	return c
}

// Race-safe counters over the shared fakes.

func (f *fakeInternalAddressService) setCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.setCalls)
}

func (f *fakeAddressForAlloc) createCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.createCalls
}

func (f *fakeAddressForAlloc) deleteCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deleteCalls
}
