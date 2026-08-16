// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// By-lane split of the VIP-acquire failure (api-conventions §By-lane code-split).
//
// `acquireFamilyVIP` used to collapse EVERY non-Unavailable vpc error into the
// single opaque `"could not allocate load balancer address"`. That conflates two
// unrelated classes:
//
//   - CAPACITY — the pool/subnet has no free address. Opaque on purpose: the
//     exact reason is infra-capacity information (security.md
//     §инфра-чувствительные данные) and must NOT be disclosed.
//   - PEER-MISSING on a CALLER-SUPPLIED reference — the `v4Source.subnetId` the
//     caller passed does not resolve at vpc. The SYNC precheck of this very same
//     RPC already answers `"subnet <id> not found"` for that condition
//     (peer_errors.go subnetPeerErr), so saying the same thing on the async lane
//     discloses NOTHING new — while saying "could not allocate" instead is a
//     doc-untruth that misattributes a client-fixable reference problem to
//     platform capacity.
//
// Under a cross-service read-your-writes window (subnet freshly provisioned via
// vpc, not yet visible to the address-create read path) the async lane is exactly
// where this fires — and the opaque text made it indistinguishable from genuine
// exhaustion, both for the operator and for a client bounded-retry.
func TestCreate_Worker_SubnetPeerMissing_ReportsUnresolvedReference(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	addr := &fakeAddressClient{allocFunc: func(_ context.Context, _ vpcclient.AllocateInternalIPRequest, _ string) (*vpcclient.AllocateResponse, error) {
		return nil, domain.ErrNotFound
	}}
	uc := newCreateUC(repo, opsRepo, createDeps{addr: addr})
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_REGIONAL
	req.V4Source = vipSubnet(lbTestSubnetRegional)

	op, err := uc.Execute(context.Background(), req)
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.NotNil(t, final.Error)
	assert.Equal(t, int32(codes.FailedPrecondition), final.Error.GetCode())
	assert.Equal(t, "subnet "+lbTestSubnetRegional+" not found", final.Error.GetMessage(),
		"an unresolved caller-supplied subnet must read the same as on the sync precheck lane")
	assert.Empty(t, repo.lbs, "the durable handle is still compensated away")
}

// The PUBLIC (auto external VIP) lane has NO caller-supplied reference: the
// missing object is the platform AddressPool of the derived underlay zone. Its
// absence is infra-topology, so the answer must stay the opaque capacity text —
// naming the pool or the underlay zone would leak placement (security.md).
func TestCreate_Worker_PublicVIPPeerMissing_StaysOpaque(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	addr := &fakeAddressClient{extAllocFn: func(_ context.Context, _ vpcclient.AllocateExternalIPRequest, _ string) (*vpcclient.AllocateResponse, error) {
		return nil, domain.ErrNotFound
	}}
	uc := newCreateUC(repo, opsRepo, createDeps{addr: addr})
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_EXTERNAL_REGIONAL
	req.V4Source = vipPublic()

	op, err := uc.Execute(context.Background(), req)
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.NotNil(t, final.Error)
	assert.Equal(t, int32(codes.FailedPrecondition), final.Error.GetCode())
	assert.Equal(t, "could not allocate load balancer address", final.Error.GetMessage(),
		"the public lane must not disclose which infra object is missing")
}

// Observability (CWE-778 silent swallow): the client answer is deliberately
// lossy, so the server MUST log what it swallowed — otherwise an allocation
// failure is unattributable in production (exactly what blocked the diagnosis of
// this class). The log carries the cause; the CLIENT answer still does not.
func TestCreate_Worker_AllocFailure_LogsSwallowedCause(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	addr := &fakeAddressClient{allocFunc: func(_ context.Context, _ vpcclient.AllocateInternalIPRequest, _ string) (*vpcclient.AllocateResponse, error) {
		return nil, domain.ErrFailedPrecondition
	}}
	uc := newCreateUC(repo, opsRepo, createDeps{addr: addr, logger: logger})
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_REGIONAL
	req.V4Source = vipSubnet(lbTestSubnetRegional)

	op, err := uc.Execute(context.Background(), req)
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.NotNil(t, final.Error)

	logged := buf.String()
	assert.Contains(t, logged, "load_balancer_vip_acquire_failed",
		"the swallowed acquire cause must be logged for observability")
	assert.Contains(t, logged, domain.ErrFailedPrecondition.Error(),
		"the log must carry the underlying peer cause the client answer drops")
	assert.NotContains(t, final.Error.GetMessage(), domain.ErrFailedPrecondition.Error(),
		"the CLIENT answer must stay the fixed opaque text (no leak)")
}

// THIRD lane, added after CI run 30135586348: nlb's OWN freshly-allocated vpc
// Address is not yet visible to vpc's per-object authz.
//
// `allocFromCreate` commits the Address and then links it; between those two calls
// vpc answered `authz_hide_existence` (NOT_FOUND) on
// InternalAddressService/SetAddressReference because the address's owner-tuple had
// not materialised yet — four times in that run, each one killing an otherwise
// healthy LoadBalancer.Create:
//
//	{"msg":"authz_hide_existence","rpc":".../SetAddressReference",
//	 "relation":"v_update","object":"vpc_address:adrj251yyhebawpehh6h"}
//
// The client now bounded-retries that window; when it still does not close, the
// answer must be the TRANSIENT one, never the capacity text. Reporting capacity here
// was factually wrong (the address had just been allocated) and actively harmful: it
// is the one message the e2e async re-drive refuses to retry, so a transient peer
// condition presented itself as a permanent platform refusal.
func TestCreate_Worker_OwnAddressNeverVisible_IsTransientNotCapacity(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	addr := &fakeAddressClient{allocFunc: func(_ context.Context, _ vpcclient.AllocateInternalIPRequest, _ string) (*vpcclient.AllocateResponse, error) {
		// What the client returns once its own-lane visibility budget is spent.
		return nil, domain.ErrUnavailable
	}}
	uc := newCreateUC(repo, opsRepo, createDeps{addr: addr, logger: logger})
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_REGIONAL
	req.V4Source = vipSubnet(lbTestSubnetRegional)

	op, err := uc.Execute(context.Background(), req)
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.NotNil(t, final.Error)

	assert.Equal(t, int32(codes.Unavailable), final.Error.GetCode(),
		"a non-converging owner-tuple window is transient and retryable, not FAILED_PRECONDITION")
	assert.Equal(t, "load balancer address allocation unavailable", final.Error.GetMessage())
	assert.NotEqual(t, "could not allocate load balancer address", final.Error.GetMessage(),
		"must never claim capacity for a visibility lag — that text is the one the e2e "+
			"async re-drive deliberately does NOT retry")

	assert.Contains(t, buf.String(), "load_balancer_vip_acquire_failed",
		"the swallowed cause stays observable on this lane too")
	assert.Empty(t, repo.lbs, "the durable handle is compensated away")
}

// FOURTH lane — the BYO link. `acquireFamilyVIP` names three ways to obtain a
// VIP, and its own doc says every acquire failure is logged with the underlying
// cause: the client answer is deliberately lossy, so the server has to keep what
// it dropped (CWE-778). Two of the three branches did that; the link branch went
// straight to the opaque answer and recorded nothing.
//
// It is the branch whose answer is the LEAST self-explanatory. `Illegal argument
// addressId` is produced by every one of NOT_FOUND / INVALID_ARGUMENT /
// PERMISSION_DENIED / link-CAS-lost (see vpc.AttachExisting), which are four
// unrelated conditions — a tenant naming a foreign address, and the platform
// refusing our own well-formed request, read identically. Without the log an
// operation that failed hours ago cannot be attributed to any of them, and the
// only remaining evidence is the very message that was designed to carry none.
//
// Locked after CI run 31888611739, where this lane failed the async operation and
// the shard verdict could name the step but nothing could name the reason.
func TestCreate_Worker_LinkFailure_LogsSwallowedCause(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	addr := &fakeAddressClient{byoFunc: func(_ context.Context, _ vpcclient.AttachExistingRequest) (*vpcclient.AllocateResponse, error) {
		return nil, domain.ErrFailedPrecondition
	}}
	uc := newCreateUC(repo, opsRepo, createDeps{reader: &fakeAddressReader{}, addr: addr, logger: logger})
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_REGIONAL
	req.V4Source = vipAddress(lbTestAddrInternal)

	op, err := uc.Execute(context.Background(), req)
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.NotNil(t, final.Error)

	logged := buf.String()
	assert.Contains(t, logged, "load_balancer_vip_acquire_failed",
		"the link lane swallows its cause too, so it must log it like its siblings")
	assert.Contains(t, logged, domain.ErrFailedPrecondition.Error(),
		"the log must carry the underlying peer cause the client answer drops")
	assert.Contains(t, logged, lbTestAddrInternal,
		"the link lane carries no subnet_id, so the address is its only coordinate — "+
			"without it the record says what was refused but not on what")

	// The paired positive: fixing observability must NOT relax the answer. Were
	// this missing, moving the cause into the client message would also pass.
	assert.Equal(t, "Illegal argument addressId", final.Error.GetMessage(),
		"the CLIENT answer stays the fixed opaque text (anti-oracle)")
	assert.NotContains(t, final.Error.GetMessage(), domain.ErrFailedPrecondition.Error(),
		"no leak of the peer cause into the client answer")
}
