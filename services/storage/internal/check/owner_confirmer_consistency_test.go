// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"
)

// owner_confirmer_consistency_test.go — the Volume owner-tuple confirm probe MUST
// read with HIGHER_CONSISTENCY (Koren-1 tail fix): the owner-tuple is written
// synchronously to the same OpenFGA store on the create path, so under the
// multi-replica deployment a default read could be served a stale-replica negative.

// fakeIAMClient — minimal InternalIAMServiceClient capturing the last CheckRequest.
// The embedded nil interface satisfies the (large) client interface; only Check is
// exercised by the confirmer.
type fakeIAMClient struct {
	iamv1.InternalIAMServiceClient
	gotReq  *iamv1.CheckRequest
	allowed bool
}

func (f *fakeIAMClient) Check(_ context.Context, req *iamv1.CheckRequest, _ ...grpc.CallOption) (*iamv1.CheckResponse, error) {
	f.gotReq = req
	return &iamv1.CheckResponse{Allowed: f.allowed}, nil
}

func TestVolumeOwnerConfirmer_UsesHigherConsistency(t *testing.T) {
	fake := &fakeIAMClient{allowed: true}
	confirmer := &VolumeOwnerConfirmer{check: &IAMCheckClient{cli: fake}}

	confirmed, err := confirmer.Confirm(
		context.Background(),
		operations.Principal{Type: "user", ID: "usr_owner"},
		"vol_123",
	)
	require.NoError(t, err)
	assert.True(t, confirmed)
	require.NotNil(t, fake.gotReq)
	assert.Equal(t, iamv1.CheckRequest_HIGHER_CONSISTENCY, fake.gotReq.GetConsistency(),
		"Volume confirm probe must read with HIGHER_CONSISTENCY (read-after-own-write)")
	assert.Contains(t, fake.gotReq.GetObject(), "vol_123")
}
