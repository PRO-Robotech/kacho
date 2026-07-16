// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/PRO-Robotech/kacho/services/compute/internal/clients"
)

// SyncRegistrar (owner-tuple op-gating P4) — синхронная post-commit регистрация
// owner-tuple через InternalIAMService.RegisterResource. Reuses the fakeIAMRegister
// recorder (register_drainer_integration_test.go) as the IAMRegisterClient.

func TestSyncRegistrar_Register_Instance(t *testing.T) {
	fake := &fakeIAMRegister{errCode: codes.OK}
	reg := clients.NewSyncRegistrarWithClient(fake)

	err := reg.Register(context.Background(), "Instance", "epd-abc", "prj-xyz",
		map[string]string{"env": "prod"})
	require.NoError(t, err)
	require.Equal(t, 1, fake.registeredCount(), "exactly one RegisterResource call")

	got := fake.registered[0]
	// project-hierarchy owner-tuple: project:<proj> #project @compute_instance:<id>.
	assert.Equal(t, "project:prj-xyz", got.GetSubjectId())
	assert.Equal(t, "project", got.GetRelation())
	assert.Equal(t, "compute_instance:epd-abc", got.GetObject())
	// β mirror-feed forwarded (labels + parent-scope) + monotonic source_version.
	assert.Equal(t, "prod", got.GetLabels()["env"])
	assert.Equal(t, "prj-xyz", got.GetParentProjectId())
	assert.NotNil(t, got.GetSourceVersion(), "source_version stamped (last-source-state-wins)")
}

func TestSyncRegistrar_Register_Disk(t *testing.T) {
	fake := &fakeIAMRegister{errCode: codes.OK}
	reg := clients.NewSyncRegistrarWithClient(fake)

	require.NoError(t, reg.Register(context.Background(), "Disk", "epdd-1", "prj-1", nil))
	require.Equal(t, 1, fake.registeredCount())
	assert.Equal(t, "compute_disk:epdd-1", fake.registered[0].GetObject())
}

func TestSyncRegistrar_Register_UnknownKindOrEmpty_NoOp(t *testing.T) {
	cases := []struct{ kind, id, proj string }{
		{"Gadget", "epd-x", "prj"}, // unknown kind
		{"Instance", "", "prj"},    // empty resource id
		{"Instance", "epd-x", ""},  // empty project id
	}
	for _, c := range cases {
		fake := &fakeIAMRegister{errCode: codes.OK}
		reg := clients.NewSyncRegistrarWithClient(fake)
		err := reg.Register(context.Background(), c.kind, c.id, c.proj, nil)
		require.NoError(t, err, "unmappable/empty intent → no-op, resource still committed (fail-safe)")
		assert.Equal(t, 0, fake.registeredCount(), "no RegisterResource call for %+v", c)
	}
}

func TestSyncRegistrar_Register_Error_Propagated(t *testing.T) {
	// perm=true → RegisterResource всегда возвращает errCode → Register оборачивает
	// и пробрасывает (best-effort: вызывающий service.syncRegisterOwner логирует WARN,
	// Create не проваливается; register-drainer — at-least-once backstop).
	fake := &fakeIAMRegister{perm: true, errCode: codes.Unavailable}
	reg := clients.NewSyncRegistrarWithClient(fake)

	err := reg.Register(context.Background(), "Instance", "epd-abc", "prj-xyz", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "compute_instance:epd-abc", "error carries the tuple object")
}
