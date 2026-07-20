// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/network"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// TestIntegration_Network_VPC_1_10_RemoveBlockCoversSubnetBeyondFirstPage —
// Finding 3 (VPC-1-10 F2 db-integrity): RemoveCidrBlocks ∉-guard обязан учитывать
// ВСЕ подсети сети, а не только первую страницу (DefaultPageSize=50). Сеть с >50
// подсетями, где единственная подсеть, покрытая удаляемым супернет-блоком, лежит
// на 2-й странице, — блок нельзя удалить (её primary осиротел бы вне супернета).
//
// RED (буг): guard читает только первую страницу (50 подсетей из 10.20.0.0/16,
// ни одна не покрыта удаляемым 10.99.0.0/16) → удаление ошибочно проходит.
// GREEN (фикс): single exists-query по subnet_cidr_blocks находит подсеть на 2-й
// странице → FAILED_PRECONDITION "network CIDR block 10.99.0.0/16 still contains subnets".
func TestIntegration_Network_VPC_1_10_RemoveBlockCoversSubnetBeyondFirstPage(t *testing.T) {
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

	const proj = "prj-remove-cidr-page"
	netID := ids.NewID(ids.PrefixNetwork)
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Networks().Insert(ctx, &domain.Network{
			ID:             netID,
			ProjectID:      proj,
			Name:           domain.RcNameVPC("core-multi-subnet"),
			IPv4CidrBlocks: []string{"10.20.0.0/16", "10.99.0.0/16"},
		})
		return e
	}))

	// 50 подсетей из 10.20.0.0/16 — ровно заполняют первую страницу (page size 50).
	for i := 0; i < 50; i++ {
		id := ids.NewID(ids.PrefixSubnet)
		cidr := fmt.Sprintf("10.20.%d.0/24", i)
		name := fmt.Sprintf("s-%d", i)
		require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
			_, e := w.Subnets().Insert(ctx, &domain.Subnet{
				ID:            id,
				ProjectID:     proj,
				Name:          domain.RcNameVPC(name),
				NetworkID:     netID,
				PlacementType: domain.PlacementZonal,
				ZoneID:        "zone-a",
				V4CidrBlocks:  []string{cidr},
			})
			return e
		}))
	}
	// Единственная подсеть из 10.99.0.0/16 — создаётся ПОСЛЕДНЕЙ → по (created_at ASC,
	// id ASC) попадает на 2-ю страницу (позиция 51). Именно её первополосный guard не видит.
	page2ID := ids.NewID(ids.PrefixSubnet)
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Subnets().Insert(ctx, &domain.Subnet{
			ID:            page2ID,
			ProjectID:     proj,
			Name:          domain.RcNameVPC("s-page2"),
			NetworkID:     netID,
			PlacementType: domain.PlacementZonal,
			ZoneID:        "zone-a",
			V4CidrBlocks:  []string{"10.99.0.0/24"},
		})
		return e
	}))

	// Удаление 10.99.0.0/16 осиротило бы подсеть 10.99.0.0/24 (⊆ него, не покрыта
	// остающимся 10.20.0.0/16) → обязано быть отвергнуто.
	or := repomock.NewOpsRepo()
	uc := network.NewRemoveCidrBlocksUseCase(r, or)
	op, err := uc.Execute(ctx, netID, []string{"10.99.0.0/16"}, nil)
	require.NoError(t, err) // op-in-response
	require.True(t, op.Done)
	require.NotNil(t, op.Error, "removing a block still covering a subnet beyond page 1 must be rejected")
	st := status.FromProto(op.Error)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	assert.Equal(t, "network CIDR block 10.99.0.0/16 still contains subnets", st.Message())
}
