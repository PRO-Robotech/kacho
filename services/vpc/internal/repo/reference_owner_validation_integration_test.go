// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo_test

// Ссылка, названная вызывающим, обязана проверяться на существование И на
// принадлежность — иначе ресурс одного тенанта ссылается на объект другого.
// В сервисе этот канон применён к пяти соседним ссылкам (подсеть у NIC, сеть у
// подсети, сеть у таблицы маршрутов, сеть у группы, подсеть у адреса) и пропущен
// у трёх: группы безопасности на интерфейсе, таблица маршрутов на подсети и
// идентификатор инстанса в привязке интерфейса.

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	coredb "github.com/PRO-Robotech/kacho/pkg/db"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	niapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/networkinterface"
	subnetapp "github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/subnet"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"

	"github.com/PRO-Robotech/kacho/pkg/pgtest"
)

// insertSGRow — группа безопасности проекта в указанной сети (пустая сеть —
// network-less группа).
func insertSGRow(ctx context.Context, t *testing.T, r kacho.Repository, projectID, networkID, name string) string {
	t.Helper()
	sgID := ids.NewID(ids.PrefixSecurityGroup)
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.SecurityGroups().Insert(ctx, &domain.SecurityGroup{
			ID:        sgID,
			ProjectID: projectID,
			NetworkID: networkID,
			Name:      domain.RcNameVPC(name),
		})
		return e
	}))
	return sgID
}

// subnetNetworkID — сеть подсети (нужна, чтобы сделать группу в той же сети).
func subnetNetworkID(ctx context.Context, t *testing.T, r kacho.Repository, subnetID string) string {
	t.Helper()
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	rec, err := rd.Subnets().Get(ctx, subnetID)
	require.NoError(t, err)
	_ = rd.Close()
	return rec.NetworkID
}

// awaitOpAny — дождаться завершения операции БЕЗ утверждения об исходе: эти
// пробы проверяют именно отказ, а общий awaitOp требует успеха.
func awaitOpAny(t *testing.T, or *repomock.OpsRepo, opID string) *operations.Operation {
	t.Helper()
	saved := repomock.AwaitOpDone(t, or, opID)
	require.True(t, saved.Done)
	return saved
}

func nicCount(ctx context.Context, t *testing.T, r kacho.Repository, projectID string) int {
	t.Helper()
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	list, _, err := rd.NetworkInterfaces().List(ctx, kacho.NetworkInterfaceFilter{ProjectID: projectID}, kacho.Pagination{})
	require.NoError(t, err)
	_ = rd.Close()
	return len(list)
}

// Группа безопасности ЧУЖОГО проекта на интерфейсе обязана быть отвергнута.
// Иначе владелец группы не может её удалить: предусловие удаления спрашивает
// «ссылается ли на меня хоть один интерфейс» по всему сервису, а найти
// ссылающийся интерфейс владелец не может — тот лежит в чужом проекте.
func TestIntegration_NIC_Create_ForeignSecurityGroup_Rejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)
	t.Cleanup(r.Close)
	or := repomock.NewOpsRepo()

	subID := insertSubnetRow(ctx, t, r, "prj-owner", "sgref")
	victimNet := insertNetworkRow(ctx, t, r, "prj-victim", "victim-net")
	foreignSG := insertSGRow(ctx, t, r, "prj-victim", victimNet, "victim-sg")

	uc := niapp.NewCreateNetworkInterfaceUseCase(r, &repomock.ProjectClient{OK: true}, or)
	op, err := uc.Execute(ctx, niapp.CreateInput{
		NetworkInterface: domain.NetworkInterface{
			ProjectID:        "prj-owner",
			Name:             domain.RcNameVPC("nic-foreign-sg"),
			SubnetID:         subID,
			SecurityGroupIDs: []string{foreignSG},
		},
	})
	require.NoError(t, err)
	got := awaitOpAny(t, or, op.ID)
	require.NotNil(t, got.Error, "группа чужого проекта обязана быть отвергнута")
	st := status.FromProto(got.Error)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, "security group "+foreignSG+" not found", st.Message(),
		"тон обязан совпадать с отсутствующей группой — иначе это оракул существования")
	assert.Zero(t, nicCount(ctx, t, r, "prj-owner"), "интерфейс не имеет права быть создан")
}

// Несуществующая группа отвергается тем же тоном (оракула нет).
func TestIntegration_NIC_Create_MissingSecurityGroup_Rejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)
	t.Cleanup(r.Close)
	or := repomock.NewOpsRepo()

	subID := insertSubnetRow(ctx, t, r, "prj-owner", "sgmiss")
	absent := ids.NewID(ids.PrefixSecurityGroup)

	uc := niapp.NewCreateNetworkInterfaceUseCase(r, &repomock.ProjectClient{OK: true}, or)
	op, err := uc.Execute(ctx, niapp.CreateInput{
		NetworkInterface: domain.NetworkInterface{
			ProjectID:        "prj-owner",
			Name:             domain.RcNameVPC("nic-miss-sg"),
			SubnetID:         subID,
			SecurityGroupIDs: []string{absent},
		},
	})
	require.NoError(t, err)
	got := awaitOpAny(t, or, op.ID)
	require.NotNil(t, got.Error)
	st := status.FromProto(got.Error)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, "security group "+absent+" not found", st.Message())
}

// Своя группа в сети подсети — проходит (negative-control: гейт не запрещает
// законное).
func TestIntegration_NIC_Create_OwnSecurityGroup_OK(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)
	t.Cleanup(r.Close)
	or := repomock.NewOpsRepo()

	subID := insertSubnetRow(ctx, t, r, "prj-owner", "sgok")
	ownSG := insertSGRow(ctx, t, r, "prj-owner", subnetNetworkID(ctx, t, r, subID), "own-sg")

	uc := niapp.NewCreateNetworkInterfaceUseCase(r, &repomock.ProjectClient{OK: true}, or)
	op, err := uc.Execute(ctx, niapp.CreateInput{
		NetworkInterface: domain.NetworkInterface{
			ProjectID:        "prj-owner",
			Name:             domain.RcNameVPC("nic-own-sg"),
			SubnetID:         subID,
			SecurityGroupIDs: []string{ownSG},
		},
	})
	require.NoError(t, err)
	got := awaitOpAny(t, or, op.ID)
	require.Nil(t, got.Error, "своя группа в сети подсети обязана приниматься")
	assert.Equal(t, 1, nicCount(ctx, t, r, "prj-owner"))
}

// Обновление интерфейса — тот же вход, тот же гейт.
func TestIntegration_NIC_Update_ForeignSecurityGroup_Rejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)
	t.Cleanup(r.Close)
	or := repomock.NewOpsRepo()

	subID := insertSubnetRow(ctx, t, r, "prj-owner", "sgupd")
	victimNet := insertNetworkRow(ctx, t, r, "prj-victim", "victim-net-u")
	foreignSG := insertSGRow(ctx, t, r, "prj-victim", victimNet, "victim-sg-u")

	createUC := niapp.NewCreateNetworkInterfaceUseCase(r, &repomock.ProjectClient{OK: true}, or)
	op, err := createUC.Execute(ctx, niapp.CreateInput{
		NetworkInterface: domain.NetworkInterface{
			ProjectID: "prj-owner",
			Name:      domain.RcNameVPC("nic-upd-sg"),
			SubnetID:  subID,
		},
	})
	require.NoError(t, err)
	awaitOp(t, or, op.ID)
	nicID := singleNICID(ctx, t, pool, "prj-owner")

	updateUC := niapp.NewUpdateNetworkInterfaceUseCase(r, or)
	upOp, err := updateUC.Execute(ctx, niapp.UpdateInput{
		NetworkInterfaceID: nicID,
		NetworkInterface:   domain.NetworkInterface{SecurityGroupIDs: []string{foreignSG}},
		UpdateMask:         []string{"security_group_ids"},
	})
	require.NoError(t, err)
	gotUp := awaitOpAny(t, or, upOp.ID)
	require.NotNil(t, gotUp.Error, "чужая группа обязана быть отвергнута и на обновлении")
	st := status.FromProto(gotUp.Error)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, "security group "+foreignSG+" not found", st.Message())

	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	nic, err := rd.NetworkInterfaces().Get(ctx, nicID)
	require.NoError(t, err)
	_ = rd.Close()
	assert.Empty(t, nic.SecurityGroupIDs, "отвергнутое обновление не имеет права записать ссылку")
}

// Привязка к инстансу на публичном создании интерфейса — не исход. Инвариант
// привязки (владелец, зона, атомарная смена) живёт в охраняемом пути привязки;
// второй писатель той же колонки его не исполняет. Поле обязано быть отвергнуто
// синхронно и с именем поля, а не принято молча.
func TestIntegration_NIC_Create_InstanceBinding_Rejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)
	t.Cleanup(r.Close)
	or := repomock.NewOpsRepo()

	subID := insertSubnetRow(ctx, t, r, "prj-owner", "nicbind")
	uc := niapp.NewCreateNetworkInterfaceUseCase(r, &repomock.ProjectClient{OK: true}, or)

	_, err = uc.Execute(ctx, niapp.CreateInput{
		NetworkInterface: domain.NetworkInterface{
			ProjectID: "prj-owner",
			Name:      domain.RcNameVPC("nic-bind"),
			SubnetID:  subID,
		},
		InstanceID: "epdfakeinstance00001",
	})
	require.Error(t, err, "привязка к инстансу на создании обязана быть отвергнута синхронно")
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, "instance_id", badRequestField(t, err), "отказ обязан называть поле")

	_, err = uc.Execute(ctx, niapp.CreateInput{
		NetworkInterface: domain.NetworkInterface{
			ProjectID: "prj-owner",
			Name:      domain.RcNameVPC("nic-bind2"),
			SubnetID:  subID,
		},
		Index: "1",
	})
	require.Error(t, err, "слот привязки без привязки — тоже принято-и-проигнорировано")
	st, ok = status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, "index", badRequestField(t, err), "отказ обязан называть поле")

	assert.Zero(t, nicCount(ctx, t, r, "prj-owner"))
}

// --- подсеть ↔ таблица маршрутов ---

func insertRouteTableRow(ctx context.Context, t *testing.T, r kacho.Repository, projectID, networkID, name string) string {
	t.Helper()
	rtID := ids.NewID(ids.PrefixRouteTable)
	require.NoError(t, legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
		_, e := w.RouteTables().Insert(ctx, &domain.RouteTable{
			ID:        rtID,
			ProjectID: projectID,
			NetworkID: networkID,
			Name:      domain.RcNameVPC(name),
		})
		return e
	}))
	return rtID
}

// Таблица маршрутов ЧУЖОЙ сети (в т.ч. чужого проекта) на подсети обязана быть
// отвергнута: таблица принадлежит своей сети по построению, поэтому такая
// привязка — состояние, которого по дизайну не бывает, а база выразить его
// запрет не может (внешний ключ проверяет только существование строки).
func TestIntegration_Subnet_Update_ForeignRouteTable_Rejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)
	t.Cleanup(r.Close)
	or := repomock.NewOpsRepo()

	subID := insertSubnetRow(ctx, t, r, "prj-owner", "rtref")
	victimNet := insertNetworkRow(ctx, t, r, "prj-victim", "victim-net-rt")
	foreignRT := insertRouteTableRow(ctx, t, r, "prj-victim", victimNet, "victim-rt")

	updateUC := subnetapp.NewUpdateSubnetUseCase(r, or)
	op, err := updateUC.Execute(ctx, subnetapp.UpdateInput{
		SubnetID:   subID,
		Subnet:     domain.Subnet{RouteTableID: foreignRT},
		UpdateMask: []string{"route_table_id"},
	})
	require.NoError(t, err)
	got := awaitOpAny(t, or, op.ID)
	require.NotNil(t, got.Error, "таблица маршрутов чужой сети обязана быть отвергнута")
	st := status.FromProto(got.Error)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, "route table "+foreignRT+" not found", st.Message())

	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	sub, err := rd.Subnets().Get(ctx, subID)
	require.NoError(t, err)
	_ = rd.Close()
	assert.NotEqual(t, foreignRT, sub.RouteTableID, "отвергнутое обновление не имеет права записать ссылку")
}

// Своя таблица маршрутов той же сети — проходит (negative-control).
func TestIntegration_Subnet_Update_OwnRouteTable_OK(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)
	t.Cleanup(r.Close)
	or := repomock.NewOpsRepo()

	subID := insertSubnetRow(ctx, t, r, "prj-owner", "rtok")
	ownRT := insertRouteTableRow(ctx, t, r, "prj-owner", subnetNetworkID(ctx, t, r, subID), "own-rt")

	updateUC := subnetapp.NewUpdateSubnetUseCase(r, or)
	op, err := updateUC.Execute(ctx, subnetapp.UpdateInput{
		SubnetID:   subID,
		Subnet:     domain.Subnet{RouteTableID: ownRT},
		UpdateMask: []string{"route_table_id"},
	})
	require.NoError(t, err)
	got := awaitOpAny(t, or, op.ID)
	require.Nil(t, got.Error, "своя таблица маршрутов той же сети обязана приниматься")

	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	sub, err := rd.Subnets().Get(ctx, subID)
	require.NoError(t, err)
	_ = rd.Close()
	assert.Equal(t, ownRT, sub.RouteTableID)
}

// Тот же гейт на создании подсети.
func TestIntegration_Subnet_Create_ForeignRouteTable_Rejected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)
	t.Cleanup(r.Close)
	or := repomock.NewOpsRepo()

	ownNet := insertNetworkRow(ctx, t, r, "prj-owner", "own-net-rtc")
	victimNet := insertNetworkRow(ctx, t, r, "prj-victim", "victim-net-rtc")
	foreignRT := insertRouteTableRow(ctx, t, r, "prj-victim", victimNet, "victim-rt-c")

	createUC := subnetapp.NewCreateSubnetUseCase(r, &repomock.ProjectClient{OK: true},
		repomock.NewZoneRegistry("zone-a"), repomock.NewRegionRegistry("reg-a"), or)
	cOp, err := createUC.Execute(ctx, domain.Subnet{
		ProjectID:    "prj-owner",
		Name:         domain.RcNameVPC("sub-foreign-rt"),
		NetworkID:    ownNet,
		ZoneID:       "zone-a",
		V4CidrBlocks: []string{"10.9.0.0/24"},
		RouteTableID: foreignRT,
	})
	require.NoError(t, err)
	require.NotNil(t, cOp.Error, "таблица маршрутов чужой сети обязана быть отвергнута и на создании")
	st := status.FromProto(cOp.Error)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, "route table "+foreignRT+" not found", st.Message())
}

// badRequestField — имя поля из BadRequest-details отказа (контракт «отвергать
// явно, с именем поля»).
func badRequestField(t *testing.T, err error) string {
	t.Helper()
	st, ok := status.FromError(err)
	require.True(t, ok)
	for _, d := range st.Details() {
		if br, isBR := d.(*errdetails.BadRequest); isBR && len(br.GetFieldViolations()) > 0 {
			return br.GetFieldViolations()[0].GetField()
		}
	}
	return ""
}

// Потолки наборов, которые задаёт вызывающий: отказ синхронный, с именем поля,
// и он же закреплён проверкой базы (иначе накопление серией законных запросов).
func TestIntegration_CallerSuppliedSets_Bounded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)
	t.Cleanup(r.Close)
	or := repomock.NewOpsRepo()

	subID := insertSubnetRow(ctx, t, r, "prj-owner", "bounds")

	// Интерфейс: групп больше потолка → синхронный отказ с именем поля.
	tooManySGs := make([]string, domain.MaxNICSecurityGroups+1)
	for i := range tooManySGs {
		tooManySGs[i] = ids.NewID(ids.PrefixSecurityGroup)
	}
	nicUC := niapp.NewCreateNetworkInterfaceUseCase(r, &repomock.ProjectClient{OK: true}, or)
	_, err = nicUC.Execute(ctx, niapp.CreateInput{
		NetworkInterface: domain.NetworkInterface{
			ProjectID:        "prj-owner",
			Name:             domain.RcNameVPC("nic-many-sg"),
			SubnetID:         subID,
			SecurityGroupIDs: tooManySGs,
		},
	})
	// Синхронно и с именем поля: величину задаёт вызывающий, поэтому отказ обязан
	// прийти ДО создания операции и до любого чтения. Ветвления по исходу здесь
	// быть не может — иначе утверждение принимает взаимоисключающие исходы.
	require.Error(t, err, "набор групп сверх потолка обязан быть отвергнут синхронно")
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, "security_group_ids", badRequestField(t, err))
	assert.Zero(t, nicCount(ctx, t, r, "prj-owner"), "операция не должна была создаваться")

	// Подсеть: диапазонов больше потолка → синхронный отказ ДО квадратичной
	// проверки пересечений.
	tooManyCidrs := make([]string, domain.MaxSubnetCidrBlocks+1)
	for i := range tooManyCidrs {
		tooManyCidrs[i] = fmt.Sprintf("10.%d.0.0/24", i)
	}
	addUC := subnetapp.NewAddCidrBlocksUseCase(r, or)
	_, err = addUC.Execute(ctx, subID, tooManyCidrs, nil)
	require.Error(t, err, "набор диапазонов сверх потолка обязан быть отвергнут синхронно")
	// Имя поля — то, которое клиент НАПИСАЛ в теле: страницы объявляют camelCase,
	// а `v4_cidr_blocks` — имя доменного поля, которого нет ни в одном сообщении
	// контракта. Проба ждала именно его и тем закрепляла отказ, называвший поле,
	// которого отправитель не посылал; предмет стережётся прежний — синхронность.
	assert.Equal(t, "ipv4CidrBlocks", badRequestField(t, err))

	// База держит тот же потолок независимо от пути записи: создаём законный
	// интерфейс и пробуем записать набор сверх потолка напрямую.
	okOp, err := nicUC.Execute(ctx, niapp.CreateInput{
		NetworkInterface: domain.NetworkInterface{
			ProjectID: "prj-owner",
			Name:      domain.RcNameVPC("nic-bounds-ok"),
			SubnetID:  subID,
		},
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpAny(t, or, okOp.ID).Error)

	_, dbErr := pool.Exec(ctx,
		`UPDATE network_interfaces SET security_group_ids = (
		     SELECT jsonb_agg(to_jsonb('sgr' || g::text)) FROM generate_series(1, $2) g)
		   WHERE subnet_id = $1`, subID, domain.MaxNICSecurityGroups+1)
	require.Error(t, dbErr, "проверка базы обязана отвергнуть набор сверх потолка")
}

// Конкуренция: создание интерфейса со ссылкой на группу против удаления той же
// группы. Исход обязан быть ровно один из двух — либо интерфейс создан и
// удаление группы отвергнуто её собственным предусловием, либо группа удалена и
// создание отвергнуто проверкой ссылки. Висячей ссылки (интерфейс создан, группа
// удалена) быть не может: проверка ссылки идёт в той же writer-TX, что и запись,
// и берёт share-lock на строке группы.
func TestIntegration_NIC_CreateWithSG_VsSecurityGroupDelete_Serialised(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool, err := coredb.NewPool(ctx, setupTestDB(t))
	require.NoError(t, err)
	pgtest.ClosePoolAtEnd(t, pool)
	r := kachopg.New(pool, nil)
	t.Cleanup(r.Close)
	or := repomock.NewOpsRepo()

	subID := insertSubnetRow(ctx, t, r, "prj-owner", "sgrace")
	sgID := insertSGRow(ctx, t, r, "prj-owner", subnetNetworkID(ctx, t, r, subID), "race-sg")

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		createErr error
		deleteErr error
	)
	start := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		uc := niapp.NewCreateNetworkInterfaceUseCase(r, &repomock.ProjectClient{OK: true}, or)
		op, e := uc.Execute(ctx, niapp.CreateInput{
			NetworkInterface: domain.NetworkInterface{
				ProjectID:        "prj-owner",
				Name:             domain.RcNameVPC("nic-race"),
				SubnetID:         subID,
				SecurityGroupIDs: []string{sgID},
			},
		})
		mu.Lock()
		defer mu.Unlock()
		if e != nil {
			createErr = e
			return
		}
		got := awaitOpAny(t, or, op.ID)
		if got.Error != nil {
			createErr = status.FromProto(got.Error).Err()
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		e := legacyWithTx(t, ctx, r, func(w kacho.RepositoryWriter) error {
			return w.SecurityGroups().Delete(ctx, sgID)
		})
		mu.Lock()
		defer mu.Unlock()
		deleteErr = e
	}()
	close(start)
	wg.Wait()

	// Ровно один исход: висячей ссылки не бывает.
	nicCreated := createErr == nil
	sgDeleted := deleteErr == nil
	require.False(t, nicCreated && sgDeleted,
		"интерфейс создан со ссылкой на удалённую группу: create=%v delete=%v", createErr, deleteErr)
	require.True(t, nicCreated || sgDeleted,
		"хотя бы одна из операций обязана пройти: create=%v delete=%v", createErr, deleteErr)

	// Состояние согласовано: если интерфейс существует — существует и группа.
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	nics, _, err := rd.NetworkInterfaces().List(ctx, kacho.NetworkInterfaceFilter{ProjectID: "prj-owner"}, kacho.Pagination{})
	require.NoError(t, err)
	_, sgErr := rd.SecurityGroups().Get(ctx, sgID)
	_ = rd.Close()
	if len(nics) > 0 {
		require.NoError(t, sgErr, "интерфейс ссылается на группу — группа обязана существовать")
	}
}
