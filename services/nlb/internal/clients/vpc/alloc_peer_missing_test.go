// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package vpc

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Allocation lane — a peer NOT_FOUND names the REFERENCED object, never "the
// address".
//
// `AllocateInternalIP` / `AllocateExternalIP` mint a NEW Address, so at the
// moment vpc answers NOT_FOUND there is no address yet: the miss is always a
// referenced object the caller supplied (vpc `assertSubnetOwned` answers
// `NotFound "Subnet <id> not found"` both for an absent subnet and for one owned
// by another project) or an infra object (no AddressPool in the underlay zone).
//
// The create lane previously routed this through `mapAllocErr("", err)`, whose
// NotFound arm formats `"address %s not found"` with an EMPTY id — it reported
// `"address  not found"` and classified the miss as `ErrInvalidArg`. Both are
// untrue (architecture.md doc-truthfulness), and the misclassification is what
// let the use-case collapse a caller-actionable "your subnet does not resolve"
// into the capacity-opaque "could not allocate load balancer address".
//
// The sentinel must be `domain.ErrNotFound` so the use-case can tell the
// peer-missing lane from the capacity lane; the wrapped text must carry the
// peer's own reason for the server log (it is NEVER echoed to the client —
// loadbalancer.allocAcquireErr maps it to a fixed contract text).
func TestAllocateInternalIP_PeerSubnetMiss_IsNotFoundSentinel(t *testing.T) {
	addrSvc := &fakeAddressForAlloc{
		createErr: status.Error(codes.NotFound, "Subnet e9b-sub-1 not found"),
	}
	conn := startFakeVPC(t, nil, nil, addrSvc, &fakeInternalAddressService{}, &fakeOperationService{})

	c := NewInternalAddressClient(conn, conn)
	_, err := c.AllocateInternalIP(ctxBackground(), AllocateInternalIPRequest{
		ProjectID: "prj-1", Name: "n", SubnetID: "e9b-sub-1",
		Owner: AddressOwner{Kind: "nlb_load_balancer", ID: "nlb-1"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrNotFound),
		"a referenced-object miss on the create lane must classify as ErrNotFound, got %v", err)
	assert.False(t, errors.Is(err, domain.ErrInvalidArg),
		"peer-missing must NOT masquerade as an invalid caller argument")
	assert.NotContains(t, err.Error(), "address  not found",
		"must not fabricate an empty-id address miss")
	assert.Contains(t, err.Error(), "Subnet e9b-sub-1 not found",
		"the peer's own reason must survive for the server log")
}

// Same discipline on the external (public auto-VIP) lane: a missing AddressPool
// in the underlay zone is a NOT_FOUND too and must reach the use-case as
// ErrNotFound (the use-case decides what — if anything — the client may learn).
func TestAllocateExternalIP_PeerMiss_IsNotFoundSentinel(t *testing.T) {
	addrSvc := &fakeAddressForAlloc{
		createErr: status.Error(codes.NotFound, "AddressPool not found"),
	}
	conn := startFakeVPC(t, nil, nil, addrSvc, &fakeInternalAddressService{}, &fakeOperationService{})

	c := NewInternalAddressClient(conn, conn)
	_, err := c.AllocateExternalIP(ctxBackground(), AllocateExternalIPRequest{
		ProjectID: "prj-1", ZoneID: "z", Owner: AddressOwner{Kind: "nlb_load_balancer", ID: "nlb-1"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrNotFound),
		"external allocate peer-miss must classify as ErrNotFound, got %v", err)
}

// Capacity/precondition classification is UNCHANGED — an exhausted pool stays
// ErrFailedPrecondition so the use-case keeps answering with the opaque
// capacity text (no capacity oracle).
func TestAllocateInternalIP_Exhausted_StaysFailedPrecondition(t *testing.T) {
	addrSvc := &fakeAddressForAlloc{
		createErr: status.Error(codes.FailedPrecondition, "subnet exhausted"),
	}
	conn := startFakeVPC(t, nil, nil, addrSvc, &fakeInternalAddressService{}, &fakeOperationService{})

	c := NewInternalAddressClient(conn, conn)
	_, err := c.AllocateInternalIP(ctxBackground(), AllocateInternalIPRequest{
		ProjectID: "prj-1", Name: "n", SubnetID: "e9b-sub-1",
		Owner: AddressOwner{Kind: "nlb_load_balancer", ID: "nlb-1"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrFailedPrecondition))
	assert.False(t, errors.Is(err, domain.ErrNotFound))
}

// `SetReference` / `FreeIP` / `Get` keep their anti-oracle NotFound→InvalidArg
// mapping (they operate on an EXISTING address id, where a miss genuinely is
// "that address id does not resolve" and must stay indistinguishable from a
// foreign one). Guards the shared mapper against an over-broad edit.
func TestSetReference_NotFound_StaysInvalidArg(t *testing.T) {
	intAddrSvc := &fakeInternalAddressService{
		setErr: status.Error(codes.NotFound, "Address adr-x not found"),
	}
	conn := startFakeVPC(t, nil, nil, &fakeAddressForAlloc{}, intAddrSvc, &fakeOperationService{})

	c := NewInternalAddressClient(conn, conn)
	err := c.SetReference(ctxBackground(), "adr-x",
		AddressOwner{Kind: "nlb_load_balancer", ID: "nlb-1"}, true)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidArg),
		"reference lane keeps the anti-oracle InvalidArg mapping, got %v", err)
}
