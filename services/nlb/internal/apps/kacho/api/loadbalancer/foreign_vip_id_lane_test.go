// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Foreign VIP-source ids (`v4Source/v6Source.subnetId` / `.addressId`) are owned
// by kacho-vpc, so their LANE is fixed by api-conventions §By-lane code-split:
//
//   - existence is decided by the OWNER (peer-validate), never locally;
//   - a well-formed-but-absent foreign id NEVER answers on the own-resource
//     direct-read lane (`NOT_FOUND` "<Resource> <id> not found") — that lane means
//     "I did not find MY row";
//   - nlb asserts NO foreign TYPE: an id carrying another domain's prefix must
//     travel to the owner untouched (B4 "чужой prefix — не наш словарь").
//
// What nlb DOES keep is a family-agnostic SYNTACTIC gate — `corevalidate.ResourceID`
// ignores its `expectedPrefix` argument by contract and only asks "is the first
// segment a prefix from the PLATFORM catalog" (`ids.KnownPrefixes()` /
// `ids.KnownHyphenPrefixes()` + the two config escape hatches). That catalog is a
// corelib-owned shared artifact, not vpc's private dictionary, so the drift hazard
// B4 guards against does not apply. The decision to keep it is recorded in
// docs/architecture/08-known-divergences.md; these tests lock what the CALLER sees
// on every branch of it.
//
// Verifies: api-conventions.md §By-lane code-split (B4) · nlb 08-known-divergences
// §"Формат чужого id (VIP-источники)".

// countingSubnetClient — SubnetClient double that records whether the owner was
// consulted at all. The "was the peer reached" bit is load-bearing here: the whole
// point of the sync gate is that obvious garbage is refused WITHOUT a peer call.
type countingSubnetClient struct {
	calls []string
	fn    func(ctx context.Context, id string) (*vpcclient.Subnet, error)
}

func (c *countingSubnetClient) Get(ctx context.Context, id string) (*vpcclient.Subnet, error) {
	c.calls = append(c.calls, id)
	// Mirrors the production adapter's own empty-guard (clients/vpc/subnet_client.go)
	// so that what these tests observe is what a caller observes.
	if id == "" {
		return nil, fmt.Errorf("%w: subnet_id is empty", domain.ErrInvalidArg)
	}
	if c.fn != nil {
		return c.fn(ctx, id)
	}
	return &vpcclient.Subnet{
		ID: id, ProjectID: "prj-a", NetworkID: "net-1",
		PlacementType: vpcclient.SubnetPlacementRegional, RegionID: "region-1",
	}, nil
}

// countingAddressReader — AddressClient double with the same call ledger.
type countingAddressReader struct {
	calls []string
	fn    func(ctx context.Context, id string) (*vpcclient.Address, error)
}

func (c *countingAddressReader) Get(ctx context.Context, id string) (*vpcclient.Address, error) {
	c.calls = append(c.calls, id)
	// Mirrors the production adapter's own empty-guard (clients/vpc/address_client.go).
	if id == "" {
		return nil, fmt.Errorf("%w: address_id is empty", domain.ErrInvalidArg)
	}
	if c.fn != nil {
		return c.fn(ctx, id)
	}
	return &vpcclient.Address{
		ID: id, ProjectID: "prj-a", Family: vpcclient.AddressFamilyIPv4,
		SubnetID: "sub-of-adr",
	}, nil
}

// vpcSubnetMissErr reproduces what the production adapter hands the use-case when
// vpc answers NotFound: `mapSubnetErr` folds NotFound into domain.ErrInvalidArg.
func vpcSubnetMissErr(id string) error {
	return fmt.Errorf("%w: Subnet %s not found", domain.ErrInvalidArg, id)
}

// internalRegionalReq — INTERNAL_REGIONAL Create skeleton (subnet/address sources
// are only legal for an INTERNAL load balancer).
func internalRegionalReq() *lbv1.CreateNetworkLoadBalancerRequest {
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_REGIONAL
	return req
}

// ---- malformed foreign id → terminal sync 400, owner not consulted -----------

// A `subnetId` that is not a Kachō id in EITHER form is refused synchronously with
// the conventional format tone `"invalid <res> id '<X>'"`, and vpc is never dialled.
func TestCreateLB_ForeignSubnetID_Malformed_SyncFormatRejectWithoutPeer(t *testing.T) {
	t.Parallel()
	sn := &countingSubnetClient{}
	uc := newCreateUC(newFakeRepo(), newFakeOpsRepo(), createDeps{subnet: sn})
	req := internalRegionalReq()
	req.V4Source = vipSubnet("garbage!!")

	_, err := uc.Execute(context.Background(), req)

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, "invalid subnet id 'garbage!!'", status.Convert(err).Message(),
		"format tone is part of the contract — a non-id must not be reported as a miss")
	assert.Empty(t, sn.calls, "obvious garbage must not cost a call to the owner")
}

// Same on the linked-address branch.
func TestCreateLB_ForeignAddressID_Malformed_SyncFormatRejectWithoutPeer(t *testing.T) {
	t.Parallel()
	ar := &countingAddressReader{}
	uc := newCreateUC(newFakeRepo(), newFakeOpsRepo(), createDeps{reader: ar})
	req := internalRegionalReq()
	req.V4Source = vipAddress("garbage!!")

	_, err := uc.Execute(context.Background(), req)

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, "invalid address id 'garbage!!'", status.Convert(err).Message())
	assert.Empty(t, ar.calls, "obvious garbage must not cost a call to the owner")
}

// ---- the property that decides the trade-off --------------------------------

// The gate is what makes a malformed foreign id TERMINAL. Drop it and the same
// request becomes `UNAVAILABLE` whenever vpc is down or unwired — telling the
// caller to retry input that can never succeed. Locked on both branches.
func TestCreateLB_ForeignSubnetID_Malformed_TerminalEvenWhenOwnerUnavailable(t *testing.T) {
	t.Parallel()
	sn := &countingSubnetClient{fn: func(context.Context, string) (*vpcclient.Subnet, error) {
		return nil, fmt.Errorf("%w: vpc down", domain.ErrUnavailable)
	}}
	uc := newCreateUC(newFakeRepo(), newFakeOpsRepo(), createDeps{subnet: sn})
	req := internalRegionalReq()
	req.V4Source = vipSubnet("garbage!!")

	_, err := uc.Execute(context.Background(), req)

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err),
		"a non-id is client-fixable — it must never be dressed up as a retryable peer outage")
	assert.Equal(t, "invalid subnet id 'garbage!!'", status.Convert(err).Message())
}

func TestCreateLB_ForeignAddressID_Malformed_TerminalEvenWhenOwnerUnavailable(t *testing.T) {
	t.Parallel()
	ar := &countingAddressReader{fn: func(context.Context, string) (*vpcclient.Address, error) {
		return nil, fmt.Errorf("%w: vpc down", domain.ErrUnavailable)
	}}
	uc := newCreateUC(newFakeRepo(), newFakeOpsRepo(), createDeps{reader: ar})
	req := internalRegionalReq()
	req.V4Source = vipAddress("garbage!!")

	_, err := uc.Execute(context.Background(), req)

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, "invalid address id 'garbage!!'", status.Convert(err).Message())
}

// ---- no local TYPE assertion on a foreign id (B4 proper) --------------------

// An id whose prefix belongs to ANOTHER resource type is still well-formed for the
// platform router. nlb must not adjudicate foreign typing: the id travels to vpc
// verbatim and the OWNER answers. (`corevalidate.ResourceID` is family-agnostic by
// contract — the `ids.PrefixSubnet` argument documents intent, it does not gate.)
func TestCreateLB_ForeignSubnetID_KnownPrefixOtherFamily_ReachesOwner(t *testing.T) {
	t.Parallel()
	const otherFamilyID = "nlbaaaaaaaaaaaaaaaaa" // `nlb` prefix — a load balancer id
	sn := &countingSubnetClient{fn: func(_ context.Context, id string) (*vpcclient.Subnet, error) {
		return nil, vpcSubnetMissErr(id)
	}}
	uc := newCreateUC(newFakeRepo(), newFakeOpsRepo(), createDeps{subnet: sn})
	req := internalRegionalReq()
	req.V4Source = vipSubnet(otherFamilyID)

	_, err := uc.Execute(context.Background(), req)

	require.Error(t, err)
	assert.Equal(t, []string{otherFamilyID}, sn.calls,
		"a foreign prefix is not our dictionary — the owner decides, not a local type check")
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, "subnet "+otherFamilyID+" not found", status.Convert(err).Message(),
		"the answer is the owner's existence verdict, not a local format verdict")
}

// ---- absent foreign id answers on the PEER lane, never the own lane ---------

// api-conventions: NOT_FOUND is the direct-read lane — "I did not find MY row".
// A foreign id that the owner cannot resolve must NOT borrow it.
func TestCreateLB_ForeignSubnetID_WellFormedAbsent_PeerLaneNotOwnNotFound(t *testing.T) {
	t.Parallel()
	const absent = "sub-absent"
	sn := &countingSubnetClient{fn: func(_ context.Context, id string) (*vpcclient.Subnet, error) {
		return nil, vpcSubnetMissErr(id)
	}}
	uc := newCreateUC(newFakeRepo(), newFakeOpsRepo(), createDeps{subnet: sn})
	req := internalRegionalReq()
	req.V4Source = vipSubnet(absent)

	_, err := uc.Execute(context.Background(), req)

	require.Error(t, err)
	assert.Equal(t, []string{absent}, sn.calls, "existence is the owner's call")
	assert.NotEqual(t, codes.NotFound, status.Code(err),
		"NOT_FOUND is the own-resource direct-read lane — a foreign id must not use it")
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, "subnet "+absent+" not found", status.Convert(err).Message())
}

// Address branch: same lane rule, but the text stays generic (anti-oracle — nlb
// never confirms anything about somebody else's address).
func TestCreateLB_ForeignAddressID_WellFormedAbsent_PeerLaneNotOwnNotFound(t *testing.T) {
	t.Parallel()
	const absent = "adr-absent"
	ar := &countingAddressReader{fn: func(_ context.Context, id string) (*vpcclient.Address, error) {
		return nil, fmt.Errorf("%w: address %s not found", domain.ErrInvalidArg, id)
	}}
	uc := newCreateUC(newFakeRepo(), newFakeOpsRepo(), createDeps{reader: ar})
	req := internalRegionalReq()
	req.V4Source = vipAddress(absent)

	_, err := uc.Execute(context.Background(), req)

	require.Error(t, err)
	assert.Equal(t, []string{absent}, ar.calls, "existence is the owner's call")
	assert.NotEqual(t, codes.NotFound, status.Code(err))
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, "Illegal argument addressId", status.Convert(err).Message())
}

// ---- an existing foreign id passes ------------------------------------------

// The gate refuses non-ids only: a real vpc subnet id is forwarded verbatim and
// the Create proceeds.
func TestCreateLB_ForeignSubnetID_Existing_ForwardedVerbatimAndAccepted(t *testing.T) {
	t.Parallel()
	sn := &countingSubnetClient{}
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	uc := newCreateUC(repo, opsRepo, createDeps{subnet: sn})
	req := internalRegionalReq()
	req.V4Source = vipSubnet(lbTestSubnetRegional)

	op, err := uc.Execute(context.Background(), req)

	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
	assert.Equal(t, []string{lbTestSubnetRegional}, sn.calls,
		"the caller-supplied foreign id reaches the owner unmodified")
}

// ---- an omitted foreign id is a request-shape error, not a phantom miss ------

// `v4Source{subnetId:""}` selects the oneof branch with no value. That is a
// malformed REQUEST (the branch demands a reference and none was given), and it
// must read as one. Letting it through produced `"subnet  not found"` — a
// contract-tone message with a hole in it, asserting the absence of a resource the
// caller never named.
func TestCreateLB_ForeignSubnetID_Empty_RejectedAsMissingReference(t *testing.T) {
	t.Parallel()
	sn := &countingSubnetClient{}
	uc := newCreateUC(newFakeRepo(), newFakeOpsRepo(), createDeps{subnet: sn})
	req := internalRegionalReq()
	req.V4Source = vipSubnet("")

	_, err := uc.Execute(context.Background(), req)

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, "v4_source.subnet_id: required", status.Convert(err).Message(),
		"the caller must read a request-shape error, not `subnet  not found` — a "+
			"contract-tone miss with the id spliced out")
	assert.Empty(t, sn.calls, "an unnamed reference is not a question for the owner")
}

func TestCreateLB_ForeignAddressID_Empty_RejectedAsMissingReference(t *testing.T) {
	t.Parallel()
	ar := &countingAddressReader{}
	uc := newCreateUC(newFakeRepo(), newFakeOpsRepo(), createDeps{reader: ar})
	req := internalRegionalReq()
	req.V6Source = vipAddress("")

	_, err := uc.Execute(context.Background(), req)

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, "v6_source.address_id: required", status.Convert(err).Message())
	assert.Empty(t, ar.calls, "an unnamed reference is not a question for the owner")
}
