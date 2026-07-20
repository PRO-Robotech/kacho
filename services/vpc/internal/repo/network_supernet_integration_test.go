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
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
)

// TestIntegration_Network_VPC_1_06_SupernetRoundTrip — SQL-сторона VPC-1-06/F2:
// declared супернет (ipv4_cidr_blocks / ipv6_cidr_blocks) + default_route_table_id
// (F3) сохраняются на Insert и эхаются на Get/List. 0015_network_supernet добавил
// колонки; repo обязан их персистить (Insert) и читать (ScanNetwork).
func TestIntegration_Network_VPC_1_06_SupernetRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()
	r := kachopg.New(pool, nil)
	defer r.Close()

	netID := ids.NewID(ids.PrefixNetwork)
	in := &domain.Network{
		ID:                  netID,
		ProjectID:           "prj-supernet",
		Name:                domain.RcNameVPC("core-prod"),
		IPv4CidrBlocks:      []string{"10.20.0.0/16"},
		IPv6CidrBlocks:      []string{"fd00:20::/48"},
		DefaultRouteTableID: "rtb-9k3m7t2q5n8v1h",
	}

	var created *kacho.NetworkRecord
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		var e error
		created, e = w.Networks().Insert(ctx, in)
		return e
	}))
	// Insert RETURNING обязан вернуть только что записанный супернет — без второго
	// round-trip.
	assert.Equal(t, []string{"10.20.0.0/16"}, created.IPv4CidrBlocks, "Insert must persist ipv4 supernet")
	assert.Equal(t, []string{"fd00:20::/48"}, created.IPv6CidrBlocks, "Insert must persist ipv6 supernet")
	assert.Equal(t, "rtb-9k3m7t2q5n8v1h", created.DefaultRouteTableID, "Insert must persist default_route_table_id")

	// Get эхает то же (durable, second-read).
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	got, err := rd.Networks().Get(ctx, netID)
	require.NoError(t, err)
	assert.Equal(t, []string{"10.20.0.0/16"}, got.IPv4CidrBlocks)
	assert.Equal(t, []string{"fd00:20::/48"}, got.IPv6CidrBlocks)
	assert.Equal(t, "rtb-9k3m7t2q5n8v1h", got.DefaultRouteTableID)

	// Пустой супернет (legacy-путь) остаётся легальным — []{} , не nil-паника.
	net2 := ids.NewID(ids.PrefixNetwork)
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Networks().Insert(ctx, &domain.Network{ID: net2, ProjectID: "prj-supernet", Name: domain.RcNameVPC("legacy-empty")})
		return e
	}))
	got2, err := rd.Networks().Get(ctx, net2)
	require.NoError(t, err)
	assert.Empty(t, got2.IPv4CidrBlocks)
	assert.Empty(t, got2.DefaultRouteTableID)
}
