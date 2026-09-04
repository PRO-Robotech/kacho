// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// Integration-тесты CQRS-реализации Gateway-репо в `internal/repo/kacho/pg`.
//
// Покрывают:
//   - Insert + Commit виден параллельному Reader;
//   - Abort() rollback'ит INSERT (запись не появляется в БД);
//   - outbox.Emit транзакционен с DML (Abort → outbox-row не вставлена).

// seedGatewayAnchor заводит сеть и ЗОНАЛЬНУЮ подсеть с блоком IPv4 — якорь
// размещения, без которого шлюз не создаётся (`gateways.subnet_id` NOT NULL + FK,
// миграция 0030) и без которого его вид нечем сверить: NAT-шлюз обязан стоять в
// подсети, несущей IPv4.
func seedGatewayAnchor(ctx context.Context, t *testing.T, r kacho.Repository, projectID, zone string) string {
	t.Helper()
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	net := newNetwork(projectID, "net-anchor-"+zone)
	_, err = w.Networks().Insert(ctx, net)
	require.NoError(t, err)
	sub := newSubnet(projectID, "sub-anchor-"+zone, net.ID, zone, []string{"10.77.0.0/24"})
	_, err = w.Subnets().Insert(ctx, sub)
	require.NoError(t, err)
	require.NoError(t, w.Commit())
	return sub.ID
}

// externalAddressFor — внешний адрес под шлюз ТРАНСЛЯЦИИ.
//
// Вид и адрес связаны биусловием на уровне базы (`gateways_nat_has_address_chk`,
// миграция 0038): шлюз трансляции без адреса — состояние незаписываемое. Фикстура
// обязана выполнять тот же инвариант, что продукт; иначе она снисходительнее его и
// прячет ровно то, ради чего ставится. Каждому шлюзу — свой адрес: один адрес двух
// шлюзов не обслуживает.
func externalAddressFor(ctx context.Context, t *testing.T, r kacho.Repository, projectID, name string) string {
	t.Helper()
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	a := newAddress(projectID, "addr-"+name, true)
	_, err = w.Addresses().Insert(ctx, a)
	require.NoError(t, err)
	require.NoError(t, w.Commit())
	return a.ID
}

func newGateway(projectID, name, subnetID, addressID string) *domain.Gateway {
	return &domain.Gateway{
		ID:                ids.NewID(ids.PrefixGateway),
		ProjectID:         projectID,
		Name:              domain.RcNameVPC(name),
		Description:       domain.RcDescription(""),
		Labels:            domain.LabelsFromMap(nil),
		GatewayType:       domain.GatewayTypeNat,
		SubnetID:          subnetID,
		ExternalAddressID: addressID,
	}
}

// TestCQRS_Gateway_WriterCommit_ReaderSees — Writer.Insert + Commit; параллельный
// Reader видит запись.
func TestCQRS_Gateway_WriterCommit_ReaderSees(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := kachopg.New(pool, nil)

	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()

	anchor := seedGatewayAnchor(ctx, t, r, "project-1", "zone-a")
	g := newGateway("project-1", "gw-1", anchor, externalAddressFor(ctx, t, r, "project-1", "gw-1"))
	created, err := w.Gateways().Insert(ctx, g)
	require.NoError(t, err)
	assert.Equal(t, g.ID, created.ID)
	// outbox emit в той же TX.
	require.NoError(t, w.Outbox().Emit(ctx, "Gateway", created.ID, created.ProjectID, "CREATED", map[string]any{"id": created.ID}))
	require.NoError(t, w.Commit())

	// Параллельный Reader видит committed запись.
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	got, err := rd.Gateways().Get(ctx, g.ID)
	require.NoError(t, err)
	assert.Equal(t, g.ID, got.ID)
	assert.Equal(t, domain.RcNameVPC("gw-1"), got.Name)
	assert.Equal(t, domain.GatewayTypeNat, got.GatewayType)
}

// TestCQRS_Gateway_WriterAbort_RollbacksInsert — Abort() rollback'ит INSERT;
// запись не появляется в БД.
func TestCQRS_Gateway_WriterAbort_RollbacksInsert(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := kachopg.New(pool, nil)

	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()

	anchor := seedGatewayAnchor(ctx, t, r, "project-1", "zone-a")
	g := newGateway("project-1", "gw-abort", anchor, externalAddressFor(ctx, t, r, "project-1", "gw-abort"))
	_, err = w.Gateways().Insert(ctx, g)
	require.NoError(t, err)
	w.Abort() // rollback

	// После Abort — Reader не видит запись.
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	_, gerr := rd.Gateways().Get(ctx, g.ID)
	require.Error(t, gerr)
}

// TestCQRS_Gateway_OutboxAtomicityWithDML — Emit в той же TX, что и DML;
// Abort выкидывает и outbox-row.
func TestCQRS_Gateway_OutboxAtomicityWithDML(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := kachopg.New(pool, nil)

	// 1) Insert + Emit + Commit → outbox-row есть.
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	anchor := seedGatewayAnchor(ctx, t, r, "project-1", "zone-a")
	g := newGateway("project-1", "gw-outbox-commit", anchor, externalAddressFor(ctx, t, r, "project-1", "gw-outbox-commit"))
	_, err = w.Gateways().Insert(ctx, g)
	require.NoError(t, err)
	require.NoError(t, w.Outbox().Emit(ctx, "Gateway", g.ID, g.ProjectID, "CREATED", map[string]any{"id": g.ID}))
	require.NoError(t, w.Commit())

	var count1 int
	err = pool.QueryRow(ctx, "SELECT count(*) FROM vpc_outbox WHERE resource_id = $1", g.ID).Scan(&count1)
	require.NoError(t, err)
	assert.Equal(t, 1, count1, "committed Emit должен вставить outbox-row")

	// 2) Insert + Emit + Abort → ни DML, ни outbox-row не должны остаться.
	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	g2 := newGateway("project-1", "gw-outbox-abort", anchor, externalAddressFor(ctx, t, r, "project-1", "gw-outbox-abort"))
	_, err = w2.Gateways().Insert(ctx, g2)
	require.NoError(t, err)
	require.NoError(t, w2.Outbox().Emit(ctx, "Gateway", g2.ID, g2.ProjectID, "CREATED", map[string]any{"id": g2.ID}))
	w2.Abort()

	var count2 int
	err = pool.QueryRow(ctx, "SELECT count(*) FROM vpc_outbox WHERE resource_id = $1", g2.ID).Scan(&count2)
	require.NoError(t, err)
	assert.Equal(t, 0, count2, "aborted Emit не должен вставить outbox-row")

	var gwCount int
	err = pool.QueryRow(ctx, "SELECT count(*) FROM gateways WHERE id = $1", g2.ID).Scan(&gwCount)
	require.NoError(t, err)
	assert.Equal(t, 0, gwCount, "aborted Insert не должен вставить gateway-row")
}

// TestCQRS_Gateway_UpdateDelete_FullCycle — Insert → Update → Delete, проверяем
// что каждый шаг через writer виден после Commit (+ outbox event).
func TestCQRS_Gateway_UpdateDelete_FullCycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := kachopg.New(pool, nil)

	// Insert.
	w1, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w1.Abort()
	anchor := seedGatewayAnchor(ctx, t, r, "project-1", "zone-a")
	g := newGateway("project-1", "gw-cycle", anchor, externalAddressFor(ctx, t, r, "project-1", "gw-cycle"))
	created, err := w1.Gateways().Insert(ctx, g)
	require.NoError(t, err)
	require.NoError(t, w1.Outbox().Emit(ctx, "Gateway", created.ID, created.ProjectID, "CREATED", map[string]any{"id": created.ID}))
	require.NoError(t, w1.Commit())

	// Update.
	w2, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w2.Abort()
	created.Name = domain.RcNameVPC("gw-cycle-updated")
	updated, err := w2.Gateways().Update(ctx, &created.Gateway)
	require.NoError(t, err)
	assert.Equal(t, domain.RcNameVPC("gw-cycle-updated"), updated.Name)
	require.NoError(t, w2.Outbox().Emit(ctx, "Gateway", updated.ID, updated.ProjectID, "UPDATED", map[string]any{"id": updated.ID}))
	require.NoError(t, w2.Commit())

	// Delete.
	w3, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w3.Abort()
	require.NoError(t, w3.Gateways().Delete(ctx, g.ID))
	require.NoError(t, w3.Outbox().Emit(ctx, "Gateway", g.ID, g.ProjectID, "DELETED", map[string]any{"id": g.ID}))
	require.NoError(t, w3.Commit())

	// Reader не видит запись после Delete.
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	_, gerr := rd.Gateways().Get(ctx, g.ID)
	require.Error(t, gerr)
}

// Assertion: kacho.GatewayRecord — это правильный тип, возвращаемый repo.
var _ *kacho.GatewayRecord = (*kacho.GatewayRecord)(nil)
