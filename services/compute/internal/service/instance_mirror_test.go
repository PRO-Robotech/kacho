// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports/portmock"
)

// TestInstanceGet_MirrorReadCtxBounded — GATE-RUN #3 root #1 regression.
//
// best-effort NIC/volume mirror reads на Get обязаны нести короткий per-call
// deadline (mirrorReadTimeout), а НЕ сырой request-ctx: иначе против Unavailable
// vpc/storage peer'а mirror крутит retry.OnUnavailable (MaxElapsed=30s) и один
// instance Get висит ~55s (2×30s, nic+volume) — доминирующий bottleneck
// instance-суита. Locked на уровне наблюдаемого: peer-client обязан получить ctx с
// deadline ≤ mirrorReadTimeout. Мутации НЕ трогаются (fail-closed 30s retry корректен).
func TestInstanceGet_MirrorReadCtxBounded(t *testing.T) {
	instanceRepo := portmock.NewInstanceRepo()
	nic := portmock.NewNicClient()
	storage := portmock.NewStorageClient()
	svc := NewInstanceService(instanceRepo, portmock.NewZoneRegistry(),
		&portmock.ProjectClient{OK: true}, nic, storage, portmock.NewOpsRepo())

	in := &domain.Instance{
		ID: ids.NewID(ids.PrefixInstance), ProjectID: "p", Name: "vm-1",
		ZoneID: "ru-central1-a", Status: domain.InstanceStatusRunning,
	}
	instanceRepo.Seed(in)

	// inbound request-ctx без дедлайна (как на read-path)
	_, err := svc.Get(context.Background(), in.ID)
	require.NoError(t, err)

	assertBounded := func(name string, ctx context.Context) {
		t.Helper()
		require.NotNil(t, ctx, "%s: mirror обязан вызвать peer-client", name)
		dl, ok := ctx.Deadline()
		require.True(t, ok,
			"%s: mirror-read ctx обязан нести bounded deadline (не сырой request-ctx → 30s retry-storm)", name)
		rem := time.Until(dl)
		require.Greater(t, rem, time.Duration(0), "%s: deadline в будущем", name)
		require.LessOrEqual(t, rem, mirrorReadTimeout+time.Second,
			"%s: deadline ≤ mirrorReadTimeout (не 30s retry-budget)", name)
	}
	assertBounded("nic", nic.LastListCtx)
	assertBounded("volume", storage.LastListCtx)
}

// TestInstanceGet_MirrorGracefulDegradeOnUnavailable — bound НЕ ломает
// graceful-degrade: Unavailable peer (down) ⇒ Get всё равно возвращает инстанс,
// просто без зеркал (data-integrity §3 output-only).
func TestInstanceGet_MirrorGracefulDegradeOnUnavailable(t *testing.T) {
	instanceRepo := portmock.NewInstanceRepo()
	nic := portmock.NewNicClient()
	storage := portmock.NewStorageClient()
	nic.ListErr = context.DeadlineExceeded    // peer «down» (bound истёк)
	storage.ListErr = context.DeadlineExceeded
	svc := NewInstanceService(instanceRepo, portmock.NewZoneRegistry(),
		&portmock.ProjectClient{OK: true}, nic, storage, portmock.NewOpsRepo())

	in := &domain.Instance{
		ID: ids.NewID(ids.PrefixInstance), ProjectID: "p", Name: "vm-2",
		ZoneID: "ru-central1-a", Status: domain.InstanceStatusRunning,
	}
	instanceRepo.Seed(in)

	got, err := svc.Get(context.Background(), in.ID)
	require.NoError(t, err, "down mirror-peer НЕ должен ронять Get (graceful-degrade)")
	require.Equal(t, in.ID, got.ID)
	require.Empty(t, got.NetworkInterfaces, "зеркало опущено при down-peer")
	require.Empty(t, got.AttachedDisks, "зеркало опущено при down-peer")
}
