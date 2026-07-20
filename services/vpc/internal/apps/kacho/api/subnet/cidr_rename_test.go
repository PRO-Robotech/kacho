// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

import (
	"testing"

	"github.com/stretchr/testify/require"

	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// VPC-1 F7 CIDR-rename presentation contract: the flat repo array
// V4CidrBlocks/V6CidrBlocks projects onto the redesign proto shape —
// ipv4_cidr_primary = blocks[0] (immutable anchor), ipv4_cidr_blocks = blocks[1:]
// (additional ranges). Same split for v6.
func TestSubnetToPb_VPC_1_F7_CidrPrimarySplit(t *testing.T) {
	t.Run("v4_primary_plus_additional", func(t *testing.T) {
		rec := &kachorepo.SubnetRecord{}
		rec.ID = "sub-t4k8n2q0m5v9h1"
		rec.V4CidrBlocks = []string{"10.20.0.0/24", "10.20.8.0/24"}

		pb, err := subnetToPb(rec)
		require.NoError(t, err)
		require.Equal(t, "10.20.0.0/24", pb.Ipv4CidrPrimary, "primary must be blocks[0]")
		require.Equal(t, []string{"10.20.8.0/24"}, pb.Ipv4CidrBlocks, "additional must be blocks[1:]")
	})

	t.Run("v6_primary_plus_additional", func(t *testing.T) {
		rec := &kachorepo.SubnetRecord{}
		rec.ID = "sub-t4k8n2q0m5v9h1"
		rec.V6CidrBlocks = []string{"fd00:20::/64", "fd00:20:1::/64"}

		pb, err := subnetToPb(rec)
		require.NoError(t, err)
		require.Equal(t, "fd00:20::/64", pb.Ipv6CidrPrimary)
		require.Equal(t, []string{"fd00:20:1::/64"}, pb.Ipv6CidrBlocks)
	})

	t.Run("v6_only_subnet_empty_v4_primary", func(t *testing.T) {
		rec := &kachorepo.SubnetRecord{}
		rec.ID = "sub-t4k8n2q0m5v9h1"
		rec.V6CidrBlocks = []string{"fd00:20::/64"}

		pb, err := subnetToPb(rec)
		require.NoError(t, err)
		require.Equal(t, "", pb.Ipv4CidrPrimary, "v4-less subnet has empty ipv4 primary")
		require.Empty(t, pb.Ipv4CidrBlocks)
		require.Equal(t, "fd00:20::/64", pb.Ipv6CidrPrimary)
		require.Empty(t, pb.Ipv6CidrBlocks)
	})

	t.Run("single_block_no_additional", func(t *testing.T) {
		rec := &kachorepo.SubnetRecord{}
		rec.ID = "sub-t4k8n2q0m5v9h1"
		rec.V4CidrBlocks = []string{"10.20.0.0/24"}

		pb, err := subnetToPb(rec)
		require.NoError(t, err)
		require.Equal(t, "10.20.0.0/24", pb.Ipv4CidrPrimary)
		require.Empty(t, pb.Ipv4CidrBlocks)
	})
}
