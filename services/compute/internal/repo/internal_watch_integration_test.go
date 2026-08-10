// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/repo"
)

// TestIntegration_OutboxEmit_OnInstanceCreate проверяет, что Insert ВМ пишет
// строку в compute_outbox в той же транзакции (CREATED-event).
//
// Раньше носителем этой проверки был Disk; ресурс снят (блочное хранение
// принадлежит kacho-storage), а проверяемое свойство — транзакционность
// outbox-эмита — принадлежит не ему, поэтому переехало на Instance.
func TestIntegration_OutboxEmit_OnInstanceCreate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	r := repo.NewInstanceRepo(pool)
	d := outboxFixtureInstance("i-outbox")
	_, _, err = r.Insert(ctx, d)
	require.NoError(t, err)

	var kind, id, eventType string
	err = pool.QueryRow(ctx, `SELECT resource_kind, resource_id, event_type FROM compute_outbox ORDER BY sequence_no DESC LIMIT 1`).Scan(&kind, &id, &eventType)
	require.NoError(t, err)
	assert.Equal(t, "Instance", kind)
	assert.Equal(t, d.ID, id)
	assert.Equal(t, "CREATED", eventType)

	// delete → DELETED-event.
	require.NoError(t, r.Delete(ctx, d.ID))
	err = pool.QueryRow(ctx, `SELECT resource_kind, resource_id, event_type FROM compute_outbox ORDER BY sequence_no DESC LIMIT 1`).Scan(&kind, &id, &eventType)
	require.NoError(t, err)
	assert.Equal(t, "DELETED", eventType)
}

// TestIntegration_OutboxListenNotify проверяет, что trigger compute_outbox_notify_trg
// шлёт pg_notify на канал compute_outbox при INSERT в outbox.
func TestIntegration_OutboxListenNotify(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	// dedicated conn для LISTEN (как в InternalWatchHandler).
	listenConn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer func() { _ = listenConn.Close(ctx) }()
	_, err = listenConn.Exec(ctx, "LISTEN compute_outbox")
	require.NoError(t, err)

	// триггер события через Insert ВМ.
	r := repo.NewInstanceRepo(pool)
	_, _, err = r.Insert(ctx, outboxFixtureInstance("i-notify"))
	require.NoError(t, err)

	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	notif, err := listenConn.WaitForNotification(waitCtx)
	require.NoError(t, err)
	assert.Equal(t, "compute_outbox", notif.Channel)
	assert.NotEmpty(t, notif.Payload)
}

// outboxFixtureInstance — минимальная валидная ВМ для outbox-проверок.
func outboxFixtureInstance(name string) *domain.Instance {
	id := ids.NewID(ids.PrefixInstance)
	return &domain.Instance{
		ID: id, ProjectID: "f", CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		Name: name, ZoneID: "ru-central1-a", Status: domain.InstanceStatusProvisioning,
		InstanceKind:       domain.InstanceKindVM,
		MachineTypeID:      "mt-std2",
		EffectiveResources: domain.EffectiveResources{VCPU: 2, MemoryMiB: 8192},
		BootSource:         domain.BootSource{Type: "storage.image", ID: "img-x:22.04", ImageKind: domain.ImageKindStorageImage},
		FQDN:               id + ".auto.internal",
	}
}
