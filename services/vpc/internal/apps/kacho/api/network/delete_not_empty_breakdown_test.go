// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// Отказ Network.Delete на непустой сети обязан назвать РАДИУС — виды и числа
// арендаторских дочерних, — а не только факт непустоты. Прежний текст
// («Network <id> is not empty») заставлял арендатора выяснять радиус перебором:
// удалить подсети, повторить, получить тот же текст из-за группы правил,
// повторить снова.
//
// Что здесь утверждается и почему именно так:
//   - перечисляются ВИДЫ И ЧИСЛА, а не идентификаторы. Число — не координата;
//     перечень идентификаторов чужих объектов ею становится, поэтому отдельная
//     проба требует их отсутствия в тексте;
//   - системные дочерние (RT по умолчанию, группа правил по умолчанию) в счёт НЕ
//     входят: их провижнит сам сервис на Create и снимает в той же транзакции
//     Delete. Без положительного контроля («только системные ⇒ УСПЕХ») проба
//     зеленела бы на запрете удаления вообще;
//   - счёт идёт ПО ВСЕМ страницам курсора. Одна страница ограничена page_size
//     (умолчание — 50), поэтому счёт по первой странице занижал бы радиус ровно
//     там, где он велик.

// pageOf — постраничная нарезка для фикстур-читателей ниже. Токен — индекс
// следующей строки; непустой ровно тогда, когда осталось ещё. Продукту токен
// непрозрачен, поэтому его форма здесь произвольна: проба заодно утверждает, что
// счётчик передаёт токен обратно КАК ЕСТЬ и не разбирает его.
//
// Фикстура намеренно СТРОЖЕ продукта: она отдаёт свою страницу (меньше
// запрошенной), поэтому «страница короче запрошенной» не может быть прочитано
// счётчиком как «страниц больше нет» — он обязан идти за токеном.
func pageOf[T any](rows []T, pageSize int, token string) ([]T, string, error) {
	start := 0
	if token != "" {
		n, err := strconv.Atoi(token)
		if err != nil {
			return nil, "", fmt.Errorf("fixture received a page token it never issued: %q", token)
		}
		start = n
	}
	if start > len(rows) {
		return nil, "", fmt.Errorf("fixture received an out-of-range page token: %q", token)
	}
	end := min(start+pageSize, len(rows))
	next := ""
	if end < len(rows) {
		next = strconv.Itoa(end)
	}
	return rows[start:end], next, nil
}

type fakeSubnetReader struct {
	rows     []*kacho.SubnetRecord
	pageSize int
	calls    int
	// stuck — курсор, не сдвигающийся вперёд: нарушение контракта чтения.
	// Счётчик обязан отвечать отказом, а не крутиться вечно.
	stuck bool
}

func (r *fakeSubnetReader) List(_ context.Context, f SubnetFilter, p Pagination) ([]*kacho.SubnetRecord, string, error) {
	r.calls++
	if r.stuck {
		return r.rows, "same-token-forever", nil
	}
	if f.NetworkID == "" {
		return nil, "", fmt.Errorf("fixture expects a network-scoped read, got an unscoped one")
	}
	return pageOf(r.rows, r.pageSize, p.PageToken)
}

type fakeRouteTableReader struct {
	rows     []*kacho.RouteTableRecord
	pageSize int
	calls    int
}

func (r *fakeRouteTableReader) List(_ context.Context, f RouteTableFilter, p Pagination) ([]*kacho.RouteTableRecord, string, error) {
	r.calls++
	if f.NetworkID == "" {
		return nil, "", fmt.Errorf("fixture expects a network-scoped read, got an unscoped one")
	}
	return pageOf(r.rows, r.pageSize, p.PageToken)
}

type fakeSGRepo struct {
	rows     []*kacho.SecurityGroupRecord
	pageSize int
	calls    int
}

func (r *fakeSGRepo) List(_ context.Context, f SecurityGroupFilter, p Pagination) ([]*kacho.SecurityGroupRecord, string, error) {
	r.calls++
	if f.NetworkID == "" {
		return nil, "", fmt.Errorf("fixture expects a network-scoped read, got an unscoped one")
	}
	return pageOf(r.rows, r.pageSize, p.PageToken)
}

// Insert/Delete на пути Network.Delete не участвуют (снятие системной группы
// идёт через writer той же транзакции). Фикстура говорит это отказом, а не
// молчаливым успехом: молчаливый успех скрыл бы переезд снятия на этот порт.
func (r *fakeSGRepo) Insert(_ context.Context, _ *domain.SecurityGroup) (*kacho.SecurityGroupRecord, error) {
	return nil, fmt.Errorf("fixture: SecurityGroupRepo.Insert must not be called on the Network.Delete path")
}

func (r *fakeSGRepo) Delete(_ context.Context, _ string) error {
	return fmt.Errorf("fixture: SecurityGroupRepo.Delete must not be called on the Network.Delete path")
}

// seedNetworkWithSystemChildren создаёт сеть ПРОДУКТОВЫМ путём Create — вместе с её системными
// дочерними (RT по умолчанию + группа правил по умолчанию). Что считать
// системным, определяет продукт, а не проба: id системной RT берётся из
// созданной сети, а не выдумывается.
func seedNetworkWithSystemChildren(t *testing.T) (*kachomock.Repository, *repomock.OpsRepo, *vpcv1.Network) {
	t.Helper()
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	create := NewCreateNetworkUseCase(kr, &repomock.ProjectClient{OK: true}, or)
	op, err := create.Execute(context.Background(), domain.Network{
		ProjectID:      "prj-b3n7k1x9q2m5t8",
		Name:           domain.RcNameVPC("delete-radius"),
		IPv4CidrBlocks: []string{"10.30.0.0/16"},
	})
	require.NoError(t, err)
	require.Nil(t, op.Error)
	var n vpcv1.Network
	require.NoError(t, op.Response.UnmarshalTo(&n))
	require.NotEmpty(t, n.DefaultRouteTableId, "системная RT обязана быть провижнена Create")
	require.NotEmpty(t, n.DefaultSecurityGroupId, "системная группа правил обязана быть провижнена Create")
	return kr, or, &n
}

func subnetRows(networkID string, ids ...string) []*kacho.SubnetRecord {
	rows := make([]*kacho.SubnetRecord, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, &kacho.SubnetRecord{Subnet: domain.Subnet{ID: id, NetworkID: networkID}})
	}
	return rows
}

// ---- отказ называет виды и числа ----

func TestDeleteUseCase_NotEmpty_RefusalNamesKindsAndCounts(t *testing.T) {
	kr, or, n := seedNetworkWithSystemChildren(t)

	subnets := &fakeSubnetReader{
		rows:     subnetRows(n.Id, "sub-aaaaaaaaaaaaaaaaa", "sub-bbbbbbbbbbbbbbbbb"),
		pageSize: 50,
	}
	// Три RT, из которых одна — системная: в счёт входят две арендаторские.
	rts := &fakeRouteTableReader{
		rows: []*kacho.RouteTableRecord{
			{RouteTable: domain.RouteTable{ID: n.DefaultRouteTableId, NetworkID: n.Id}},
			{RouteTable: domain.RouteTable{ID: "rtb-tenant0000000001", NetworkID: n.Id}},
			{RouteTable: domain.RouteTable{ID: "rtb-tenant0000000002", NetworkID: n.Id}},
		},
		pageSize: 50,
	}
	// Две группы правил, из которых одна — системная: в счёт входит одна.
	sgs := &fakeSGRepo{
		rows: []*kacho.SecurityGroupRecord{
			{SecurityGroup: domain.SecurityGroup{ID: n.DefaultSecurityGroupId, NetworkID: n.Id, DefaultForNetwork: true}},
			{SecurityGroup: domain.SecurityGroup{ID: "sg-tenant00000000001", NetworkID: n.Id}},
		},
		pageSize: 50,
	}

	uc := NewDeleteNetworkUseCase(kr, subnets, rts, sgs, or)
	_, err := uc.Execute(context.Background(), n.Id)
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.FailedPrecondition, st.Code(), "код отказа — часть контракта")
	assert.Equal(t,
		fmt.Sprintf("Network %s is not empty (subnets: 2, route tables: 2, security groups: 1)", n.Id),
		st.Message(),
		"отказ обязан назвать ВСЕ мешающие виды и их числа одним ответом")
}

// Вид с нулём мешающих в перечень не попадает: «subnets: 0» ничего не сообщает,
// а перечень с нулями невозможно прочитать как радиус.
func TestDeleteUseCase_NotEmpty_RefusalOmitsKindsThatDoNotBlock(t *testing.T) {
	kr, or, n := seedNetworkWithSystemChildren(t)

	subnets := &fakeSubnetReader{pageSize: 50} // ноль подсетей
	rts := &fakeRouteTableReader{
		rows: []*kacho.RouteTableRecord{
			{RouteTable: domain.RouteTable{ID: n.DefaultRouteTableId, NetworkID: n.Id}},
		},
		pageSize: 50,
	} // только системная
	sgs := &fakeSGRepo{
		rows: []*kacho.SecurityGroupRecord{
			{SecurityGroup: domain.SecurityGroup{ID: n.DefaultSecurityGroupId, NetworkID: n.Id, DefaultForNetwork: true}},
			{SecurityGroup: domain.SecurityGroup{ID: "sg-tenant00000000001", NetworkID: n.Id}},
			{SecurityGroup: domain.SecurityGroup{ID: "sg-tenant00000000002", NetworkID: n.Id}},
		},
		pageSize: 50,
	}

	uc := NewDeleteNetworkUseCase(kr, subnets, rts, sgs, or)
	_, err := uc.Execute(context.Background(), n.Id)
	require.Error(t, err)

	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	assert.Equal(t,
		fmt.Sprintf("Network %s is not empty (security groups: 2)", n.Id),
		st.Message())
}

// Идентификаторы дочерних в отказе не печатаются. Число — не координата;
// перечень идентификаторов чужих объектов ею становится.
func TestDeleteUseCase_NotEmpty_RefusalCarriesNoChildIdentifiers(t *testing.T) {
	kr, or, n := seedNetworkWithSystemChildren(t)

	childIDs := []string{
		"sub-aaaaaaaaaaaaaaaaa",
		"rtb-tenant0000000001",
		"sg-tenant00000000001",
		n.DefaultRouteTableId,
		n.DefaultSecurityGroupId,
	}

	subnets := &fakeSubnetReader{rows: subnetRows(n.Id, childIDs[0]), pageSize: 50}
	rts := &fakeRouteTableReader{
		rows: []*kacho.RouteTableRecord{
			{RouteTable: domain.RouteTable{ID: n.DefaultRouteTableId, NetworkID: n.Id}},
			{RouteTable: domain.RouteTable{ID: childIDs[1], NetworkID: n.Id}},
		},
		pageSize: 50,
	}
	sgs := &fakeSGRepo{
		rows: []*kacho.SecurityGroupRecord{
			{SecurityGroup: domain.SecurityGroup{ID: n.DefaultSecurityGroupId, NetworkID: n.Id, DefaultForNetwork: true}},
			{SecurityGroup: domain.SecurityGroup{ID: childIDs[2], NetworkID: n.Id}},
		},
		pageSize: 50,
	}

	uc := NewDeleteNetworkUseCase(kr, subnets, rts, sgs, or)
	_, err := uc.Execute(context.Background(), n.Id)
	require.Error(t, err)
	st, _ := status.FromError(err)

	for _, id := range childIDs {
		assert.NotContains(t, st.Message(), id,
			"идентификатор дочернего ресурса не печатается в отказе")
	}
	// Положительная половина: собственный id адресата отказа В тексте есть —
	// иначе проба выше зеленела бы на пустом сообщении.
	assert.Contains(t, st.Message(), n.Id, "отказ обязан называть сеть, о которой он")
}

// Счёт идёт по ВСЕМ страницам курсора: пять подсетей, страница по две ⇒ «5», а не
// «2». Без этого число было бы верхней границей страницы, выданной за измерение.
func TestDeleteUseCase_NotEmpty_CountSpansAllCursorPages(t *testing.T) {
	kr, or, n := seedNetworkWithSystemChildren(t)

	subnets := &fakeSubnetReader{
		rows: subnetRows(n.Id,
			"sub-page1aaaaaaaaaa", "sub-page1bbbbbbbbbb",
			"sub-page2aaaaaaaaaa", "sub-page2bbbbbbbbbb",
			"sub-page3aaaaaaaaaa"),
		pageSize: 2,
	}
	rts := &fakeRouteTableReader{
		rows: []*kacho.RouteTableRecord{
			{RouteTable: domain.RouteTable{ID: n.DefaultRouteTableId, NetworkID: n.Id}},
		},
		pageSize: 2,
	}
	sgs := &fakeSGRepo{
		rows: []*kacho.SecurityGroupRecord{
			{SecurityGroup: domain.SecurityGroup{ID: n.DefaultSecurityGroupId, NetworkID: n.Id, DefaultForNetwork: true}},
		},
		pageSize: 2,
	}

	uc := NewDeleteNetworkUseCase(kr, subnets, rts, sgs, or)
	_, err := uc.Execute(context.Background(), n.Id)
	require.Error(t, err)

	st, _ := status.FromError(err)
	assert.Equal(t,
		fmt.Sprintf("Network %s is not empty (subnets: 5)", n.Id),
		st.Message(),
		"число обязано быть суммой по всем страницам, а не размером первой")
	assert.Equal(t, 3, subnets.calls, "три страницы ⇒ три обхода курсора")
}

// Курсор, не сдвинувшийся вперёд, — нарушение контракта чтения. Отвечать на него
// недосчётом значило бы выдать заниженное число за измеренное, поэтому это
// отказ с фиксированным непрозрачным текстом (и НЕ вечный цикл: сама проба
// завершается только потому, что цикл прерван).
func TestDeleteUseCase_NotEmpty_NonAdvancingCursorIsRefusedNotUndercounted(t *testing.T) {
	kr, or, n := seedNetworkWithSystemChildren(t)

	subnets := &fakeSubnetReader{
		rows:     subnetRows(n.Id, "sub-aaaaaaaaaaaaaaaaa"),
		pageSize: 1,
		stuck:    true,
	}

	uc := NewDeleteNetworkUseCase(kr, subnets, nil, nil, or)
	_, err := uc.Execute(context.Background(), n.Id)
	require.Error(t, err)

	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal database error", st.Message(),
		"неотображённая ошибка отдаётся фиксированным текстом, без диагностики наружу")
	assert.NotContains(t, st.Message(), "cursor", "внутренняя причина наружу не течёт")
}

// ---- ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ ----

// Сеть, у которой есть только СИСТЕМНЫЕ дочерние, удаляется. Без этой пробы
// отрицания выше зеленели бы и на запрете удаления вообще.
func TestDeleteUseCase_OnlySystemChildren_Deletes(t *testing.T) {
	kr, or, n := seedNetworkWithSystemChildren(t)

	subnets := &fakeSubnetReader{pageSize: 50}
	rts := &fakeRouteTableReader{
		rows: []*kacho.RouteTableRecord{
			{RouteTable: domain.RouteTable{ID: n.DefaultRouteTableId, NetworkID: n.Id}},
		},
		pageSize: 50,
	}
	sgs := &fakeSGRepo{
		rows: []*kacho.SecurityGroupRecord{
			{SecurityGroup: domain.SecurityGroup{ID: n.DefaultSecurityGroupId, NetworkID: n.Id, DefaultForNetwork: true}},
		},
		pageSize: 50,
	}

	uc := NewDeleteNetworkUseCase(kr, subnets, rts, sgs, or)
	op, err := uc.Execute(context.Background(), n.Id)
	require.NoError(t, err, "системные дочерние сеть непустой НЕ делают")
	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.True(t, saved.Done)
	require.Nil(t, saved.Error)

	// Каскад системных дочерних остаётся: сеть и оба её системных ребёнка сняты.
	assert.Empty(t, kr.Networks(), "сеть удалена")
	assert.Empty(t, kr.RouteTables(), "системная RT снята той же транзакцией")
	assert.Empty(t, kr.SecurityGroups(), "системная группа правил снята той же транзакцией")

	// Все три вида действительно опрошены — «успех» не от того, что читатель молчал.
	assert.Equal(t, 1, subnets.calls)
	assert.Equal(t, 1, rts.calls)
	assert.Equal(t, 1, sgs.calls)
}

// Отсутствующий читатель вида не выдумывает ни числа, ни отказа: класс просто не
// проверяется (scoped wiring юнитов).
func TestDeleteUseCase_NilReaders_SkipTheirClass(t *testing.T) {
	kr, or, n := seedNetworkWithSystemChildren(t)

	uc := NewDeleteNetworkUseCase(kr, nil, nil, nil, or)
	op, err := uc.Execute(context.Background(), n.Id)
	require.NoError(t, err)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.True(t, saved.Done)
	require.Nil(t, saved.Error)
}

// Малформед-id отвергается ПЕРВЫМ стейтментом — до любого чтения дочерних.
// Иначе перечисление радиуса стало бы работой, выполненной по мусорному вводу.
func TestDeleteUseCase_MalformedID_RefusedBeforeChildReads(t *testing.T) {
	kr, or, _ := seedNetworkWithSystemChildren(t)
	subnets := &fakeSubnetReader{pageSize: 50}

	uc := NewDeleteNetworkUseCase(kr, subnets, nil, nil, or)
	_, err := uc.Execute(context.Background(), "not-a-network-id")
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.True(t, strings.HasPrefix(st.Message(), "invalid network id"),
		"тон отказа на формат — часть контракта, получено %q", st.Message())
	assert.Zero(t, subnets.calls, "дочерние не читаются по неразобранному id")
}
