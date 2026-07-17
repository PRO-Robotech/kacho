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
)

// check_client_consistency_test.go — the compute owner-tuple confirm probe reads
// with HIGHER_CONSISTENCY (Koren-1 tail fix): the owner-tuple is written
// synchronously to the same OpenFGA store on the create path, so under the
// multi-replica deployment a default read could be served a stale-replica negative.

type fakeIAMClient struct {
	iamv1.InternalIAMServiceClient
	gotReq  *iamv1.CheckRequest
	allowed bool
}

func (f *fakeIAMClient) Check(_ context.Context, req *iamv1.CheckRequest, _ ...grpc.CallOption) (*iamv1.CheckResponse, error) {
	f.gotReq = req
	return &iamv1.CheckResponse{Allowed: f.allowed}, nil
}

// Default Check keeps the cache-eligible read (hot enforcement gate).
func TestIAMCheckClient_Check_DefaultConsistency(t *testing.T) {
	fake := &fakeIAMClient{allowed: true}
	c := &IAMCheckClient{cli: fake}
	_, err := c.Check(context.Background(), "user:u1", "v_update", "compute_instance:cmp_1")
	require.NoError(t, err)
	require.NotNil(t, fake.gotReq)
	assert.Equal(t, iamv1.CheckRequest_CONSISTENCY_UNSPECIFIED, fake.gotReq.GetConsistency())
}

// CheckConsistent forces HIGHER_CONSISTENCY (read-after-own-write confirm probe).
func TestIAMCheckClient_CheckConsistent_HigherConsistency(t *testing.T) {
	fake := &fakeIAMClient{allowed: true}
	c := &IAMCheckClient{cli: fake}
	allowed, err := c.CheckConsistent(context.Background(), "user:u1", "v_update", "compute_instance:cmp_1")
	require.NoError(t, err)
	assert.True(t, allowed)
	require.NotNil(t, fake.gotReq)
	assert.Equal(t, iamv1.CheckRequest_HIGHER_CONSISTENCY, fake.gotReq.GetConsistency(),
		"compute confirm probe must read with HIGHER_CONSISTENCY")
}
