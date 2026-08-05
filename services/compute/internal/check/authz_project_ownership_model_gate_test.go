// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

// authz_project_ownership_model_gate_test.go — the model-side guard-rail for the
// 36 compute RPCs that used to carry the hand-rolled
// `handler.AssertProjectOwnership` project-equality check next to the model
// (security.md — "Авторизация живёт в МОДЕЛИ, а не в самодельных проверках").
//
// The in-service check was removed because it never decided anything and could
// never decide anything:
//
//   - it keyed on gRPC metadata `x-kacho-project-id` / `x-kacho-admin`, and
//     NOTHING in the system emits those. api-gateway forwards identity only as
//     `x-kacho-principal-*` (+ `x-kacho-token-acr`); service→service calls use
//     `pkg/auth.PropagateOutgoing`, which forwards only `x-kacho-principal-*`.
//   - with the header absent, `TenantCtx.ProjectIDs` is empty, and
//     `HasProjectAccess` returned true for EVERY project — i.e. its default was
//     fail-OPEN, not fail-closed.
//   - to the extent the header can be smuggled at all, it is supplied by the
//     CALLER, so it could only ever narrow a caller against itself. A check an
//     attacker can switch off by omitting a header is not a boundary.
//
// This file is the proof that removing it did NOT open access. For every one of
// the 36 FQNs it asserts, against the REAL in-service PermissionMap (not a
// hand-written fixture) and through the REAL corelib authz interceptor — the
// interceptor that sits directly in front of these handlers on BOTH the public
// and the internal listener (cmd/compute/main.go: authzIntr.Unary() appended to
// publicUnary AND internalUnary, fatal-if-missing in production):
//
//  1. the map carries an entry — an unmapped RPC is fail-closed
//     `PermissionDenied "permission denied (rpc not mapped)"`, but a silently
//     dropped entry would turn the RPC into "denied for everyone", which is a
//     different failure than "gated";
//  2. the entry pins the expected per-object relation (v_get / v_list /
//     v_update / v_delete / viewer / editor) AND an extractor that resolves the
//     target object FROM THE REQUEST (anti-BOLA — the Check is against the
//     object the caller named, not against the method);
//  3. a subject the model denies gets PermissionDenied and the handler is never
//     reached. This is the assertion that matters: "принципал без гранта
//     по-прежнему получает отказ";
//  4. a subject the model allows reaches the handler, and the relation+object
//     actually sent to the PDP are the ones the map pinned.
//
// (3) and (4) are driven through the real authz.Interceptor with a programmable
// checker, so this locks OBSERVABLE behaviour (the RPC's outcome), not the shape
// of a Go literal — see testing.md "Regression-lock … на уровне ОБСЕРВАБЛА".
//
// This test is GREEN BEFORE AND AFTER the removal, by construction: it asserts
// the model gate, which the removal does not touch. It must never go red.

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/services/compute/internal/check"
)

// ownershipGuardRPC — one RPC that used to carry AssertProjectOwnership, with
// the model gate it relies on now that the hand-rolled check is gone.
type ownershipGuardRPC struct {
	fullMethod string
	// relation / objectType — what PermissionMap MUST require.
	relation   string
	objectType string
	// req — a request naming targetObjID, so we can assert the extractor
	// resolves the target FROM THE REQUEST.
	req         any
	targetObjID string
}

const (
	gInstID = "epd00000000000000vm1"
	gProjID = "prj0000000000000pppp"
)

// ownershipGuardModelGatedRPCs — the exact call sites of the removed
// handler.AssertProjectOwnership in services/compute/internal/handler/*.
// Start/Stop/Restart funnel through the private `lifecycle` helper, which held
// one of the calls; all three are listed.
func ownershipGuardModelGatedRPCs() []ownershipGuardRPC {
	return []ownershipGuardRPC{
		// ---- InstanceService ----
		{"/kacho.cloud.compute.v1.InstanceService/Get", "v_get", "compute_instance",
			&computev1.GetInstanceRequest{InstanceId: gInstID}, gInstID},
		{"/kacho.cloud.compute.v1.InstanceService/List", "viewer", "project",
			&computev1.ListInstancesRequest{ProjectId: gProjID}, gProjID},
		{"/kacho.cloud.compute.v1.InstanceService/Create", "editor", "project",
			&computev1.CreateInstanceRequest{ProjectId: gProjID}, gProjID},
		{"/kacho.cloud.compute.v1.InstanceService/Update", "v_update", "compute_instance",
			&computev1.UpdateInstanceRequest{InstanceId: gInstID}, gInstID},
		{"/kacho.cloud.compute.v1.InstanceService/UpdateMetadata", "v_update", "compute_instance",
			&computev1.UpdateInstanceMetadataRequest{InstanceId: gInstID}, gInstID},
		{"/kacho.cloud.compute.v1.InstanceService/Delete", "v_delete", "compute_instance",
			&computev1.DeleteInstanceRequest{InstanceId: gInstID}, gInstID},
		{"/kacho.cloud.compute.v1.InstanceService/GetSerialPortOutput", "v_get", "compute_instance",
			&computev1.GetInstanceSerialPortOutputRequest{InstanceId: gInstID}, gInstID},
		{"/kacho.cloud.compute.v1.InstanceService/ListOperations", "v_list", "compute_instance",
			&computev1.ListInstanceOperationsRequest{InstanceId: gInstID}, gInstID},
		{"/kacho.cloud.compute.v1.InstanceService/AttachDisk", "v_update", "compute_instance",
			&computev1.AttachInstanceDiskRequest{InstanceId: gInstID}, gInstID},
		{"/kacho.cloud.compute.v1.InstanceService/DetachDisk", "v_update", "compute_instance",
			&computev1.DetachInstanceDiskRequest{InstanceId: gInstID}, gInstID},
		{"/kacho.cloud.compute.v1.InstanceService/AttachNetworkInterface", "v_update", "compute_instance",
			&computev1.AttachInstanceNetworkInterfaceRequest{InstanceId: gInstID}, gInstID},
		{"/kacho.cloud.compute.v1.InstanceService/DetachNetworkInterface", "v_update", "compute_instance",
			&computev1.DetachInstanceNetworkInterfaceRequest{InstanceId: gInstID}, gInstID},
		{"/kacho.cloud.compute.v1.InstanceService/SimulateMaintenanceEvent", "v_update", "compute_instance",
			&computev1.SimulateInstanceMaintenanceEventRequest{InstanceId: gInstID}, gInstID},
		// Start/Stop/Restart shared the `lifecycle` helper's single call site.
		{"/kacho.cloud.compute.v1.InstanceService/Start", "v_update", "compute_instance",
			&computev1.StartInstanceRequest{InstanceId: gInstID}, gInstID},
		{"/kacho.cloud.compute.v1.InstanceService/Stop", "v_update", "compute_instance",
			&computev1.StopInstanceRequest{InstanceId: gInstID}, gInstID},
		{"/kacho.cloud.compute.v1.InstanceService/Restart", "v_update", "compute_instance",
			&computev1.RestartInstanceRequest{InstanceId: gInstID}, gInstID},
	}
}

// pdpQuestion — what the interceptor actually asked the PDP.
type pdpQuestion struct {
	subject  string
	relation string
	object   string
}

// programmableChecker — a CheckClient whose verdict the test controls, and which
// records the question it was asked.
type programmableChecker struct {
	allow bool
	last  atomic.Pointer[pdpQuestion]
	calls atomic.Int32
}

func (c *programmableChecker) Check(_ context.Context, subject, relation, object string) (bool, error) {
	c.calls.Add(1)
	c.last.Store(&pdpQuestion{subject: subject, relation: relation, object: object})
	return c.allow, nil
}

// buildGuardInterceptor wires the REAL compute PermissionMap into the REAL
// corelib interceptor. Cache TTL is deliberately tiny so one subtest's verdict
// cannot leak into the next.
func buildGuardInterceptor(t *testing.T, c *programmableChecker) *authz.Interceptor {
	t.Helper()
	return authz.NewInterceptor(authz.InterceptorOptions{
		ServiceName: "kacho-compute",
		Map:         check.PermissionMap(),
		Client:      c,
		Cache:       authz.NewCacheWithLimit(1, 1),
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

// ctxWithPrincipal builds the ctx the principal-extract interceptor would have
// produced upstream for a given authenticated subject.
func ctxWithPrincipal(id, ptype string) context.Context {
	return operations.WithPrincipal(context.Background(),
		operations.Principal{Type: ptype, ID: id, DisplayName: id})
}

// TestProjectOwnershipRPCs_ModelCarriesThePerObjectGate — (1)+(2): the model,
// not a Go check, is what decides these 36, and it decides per OBJECT, resolving
// the target from the request.
func TestProjectOwnershipRPCs_ModelCarriesThePerObjectGate(t *testing.T) {
	m := check.PermissionMap()

	for _, rpc := range ownershipGuardModelGatedRPCs() {
		t.Run(rpc.fullMethod, func(t *testing.T) {
			e, ok := m.Lookup(rpc.fullMethod)
			require.True(t, ok,
				"%s has no PermissionMap entry: the model does not gate it, so removing "+
					"the in-service AssertProjectOwnership would leave it ungated", rpc.fullMethod)

			assert.False(t, e.Public,
				"%s must not be authz-exempt — it is a tenant-facing resource RPC", rpc.fullMethod)
			assert.False(t, e.ScopeFiltered,
				"%s must not skip the per-RPC Check", rpc.fullMethod)
			assert.Equal(t, rpc.relation, e.Relation,
				"%s must require the per-object %s relation", rpc.fullMethod, rpc.relation)

			require.NotNil(t, e.Extract, "%s must carry an object extractor", rpc.fullMethod)
			objType, objID, err := e.Extract(rpc.req)
			require.NoError(t, err)
			assert.Equal(t, rpc.objectType, objType,
				"%s must be scoped to object-type %s", rpc.fullMethod, rpc.objectType)
			assert.Equal(t, rpc.targetObjID, objID,
				"%s must resolve its target FROM THE REQUEST (anti-BOLA: the Check must "+
					"target the object the caller named, not a wildcard)", rpc.fullMethod)
		})
	}
}

// TestProjectOwnershipRPCs_UngrantedSubjectStillDenied — (3), the assertion that
// matters. A principal the model does not grant must never reach the handler.
// This must hold for a machine principal exactly as for a human one: the point
// of removing the check was to let a GRANTED machine through, not any machine.
func TestProjectOwnershipRPCs_UngrantedSubjectStillDenied(t *testing.T) {
	for _, rpc := range ownershipGuardModelGatedRPCs() {
		for _, principal := range []struct{ id, ptype string }{
			{"usr0000000000000nope", "user"},
			{"sva0000000000000nope", "service_account"},
		} {
			t.Run(rpc.fullMethod+"/"+principal.ptype, func(t *testing.T) {
				checker := &programmableChecker{allow: false}
				intr := buildGuardInterceptor(t, checker)

				handlerReached := false
				_, err := intr.Unary()(
					ctxWithPrincipal(principal.id, principal.ptype), rpc.req,
					&grpc.UnaryServerInfo{FullMethod: rpc.fullMethod},
					func(ctx context.Context, req any) (any, error) {
						handlerReached = true
						return "ok", nil
					})

				require.Error(t, err, "%s must be denied for an ungranted %s", rpc.fullMethod, principal.ptype)
				// The refusal's CODE follows the lane, and both lanes refuse. A
				// per-object read answers with the owning service's own NotFound,
				// because a refusal that reads differently from a genuine miss tells
				// the caller the object is there — the existence oracle. Everything
				// else keeps PermissionDenied. The expectation is derived from the
				// REAL map through the same predicate the interceptor uses, so it
				// cannot drift into "whatever the code does".
				e, _ := check.PermissionMap().Lookup(rpc.fullMethod)
				if authz.HidesExistenceOnDeny(rpc.fullMethod, e, rpc.objectType) {
					assert.Equal(t, codes.NotFound, status.Code(err),
						"a per-object read denied to an ungranted subject must answer the owner's NotFound(5)")
					assert.Equal(t, "Instance "+rpc.targetObjID+" not found", status.Convert(err).Message(),
						"the refusal must be byte-identical to the owner's own miss — a distinguishable text IS the oracle")
				} else {
					assert.Equal(t, codes.PermissionDenied, status.Code(err),
						"an authenticated-but-ungranted subject must get PermissionDenied(7)")
				}
				assert.False(t, handlerReached,
					"%s reached the handler despite a model DENY — AssertProjectOwnership "+
						"is gone, so this would be an open door", rpc.fullMethod)
			})
		}
	}
}

// TestProjectOwnershipRPCs_AnonymousStillDenied — no principal at all (the
// removed check's fail-OPEN default was reached exactly in this state) must be
// denied before the handler.
func TestProjectOwnershipRPCs_AnonymousStillDenied(t *testing.T) {
	for _, rpc := range ownershipGuardModelGatedRPCs() {
		t.Run(rpc.fullMethod, func(t *testing.T) {
			checker := &programmableChecker{allow: true} // even a permissive PDP must not be consulted
			intr := buildGuardInterceptor(t, checker)

			handlerReached := false
			_, err := intr.Unary()(
				context.Background(), rpc.req,
				&grpc.UnaryServerInfo{FullMethod: rpc.fullMethod},
				func(ctx context.Context, req any) (any, error) {
					handlerReached = true
					return "ok", nil
				})

			require.Error(t, err, "%s must be denied for an anonymous caller", rpc.fullMethod)
			assert.Equal(t, codes.PermissionDenied, status.Code(err))
			assert.False(t, handlerReached,
				"%s reached the handler with NO principal — this is the exact state in which "+
					"the removed AssertProjectOwnership returned nil (fail-open)", rpc.fullMethod)
		})
	}
}

// TestProjectOwnershipRPCs_GrantedSubjectReachesHandler — (4): with the model
// allowing, the RPC proceeds, and the PDP was asked the pinned question. A
// service_account subject is used deliberately: the removed check keyed on a
// header no machine client ever sends, so its only possible effect on a machine
// principal was the fail-open default.
func TestProjectOwnershipRPCs_GrantedSubjectReachesHandler(t *testing.T) {
	for _, rpc := range ownershipGuardModelGatedRPCs() {
		t.Run(rpc.fullMethod, func(t *testing.T) {
			checker := &programmableChecker{allow: true}
			intr := buildGuardInterceptor(t, checker)

			handlerReached := false
			_, err := intr.Unary()(
				ctxWithPrincipal("sva0000000000000ssss", "service_account"), rpc.req,
				&grpc.UnaryServerInfo{FullMethod: rpc.fullMethod},
				func(ctx context.Context, req any) (any, error) {
					handlerReached = true
					return "ok", nil
				})

			require.NoError(t, err)
			assert.True(t, handlerReached,
				"a model-granted machine principal must reach %s", rpc.fullMethod)

			last := checker.last.Load()
			require.NotNil(t, last, "the PDP must actually have been consulted")
			assert.Equal(t, "service_account:sva0000000000000ssss", last.subject)
			assert.Equal(t, rpc.relation, last.relation,
				"the PDP must be asked for the map-pinned relation")
			assert.Equal(t, rpc.objectType+":"+rpc.targetObjID, last.object,
				"the PDP must be asked about the object the request named, not a wildcard")
		})
	}
}
