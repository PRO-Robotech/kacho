// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
)

// Integration-тесты within-service refcheck на SecurityGroup.Delete
// (issue PRO-Robotech/kacho-vpc#27, gap G6). SG, на который ссылается NIC через
// `network_interfaces.security_group_ids[]` (jsonb-массив в той же БД kacho_vpc),
// НЕ может быть удалён — иначе остаётся dangling ref (нарушение rule #10). FK/
// partial-UNIQUE невозможны (jsonb-массив, не scalar-колонка) → refcheck выражен
// как software-check ВНУТРИ той же writer-TX, что DELETE (не TOCTOU): SG-row
// берётся `FOR UPDATE`, проверяется `security_group_ids @> jsonb_build_array($id)`,
// затем DELETE. Всё в одной pgx.Tx.

// makeSGWithOptionalNIC создаёт network + non-default SG + subnet, и опционально
// NIC, ссылающийся на SG через security_group_ids[]. Каждый вызов — изолированный
// network (свой CIDR-неймспейс для EXCLUDE-констрейнтов подсети). Возвращает id
// SG и (если nicRefsSG) id NIC.
func makeSGWithOptionalNIC(
	t *testing.T, ctx context.Context, r *kachopg.Repository, pool *pgxpool.Pool,
	projectID, suffix, mac string, nicRefsSG bool,
) (sgID, nicID string) {
	t.Helper()

	// network + non-default SG в одной writer-TX.
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	net := insertNetworkInTx(t, ctx, w, projectID, "net-sgref-"+suffix)
	require.NoError(t, w.Outbox().Emit(ctx, "Network", net.ID, "CREATED", map[string]any{"id": net.ID}))
	sg := &domain.SecurityGroup{
		ID:          ids.NewID(ids.PrefixSecurityGroup),
		ProjectID:   projectID,
		NetworkID:   net.ID,
		Name:        domain.RcNameVPC("sg-ref-" + suffix),
		Description: domain.RcDescription(""),
		Labels:      domain.LabelsFromMap(nil),
	}
	createdSG, err := w.SecurityGroups().Insert(ctx, sg)
	require.NoError(t, err)
	require.NoError(t, w.Outbox().Emit(ctx, "SecurityGroup", createdSG.ID, "CREATED", map[string]any{"id": createdSG.ID}))
	require.NoError(t, w.Commit())
	sgID = createdSG.ID

	// parent Subnet для NIC (raw SQL — как insertSubnetForNIC).
	subnetID := ids.NewID(ids.PrefixSubnet)
	_, err = pool.Exec(ctx,
		`INSERT INTO subnets(id, project_id, network_id, zone_id, placement_type, name, description, labels, v4_cidr_blocks, v6_cidr_blocks)
		 VALUES ($1,$2,$3,$4,'ZONAL',$5,$6,'{}'::jsonb, ARRAY['10.0.0.0/24']::text[], ARRAY[]::text[])`,
		subnetID, projectID, net.ID, "zone-a", "sn-sgref-"+suffix, "")
	require.NoError(t, err)

	if nicRefsSG {
		w2, err := r.Writer(ctx)
		require.NoError(t, err)
		nic := &domain.NetworkInterface{
			ID:               ids.NewID(ids.PrefixNetworkInterface),
			ProjectID:        projectID,
			Name:             domain.RcNameVPC("nic-sgref-" + suffix),
			Description:      domain.RcDescription(""),
			Labels:           domain.LabelsFromMap(nil),
			SubnetID:         subnetID,
			MAC:              mac,
			Status:           domain.NIStatusAvailable,
			SecurityGroupIDs: []string{sgID},
		}
		created, err := w2.NetworkInterfaces().Insert(ctx, nic)
		require.NoError(t, err)
		require.NoError(t, w2.Commit())
		nicID = created.ID
	}
	return sgID, nicID
}

// deleteSGTx — Delete(sgID) в собственной writer-TX (Commit при успехе, Abort
// иначе). Возвращает ошибку repo-слоя (sentinel).
func deleteSGTx(ctx context.Context, r *kachopg.Repository, sgID string) error {
	w, err := r.Writer(ctx)
	if err != nil {
		return err
	}
	defer w.Abort()
	if derr := w.SecurityGroups().Delete(ctx, sgID); derr != nil {
		return derr
	}
	return w.Commit()
}

func sgExists(ctx context.Context, t *testing.T, pool *pgxpool.Pool, sgID string) bool {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM security_groups WHERE id = $1", sgID).Scan(&n))
	return n == 1
}

// TestCQRS_SG_Delete_BlockedByNICReference — SG, на который ссылается NIC через
// security_group_ids[], НЕ удаляется: Delete → ErrFailedPrecondition, SG на месте.
func TestCQRS_SG_Delete_BlockedByNICReference(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()
	r := kachopg.New(pool, nil)

	sgID, _ := makeSGWithOptionalNIC(t, ctx, r, pool, "prj-sgref-blocked", "blk", "0e:aa:00:00:00:01", true)

	derr := deleteSGTx(ctx, r, sgID)
	require.Error(t, derr, "SG.Delete должен быть отклонён, пока NIC ссылается на SG")
	assert.True(t, errors.Is(derr, repo.ErrFailedPrecondition),
		"ожидаем ErrFailedPrecondition (in-use), получили: %v", derr)
	assert.Contains(t, derr.Error(), "in use",
		"текст контракта: security group is in use by network interface(s)")
	assert.True(t, sgExists(ctx, t, pool, sgID), "SG должен остаться после отклонённого Delete")
}

// TestCQRS_SG_Delete_NoNICReference_Succeeds — SG без NIC-референса удаляется
// нормально (refcheck не ложно-срабатывает). NIC ссылается на ДРУГОЙ SG.
func TestCQRS_SG_Delete_NoNICReference_Succeeds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()
	r := kachopg.New(pool, nil)

	// SG-keep — на него ссылается NIC; SG-free — свободен, его и удаляем.
	_, _ = makeSGWithOptionalNIC(t, ctx, r, pool, "prj-sgref-free", "keep", "0e:bb:00:00:00:01", true)
	freeSG, _ := makeSGWithOptionalNIC(t, ctx, r, pool, "prj-sgref-free", "free", "0e:bb:00:00:00:02", false)

	derr := deleteSGTx(ctx, r, freeSG)
	require.NoError(t, derr, "SG без NIC-референса должен удаляться")
	assert.False(t, sgExists(ctx, t, pool, freeSG), "SG должен исчезнуть после Delete")
}

// TestCQRS_SG_Delete_Concurrent_Referenced_AllBlocked — N конкурентных Delete по
// SG, на который ссылается NIC: НИ ОДИН не проходит (все → ErrFailedPrecondition),
// SG выживает. Спорный путь refcheck под -race.
func TestCQRS_SG_Delete_Concurrent_Referenced_AllBlocked(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()
	r := kachopg.New(pool, nil)

	sgID, _ := makeSGWithOptionalNIC(t, ctx, r, pool, "prj-sgref-conc-ref", "cref", "0e:cc:00:00:00:01", true)

	const n = 4
	var start sync.WaitGroup
	start.Add(1)
	var wg sync.WaitGroup
	results := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			start.Wait()
			results[idx] = deleteSGTx(ctx, r, sgID)
		}(i)
	}
	start.Done()
	wg.Wait()

	blocked := 0
	for _, e := range results {
		require.Error(t, e, "ни один конкурентный Delete не должен пройти, пока NIC ссылается на SG")
		assert.True(t, errors.Is(e, repo.ErrFailedPrecondition), "ожидаем FailedPrecondition, получили: %v", e)
		blocked++
	}
	assert.Equal(t, n, blocked, "все %d Delete должны быть отклонены", n)
	assert.True(t, sgExists(ctx, t, pool, sgID), "SG обязан выжить")
}

// TestCQRS_SG_Delete_Concurrent_Unreferenced_ExactlyOne — N конкурентных Delete по
// свободному SG: ровно один проходит (FOR UPDATE сериализует), остальные → NotFound
// (row уже удалён). data-integrity.md чек-лист #5.
func TestCQRS_SG_Delete_Concurrent_Unreferenced_ExactlyOne(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()
	r := kachopg.New(pool, nil)

	sgID, _ := makeSGWithOptionalNIC(t, ctx, r, pool, "prj-sgref-conc-free", "cfree", "0e:dd:00:00:00:01", false)

	const n = 4
	var start sync.WaitGroup
	start.Add(1)
	var wg sync.WaitGroup
	results := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			start.Wait()
			results[idx] = deleteSGTx(ctx, r, sgID)
		}(i)
	}
	start.Done()
	wg.Wait()

	ok, notFound := 0, 0
	for _, e := range results {
		switch {
		case e == nil:
			ok++
		case errors.Is(e, repo.ErrNotFound):
			notFound++
		default:
			t.Fatalf("unexpected delete error: %v", e)
		}
	}
	assert.Equal(t, 1, ok, "ровно один Delete должен пройти")
	assert.Equal(t, n-1, notFound, "остальные → NotFound (row уже удалён)")
	assert.False(t, sgExists(ctx, t, pool, sgID), "SG должен исчезнуть")
}
