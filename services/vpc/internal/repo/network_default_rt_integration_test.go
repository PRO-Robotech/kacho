// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/network"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/subnet"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/migrations"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/cqrsadapter"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// setupTestDBUpToVPC — база на общем Postgres пакета с миграциями ТОЛЬКО до
// version (для backfill-теста: посеять legacy-состояние до 0017, затем догнать).
//
// Шаблон здесь НЕ подходит по построению: он мигрирован до головы, а этому тесту
// нужна голова прошлого. Поэтому база создаётся ПУСТОЙ и цепочка проходится сама —
// экономится подъём контейнера, но не проигрывание миграций.
func setupTestDBUpToVPC(t testing.TB, version int64) string {
	t.Helper()
	dsn := newSharedDatabase(t, false)

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	goose.SetBaseFS(migrations.FS)
	require.NoError(t, goose.SetDialect("postgres"))
	require.NoError(t, goose.UpTo(db, ".", version))
	return appendSearchPathOptions(dsn)
}

// migrateVPCTo догоняет уже поднятую БД до version.
func migrateVPCTo(t testing.TB, dsn string, version int64) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	goose.SetBaseFS(migrations.FS)
	require.NoError(t, goose.SetDialect("postgres"))
	require.NoError(t, goose.UpTo(db, ".", version))
}

// TestIntegration_Network_VPC_1_11_DefaultRouteTableProvisioned — SQL-сторона F3:
// Network.Create провижнит системную RouteTable и проставляет её id в
// networks.default_route_table_id одной writer-TX (без orphan-окна).
//
// RED (баг): колонку не писал НИ ОДИН прод-путь — сеть всегда коммитилась с
// пустым default_route_table_id, а публичное поле Network.defaultRouteTableId
// возвращало "" (мёртвое поле контракта; миграция 0015 утверждала обратное).
func TestIntegration_Network_VPC_1_11_DefaultRouteTableProvisioned(t *testing.T) {
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

	or := repomock.NewOpsRepo()
	uc := network.NewCreateNetworkUseCase(r, &repomock.ProjectClient{OK: true}, or)
	op, err := uc.Execute(ctx, domain.Network{
		ProjectID: "prj-default-rt", Name: domain.RcNameVPC("core-default-rt"),
		IPv4CidrBlocks: []string{"10.30.0.0/16"},
	})
	require.NoError(t, err)
	require.Nil(t, op.Error)
	var n vpcv1.Network
	require.NoError(t, op.Response.UnmarshalTo(&n))
	require.NotEmpty(t, n.DefaultRouteTableId, "Create обязан провижнить default RT")

	// В БД лежит РОВНО одна RT этой сети, и она же — объявленный дефолт.
	var rtCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM kacho_vpc.route_tables WHERE network_id = $1`, n.Id).Scan(&rtCount))
	assert.Equal(t, 1, rtCount, "ровно одна системная RT на свежей сети")
	var stored string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COALESCE(default_route_table_id,'') FROM kacho_vpc.networks WHERE id = $1`, n.Id).Scan(&stored))
	assert.Equal(t, n.DefaultRouteTableId, stored, "колонка durable, не только op-response")

	var rtNetwork string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT network_id FROM kacho_vpc.route_tables WHERE id = $1`, stored).Scan(&rtNetwork))
	assert.Equal(t, n.Id, rtNetwork, "дефолт указывает на RT ЭТОЙ сети, не висячий id")
}

// TestIntegration_Subnet_VPC_1_37_AutoAssocUsesDeclaredDefault — F8: подсеть без
// явного routeTableId получает ИМЕННО network.defaultRouteTableId°, даже когда в
// сети есть RT СТАРШЕ дефолтной. Это и лочит снятие триггера subnet_auto_pick_rt
// (0017): он выбрал бы «самую раннюю» RT, то есть другую.
func TestIntegration_Subnet_VPC_1_37_AutoAssocUsesDeclaredDefault(t *testing.T) {
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

	const proj = "prj-auto-rt"
	or := repomock.NewOpsRepo()
	netUC := network.NewCreateNetworkUseCase(r, &repomock.ProjectClient{OK: true}, or)
	op, err := netUC.Execute(ctx, domain.Network{
		ProjectID: proj, Name: domain.RcNameVPC("core-auto-rt"),
		IPv4CidrBlocks: []string{"10.31.0.0/16"},
	})
	require.NoError(t, err)
	require.Nil(t, op.Error)
	var n vpcv1.Network
	require.NoError(t, op.Response.UnmarshalTo(&n))
	defaultRT := n.DefaultRouteTableId
	require.NotEmpty(t, defaultRT)

	// Вторая RT той же сети, СТАРШЕ системной по created_at — «самая ранняя»
	// для снятого триггера.
	olderRT := ids.NewID(ids.PrefixRouteTable)
	_, err = pool.Exec(ctx, `INSERT INTO kacho_vpc.route_tables (id, project_id, created_at, name, network_id)
		VALUES ($1,$2,$3,$4,$5)`, olderRT, proj, time.Now().UTC().Add(-time.Hour), "older-rt", n.Id)
	require.NoError(t, err)

	subUC := subnet.NewCreateSubnetUseCase(r, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry("zone-a"), repomock.NewRegionRegistry("reg-a"), or)
	sOp, err := subUC.Execute(ctx, domain.Subnet{
		ProjectID: proj, NetworkID: n.Id, Name: domain.RcNameVPC("s-auto-rt"),
		ZoneID: "zone-a", V4CidrBlocks: []string{"10.31.7.0/24"},
	})
	require.NoError(t, err)
	require.Nil(t, sOp.Error)
	var s vpcv1.Subnet
	require.NoError(t, sOp.Response.UnmarshalTo(&s))
	assert.Equal(t, defaultRT, s.RouteTableId,
		"подсеть обязана взять объявленный default RT, а не «самую раннюю» RT сети")

	// И то же самое durable в БД (триггер не переписал значение).
	var storedRT *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT route_table_id FROM kacho_vpc.subnets WHERE id = $1`, s.Id).Scan(&storedRT))
	require.NotNil(t, storedRT)
	assert.Equal(t, defaultRT, *storedRT)
}

// TestIntegration_Network_VPC_1_11_DefaultRTBackfill — миграция 0017 backfill'ит
// legacy-сети (default_route_table_id = ”) самой ранней существующей RT, то есть
// ровно тем значением, которое выбрал бы снимаемый триггер: семантика для уже
// живущих сетей не меняется, просто становится явной.
func TestIntegration_Network_VPC_1_11_DefaultRTBackfill(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	dsn := setupTestDBUpToVPC(t, 16) // состояние ДО 0017
	pool, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)

	const proj = "prj-backfill"
	netID := ids.NewID(ids.PrefixNetwork)
	_, err = pool.Exec(ctx, `INSERT INTO kacho_vpc.networks (id, project_id, name) VALUES ($1,$2,$3)`,
		netID, proj, "legacy-net")
	require.NoError(t, err)
	earliest := ids.NewID(ids.PrefixRouteTable)
	later := ids.NewID(ids.PrefixRouteTable)
	_, err = pool.Exec(ctx, `INSERT INTO kacho_vpc.route_tables (id, project_id, created_at, name, network_id) VALUES
		($1,$2,$3,'rt-earliest',$5), ($4,$2,$6,'rt-later',$5)`,
		earliest, proj, time.Now().UTC().Add(-2*time.Hour), later, netID, time.Now().UTC())
	require.NoError(t, err)
	// Сеть без единой RT — backfill'ить нечем, обязана остаться пустой.
	emptyNet := ids.NewID(ids.PrefixNetwork)
	_, err = pool.Exec(ctx, `INSERT INTO kacho_vpc.networks (id, project_id, name) VALUES ($1,$2,$3)`,
		emptyNet, proj, "legacy-net-no-rt")
	require.NoError(t, err)

	pool.Close()
	migrateVPCTo(t, dsn, 17)
	pool2, err := coredb.NewPool(ctx, dsn)
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool2)

	var got string
	require.NoError(t, pool2.QueryRow(ctx,
		`SELECT COALESCE(default_route_table_id,'') FROM kacho_vpc.networks WHERE id = $1`, netID).Scan(&got))
	assert.Equal(t, earliest, got, "backfill берёт самую раннюю RT — семантику снятого триггера")
	require.NoError(t, pool2.QueryRow(ctx,
		`SELECT COALESCE(default_route_table_id,'') FROM kacho_vpc.networks WHERE id = $1`, emptyNet).Scan(&got))
	assert.Empty(t, got, "сети без RT указывать не на что")

	// Триггер снят — новая подсеть без routeTableId больше не получает RT «сама».
	var trg int
	require.NoError(t, pool2.QueryRow(ctx,
		`SELECT count(*) FROM pg_trigger WHERE tgname = 'subnet_auto_pick_rt_trg'`).Scan(&trg))
	assert.Zero(t, trg, "subnet_auto_pick_rt_trg обязан быть снят (0017)")

	// Здесь дерево остановлено на версии 17, поэтому парный rt_auto_assoc_subnets
	// ещё на месте — его снимает 0019 (второй DB-механизм выбора RT, F8 запрещает
	// держать два). Отсутствие на HEAD лочит
	// TestIntegration_VPC_RouteTableInsert_NeverRebindsSubnets.
	require.NoError(t, pool2.QueryRow(ctx,
		`SELECT count(*) FROM pg_trigger WHERE tgname = 'rt_auto_assoc_subnets_trg'`).Scan(&trg))
	assert.Equal(t, 1, trg, "на версии 17 триггер ещё жив; снимает его 0019")

	// Ссылочная целостность backfill'а: дефолт (если задан) — RT ТОЙ ЖЕ сети.
	var bad int
	require.NoError(t, pool2.QueryRow(ctx, `
		SELECT count(*) FROM kacho_vpc.networks n
		 WHERE COALESCE(n.default_route_table_id,'') <> ''
		   AND NOT EXISTS (SELECT 1 FROM kacho_vpc.route_tables r
		                    WHERE r.id = n.default_route_table_id AND r.network_id = n.id)`).Scan(&bad))
	assert.Zero(t, bad)
}

// TestIntegration_Network_Delete_IgnoresOwnDefaultRouteTable — системная RT не
// должна делать сеть неудаляемой. Симметрия с default-SG: собственный
// system-provisioned ресурс исключён из emptiness-проверки и снимается в той же
// writer-TX, что и сама сеть; ЧУЖАЯ (tenant) RT по-прежнему держит сеть.
//
// RED (регрессия, которую вносит F3, если её не закрыть): после провижна default
// RT `checkNetworkEmpty` видит одну route_tables-строку и КАЖДЫЙ Network.Delete
// отвечает FAILED_PRECONDITION "Network <id> is not empty (route tables: N)" — сеть нельзя удалить
// вообще никогда.
func TestIntegration_Network_Delete_IgnoresOwnDefaultRouteTable(t *testing.T) {
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

	const proj = "prj-del-default-rt"
	or := repomock.NewOpsRepo()
	// Группа правил по умолчанию создаётся БЕЗУСЛОВНО, поэтому изолировать ветвь
	// таблицы маршрутизации «сетью без группы» больше НЕЛЬЗЯ: такого состояния не
	// существует. Прежняя редакция строила его настройкой стенда, а Delete собирала
	// БЕЗ репозитория групп — и это работало ровно потому, что группы не было.
	//
	// Теперь провязка та же, что в бою: удаление получает репозиторий групп и
	// снимает системную группу в той же транзакции. Предмет пробы от этого не
	// изменился — она о том, что СОБСТВЕННАЯ системная таблица маршрутизации сети не
	// делает её неудаляемой; системная группа теперь просто часть неизбежной
	// обстановки.
	createUC := network.NewCreateNetworkUseCase(r, &repomock.ProjectClient{OK: true}, or)
	mk := func(name string) *vpcv1.Network {
		op, cerr := createUC.Execute(ctx, domain.Network{ProjectID: proj, Name: domain.RcNameVPC(name)})
		require.NoError(t, cerr)
		require.Nil(t, op.Error)
		var n vpcv1.Network
		require.NoError(t, op.Response.UnmarshalTo(&n))
		require.NotEmpty(t, n.DefaultRouteTableId)
		return &n
	}

	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	delUC := network.NewDeleteNetworkUseCase(r,
		rd.Subnets(), rd.RouteTables(), cqrsadapter.NewSecurityGroup(r), or)

	// Только системные default-ресурсы → сеть удаляется.
	plain := mk("del-plain")
	dOp, err := delUC.Execute(ctx, plain.Id)
	require.NoError(t, err, "собственная default-RT не должна блокировать Delete")
	require.Nil(t, dOp.Error)
	var left int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM kacho_vpc.route_tables WHERE id = $1`, plain.DefaultRouteTableId).Scan(&left))
	assert.Zero(t, left, "системная RT снимается вместе с сетью — orphan не остаётся")

	// Tenant-RT сверх системной → сеть по-прежнему не пуста.
	withTenantRT := mk("del-tenant-rt")
	tenantRT := ids.NewID(ids.PrefixRouteTable)
	_, err = pool.Exec(ctx, `INSERT INTO kacho_vpc.route_tables (id, project_id, name, network_id)
		VALUES ($1,$2,$3,$4)`, tenantRT, proj, "tenant-rt", withTenantRT.Id)
	require.NoError(t, err)
	_, err = delUC.Execute(ctx, withTenantRT.Id)
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	// Отказ ПЕРЕЧИСЛЯЕТ мешающее по видам и числам: прежний текст называл только
	// факт непустоты, и арендатор выяснял радиус перебором. Идентификаторы
	// дочерних в текст не попадают — число координатой не является, перечень
	// идентификаторов чужих объектов ею становится.
	assert.Equal(t, "Network "+withTenantRT.Id+" is not empty (route tables: 1)", st.Message())
}

// TestIntegration_Network_DefaultRT_FKOnDelete — within-service ссылка
// networks.default_route_table_id → route_tables(id) обязана держаться FK
// (data-integrity §within-service п.1), ровно как networks.default_security_group_id
// (0005, ON DELETE SET NULL).
//
// RED: FK не было — тенант, удаливший RT, на которую указывает дефолт сети,
// оставлял dangling-ссылку: Subnet.Create подставлял несуществующий
// route_table_id и падал на FK subnets.route_table_id (23503) — то есть удаление
// одной RT ломало создание подсетей в сети целиком.
// GREEN: ON DELETE SET NULL — дефолт очищается, подсеть создаётся без RT.
func TestIntegration_Network_DefaultRT_FKOnDelete(t *testing.T) {
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

	const proj = "prj-default-rt-fk"
	or := repomock.NewOpsRepo()
	netUC := network.NewCreateNetworkUseCase(r, &repomock.ProjectClient{OK: true}, or)
	op, err := netUC.Execute(ctx, domain.Network{
		ProjectID: proj, Name: domain.RcNameVPC("core-rt-fk"),
		IPv4CidrBlocks: []string{"10.32.0.0/16"},
	})
	require.NoError(t, err)
	require.Nil(t, op.Error)
	var n vpcv1.Network
	require.NoError(t, op.Response.UnmarshalTo(&n))
	require.NotEmpty(t, n.DefaultRouteTableId)

	// Тенант удаляет RT, на которую указывает дефолт сети.
	_, err = pool.Exec(ctx, `DELETE FROM kacho_vpc.route_tables WHERE id = $1`, n.DefaultRouteTableId)
	require.NoError(t, err)

	// Ссылка не должна остаться висячей.
	var dangling int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM kacho_vpc.networks nw
		 WHERE nw.id = $1
		   AND COALESCE(nw.default_route_table_id,'') <> ''
		   AND NOT EXISTS (SELECT 1 FROM kacho_vpc.route_tables r WHERE r.id = nw.default_route_table_id)`,
		n.Id).Scan(&dangling))
	assert.Zero(t, dangling, "dangling networks.default_route_table_id после удаления RT")

	// И создание подсетей в этой сети не ломается.
	subUC := subnet.NewCreateSubnetUseCase(r, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry("zone-a"), repomock.NewRegionRegistry("reg-a"), or)
	sOp, err := subUC.Execute(ctx, domain.Subnet{
		ProjectID: proj, NetworkID: n.Id, Name: domain.RcNameVPC("s-after-rt-drop"),
		ZoneID: "zone-a", V4CidrBlocks: []string{"10.32.9.0/24"},
	})
	require.NoError(t, err)
	require.Nil(t, sOp.Error, "Subnet.Create не должен зависеть от уже удалённой default-RT")
	var s vpcv1.Subnet
	require.NoError(t, sOp.Response.UnmarshalTo(&s))
	assert.Empty(t, s.RouteTableId)
}
