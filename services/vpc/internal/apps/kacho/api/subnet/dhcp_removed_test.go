// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
)

// VPC-1-43: DhcpOptions is dropped by design. Network-level DHCP/DNS-resolver
// knobs (dhcp_options / domain_name_servers / ntp_servers) are absent from the
// Subnet write and read contract. Reflection-based so the test compiles both
// before (RED: field present) and after (GREEN: field nil) the proto removal.
func TestSubnet_VPC_1_43_DhcpOptionsRemoved(t *testing.T) {
	fieldAbsent := func(t *testing.T, d protoreflect.MessageDescriptor, name string) {
		t.Helper()
		require.Nilf(t, d.Fields().ByName(protoreflect.Name(name)),
			"field %q must be absent from %s (VPC-1-43 DhcpOptions dropped by design)", name, d.Name())
	}

	subnetMD := (&vpcv1.Subnet{}).ProtoReflect().Descriptor()
	fieldAbsent(t, subnetMD, "dhcp_options")

	fieldAbsent(t, (&vpcv1.CreateSubnetRequest{}).ProtoReflect().Descriptor(), "dhcp_options")
	fieldAbsent(t, (&vpcv1.UpdateSubnetRequest{}).ProtoReflect().Descriptor(), "dhcp_options")

	// The DhcpOptions message type itself is removed (LEAN — no dead type/nested
	// domain_name_servers / ntp_servers reachable in the vpc v1 file descriptor).
	file := subnetMD.ParentFile()
	require.Nil(t, file.Messages().ByName("DhcpOptions"),
		"DhcpOptions message must be removed from the vpc v1 proto file")
}
