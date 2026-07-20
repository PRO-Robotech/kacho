// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package protoconv_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/protoconv"
)

// TestInstance_ProjectsExpectedFields — locks the tenant-facing projection of the
// COMP-1 redesigned Instance: the NEW-model fields (instanceKind, machineTypeId,
// effectiveResources, bootSource, serviceAccount Referrer, statusReason, the kind-
// gated vm_spec oneof arm) must round-trip domain→proto. Complements the
// descriptor-level infra-omission guard below. Retired vendor-cruft fields
// (platform_id/resources/image/...) are gone from both domain and proto and are
// therefore not asserted (removed behaviour).
func TestInstance_ProjectsExpectedFields(t *testing.T) {
	created := time.Date(2026, 7, 6, 10, 30, 45, 500_000_000, time.UTC)
	in := &domain.Instance{
		ID:                  "ins-0000000000000001",
		ProjectID:           "prj0000000000000001",
		CreatedAt:           created,
		Name:                "vm-full",
		Description:         "desc",
		Labels:              map[string]string{"team": "core"},
		ZoneID:              "ru-central1-b",
		Status:              domain.InstanceStatusStopped,
		StatusReason:        "takes effect on next boot",
		Metadata:            map[string]string{"k": "v"},
		CPUGuaranteePercent: 50,
		InstanceKind:        domain.InstanceKindVM,
		MachineTypeID:       "mt-std2",
		EffectiveResources:  domain.EffectiveResources{VCPU: 4, MemoryMiB: 8192},
		BootSource: domain.BootSource{
			Type:      "storage.image",
			ID:        "img-x:22.04",
			ImageKind: domain.ImageKindStorageImage,
		},
		ServiceAccountID: "sva0000000000000001",
		VMSpec:           &domain.VMSpec{},
	}

	out := protoconv.Instance(in)
	require.NotNil(t, out)

	assert.Equal(t, in.ID, out.GetId())
	assert.Equal(t, in.ProjectID, out.GetProjectId())
	// created_at truncated to whole seconds (Kachō timestamp convention).
	assert.Equal(t, created.Truncate(time.Second).Unix(), out.GetCreatedAt().GetSeconds())
	assert.Zero(t, out.GetCreatedAt().GetNanos(), "created_at must truncate sub-second precision")
	assert.Equal(t, in.Name, out.GetName())
	assert.Equal(t, in.Description, out.GetDescription())
	assert.Equal(t, in.Labels, out.GetLabels())
	assert.Equal(t, in.ZoneID, out.GetZoneId())
	assert.Equal(t, computev1.Instance_Status(in.Status), out.GetStatus())
	assert.Equal(t, in.StatusReason, out.GetStatusReason())
	assert.Equal(t, in.Metadata, out.GetMetadata())
	assert.Equal(t, in.FQDN, out.GetFqdn())
	assert.Equal(t, int32(50), out.GetCpuGuaranteePercent())

	// NEW sizing/kind/boot projection.
	assert.Equal(t, computev1.InstanceKind_VM, out.GetInstanceKind())
	assert.Equal(t, in.MachineTypeID, out.GetMachineTypeId())
	require.NotNil(t, out.GetEffectiveResources())
	assert.Equal(t, int32(4), out.GetEffectiveResources().GetVCpu())
	assert.Equal(t, int64(8192), out.GetEffectiveResources().GetMemoryMib())
	require.NotNil(t, out.GetBootSource())
	assert.Equal(t, "storage.image", out.GetBootSource().GetType())
	assert.Equal(t, "img-x:22.04", out.GetBootSource().GetId())

	// serviceAccountId echoed as a class-C Referrer (F4/B2).
	require.NotNil(t, out.GetServiceAccount())
	assert.Equal(t, "iam.service_account", out.GetServiceAccount().GetType())
	assert.Equal(t, in.ServiceAccountID, out.GetServiceAccount().GetId())

	// VM kind → vm_spec oneof arm populated.
	require.NotNil(t, out.GetVmSpec())
	assert.Nil(t, out.GetContainerSpec())
}

// TestInstance_ProjectsContainerSpec — CONTAINER kind projects the container_spec
// oneof arm (and not vm_spec).
func TestInstance_ProjectsContainerSpec(t *testing.T) {
	in := &domain.Instance{
		ID:            "ins-0000000000000002",
		ProjectID:     "prj0000000000000002",
		Name:          "job-1",
		Status:        domain.InstanceStatusRunning,
		InstanceKind:  domain.InstanceKindContainer,
		MachineTypeID: "mt-std2",
		BootSource:    domain.BootSource{Type: "registry.image", ID: "repo/app:1.0", ImageKind: domain.ImageKindOCIImage},
		ContainerSpec: &domain.ContainerSpec{Command: []string{"/bin/run"}},
	}
	out := protoconv.Instance(in)
	require.NotNil(t, out)
	assert.Equal(t, computev1.InstanceKind_CONTAINER, out.GetInstanceKind())
	require.NotNil(t, out.GetContainerSpec())
	assert.Equal(t, []string{"/bin/run"}, out.GetContainerSpec().GetCommand())
	assert.Nil(t, out.GetVmSpec())
}

// TestInstanceMessage_HasNoHostPlacementField — encodes the proto-contract removal
// at the descriptor level: the public computev1.Instance message must not declare a
// host_id / host_group_id field. If a future proto bump re-introduces one, this
// fails, forcing a conscious security decision (Internal-only vs public) before
// protoconv can project it (security.md infra-sensitive placement).
func TestInstanceMessage_HasNoHostPlacementField(t *testing.T) {
	fields := (&computev1.Instance{}).ProtoReflect().Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		name := string(fields.Get(i).Name())
		require.NotEqual(t, "host_id", name, "public Instance must not expose host_id (infra-sensitive, Internal-only)")
		require.NotEqual(t, "host_group_id", name, "public Instance must not expose host_group_id (infra-sensitive, Internal-only)")
		require.False(t, strings.Contains(name, "host_id") || strings.Contains(name, "host_group"),
			"suspicious host-placement field on public Instance: %q", name)
	}
}
