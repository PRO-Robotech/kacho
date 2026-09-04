// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// setupTestDB hands the caller its own migrated database on the package's shared
// Postgres, and returns the DSN for it.
//
// It clones the template TestMain migrated once, rather than starting a container
// and replaying every migration per call — see testmain_integration_test.go for
// why. The caller still gets a database of its own: nothing is shared between
// tests but the server process, so row-level contention inside a test behaves
// exactly as it did when each test owned a container.
func setupTestDB(t testing.TB) string {
	t.Helper()
	// Учёт числа ресурсов: вставка строки ресурса СПИСЫВАЕТ место, и списать его
	// не с чего, пока у проекта нет строки учёта. На живом пути её заводит
	// материализация перед writer-транзакцией; проба идёт мимо use-case'а, прямо
	// в репозиторий, поэтому базу в то же состояние приводит фикстура.
	//
	// Сеется она в ШАБЛОН, один раз за прогон (`prepareTemplate`), и клон её
	// наследует — здесь звать нечего. Перечень проектов фикстура ВЫВОДИТ из
	// исходников проб пакета, а не выписывает: разбор, замер и причина переезда
	// в шаблон — `quota_fixture_integration_test.go`.
	return appendSearchPathOptions(newSharedDatabase(t, true))
}

// appendSearchPathOptions — приведение схемы для пакета, который собирает DSN САМ:
// свой контейнер и своя раскладка баз, поэтому `pgtest.Config` его не касается.
// Реализация — общая (`pgtest.WithSearchPath`): своя копия здесь уже была и
// разошлась бы с остальными молча.
func appendSearchPathOptions(dsn string) string {
	return pgtest.WithSearchPath(dsn, "kacho_vpc,public")
}

func legacyWithTx(t *testing.T, ctx context.Context, r kacho.Repository, fn func(kacho.RepositoryWriter) error) error {
	t.Helper()
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	if err := fn(w); err != nil {
		w.Abort()
		return err
	}
	return w.Commit()
}

func TestIntegration_NetworkRepo_CRUD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	dsn := setupTestDB(t)

	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := kachopg.New(pool, nil)
	defer r.Close()

	n := &domain.Network{
		ID:          ids.NewUID(),
		ProjectID:   "project-1",
		Name:        domain.RcNameVPC("test-network"),
		Description: domain.RcDescription("test"),
		Labels:      domain.LabelsFromMap(map[string]string{"env": "test"}),
	}
	var created *kacho.NetworkRecord
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		var e error
		created, e = w.Networks().Insert(ctx, n)
		return e
	}))
	assert.Equal(t, n.ID, created.ID)
	assert.Equal(t, domain.RcNameVPC("test-network"), created.Name)
	val, _ := created.Labels.Get("env")
	assert.Equal(t, domain.LabelVal("test"), val)

	// Get
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	got, err := rd.Networks().Get(ctx, n.ID)
	require.NoError(t, rd.Close())
	require.NoError(t, err)
	assert.Equal(t, n.ID, got.ID)

	// List
	rd2, err := r.Reader(ctx)
	require.NoError(t, err)
	nets, nextToken, err := rd2.Networks().List(ctx, kacho.NetworkFilter{ProjectID: "project-1"}, kacho.Pagination{})
	require.NoError(t, rd2.Close())
	require.NoError(t, err)
	assert.Len(t, nets, 1)
	assert.Empty(t, nextToken)

	// Update.
	got.Name = domain.RcNameVPC("updated-network")
	got.Description = domain.RcDescription("updated")
	var updated *kacho.NetworkRecord
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		var e error
		updated, e = w.Networks().Update(ctx, &got.Network)
		return e
	}))
	assert.Equal(t, domain.RcNameVPC("updated-network"), updated.Name)

	// Delete.
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		return w.Networks().Delete(ctx, n.ID)
	}))

	rd3, err := r.Reader(ctx)
	require.NoError(t, err)
	_, err = rd3.Networks().Get(ctx, n.ID)
	require.NoError(t, rd3.Close())
	assert.ErrorIs(t, err, helpers.ErrNotFound)
}

func TestIntegration_SubnetRepo_CidrBlocks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	dsn := setupTestDB(t)

	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := kachopg.New(pool, nil)
	defer r.Close()

	net := &domain.Network{
		ID:        ids.NewUID(),
		ProjectID: "project-1",
		Name:      domain.RcNameVPC("net-for-subnet"),
	}
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Networks().Insert(ctx, net)
		return e
	}))

	sub := &domain.Subnet{
		ID:            ids.NewUID(),
		ProjectID:     "project-1",
		Name:          domain.RcNameVPC("test-subnet"),
		NetworkID:     net.ID,
		PlacementType: domain.PlacementZonal,
		ZoneID:        "zone-a",
		V4CidrBlocks:  []string{"10.0.0.0/24", "10.1.0.0/24"},
	}
	var created *kacho.SubnetRecord
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		var e error
		created, e = w.Subnets().Insert(ctx, sub)
		return e
	}))
	assert.Equal(t, []string{"10.0.0.0/24", "10.1.0.0/24"}, created.V4CidrBlocks)

	// Get.
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	got, err := rd.Subnets().Get(ctx, sub.ID)
	require.NoError(t, rd.Close())
	require.NoError(t, err)
	assert.Equal(t, []string{"10.0.0.0/24", "10.1.0.0/24"}, got.V4CidrBlocks)
	assert.Empty(t, got.V6CidrBlocks)
}

func TestIntegration_AddressRepo_ExternalAndInternal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	dsn := setupTestDB(t)

	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := kachopg.New(pool, nil)
	defer r.Close()

	// External address.
	extAddr := &domain.Address{
		ID:        ids.NewUID(),
		ProjectID: "project-1",
		Name:      domain.RcNameVPC("my-external-ip"),
		Type:      domain.AddressTypeExternal,
		IpVersion: domain.IpVersionIPv4,
		Reserved:  true,
		ExternalIpv4: &domain.ExternalIpv4Spec{
			Address: "203.0.113.10",
			ZoneID:  "zone-a",
		},
	}
	var created *kacho.AddressRecord
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		var e error
		created, e = w.Addresses().Insert(ctx, extAddr)
		return e
	}))
	require.NotNil(t, created.ExternalIpv4)
	assert.Equal(t, "203.0.113.10", created.ExternalIpv4.Address)
	assert.Nil(t, created.InternalIpv4)

	// Internal address — addresses.internal_subnet_id has an FK to subnets.
	net := &domain.Network{
		ID:        ids.NewUID(),
		ProjectID: "project-1",
		Name:      domain.RcNameVPC("net-for-internal-addr"),
	}
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Networks().Insert(ctx, net)
		return e
	}))
	sub := &domain.Subnet{
		ID:            ids.NewUID(),
		ProjectID:     "project-1",
		Name:          domain.RcNameVPC("sub-for-internal-addr"),
		NetworkID:     net.ID,
		PlacementType: domain.PlacementZonal,
		ZoneID:        "zone-a",
		V4CidrBlocks:  []string{"10.0.0.0/24"},
	}
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Subnets().Insert(ctx, sub)
		return e
	}))

	intAddr := &domain.Address{
		ID: ids.NewUID(), Name: fixtureName(),
		ProjectID: "project-1",
		Type:      domain.AddressTypeInternal,
		IpVersion: domain.IpVersionIPv4,
		InternalIpv4: &domain.InternalIpv4Spec{
			Address:  "10.0.0.5",
			SubnetID: sub.ID,
		},
	}
	var created2 *kacho.AddressRecord
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		var e error
		created2, e = w.Addresses().Insert(ctx, intAddr)
		return e
	}))
	require.NotNil(t, created2.InternalIpv4)
	assert.Equal(t, "10.0.0.5", created2.InternalIpv4.Address)
}

func TestIntegration_AddressRepo_References(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	dsn := setupTestDB(t)

	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := kachopg.New(pool, nil)
	defer r.Close()

	addr := &domain.Address{
		ID:        ids.NewUID(),
		ProjectID: "project-1",
		Name:      domain.RcNameVPC("ref-tracked-ip"),
		Type:      domain.AddressTypeExternal,
		IpVersion: domain.IpVersionIPv4,
		ExternalIpv4: &domain.ExternalIpv4Spec{
			Address: "203.0.113.77",
			ZoneID:  "zone-a",
		},
	}
	var created *kacho.AddressRecord
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		var e error
		created, e = w.Addresses().Insert(ctx, addr)
		return e
	}))
	assert.False(t, created.Used)

	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	_, err = rd.Addresses().GetReference(ctx, addr.ID)
	require.NoError(t, rd.Close())
	require.ErrorIs(t, err, helpers.ErrNotFound)

	// SetReference → upsert + used=true.
	var ref *domain.AddressReference
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		var e error
		ref, e = w.Addresses().SetReference(ctx, &domain.AddressReference{
			AddressID:    addr.ID,
			ReferrerType: "compute_instance",
			ReferrerID:   "epdinstance00000001",
			ReferrerName: "vm-1",
		})
		return e
	}))
	assert.Equal(t, "compute_instance", ref.ReferrerType)
	assert.Equal(t, "epdinstance00000001", ref.ReferrerID)
	assert.Equal(t, "vm-1", ref.ReferrerName)
	assert.False(t, ref.AttachedAt.IsZero())

	rd2, err := r.Reader(ctx)
	require.NoError(t, err)
	got, err := rd2.Addresses().Get(ctx, addr.ID)
	require.NoError(t, rd2.Close())
	require.NoError(t, err)
	assert.True(t, got.Used)

	// Re-set с ДРУГИМ referrer → ErrFailedPrecondition (address уже занят).
	err = legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Addresses().SetReference(ctx, &domain.AddressReference{
			AddressID:    addr.ID,
			ReferrerType: "compute_instance",
			ReferrerID:   "epdinstance00000002",
		})
		return e
	})
	require.ErrorIs(t, err, helpers.ErrFailedPrecondition,
		"SetReference к занятому address от чужого referrer → helpers.ErrFailedPrecondition")

	// Idempotent re-set с ТЕМ ЖЕ referrer.
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		var e error
		ref, e = w.Addresses().SetReference(ctx, &domain.AddressReference{
			AddressID:    addr.ID,
			ReferrerType: "compute_instance",
			ReferrerID:   "epdinstance00000001",
			ReferrerName: "vm-1-renamed",
		})
		return e
	}))
	assert.Equal(t, "epdinstance00000001", ref.ReferrerID)
	assert.Equal(t, "vm-1-renamed", ref.ReferrerName)

	// GetReference.
	rd3, err := r.Reader(ctx)
	require.NoError(t, err)
	ref, err = rd3.Addresses().GetReference(ctx, addr.ID)
	require.NoError(t, err)
	assert.Equal(t, "epdinstance00000001", ref.ReferrerID)
	assert.Equal(t, "vm-1-renamed", ref.ReferrerName)

	// Batch lookup.
	refs, err := rd3.Addresses().ReferencesForAddresses(ctx, []string{addr.ID, "e9bnonexistent00001"})
	require.NoError(t, rd3.Close())
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, "epdinstance00000001", refs[addr.ID].ReferrerID)

	// ClearReference.
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		return w.Addresses().ClearReference(ctx, addr.ID)
	}))
	rd4, err := r.Reader(ctx)
	require.NoError(t, err)
	got, err = rd4.Addresses().Get(ctx, addr.ID)
	require.NoError(t, err)
	assert.False(t, got.Used)
	_, err = rd4.Addresses().GetReference(ctx, addr.ID)
	require.NoError(t, rd4.Close())
	require.ErrorIs(t, err, helpers.ErrNotFound)

	// ClearReference again — no-op.
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		return w.Addresses().ClearReference(ctx, addr.ID)
	}))

	// SetReference on non-existent address → NotFound.
	err = legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Addresses().SetReference(ctx, &domain.AddressReference{
			AddressID: "e9bnonexistent00001", ReferrerType: "compute_instance", ReferrerID: "x",
		})
		return e
	})
	require.ErrorIs(t, err, helpers.ErrNotFound)

	// FK CASCADE: deleting the address removes the reference row too.
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Addresses().SetReference(ctx, &domain.AddressReference{
			AddressID: addr.ID, ReferrerType: "compute_instance", ReferrerID: "epdinstance00000003",
		})
		return e
	}))
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		return w.Addresses().Delete(ctx, addr.ID)
	}))
	rd5, err := r.Reader(ctx)
	require.NoError(t, err)
	_, err = rd5.Addresses().GetReference(ctx, addr.ID)
	require.NoError(t, rd5.Close())
	require.ErrorIs(t, err, helpers.ErrNotFound)
}

func TestIntegration_RouteTableRepo_StaticRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	dsn := setupTestDB(t)

	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := kachopg.New(pool, nil)
	defer r.Close()

	net := &domain.Network{
		ID:        ids.NewUID(),
		ProjectID: "project-1",
		Name:      domain.RcNameVPC("net-for-rt"),
	}
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Networks().Insert(ctx, net)
		return e
	}))

	rt := &domain.RouteTable{
		ID:        ids.NewUID(),
		ProjectID: "project-1",
		Name:      domain.RcNameVPC("test-rt"),
		NetworkID: net.ID,
		StaticRoutes: []domain.StaticRoute{
			{DestinationPrefix: "0.0.0.0/0", NextHopAddress: "192.168.0.1"},
			{DestinationPrefix: "10.0.0.0/8", NextHopAddress: "192.168.0.254"},
		},
	}
	var created *kacho.RouteTableRecord
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		var e error
		created, e = w.RouteTables().Insert(ctx, rt)
		return e
	}))
	require.Len(t, created.StaticRoutes, 2)
	assert.Equal(t, "0.0.0.0/0", created.StaticRoutes[0].DestinationPrefix)

	// Update static routes.
	created.StaticRoutes = []domain.StaticRoute{
		{DestinationPrefix: "0.0.0.0/0", NextHopAddress: "10.10.10.1"},
	}
	var updated *kacho.RouteTableRecord
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		var e error
		updated, e = w.RouteTables().Update(ctx, &created.RouteTable)
		return e
	}))
	require.Len(t, updated.StaticRoutes, 1)
	assert.Equal(t, "10.10.10.1", updated.StaticRoutes[0].NextHopAddress)
}
