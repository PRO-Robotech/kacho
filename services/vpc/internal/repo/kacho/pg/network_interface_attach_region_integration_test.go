// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// The anycast exception drops the ZONAL comparison — it does not drop coherence.
// A REGIONAL (anycast) subnet has no zone to compare against, so what remains is
// the regional lane: the subnet's region must be the region of the instance's
// zone (data-integrity.md §Placement-coherence, "зональный ↔ региональный — зона
// consumer'а ∈ регион peer'а"). The attach CAS took the REGIONAL branch as a
// blanket pass, so an anycast subnet of ANY region was attachable.

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

func TestNICAttach_RegionCoherence_AnycastForeignRegion(t *testing.T) {
	e := newNICAttachEnv(t)
	projectID := "prj-region"
	netID := e.makeNetwork(t, projectID)
	// anycast subnet of region-2; the instance lives in zone-1 of region-1.
	subForeign := e.makeRegionalSubnet(t, projectID, netID, "region-2", "10.7.0.0/24")
	nicForeign := e.makeFreeNIC(t, projectID, subForeign, "0e:00:00:00:0c:01")

	_, err := e.attach(kacho.AttachNICParams{
		NICID: nicForeign, InstanceID: "epdinstfr00000001", InstanceName: "vm",
		InstanceZoneID: "zone-1", InstanceRegionID: "region-1",
		ProjectID: projectID, Index: kacho.AutoIndex,
	})
	require.Error(t, err, "anycast subnet of a foreign region must not be attachable")
	require.True(t, errors.Is(err, helpers.ErrNICRegionMismatch), "got %v", err)
}

// The region of the instance's zone is resolved from the owner of Geography. When
// that resolution fails the region is absent, and an anycast subnet cannot be
// shown coherent — the attach fails closed rather than passing unchecked.
func TestNICAttach_RegionCoherence_UnresolvedRegion_FailsClosed(t *testing.T) {
	e := newNICAttachEnv(t)
	projectID := "prj-region-unres"
	netID := e.makeNetwork(t, projectID)
	subAny := e.makeRegionalSubnet(t, projectID, netID, "region-1", "10.8.0.0/24")
	nicAny := e.makeFreeNIC(t, projectID, subAny, "0e:00:00:00:0c:02")

	_, err := e.attach(kacho.AttachNICParams{
		NICID: nicAny, InstanceID: "epdinstur00000002", InstanceName: "vm",
		InstanceZoneID: "zone-1", InstanceRegionID: "", // geo could not answer
		ProjectID: projectID, Index: kacho.AutoIndex,
	})
	require.Error(t, err, "unverifiable region must not pass unchecked")
	require.True(t, errors.Is(err, helpers.ErrNICRegionUnverifiable), "got %v", err)
}

// A ZONAL attach is unaffected by an unresolved region: the zonal lane does not
// need it, so a geo outage must not widen the blast radius.
func TestNICAttach_RegionCoherence_ZonalUnaffectedByUnresolvedRegion(t *testing.T) {
	e := newNICAttachEnv(t)
	projectID := "prj-region-zonal"
	netID := e.makeNetwork(t, projectID)
	subZ := e.makeZonalSubnet(t, projectID, netID, "zone-1", "10.9.0.0/24")
	nicZ := e.makeFreeNIC(t, projectID, subZ, "0e:00:00:00:0c:03")

	_, err := e.attach(kacho.AttachNICParams{
		NICID: nicZ, InstanceID: "epdinstzu00000003", InstanceName: "vm",
		InstanceZoneID: "zone-1", InstanceRegionID: "",
		ProjectID: projectID, Index: kacho.AutoIndex,
	})
	require.NoError(t, err, "zonal lane must not depend on the region resolution")
}
