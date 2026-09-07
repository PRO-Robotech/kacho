// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

// Регистрация owner-tuple в FGA для Gateway должна нести labels на двух путях:
// (a) Create эмитит register-intent с labels + parent_project_id (иначе
// resource_mirror в kaname без labels и label-селектор не матчит даже
// свежесозданный Gateway); (b) Update при смене labels переэмитит
// register-intent с обновленными labels. Не-label Update нового intent не
// порождает.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	gwapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/gateway"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// seedGatewayAnchorSQL заводит сеть и ЗОНАЛЬНУЮ подсеть с блоком IPv4 прямо в БД
// и возвращает id подсети — якорь размещения шлюза (`gateways.subnet_id` NOT NULL
// + FK, миграция 0030). Предмет этих проб — намерение регистрации, а не якорь,
// поэтому он ставится минимальным SQL, без прогона use-case'ов подсети.
func seedGatewayAnchorSQL(ctx context.Context, t *testing.T, pool *pgxpool.Pool, projectID string) string {
	t.Helper()
	netID := ids.NewID(ids.PrefixNetwork)
	subID := ids.NewID(ids.PrefixSubnet)
	_, err := pool.Exec(ctx,
		`INSERT INTO kacho_vpc.networks (id, project_id, name) VALUES ($1, $2, $3)`,
		netID, projectID, "net-anchor")
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO kacho_vpc.subnets (id, project_id, name, network_id, placement_type, zone_id, v4_cidr_blocks)
		 VALUES ($1, $2, $3, $4, 'ZONAL', $5, $6)`,
		subID, projectID, "sub-anchor", netID, "zone-a", []string{"10.79.0.0/24"})
	require.NoError(t, err)
	seedDefaultExternalPool(ctx, t, pool, "zone-a", "198.51.100.0/28")
	return subID
}

// seedDefaultExternalPool — пул внешних адресов по умолчанию для зоны якоря.
//
// Нужен потому, что эти пробы идут ПРОДУКТОВЫМ путём создания, а он выделяет
// шлюзу трансляции внешний адрес: вид и адрес связаны биусловием на уровне базы
// (миграция 0038). Без пула создание отвечает «нет свободного внешнего адреса»,
// и проба падает на нехватке фикстуры, ничего не сказав о том, что называет её
// имя, — эмиссии намерения регистрации.
func seedDefaultExternalPool(ctx context.Context, t *testing.T, pool *pgxpool.Pool, zoneID, cidr string) {
	t.Helper()
	poolID := ids.NewID("apl")
	_, err := pool.Exec(ctx, `
		INSERT INTO kacho_vpc.address_pools (id, name, v4_cidr_blocks, kind, zone_id, is_default)
		VALUES ($1, $2, ARRAY[$3]::text[], 1, $4, true)`,
		poolID, "pool-"+poolID, cidr, zoneID)
	require.NoError(t, err)

	r := kachopg.New(pool, nil)
	t.Cleanup(r.Close)
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	require.NoError(t, w.AddressPools().PopulateFreelistForPool(ctx, poolID))
	require.NoError(t, w.Commit())
}

// singleGatewayID возвращает единственный id gateway в проекте.
func singleGatewayID(ctx context.Context, t *testing.T, pool *pgxpool.Pool, projectID string) string {
	t.Helper()
	var id string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT id FROM kacho_vpc.gateways WHERE project_id = $1`, projectID).Scan(&id))
	return id
}

// TestGatewayRepo_T32Create01_CreateEmitsLabels_UpdateRevokes проверяет оба пути:
// Create обязан эмитить labels + parent_project_id; Update со сменой labels —
// переэмитить; не-label Update — без лишнего intent.
func TestGatewayRepo_T32Create01_CreateEmitsLabels_UpdateRevokes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := kachopg.New(pool, nil)
	t.Cleanup(r.Close)
	or := repomock.NewOpsRepo()
	pc := &repomock.ProjectClient{OK: true}

	createUC := gwapp.NewCreateGatewayUseCase(r, pc, or)
	updateUC := gwapp.NewUpdateGatewayUseCase(r, or)

	// --- Create Gateway с labels ---
	anchor := seedGatewayAnchorSQL(ctx, t, pool, "prj-A")
	op, err := createUC.Execute(ctx, domain.Gateway{
		ProjectID:   "prj-A",
		Name:        domain.RcNameVPC("gw-okun"),
		GatewayType: domain.GatewayTypeNat,
		SubnetID:    anchor,
		Labels:      domain.LabelsFromMap(map[string]string{"gw": "okun"}),
	})
	require.NoError(t, err)
	awaitOp(t, or, op.ID)

	gwID := singleGatewayID(ctx, t, pool, "prj-A")

	createRegs := registerPayloads(ctx, t, pool, gwID)
	require.Len(t, createRegs, 1, "exactly one register intent on Create")
	assert.Equal(t, map[string]string{"gw": "okun"}, createRegs[0].Labels,
		"BUG (a): Gateway Create MUST emit labels, not a bare tuple — else selector never matches")
	assert.Equal(t, "prj-A", createRegs[0].ParentProjectID, "parent_project_id = Gateway project_id")
	assert.Equal(t, "vpc_gateway:"+gwID, createRegs[0].Tuple.Object)

	// --- Update labels (okun → sudak) ---
	upOp, err := updateUC.Execute(ctx, gwapp.UpdateInput{
		GatewayID:  gwID,
		Gateway:    domain.Gateway{Labels: domain.LabelsFromMap(map[string]string{"gw": "sudak"})},
		UpdateMask: []string{"labels"},
	})
	require.NoError(t, err)
	awaitOp(t, or, upOp.ID)

	updateRegs := registerPayloads(ctx, t, pool, gwID)
	require.Len(t, updateRegs, 2, "BUG (b): Gateway Update(labels) MUST re-emit a register intent")
	assert.Equal(t, map[string]string{"gw": "sudak"}, updateRegs[1].Labels,
		"the Update register intent carries the refreshed labels (revoke old selector)")
	assert.Equal(t, "prj-A", updateRegs[1].ParentProjectID)
	require.False(t, updateRegs[1].SourceVersion.IsZero(), "source_version проставлен на Update intent")

	// --- Не-label Update → без лишнего register-intent ---
	nlOp, err := updateUC.Execute(ctx, gwapp.UpdateInput{
		GatewayID:  gwID,
		Gateway:    domain.Gateway{Description: domain.RcDescription("renamed")},
		UpdateMask: []string{"description"},
	})
	require.NoError(t, err)
	awaitOp(t, or, nlOp.ID)
	require.Len(t, registerPayloads(ctx, t, pool, gwID), 2,
		"non-label Update → no new register intent (G-2)")
}

// TestGatewayRepo_T32FullPatch01_EmptyMaskEmits — пустой update_mask
// (full-object PATCH) обязан переэмитить register-intent.
func TestGatewayRepo_T32FullPatch01_EmptyMaskEmits(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDB(t)
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	r := kachopg.New(pool, nil)
	t.Cleanup(r.Close)
	or := repomock.NewOpsRepo()
	pc := &repomock.ProjectClient{OK: true}
	createUC := gwapp.NewCreateGatewayUseCase(r, pc, or)
	updateUC := gwapp.NewUpdateGatewayUseCase(r, or)

	anchor := seedGatewayAnchorSQL(ctx, t, pool, "prj-A")
	op, err := createUC.Execute(ctx, domain.Gateway{
		ProjectID: "prj-A", Name: domain.RcNameVPC("gw-fp"),
		GatewayType: domain.GatewayTypeNat,
		SubnetID:    anchor,
		Labels:      domain.LabelsFromMap(map[string]string{"gw": "treska"}),
	})
	require.NoError(t, err)
	awaitOp(t, or, op.ID)
	gwID := singleGatewayID(ctx, t, pool, "prj-A")

	upOp, err := updateUC.Execute(ctx, gwapp.UpdateInput{GatewayID: gwID, Gateway: domain.Gateway{}})
	require.NoError(t, err)
	awaitOp(t, or, upOp.ID)

	regs := registerPayloads(ctx, t, pool, gwID)
	require.Len(t, regs, 2, "пустой mask = full PATCH ⇒ labelsInMask=true ⇒ emit")
	assert.Empty(t, regs[1].Labels, "full-PATCH обнулил labels → intent с пустыми labels")
}
