// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package vpc

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/apps/kacho/api/shared"
)

// WHAT THE OWN LANE PROMISES WHEN ITS BUDGET RUNS OUT.
//
// `linkOwnAddress` / `freeOwnAddress` retry over the per-object authz
// materialisation window and, when it has not closed in time, answer
// `domain.ErrUnavailable` — "visibility has not caught up yet, come back". That
// verdict is retryable by construction: materialisation is eventually consistent,
// so the next attempt may well succeed.
//
// The peer answer that drove those retries is, by construction, the hide-existence
// NOT_FOUND (or PERMISSION_DENIED) of a per-object authz deny — `ownResourceInvisible`
// lets nothing else onto this lane. Two things follow, both asserted here on what a
// CALLER can observe, never on how the error is assembled:
//
//  1. THE CODE. Carrying that peer status inside the returned chain hands the caller
//     a TERMINAL verdict: `shared.MapDomainErr` — nlb's single peer-error mapper —
//     deliberately passes a ready gRPC status through first, so a NOT_FOUND riding in
//     the chain silently overrules the UNAVAILABLE this lane meant. The caller then
//     stops where it was supposed to retry, and is told its own freshly created
//     address does not exist — which is false: nlb minted and committed it moments
//     earlier.
//
//  2. THE TEXT. `%w`-ing a gRPC error renders its transport envelope
//     ("rpc error: code = … desc = …") into the message. Kachō message tone is part
//     of the contract (api-conventions.md) and has no room for the wire wrapper.
//     Re-telling the peer's hide-existence sentence is worse still: that sentence
//     exists to disclose nothing, and repeating it outward states an absence that is
//     not true.
//
// The cause is not lost — it is LOGGED at the point of surrender (CWE-778), where it
// belongs; it is simply not part of the answer to the caller.

// alwaysDenyingLinkClient wires the real client to a vpc whose link call is denied
// for good — the materialisation window never closes, so the bounded budget is
// always spent.
func alwaysDenyingLinkClient(t *testing.T, addressID string, denial error) InternalAddressClient {
	t.Helper()
	addrSvc := &fakeAddressForAlloc{
		createResp: &vpcpb.Address{
			Id: addressID,
			Address: &vpcpb.Address_InternalIpv4Address{
				InternalIpv4Address: &vpcpb.InternalIpv4Address{Address: "10.0.0.11"},
			},
		},
	}
	intAddrSvc := &fakeInternalAddressService{setErr: denial}
	conn := startFakeVPC(t, nil, nil, addrSvc, intAddrSvc, &fakeOperationService{})
	return newFastRetryInternalAddressClient(t, conn)
}

// assertRetryableAndClean states the two observables of the exhausted own lane.
func assertRetryableAndClean(t *testing.T, err error, addressID string) {
	t.Helper()
	require.Error(t, err)

	st, _ := status.FromError(shared.MapDomainErr(err))
	assert.Equal(t, codes.Unavailable, st.Code(),
		"an unfinished materialisation window is a transient condition the caller must "+
			"retry; a terminal code tells it to give up instead. got %q", st.Message())
	assert.NotContains(t, st.Message(), "rpc error:",
		"the gRPC transport envelope is not part of any Kachō message")
	assert.NotContains(t, strings.ToLower(st.Message()), "not found",
		"the peer's hide-existence sentence must not be re-told: the address exists, "+
			"nlb committed it moments ago")
	assert.NotContains(t, strings.ToLower(st.Message()), "permission denied",
		"the peer's authz denial is an internal detail of the retry, not the answer")
	assert.Contains(t, st.Message(), addressID,
		"the answer still names the address it is about")
}

// The link call: the caller is a use-case that must be free to retry.
func TestLinkOwnAddress_WindowNeverCloses_AnswersRetryableWithoutPeerStatus(t *testing.T) {
	c := alwaysDenyingLinkClient(t, "adr-own-10",
		status.Error(codes.NotFound, "Address adr-own-10 not found"))

	_, err := c.AllocateInternalIP(ctxBackground(), AllocateInternalIPRequest{
		ProjectID: "prj-1", Name: "vip", SubnetID: "sub-1",
		Owner: AddressOwner{Kind: "nlb_load_balancer", ID: "nlb-1"},
	})
	assertRetryableAndClean(t, err, "adr-own-10")
}

// vpc answers PERMISSION_DENIED on the resources that do not hide existence. Same
// lane, same verdict — and the same duty not to let that code decide the answer.
func TestLinkOwnAddress_DeniedNotHidden_AnswersRetryableWithoutPeerStatus(t *testing.T) {
	c := alwaysDenyingLinkClient(t, "adr-own-11",
		status.Error(codes.PermissionDenied, "permission denied"))

	_, err := c.AllocateInternalIP(ctxBackground(), AllocateInternalIPRequest{
		ProjectID: "prj-1", Name: "vip", SubnetID: "sub-1",
		Owner: AddressOwner{Kind: "nlb_load_balancer", ID: "nlb-1"},
	})
	assertRetryableAndClean(t, err, "adr-own-11")
}

// The compensating free rides the same lane and owes the same answer: whoever
// reports the unreclaimed lease reports a transient peer condition, in the tone this
// product speaks — not a NOT_FOUND about an address that is demonstrably there.
func TestFreeOwnAddress_WindowNeverCloses_AnswersRetryableWithoutPeerStatus(t *testing.T) {
	addrSvc := &fakeAddressForAlloc{
		createResp: &vpcpb.Address{Id: "adr-own-12"},
		deleteErr:  status.Error(codes.NotFound, "Address adr-own-12 not found"), // never closes
	}
	conn := startFakeVPC(t, nil, nil, addrSvc, &fakeInternalAddressService{}, &fakeOperationService{})
	c, ok := newFastRetryInternalAddressClient(t, conn).(*internalAddressClient)
	require.True(t, ok)

	err := c.freeOwnAddress(ctxBackground(), "adr-own-12")
	assertRetryableAndClean(t, err, "adr-own-12")
}
