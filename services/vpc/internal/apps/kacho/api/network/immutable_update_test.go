// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/ids"
)

// VPC-1-07 / VPC-1-20: the declared supernet (ipv4/ipv6_cidr_blocks) and
// project_id are immutable through Update — the immutable-switch fires BEFORE
// corevalidate.UpdateMask so the reject carries the conventional immutable
// message (snake_case field, parity with Subnet — see placement_contract_test),
// not a generic "unknown field". Supernet is mutated only via the verb-pair
// :add-cidr-blocks / :remove-cidr-blocks. The switch is a use-case sync-guard
// that precedes any repo/ops call, so it is exercised directly at that layer.
func TestNetwork_ImmutableUpdate_SupernetAndProject(t *testing.T) {
	uc := NewUpdateNetworkUseCase(nil, nil) // switch returns before touching repo/ops
	netID := ids.NewID(ids.PrefixNetwork)

	cases := []struct {
		field string
		msg   string
	}{
		{"ipv4_cidr_blocks", "ipv4_cidr_blocks is immutable after Network.Create"}, // VPC-1-07
		{"ipv6_cidr_blocks", "ipv6_cidr_blocks is immutable after Network.Create"},
		{"project_id", "project_id is immutable after Network.Create"}, // VPC-1-20
		{"default_security_group_id", "default_security_group_id is immutable after Network.Create"},
		{"default_route_table_id", "default_route_table_id is immutable after Network.Create"},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			_, err := uc.Execute(context.Background(), UpdateInput{
				NetworkID:  netID,
				UpdateMask: []string{tc.field},
			})
			require.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.InvalidArgument, st.Code(), "field %s", tc.field)
			assert.Equal(t, tc.msg, st.Message(), "field %s immutable message (contract tone)", tc.field)
		})
	}
}
