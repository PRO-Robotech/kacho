// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

// subnet_reserved_prefix_integration_test.go — подсеть поверх диапазона, который
// платформа держит за собой, не доходит до БД.
//
// ЧЕМ ЭТО ОТЛИЧАЕТСЯ ОТ ЮНИТА. Юнит на use-case утверждает КОД и ТЕКСТ отказа.
// Здесь утверждается ИСХОД в хранилище: строки нет, дочерняя таблица диапазонов не
// пополнилась, адресный план сети не занят. Отказ, случившийся после записи, дал бы
// тот же код и тот же текст — и оставил бы за собой занятое имя и занятый диапазон.
//
// Рядом с каждым отрицанием стоит положительный контроль на ТОЙ ЖЕ базе: законный
// блок коммитится и виден строкой. Без него «строк нет» зеленело бы и на сломанной
// записи вообще.

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/network"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/subnet"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// reservedRangeUnderTest — служебный диапазон стенда для случаев ниже. Лежит ВНУТРИ
// супернета сети намеренно: иначе отказ пришёл бы от проверки супернета, и случай
// проверял бы не свой предмет.
const reservedRangeUnderTest = "10.42.7.0/24"

// TestIntegration_Subnet_ReservedPrefix_NotWrittenToTheDatabase — отказ ДО записи,
// оба направления вложенности, плюс положительный контроль на той же базе.
func TestIntegration_Subnet_ReservedPrefix_NotWrittenToTheDatabase(t *testing.T) {
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

	const proj = "prj-reserved"
	or := repomock.NewOpsRepo()

	netUC := network.NewCreateNetworkUseCase(r, &repomock.ProjectClient{OK: true}, or)
	nOp, err := netUC.Execute(ctx, domain.Network{
		ProjectID: proj, Name: domain.RcNameVPC("core-reserved"),
		IPv4CidrBlocks: []string{"10.42.0.0/16"},
	})
	require.NoError(t, err)
	require.Nil(t, nOp.Error)
	var n vpcv1.Network
	require.NoError(t, nOp.Response.UnmarshalTo(&n))

	subUC := subnet.NewCreateSubnetUseCase(r, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry("zone-a"), repomock.NewRegionRegistry("reg-a"), or).
		WithReservedPrefixes(domain.NewReservedPrefixes(reservedRangeUnderTest))

	for i, tc := range []struct {
		name  string
		block string
	}{
		{"блок арендатора совпадает со служебным", reservedRangeUnderTest},
		{"блок арендатора ПОГЛОЩАЕТ служебный", "10.42.0.0/20"},
		{"блок арендатора внутри служебного", "10.42.7.128/25"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			op, cerr := subUC.Execute(ctx, domain.Subnet{
				ProjectID: proj, NetworkID: n.Id,
				Name:   domain.RcNameVPC(fmt.Sprintf("s-refused-%d", i)),
				ZoneID: "zone-a", V4CidrBlocks: []string{tc.block},
			})
			require.Error(t, cerr, "подсеть поверх служебного диапазона обязана отвергаться")
			require.Nil(t, op, "отказ синхронный: операции не создаётся")
			assert.Equal(t, codes.InvalidArgument, status.Convert(cerr).Code())

			// ИСХОД В ХРАНИЛИЩЕ: ни строки подсети, ни записи в дочерней таблице
			// диапазонов. Именно это отличает «отвергнуто» от «отвергнуто после записи».
			var subnets int
			require.NoError(t, pool.QueryRow(ctx,
				`SELECT count(*) FROM kacho_vpc.subnets WHERE network_id = $1`, n.Id).Scan(&subnets))
			assert.Zero(t, subnets, "отвергнутая подсеть не вправе оставить строку")

			var blocks int
			require.NoError(t, pool.QueryRow(ctx,
				`SELECT count(*) FROM kacho_vpc.subnet_cidr_blocks WHERE network_id = $1`,
				n.Id).Scan(&blocks))
			assert.Zero(t, blocks, "и не вправе занять диапазон в адресном плане сети")
		})
	}

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ на той же базе: законный блок коммитится.
	// Без него «ноль строк» выше зеленело бы и на сломанной записи вообще.
	okOp, err := subUC.Execute(ctx, domain.Subnet{
		ProjectID: proj, NetworkID: n.Id, Name: domain.RcNameVPC("s-lawful"),
		ZoneID: "zone-a", V4CidrBlocks: []string{"10.42.200.0/24"},
	})
	require.NoError(t, err)
	require.Nil(t, okOp.Error)
	var created vpcv1.Subnet
	require.NoError(t, okOp.Response.UnmarshalTo(&created))

	var stored string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT id FROM kacho_vpc.subnets WHERE network_id = $1`, n.Id).Scan(&stored))
	assert.Equal(t, created.Id, stored, "законная подсеть обязана быть durable")
}

// TestIntegration_Subnet_ReservedPrefix_AddCidrBlocksRefused — второй и последний
// глагол, объявляющий диапазон подсети. Закрыв только создание, мы оставили бы
// обход в один запрос: создать законным блоком и добавить служебный.
func TestIntegration_Subnet_ReservedPrefix_AddCidrBlocksRefused(t *testing.T) {
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

	const proj = "prj-reserved-add"
	or := repomock.NewOpsRepo()
	reserved := domain.NewReservedPrefixes(reservedRangeUnderTest)

	netUC := network.NewCreateNetworkUseCase(r, &repomock.ProjectClient{OK: true}, or)
	nOp, err := netUC.Execute(ctx, domain.Network{
		ProjectID: proj, Name: domain.RcNameVPC("core-reserved-add"),
		IPv4CidrBlocks: []string{"10.42.0.0/16"},
	})
	require.NoError(t, err)
	require.Nil(t, nOp.Error)
	var n vpcv1.Network
	require.NoError(t, nOp.Response.UnmarshalTo(&n))

	subUC := subnet.NewCreateSubnetUseCase(r, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry("zone-a"), repomock.NewRegionRegistry("reg-a"), or).
		WithReservedPrefixes(reserved)
	cOp, err := subUC.Execute(ctx, domain.Subnet{
		ProjectID: proj, NetworkID: n.Id, Name: domain.RcNameVPC("s-grow"),
		ZoneID: "zone-a", V4CidrBlocks: []string{"10.42.100.0/24"},
	})
	require.NoError(t, err)
	require.Nil(t, cOp.Error)
	var created vpcv1.Subnet
	require.NoError(t, cOp.Response.UnmarshalTo(&created))

	add := subnet.NewAddCidrBlocksUseCase(r, or).WithReservedPrefixes(reserved)

	op, aerr := add.Execute(ctx, created.Id, []string{reservedRangeUnderTest}, nil)
	require.Error(t, aerr, "добавление служебного диапазона обязано отвергаться")
	require.Nil(t, op, "отказ синхронный: операции не создаётся")
	assert.Equal(t, codes.InvalidArgument, status.Convert(aerr).Code())

	// Набор диапазонов подсети не изменился — остался ровно тот, с которым она
	// создавалась.
	var blocks []string
	rows, qerr := pool.Query(ctx,
		`SELECT block::text FROM kacho_vpc.subnet_cidr_blocks WHERE subnet_id = $1 ORDER BY block`,
		created.Id)
	require.NoError(t, qerr)
	for rows.Next() {
		var c string
		require.NoError(t, rows.Scan(&c))
		blocks = append(blocks, c)
	}
	rows.Close()
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"10.42.100.0/24"}, blocks,
		"отвергнутое добавление не вправе изменить набор диапазонов")

	// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ тем же глаголом: законный блок добавляется и durable.
	okOp, okErr := add.Execute(ctx, created.Id, []string{"10.42.101.0/24"}, nil)
	require.NoError(t, okErr)
	require.NotNil(t, okOp)
	require.Nil(t, okOp.Error)

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM kacho_vpc.subnet_cidr_blocks WHERE subnet_id = $1`,
		created.Id).Scan(&count))
	assert.Equal(t, 2, count, "законное добавление обязано быть durable")
}
