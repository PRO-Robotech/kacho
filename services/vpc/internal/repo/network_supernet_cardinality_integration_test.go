// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

func cardBlocks(off, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		v := off + i
		out = append(out, fmt.Sprintf("10.%d.%d.0/24", v/256, v%256))
	}
	return out
}

// TestIntegration_Network_SupernetCardinality_DBCheck — DB-backstop потолка
// declared-супернета (миграция 0016, CHECK networks_cidr_blocks_cardinality).
// Software-проверка в use-case — первая линия; CHECK обязан отвергнуть
// over-cap запись от ЛЮБОГО writer'а (23514 → ErrInvalidArgument), а запись
// ровно на потолке — пропустить (BVA).
func TestIntegration_Network_SupernetCardinality_DBCheck(t *testing.T) {
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

	// Ровно на потолке — проходит.
	atCap := ids.NewID(ids.PrefixNetwork)
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Networks().Insert(ctx, &domain.Network{
			ID: atCap, ProjectID: "prj-card", Name: domain.RcNameVPC("at-cap"),
			IPv4CidrBlocks: cardBlocks(0, domain.MaxNetworkCidrBlocks),
		})
		return e
	}))

	// Выше потолка на Insert — отвергнуто CHECK'ом (23514 → InvalidArgument).
	over := ids.NewID(ids.PrefixNetwork)
	err = legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Networks().Insert(ctx, &domain.Network{
			ID: over, ProjectID: "prj-card", Name: domain.RcNameVPC("over-cap"),
			IPv4CidrBlocks: cardBlocks(0, domain.MaxNetworkCidrBlocks+1),
		})
		return e
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, repo.ErrInvalidArg, "over-cap Insert → 23514 → InvalidArgument")

	// Выше потолка на SetCidrBlocks (путь :add-cidr-blocks) — тоже отвергнуто.
	err = legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Networks().SetCidrBlocks(ctx, atCap, cardBlocks(0, domain.MaxNetworkCidrBlocks+1), nil)
		return e
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, repo.ErrInvalidArg, "over-cap SetCidrBlocks → 23514 → InvalidArgument")

	// Строка не пострадала — отвергнутая TX откатилась целиком.
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	got, err := rd.Networks().Get(ctx, atCap)
	require.NoError(t, err)
	assert.Len(t, got.IPv4CidrBlocks, domain.MaxNetworkCidrBlocks)

	// Сужение over-cap-набора потолком не блокируется (shrink всегда легален).
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Networks().SetCidrBlocks(ctx, atCap, cardBlocks(0, 3), nil)
		return e
	}))
}
