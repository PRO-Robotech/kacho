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
