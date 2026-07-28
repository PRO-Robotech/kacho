// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package address

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
)

// TestApplyAddressSpec_NoBranchSelected_ReturnsErrorNotPanic pins the dispatch to
// say what it means.
//
// `address_spec` is a oneof with four arms. The dispatch used to send three of
// them to their own case and everything else to `default:`, where it read
// `in.ExternalIpv6Spec` straight away — so `default:` did not mean "unknown
// branch", it meant "must be the fourth one". Nothing local said so. The only
// thing keeping that dereference safe was a guard in Execute, several hundred
// lines and one call away, and the comment above the switch asserted the
// invariant rather than the code enforcing it.
//
// This is not theoretical. A fixture that wrapped the oneof in its own name had
// the edge discard the wrapper whole, and what reached the service was a request
// with no branch selected at all — four call sites, every archived run, for
// months. The guard held. Had it not, or had a fifth arm been added and routed to
// `default:` by omission, the same input would have dereferenced nil inside the
// operation worker instead of being refused at the front door.
//
// A branch must be selected by its own condition, and `default:` must be a
// refusal.
func TestApplyAddressSpec_NoBranchSelected_ReturnsErrorNotPanic(t *testing.T) {
	u := &CreateAddressUseCase{}
	a := &domain.Address{}

	err := u.applyAddressSpec(context.Background(), a, CreateInput{ProjectID: "prj-x"})

	require.Error(t, err, "a request with no oneof arm selected must be refused, not applied")
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Equal(t, "address_spec required", status.Convert(err).Message(),
		"the refusal keeps the contract tone Execute already uses for this input")
	require.Empty(t, a.Type, "nothing may be written onto the address when no branch was chosen")
	require.Nil(t, a.ExternalIpv6, "least of all the arm that happened to sit in default")
}

// TestApplyAddressSpec_ExternalIpv6_SelectedByItsOwnCondition is the positive half:
// the fourth arm still works, and now it is reached because it was named, not
// because everything else was excluded.
func TestApplyAddressSpec_ExternalIpv6_SelectedByItsOwnCondition(t *testing.T) {
	u := &CreateAddressUseCase{}
	a := &domain.Address{}

	err := u.applyAddressSpec(context.Background(), a, CreateInput{
		ProjectID:        "prj-x",
		ExternalIpv6Spec: &ExternalAddrSpec{ZoneID: "zone-a"},
	})

	require.NoError(t, err)
	require.Equal(t, domain.AddressTypeExternal, a.Type)
	require.Equal(t, domain.IpVersionIPv6, a.IpVersion)
	require.NotNil(t, a.ExternalIpv6)
	require.Equal(t, "zone-a", a.ExternalIpv6.ZoneID)
}
