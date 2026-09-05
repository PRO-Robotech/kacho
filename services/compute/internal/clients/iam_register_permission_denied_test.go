// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients_test

// iam_register_permission_denied_test.go — a denial on authorization grounds from
// kaname is TERMINAL for the register drainer.
//
// Repeating an identical request IAM refused on authorization grounds cannot start
// succeeding: the decision is a function of (caller, relation, object) and a retry
// changes none of them. Classifying it transient therefore does not buy a later
// success — it buys a row that never reaches the poison gate (the drainer caps a
// transient row's attempt_count one below MaxAttempts on purpose) and so stays in
// the claim query's blocking set forever, wedging every later row of the same
// partition. Partitions are keyed per resource and a resource's UNregistration is
// queued behind its registration, so the wedge lets a grant outlive the resource it
// grants. Poisoning is the safe direction: the write never happened, the partition
// unblocks, and the periodic redrive backstop replays the poisoned row, so a cause
// that was temporary succeeds on a later pass.

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/outbox/drainer"

	"github.com/PRO-Robotech/kacho/services/compute/internal/clients"
	"github.com/PRO-Robotech/kacho/services/compute/internal/fgaintent"
)

// denyingRegisterClient refuses every call on authorization grounds.
type denyingRegisterClient struct{}

func (denyingRegisterClient) RegisterResource(
	context.Context, *iamv1.RegisterResourceRequest, ...grpc.CallOption,
) (*iamv1.RegisterResourceResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "no fga_writer relation on iam_fgaproxy:system")
}

func (denyingRegisterClient) UnregisterResource(
	context.Context, *iamv1.UnregisterResourceRequest, ...grpc.CallOption,
) (*iamv1.UnregisterResourceResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "no fga_writer relation on iam_fgaproxy:system")
}

func TestIAMRegisterApplier_PermissionDenied_IsPermanent(t *testing.T) {
	tuple, ok := fgaintent.ProjectHierarchyTuple("Instance", "ins-aaaaaaaaaaaaaaaaa", "prj-aaaaaaaaaaaaaaaa")
	require.True(t, ok, "fixture tuple must build")

	applier := clients.NewIAMRegisterApplierWithClient(denyingRegisterClient{})

	for _, event := range []string{fgaintent.EventRegister, fgaintent.EventUnregister} {
		t.Run(event, func(t *testing.T) {
			err := applier.Apply(context.Background(), event,
				fgaintent.Payload{Tuples: []fgaintent.Tuple{tuple}})
			require.Error(t, err)
			assert.Truef(t, errors.Is(err, drainer.ErrPermanent),
				"%s refused on authorization grounds must poison: an identical retry cannot succeed, "+
					"and a head that never poisons wedges its partition — including the unregistration behind it", event)
		})
	}
}
