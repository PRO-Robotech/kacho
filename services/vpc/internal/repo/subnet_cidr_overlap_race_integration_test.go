// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
)

// TestIntegration_Subnet_VPC_1_33_ConcurrentOverlapCidr_OneWinner — VPC-1-33 (F7):
// две конкурентные Subnet.Create одной сети с пересекающимися ipv4CidrPrimary
// (10.20.0.0/24 и 10.20.0.128/25 ⊂ /24) → ровно одна коммитится, вторая ловит
// EXCLUDE gist 23P01 → ErrFailedPrecondition (declarative race-free by
// construction, ban #10 — не software check-then-act). Ноль двойного IPAM.
//
// Имена подсетей РАЗНЫЕ — иначе конфликт словил бы UNIQUE(project,name), а мы
// проверяем именно overlap-EXCLUDE. Детерминизм тот же, что в name-race:
// blocker-TX держит слот, waitForLockWaiter ловит ожидание, без time.Sleep.
func TestIntegration_Subnet_VPC_1_33_ConcurrentOverlapCidr_OneWinner(t *testing.T) {
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

	const projectID = "prj-ovlrace"
	netID := ids.NewID(ids.PrefixNetwork)
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Networks().Insert(ctx, &domain.Network{
			ID: netID, ProjectID: projectID, Name: domain.RcNameVPC("n-ovl"),
			IPv4CidrBlocks: []string{"10.20.0.0/16"},
		})
		return e
	}))

	mkSub := func(name, cidr string) *domain.Subnet {
		return &domain.Subnet{
			ID: ids.NewID(ids.PrefixSubnet), ProjectID: projectID, Name: domain.RcNameVPC(name),
			NetworkID: netID, PlacementType: domain.PlacementZonal, ZoneID: "zone-a",
			V4CidrBlocks: []string{cidr},
		}
	}

	// TX-A: вставляет 10.20.0.0/24 и держит (без commit).
	wa, err := r.Writer(ctx)
	require.NoError(t, err)
	_, err = wa.Subnets().Insert(ctx, mkSub("sub-a", "10.20.0.0/24"))
	require.NoError(t, err)

	// TX-B в горутине: пересекающийся 10.20.0.128/25 → блокируется на EXCLUDE gist.
	bDone := make(chan error, 1)
	go func() {
		wb, werr := r.Writer(ctx)
		if werr != nil {
			bDone <- werr
			return
		}
		defer wb.Abort()
		if _, ierr := wb.Subnets().Insert(ctx, mkSub("sub-b", "10.20.0.128/25")); ierr != nil {
			bDone <- ierr
			return
		}
		bDone <- wb.Commit()
	}()

	waitForLockWaiter(t, ctx, pool)

	// TX-A коммитит → выигрывает; TX-B получает 23P01.
	require.NoError(t, wa.Commit())

	bErr := <-bDone
	require.Error(t, bErr, "second concurrent Create with overlapping CIDR must fail")
	require.ErrorIs(t, bErr, repo.ErrFailedPrecondition,
		"loser must map to ErrFailedPrecondition (23P01 EXCLUDE), NOT ErrAlreadyExists (that is only for UNIQUE(name))")

	// Ровно одна подсеть в сети — ноль двойного IPAM-выделения.
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	subs, _, err := rd.Subnets().List(ctx, kacho.SubnetFilter{ProjectID: projectID, NetworkID: netID}, kacho.Pagination{})
	require.NoError(t, err)
	require.Len(t, subs, 1, "exactly one subnet must survive the overlap race")
}
