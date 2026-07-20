// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
)

// VPC-1-45: Subnet List free-form filter whitelist принимает zone_id / network_id
// (в дополнение к name / placement_type). Неизвестное поле фильтра → InvalidArgument
// (whitelist энфорсится в filter.Parse, защита от SQL-инъекции — CWE-89).
// RED без правки whitelist: filter="zone_id=…" → InvalidArgument (unknown field) →
// require.NoError падал бы. GREEN после добавления zone_id/network_id в whitelist.
func TestSubnetList_FilterByZoneAndNetwork_VPC_1_45(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test (testcontainers); skipped in -short")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	r := kachopg.New(pool, nil)
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	mkNet := func(id string) {
		_, e := w.Networks().Insert(ctx, &domain.Network{
			ID: id, ProjectID: "prj_zf", Name: domain.RcNameVPC(id), Labels: domain.LabelsFromMap(nil),
		})
		require.NoError(t, e)
	}
	mkNet("enp_zf1")
	mkNet("enp_zf2")
	mkSub := func(name, netID, zone, cidr string) string {
		s, e := w.Subnets().Insert(ctx, newSubnet("prj_zf", name, netID, zone, []string{cidr}))
		require.NoError(t, e)
		return s.ID
	}
	_ = mkSub("sub-a", "enp_zf1", "zone-a", "10.10.0.0/24")       // zone-a, net1
	bZoneB := mkSub("sub-b", "enp_zf1", "zone-b", "10.11.0.0/24") // zone-b, net1
	cNet2 := mkSub("sub-c", "enp_zf2", "zone-a", "10.12.0.0/24")  // zone-a, net2
	require.NoError(t, w.Commit())

	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()

	// filter=zone_id="zone-b" → только bZoneB (значения-слаги с дефисом требуют кавычек)
	got, _, err := rd.Subnets().List(ctx, kacho.SubnetFilter{ProjectID: "prj_zf", Filter: `zone_id="zone-b"`}, kacho.Pagination{})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, bZoneB, got[0].ID)

	// filter=network_id="enp_zf2" → только cNet2
	got, _, err = rd.Subnets().List(ctx, kacho.SubnetFilter{ProjectID: "prj_zf", Filter: `network_id="enp_zf2"`}, kacho.Pagination{})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, cNet2, got[0].ID)

	// filter=zone_id="zone-a" → два (sub-a net1 + sub-c net2)
	got, _, err = rd.Subnets().List(ctx, kacho.SubnetFilter{ProjectID: "prj_zf", Filter: `zone_id="zone-a"`}, kacho.Pagination{})
	require.NoError(t, err)
	require.Len(t, got, 2)

	// неизвестное поле фильтра → InvalidArgument (whitelist энфорсится)
	_, _, err = rd.Subnets().List(ctx, kacho.SubnetFilter{ProjectID: "prj_zf", Filter: `description="x"`}, kacho.Pagination{})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}
