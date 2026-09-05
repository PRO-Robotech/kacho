// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/subnet"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// startSupernetShrink моделирует конкурентный Network.RemoveCidrBlocks: берёт
// на строке сети ТОТ ЖЕ row-lock (`SELECT … FOR UPDATE`), что берёт
// NetworkWriterIface.GetForUpdate, держит его `hold`, затем переписывает
// супернет и коммитит. Возвращает канал, закрываемый после COMMIT.
//
// Детерминизм вместо «двух горутин на барьере»: lock захвачен ДО старта
// проверяемой операции, поэтому исход однозначен — операция либо сериализуется
// на этом локе (и увидит новый супернет), либо нет (и примет решение по
// устаревшему снимку).
func startSupernetShrink(t *testing.T, ctx context.Context, pool *pgxpool.Pool, netID string, newV4 []string, hold time.Duration) <-chan struct{} {
	t.Helper()
	locked := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		tx, err := pool.Begin(ctx)
		if err != nil {
			close(locked)
			t.Errorf("begin shrink tx: %v", err)
			return
		}
		var got string
		if err := tx.QueryRow(ctx, `SELECT id FROM kacho_vpc.networks WHERE id = $1 FOR UPDATE`, netID).Scan(&got); err != nil {
			close(locked)
			_ = tx.Rollback(ctx)
			t.Errorf("lock network row: %v", err)
			return
		}
		close(locked)
		time.Sleep(hold)
		if _, err := tx.Exec(ctx, `UPDATE kacho_vpc.networks SET ipv4_cidr_blocks = $2 WHERE id = $1`, netID, newV4); err != nil {
			_ = tx.Rollback(ctx)
			t.Errorf("shrink supernet: %v", err)
			return
		}
		if err := tx.Commit(ctx); err != nil {
			t.Errorf("commit shrink: %v", err)
		}
	}()
	<-locked
	return done
}

// assertSupernetContainment — инвариант F7 на пост-состоянии: КАЖДЫЙ блок каждой
// подсети сети обязан лежать внутри одного из объявленных супернет-блоков (при
// непустом супернете). Нарушение = подсеть-сирота вне адресного пространства сети.
func assertSupernetContainment(t *testing.T, ctx context.Context, pool *pgxpool.Pool, netID string) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT b.block::text
		  FROM kacho_vpc.subnet_cidr_blocks b
		  JOIN kacho_vpc.subnets s ON s.id = b.subnet_id
		 WHERE s.network_id = $1
		   AND family(b.block) = 4
		   AND NOT EXISTS (
		         SELECT 1
		           FROM kacho_vpc.networks n, unnest(n.ipv4_cidr_blocks) AS sup(cidr)
		          WHERE n.id = $1
		            AND cardinality(n.ipv4_cidr_blocks) > 0
		            AND sup.cidr::cidr >>= b.block
		       )`, netID)
	require.NoError(t, err)
	defer rows.Close()
	var orphans []string
	for rows.Next() {
		var b string
		require.NoError(t, rows.Scan(&b))
		orphans = append(orphans, b)
	}
	require.NoError(t, rows.Err())
	assert.Empty(t, orphans, "F7: subnet CIDR(s) вне объявленного супернета сети — подсеть-сирота")
}

// TestIntegration_Subnet_Create_SupernetShrinkRace — F7 (VPC-1-34) под гонкой:
// Subnet.Create обязан сериализоваться с конкурентной мутацией супернета
// родительской сети, а не решать по устаревшему снимку.
//
// RED (баг): doCreate читал networks обычным SELECT без row-lock, поэтому пока
// конкурентный Network.RemoveCidrBlocks держал `FOR UPDATE` и ещё не
// закоммитился, containment-backstop проходил по старому супернету → подсеть
// коммитилась вне итогового адресного пространства сети (её ∉-guard, в свою
// очередь, незакоммиченную подсеть не видит — окно двустороннее).
// GREEN: backstop берёт на строке сети share-lock → ждёт коммита writer'а,
// перечитывает актуальный супернет и отвергает
// InvalidArgument "subnet CIDR 10.1.5.0/24 is not within any network CIDR block".
func TestIntegration_Subnet_Create_SupernetShrinkRace(t *testing.T) {
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

	const proj = "prj-supernet-race"
	netID := ids.NewID(ids.PrefixNetwork)
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.Networks().Insert(ctx, &domain.Network{
			ID: netID, ProjectID: proj, Name: domain.RcNameVPC("core-race"),
			IPv4CidrBlocks: []string{"10.1.0.0/16"},
		})
		return e
	}))

	// Конкурентный шринк супернета 10.1.0.0/16 → 10.2.0.0/16 (10.1.5.0/24 больше
	// не покрыт). Лок уже взят к моменту возврата.
	shrunk := startSupernetShrink(t, ctx, pool, netID, []string{"10.2.0.0/16"}, 700*time.Millisecond)

	or := repomock.NewOpsRepo()
	uc := subnet.NewCreateSubnetUseCase(r, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry("zone-a"), repomock.NewRegionRegistry("reg-a"), or)
	op, err := uc.Execute(ctx, domain.Subnet{
		ProjectID: proj, NetworkID: netID, Name: domain.RcNameVPC("s-race"),
		ZoneID: "zone-a", V4CidrBlocks: []string{"10.1.5.0/24"},
	})
	require.NoError(t, err) // op-in-response: отказ приходит в op.Error
	<-shrunk

	require.True(t, op.Done)
	require.NotNil(t, op.Error, "Create поверх конкурентно суженного супернета обязан быть отвергнут")
	st := status.FromProto(op.Error)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, "subnet CIDR 10.1.5.0/24 is not within any network CIDR block", st.Message())
	assertSupernetContainment(t, ctx, pool, netID)
}

// TestIntegration_Subnet_AddCidrBlocks_SupernetShrinkRace — зеркальный кейс для
// Subnet.AddCidrBlocks: расширение подсети валидируется против супернета, который
// конкурентный writer в этот момент переписывает.
//
// RED: parent network читался обычным SELECT (без лока) → блок 10.2.3.0/24
// принимался по устаревшему супернету и коммитился вне итогового набора.
// GREEN: share-lock на сети (взятый ДО subnet-лока — единый глобальный порядок
// network → subnet, без inversion с Network.Delete) сериализует с writer'ом.
func TestIntegration_Subnet_AddCidrBlocks_SupernetShrinkRace(t *testing.T) {
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

	const proj = "prj-supernet-race-add"
	netID := ids.NewID(ids.PrefixNetwork)
	subID := ids.NewID(ids.PrefixSubnet)
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		if _, e := w.Networks().Insert(ctx, &domain.Network{
			ID: netID, ProjectID: proj, Name: domain.RcNameVPC("core-race-add"),
			IPv4CidrBlocks: []string{"10.1.0.0/16", "10.2.0.0/16"},
		}); e != nil {
			return e
		}
		_, e := w.Subnets().Insert(ctx, &domain.Subnet{
			ID: subID, ProjectID: proj, Name: domain.RcNameVPC("s-race-add"),
			NetworkID: netID, PlacementType: domain.PlacementZonal, ZoneID: "zone-a",
			V4CidrBlocks: []string{"10.1.5.0/24"},
		})
		return e
	}))

	// Конкурентно снимаем 10.2.0.0/16 — добавляемый 10.2.3.0/24 перестаёт быть покрытым.
	shrunk := startSupernetShrink(t, ctx, pool, netID, []string{"10.1.0.0/16"}, 700*time.Millisecond)

	or := repomock.NewOpsRepo()
	uc := subnet.NewAddCidrBlocksUseCase(r, or)
	op, err := uc.Execute(ctx, subID, []string{"10.2.3.0/24"}, nil)
	require.NoError(t, err)
	<-shrunk

	require.True(t, op.Done)
	require.NotNil(t, op.Error, "AddCidrBlocks поверх конкурентно суженного супернета обязан быть отвергнут")
	st := status.FromProto(op.Error)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, "subnet CIDR 10.2.3.0/24 is not within any network CIDR block", st.Message())
	assertSupernetContainment(t, ctx, pool, netID)
}
