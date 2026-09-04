// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

// Внешний адрес шлюза трансляции — против НАСТОЯЩЕГО Postgres, а не против
// дублёра.
//
// Почему не in-memory-дублёр репозитория, которым пользуются остальные пробы
// use-case'а шлюза: предмет здесь — учёт ОГРАНИЧЕННОГО пула (freelist, атомарный
// обмен, частичный UNIQUE, биусловие вида и адреса). Дублёр пришлось бы научить
// всему этому заново, и он оказался бы СНИСХОДИТЕЛЬНЕЕ настоящего ровно там, где
// проверка и нужна (`testing.md` §«дублёр, принимающий больше настоящего»).
// Здесь use-case собран поверх настоящего pg-репозитория; вымышлены только
// сосед (`ProjectClient`) и журнал операций.

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/gateway"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

const gwFixtureProject = "b1gtestproject00000"

// gwFixture — якорь и пул, из которого шлюзу полагается взять адрес.
type gwFixture struct {
	pgPool   *pgxpool.Pool
	repo     kacho.Repository
	subnetID string
	poolID   string
}

// seedGatewayFixture заводит сеть, подсеть-якорь и пул-по-умолчанию.
//
// zoneID пустой означает REGIONAL (anycast) якорь: у такой подсети зоны нет
// вовсе, и пул ей полагается зоне-НЕзависимый. Это не «особый случай теста», а
// вторая законная посадка, которую обязана обслуживать та же ветвь кода.
func seedGatewayFixture(t *testing.T, ctx context.Context, pgPool *pgxpool.Pool, r kacho.Repository, zoneID, cidr string) gwFixture {
	t.Helper()

	netID := ids.NewID(ids.PrefixNetwork)
	_, err := pgPool.Exec(ctx,
		`INSERT INTO networks (id, project_id, name) VALUES ($1, $2, $3)`,
		netID, gwFixtureProject, "net-"+netID)
	require.NoError(t, err)

	subnetID := ids.NewID(ids.PrefixSubnet)
	placement, regionID := "ZONAL", ""
	if zoneID == "" {
		placement, regionID = "REGIONAL", "ru-central1"
	}
	_, err = pgPool.Exec(ctx, `
		INSERT INTO subnets (id, project_id, name, network_id, zone_id, region_id, placement_type, v4_cidr_blocks)
		VALUES ($1, $2, $3, $4, $5, $6, $7, ARRAY['10.10.0.0/24']::text[])`,
		subnetID, gwFixtureProject, "sub-"+subnetID, netID, zoneID, regionID, placement)
	require.NoError(t, err)

	// Пул по умолчанию для (зона, вид). zone_id хранится NULL для
	// зоне-независимого пула — ровно то, что читает GetDefaultForZone("").
	poolID := ids.NewID("apl")
	var zoneArg any
	if zoneID != "" {
		zoneArg = zoneID
	}
	_, err = pgPool.Exec(ctx, `
		INSERT INTO address_pools (id, name, v4_cidr_blocks, kind, zone_id, is_default)
		VALUES ($1, $2, ARRAY[$3]::text[], 1, $4, true)`,
		poolID, "pool-"+poolID, cidr, zoneArg)
	require.NoError(t, err)

	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	require.NoError(t, w.AddressPools().PopulateFreelistForPool(ctx, poolID))
	require.NoError(t, w.Commit())

	return gwFixture{pgPool: pgPool, repo: r, subnetID: subnetID, poolID: poolID}
}

func newGatewayFixture(t *testing.T, ctx context.Context, zoneID, cidr string) gwFixture {
	t.Helper()
	pgPool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pgPool)
	r := kachopg.New(pgPool, nil)
	t.Cleanup(func() { r.Close() })
	return seedGatewayFixture(t, ctx, pgPool, r, zoneID, cidr)
}

// createGateway прогоняет настоящий use-case и дожидается исхода операции.
func (f gwFixture) createGateway(t *testing.T, ctx context.Context, name string, kind domain.GatewayType) (string, *repomock.OpsRepo, string) {
	t.Helper()
	or := repomock.NewOpsRepo()
	uc := gateway.NewCreateGatewayUseCase(f.repo, &repomock.ProjectClient{OK: true}, or)
	op, err := uc.Execute(ctx, domain.Gateway{
		ProjectID:   gwFixtureProject,
		Name:        domain.RcNameVPC(name),
		GatewayType: kind,
		SubnetID:    f.subnetID,
	})
	require.NoError(t, err)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.True(t, saved.Done)

	var gwID string
	if saved.Error == nil {
		require.NoError(t, f.pgPool.QueryRow(ctx,
			`SELECT id FROM gateways WHERE project_id = $1 AND name = $2`,
			gwFixtureProject, name).Scan(&gwID))
	}
	msg := ""
	if saved.Error != nil {
		msg = saved.Error.Message
	}
	return gwID, or, msg
}

func (f gwFixture) freeCount(t *testing.T, ctx context.Context) int {
	t.Helper()
	return poolFreeCount(t, ctx, f.pgPool, f.poolID)
}

func (f gwFixture) externalAddressOf(t *testing.T, ctx context.Context, gwID string) string {
	t.Helper()
	var addrID *string
	require.NoError(t, f.pgPool.QueryRow(ctx,
		`SELECT external_address_id FROM gateways WHERE id = $1`, gwID).Scan(&addrID))
	if addrID == nil {
		return ""
	}
	return *addrID
}

// Шлюз трансляции ОБЯЗАН получить внешний адрес, и привязка обязана быть видна
// ВЛАДЕЛЬЦУ адреса — то есть в его собственной строке ссылки, а не только в
// колонке шлюза. Это (а) и половина (б) сразу.
func TestIntegration_Gateway_NatCreate_AllocatesAndBindsExternalAddress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	f := newGatewayFixture(t, ctx, "zone-a", "198.51.100.0/28")

	before := f.freeCount(t, ctx)
	require.Positive(t, before, "фикстура обязана дать непустой пул — иначе проба зеленела бы на пустом")

	gwID, _, errMsg := f.createGateway(t, ctx, "gw-nat-alloc", domain.GatewayTypeNat)
	require.Empty(t, errMsg, "создание шлюза трансляции не должно падать")
	require.NotEmpty(t, gwID)

	addrID := f.externalAddressOf(t, ctx, gwID)
	require.NotEmpty(t, addrID, "шлюз трансляции обязан нести внешний адрес")

	// Аренда действительно взята из пула, а не выдумана.
	assert.Equal(t, before-1, f.freeCount(t, ctx), "ровно одна аренда обязана уйти из пула")

	// Сторона ВЛАДЕЛЬЦА адреса: адрес занят и называет шлюз.
	rd, err := f.repo.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()

	addr, err := rd.Addresses().Get(ctx, addrID)
	require.NoError(t, err, "адрес шлюза обязан существовать как ресурс")
	assert.True(t, addr.Used, "адрес обязан быть помечен занятым")
	assert.Equal(t, domain.AddressTypeExternal, addr.Type)
	assert.Equal(t, domain.IpVersionIPv4, addr.IpVersion)
	require.NotNil(t, addr.ExternalIpv4)
	assert.NotEmpty(t, addr.ExternalIpv4.Address, "адресу обязан быть выдан IP")

	ref, err := rd.Addresses().GetReference(ctx, addrID)
	require.NoError(t, err, "владелец адреса обязан видеть ссылку")
	require.NotNil(t, ref)
	assert.Equal(t, domain.GatewayReferrerType, ref.ReferrerType)
	assert.Equal(t, gwID, ref.ReferrerID, "ссылка обязана называть ИМЕННО этот шлюз")
	assert.True(t, ref.Owned, "адрес заказан шлюзом — его жизнь связана со шлюзом")
}

// Положительный контроль к предыдущей пробе с другой стороны: у вида «только
// исход» адреса нет by design, и отсутствие обязано быть НАСТОЯЩИМ отсутствием,
// а не «ещё не выдали».
func TestIntegration_Gateway_EgressOnly_TakesNoAddressAndNoLease(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	f := newGatewayFixture(t, ctx, "zone-a", "198.51.100.0/28")

	// Якорю нужен IPv6-блок — вид «только исход» его требует.
	_, err := f.pgPool.Exec(ctx,
		`UPDATE subnets SET v6_cidr_blocks = ARRAY['2001:db8::/64']::text[] WHERE id = $1`, f.subnetID)
	require.NoError(t, err)

	before := f.freeCount(t, ctx)
	gwID, _, errMsg := f.createGateway(t, ctx, "gw-egress-noaddr", domain.GatewayTypeEgressOnly)
	require.Empty(t, errMsg)
	require.NotEmpty(t, gwID)

	assert.Empty(t, f.externalAddressOf(t, ctx, gwID), "у вида «только исход» публичного адреса нет")
	assert.Equal(t, before, f.freeCount(t, ctx), "ни одна аренда не должна быть взята")
}

// Когерентность размещения: адрес берётся из пула зоны ЯКОРЯ. Anycast-ветвь —
// положительный контроль: у REGIONAL-подсети зоны нет вовсе, и она обслуживается
// зоне-независимым пулом, а не отвергается.
func TestIntegration_Gateway_AnchorPlacementDecidesPool(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	for _, tc := range []struct {
		name   string
		zoneID string
	}{
		{"зональный якорь берёт адрес своей зоны", "zone-a"},
		{"anycast-якорь берёт зоне-независимый адрес", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			f := newGatewayFixture(t, ctx, tc.zoneID, "198.51.100.0/28")

			gwID, _, errMsg := f.createGateway(t, ctx, "gw-coherence", domain.GatewayTypeNat)
			require.Empty(t, errMsg, "обе посадки якоря обязаны обслуживаться")
			addrID := f.externalAddressOf(t, ctx, gwID)
			require.NotEmpty(t, addrID)

			// Адрес обязан объявлять зону ЯКОРЯ — ни свою, ни чужую.
			var zone string
			var poolID string
			require.NoError(t, f.pgPool.QueryRow(ctx,
				`SELECT coalesce(external_ipv4 ->> 'zone_id', ''), coalesce(external_ipv4 ->> 'address_pool_id', '')
				   FROM addresses WHERE id = $1`, addrID).Scan(&zone, &poolID))
			assert.Equal(t, tc.zoneID, zone, "зона адреса обязана совпасть с зоной якоря")
			assert.Equal(t, f.poolID, poolID, "адрес обязан прийти из пула, отвечающего за это размещение")
		})
	}
}

// Зональный якорь, для зоны которого пула нет, НЕ имеет права молча
// провалиться в зоне-независимый пул: это дало бы адрес, объявляющий зону,
// которой у его префикса нет. Отрицание к предыдущей паре.
func TestIntegration_Gateway_ZonalAnchorDoesNotFallBackToAnycastPool(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pgPool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pgPool)
	r := kachopg.New(pgPool, nil)
	defer r.Close()

	// Якорь зональный, а единственный пул — зоне-независимый.
	f := seedGatewayFixture(t, ctx, pgPool, r, "", "198.51.100.0/28")
	_, err = pgPool.Exec(ctx,
		`UPDATE subnets SET placement_type='ZONAL', zone_id='zone-lonely', region_id='' WHERE id = $1`, f.subnetID)
	require.NoError(t, err)

	before := f.freeCount(t, ctx)
	gwID, _, errMsg := f.createGateway(t, ctx, "gw-no-zonal-pool", domain.GatewayTypeNat)
	require.NotEmpty(t, errMsg, "зона без своего пула обязана получить отказ, а не чужой адрес")
	assert.Empty(t, gwID)
	assert.Equal(t, before, f.freeCount(t, ctx), "отказ не должен стоить пулу ни одной аренды")
}

// (г) Возврат аренды на пути УДАЛЕНИЯ шлюза.
func TestIntegration_Gateway_Delete_ReturnsLeaseAndReleasesAddress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	f := newGatewayFixture(t, ctx, "zone-a", "198.51.100.0/28")

	before := f.freeCount(t, ctx)
	gwID, _, errMsg := f.createGateway(t, ctx, "gw-del-lease", domain.GatewayTypeNat)
	require.Empty(t, errMsg)
	addrID := f.externalAddressOf(t, ctx, gwID)
	require.NotEmpty(t, addrID)
	require.Equal(t, before-1, f.freeCount(t, ctx))

	or := repomock.NewOpsRepo()
	del := gateway.NewDeleteGatewayUseCase(f.repo, or)
	op, err := del.Execute(ctx, gwID)
	require.NoError(t, err)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.True(t, saved.Done)
	require.Nil(t, saved.Error, "удаление шлюза не должно падать")

	assert.Equal(t, before, f.freeCount(t, ctx), "аренда обязана вернуться в пул")

	// Адрес, заказанный шлюзом, уходит вместе с ним — иначе он остался бы
	// занятым навсегда, и «вернули аренду» было бы неправдой на уровне ресурса.
	var n int
	require.NoError(t, f.pgPool.QueryRow(ctx, `SELECT count(*) FROM addresses WHERE id = $1`, addrID).Scan(&n))
	assert.Zero(t, n, "адрес шлюза обязан быть снят вместе со шлюзом")
	require.NoError(t, f.pgPool.QueryRow(ctx,
		`SELECT count(*) FROM address_references WHERE address_id = $1`, addrID).Scan(&n))
	assert.Zero(t, n, "строка ссылки обязана уйти вместе с адресом")
}

// Проба ИСЧЕРПАНИЯ: N выделений → N удалений → N выделений снова проходит.
// Без возврата аренды второй круг упирается в пустой пул.
func TestIntegration_Gateway_PoolSurvivesAllocateReleaseRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	// /29 → 6 пригодных адресов; берём весь пул целиком, чтобы «снова проходит»
	// означало именно возврат, а не запас.
	f := newGatewayFixture(t, ctx, "zone-a", "198.51.100.0/29")
	capacity := f.freeCount(t, ctx)
	require.Positive(t, capacity)

	round := func(tag string) []string {
		ids := make([]string, 0, capacity)
		for i := 0; i < capacity; i++ {
			gwID, _, errMsg := f.createGateway(t, ctx, fmt.Sprintf("gw-%s-%d", tag, i), domain.GatewayTypeNat)
			require.Emptyf(t, errMsg, "%s: выделение %d обязано пройти", tag, i)
			ids = append(ids, gwID)
		}
		return ids
	}

	first := round("r1")
	assert.Zero(t, f.freeCount(t, ctx), "пул обязан быть выбран до дна")

	// Пул пуст — следующий шлюз обязан получить отказ, а не выдуманный адрес.
	_, _, errMsg := f.createGateway(t, ctx, "gw-overflow", domain.GatewayTypeNat)
	require.NotEmpty(t, errMsg, "на исчерпанном пуле создание обязано отказать")

	for _, gwID := range first {
		or := repomock.NewOpsRepo()
		op, err := gateway.NewDeleteGatewayUseCase(f.repo, or).Execute(ctx, gwID)
		require.NoError(t, err)
		require.Nil(t, repomock.AwaitOpDone(t, or, op.ID).Error)
	}
	require.Equal(t, capacity, f.freeCount(t, ctx), "пул обязан восстановиться целиком")

	round("r2")
	assert.Zero(t, f.freeCount(t, ctx), "второй круг обязан пройти полностью")
}

// (б) Один адрес не может обслуживать два шлюза — держит БАЗА, а не проверка
// перед записью. Отрицание с положительным контролем: первая привязка проходит,
// вторая на тот же адрес — нет.
func TestIntegration_Gateway_OneAddressCannotBackTwoGateways(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	f := newGatewayFixture(t, ctx, "zone-a", "198.51.100.0/28")

	gwID, _, errMsg := f.createGateway(t, ctx, "gw-owner", domain.GatewayTypeNat)
	require.Empty(t, errMsg)
	addrID := f.externalAddressOf(t, ctx, gwID)
	require.NotEmpty(t, addrID)

	// Положительный контроль: строка второго шлюза сама по себе законна.
	okID := ids.NewID(ids.PrefixGateway)
	_, err := f.pgPool.Exec(ctx, `
		INSERT INTO gateways (id, project_id, name, gateway_type, subnet_id, external_address_id)
		VALUES ($1, $2, $3, 'EGRESS_ONLY', $4, NULL)`,
		okID, gwFixtureProject, "gw-control", f.subnetID)
	require.NoError(t, err, "контроль: шлюз без адреса обязан вставляться")

	// А тот же адрес во втором шлюзе — нет.
	dupID := ids.NewID(ids.PrefixGateway)
	_, err = f.pgPool.Exec(ctx, `
		INSERT INTO gateways (id, project_id, name, gateway_type, subnet_id, external_address_id)
		VALUES ($1, $2, $3, 'NAT', $4, $5)`,
		dupID, gwFixtureProject, "gw-thief", f.subnetID, addrID)
	require.Error(t, err, "один адрес не может обслуживать два шлюза")
}

// Вид и адрес связаны биусловием: шлюз трансляции без адреса не записывается,
// и адрес не приписывается виду «только исход».
func TestIntegration_Gateway_KindAndAddressAreBoundByTheDatabase(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	f := newGatewayFixture(t, ctx, "zone-a", "198.51.100.0/28")

	gwID, _, errMsg := f.createGateway(t, ctx, "gw-bicond", domain.GatewayTypeNat)
	require.Empty(t, errMsg)
	addrID := f.externalAddressOf(t, ctx, gwID)
	require.NotEmpty(t, addrID)

	// Шлюз трансляции без адреса — незаписываем.
	_, err := f.pgPool.Exec(ctx, `
		INSERT INTO gateways (id, project_id, name, gateway_type, subnet_id, external_address_id)
		VALUES ($1, $2, $3, 'NAT', $4, NULL)`,
		ids.NewID(ids.PrefixGateway), gwFixtureProject, "gw-nat-naked", f.subnetID)
	require.Error(t, err, "шлюз трансляции без внешнего адреса не имеет права существовать")

	// «Только исход» с адресом — тоже.
	_, err = f.pgPool.Exec(ctx, `
		UPDATE gateways SET gateway_type = 'EGRESS_ONLY' WHERE id = $1`, gwID)
	require.Error(t, err, "у вида «только исход» публичного адреса быть не может")
}

// Гонка: N одновременных создателей на пуле ровно из N адресов. Каждый обязан
// получить СВОЙ адрес — ни одного дубля, ни одного лишнего pop.
func TestIntegration_Gateway_ConcurrentCreates_EachGetsItsOwnAddress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	f := newGatewayFixture(t, ctx, "zone-a", "198.51.100.0/29")
	capacity := f.freeCount(t, ctx)
	require.Positive(t, capacity)

	var (
		wg   sync.WaitGroup
		ok   atomic.Int32
		mu   sync.Mutex
		seen = map[string]string{}
	)
	for i := 0; i < capacity; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			or := repomock.NewOpsRepo()
			uc := gateway.NewCreateGatewayUseCase(f.repo, &repomock.ProjectClient{OK: true}, or)
			op, err := uc.Execute(ctx, domain.Gateway{
				ProjectID:   gwFixtureProject,
				Name:        domain.RcNameVPC(fmt.Sprintf("gw-race-%d", i)),
				GatewayType: domain.GatewayTypeNat,
				SubnetID:    f.subnetID,
			})
			if err != nil {
				return
			}
			if repomock.AwaitOpDone(t, or, op.ID).Error == nil {
				ok.Add(1)
			}
		}(i)
	}
	wg.Wait()

	require.Equal(t, int32(capacity), ok.Load(), "все %d создателей обязаны пройти на пуле такого же размера", capacity)

	rows, err := f.pgPool.Query(ctx, `
		SELECT g.id, a.external_ipv4 ->> 'address'
		  FROM gateways g JOIN addresses a ON a.id = g.external_address_id
		 WHERE g.project_id = $1`, gwFixtureProject)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var gwID, ip string
		require.NoError(t, rows.Scan(&gwID, &ip))
		mu.Lock()
		prev, dup := seen[ip]
		require.Falsef(t, dup, "адрес %s выдан дважды: шлюзам %s и %s", ip, prev, gwID)
		seen[ip] = gwID
		mu.Unlock()
	}
	require.NoError(t, rows.Err())
	assert.Len(t, seen, capacity, "каждому шлюзу — свой адрес")
	assert.Zero(t, f.freeCount(t, ctx), "лишних pop'ов быть не должно")
}

// Сорвавшееся создание НЕ оставляет аренду за собой. Отказ вызывается
// НАСТОЯЩЕЙ причиной (проект не существует — отвечает сосед), уже ПОСЛЕ того как
// путь дошёл бы до выделения; аренда обязана вернуться откатом транзакции, а не
// уборкой в отдельной очереди.
func TestIntegration_Gateway_FailedCreateStrandsNoLease(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	f := newGatewayFixture(t, ctx, "zone-a", "198.51.100.0/28")

	before := f.freeCount(t, ctx)

	or := repomock.NewOpsRepo()
	uc := gateway.NewCreateGatewayUseCase(f.repo, &repomock.ProjectClient{OK: false}, or)
	op, err := uc.Execute(ctx, domain.Gateway{
		ProjectID:   gwFixtureProject,
		Name:        domain.RcNameVPC("gw-doomed"),
		GatewayType: domain.GatewayTypeNat,
		SubnetID:    f.subnetID,
	})
	require.NoError(t, err)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.NotNil(t, saved.Error)
	assert.Equal(t, int32(codes.NotFound), saved.Error.Code)

	assert.Equal(t, before, f.freeCount(t, ctx), "сорвавшееся создание не имеет права держать аренду")

	var orphan int
	require.NoError(t, f.pgPool.QueryRow(ctx,
		`SELECT count(*) FROM addresses WHERE project_id = $1`, gwFixtureProject).Scan(&orphan))
	assert.Zero(t, orphan, "адрес несозданного шлюза не должен остаться")
}
