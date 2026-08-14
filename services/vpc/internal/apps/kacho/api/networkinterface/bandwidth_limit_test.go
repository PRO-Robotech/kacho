// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package networkinterface

// Ограничение полосы, задаваемое арендатором, — приём и отказ на пути запроса.
//
// # Что здесь предмет
//
// Правило приёма («умение объявлено» + промежуток величин) живёт в домене и
// проверено там пообъектно (`domain.BandwidthLimitPolicy`). Здесь предмет другой и
// он не выводится из первого: доезжает ли решение до ТОГО МЕСТА, где вызывающий
// его увидит, — синхронно, ДО создания операции, с именем поля в отказе. Проба,
// зовущая правило напрямую, об этом не утверждает ничего и осталась бы зелёной,
// если бы use-case правило не спрашивал вовсе.
//
// # Почему отказ обязан быть СИНХРОННЫМ, а не в операции
//
// Величину задаёт вызывающий, и её негодность видна без единого обращения к БД и
// без единого вызова соседа. Отдав такой отказ в асинхронную часть, мы отдали бы
// вызывающему успешно созданную операцию, которая упадёт позже, — то есть ответ
// «принято» на то, что не принято.
//
// # Пары, а не одиночные утверждения
//
// У каждого отрицания здесь стоит положительный контроль с ТЕМ ЖЕ телом запроса и
// отличием ровно в предмете пробы. Без него «отвергнуто» неотличимо от «отвергается
// всё», а «принято» — от «не читается вовсе».

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// bwStandCeilingMbps — гарантия, которую объявляет СТЕНД этой фикстуры. Строго
// выше опубликованного пола продукта: иначе принимаемый промежуток пуст, и
// положительные пробы ниже были бы неконструируемы.
const bwStandCeilingMbps = 10000

// bwFixture — минимальная обвязка: подсеть-родитель + use-case'ы создания и
// изменения с заданной посадкой.
type bwFixture struct {
	kr     *kachomock.Repository
	ops    *repomock.OpsRepo
	create *CreateNetworkInterfaceUseCase
	update *UpdateNetworkInterfaceUseCase
}

func newBWFixture(t *testing.T, settable bool) *bwFixture {
	t.Helper()
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	kr.SeedSubnet(&kachorepo.SubnetRecord{
		Subnet: domain.Subnet{ID: "e9bsub1", ProjectID: "f1", Name: domain.RcNameVPC("sn")},
	})
	policy := domain.NewBandwidthLimitPolicy(settable, bwStandCeilingMbps)
	return &bwFixture{
		kr:  kr,
		ops: or,
		create: NewCreateNetworkInterfaceUseCase(kr, &repomock.ProjectClient{OK: true}, or).
			WithBandwidthLimitPolicy(policy),
		update: NewUpdateNetworkInterfaceUseCase(kr, or).WithBandwidthLimitPolicy(policy),
	}
}

func (f *bwFixture) createInput(limit int64) CreateInput {
	return CreateInput{NetworkInterface: domain.NetworkInterface{
		ProjectID:          "f1",
		Name:               "nic",
		SubnetID:           "e9bsub1",
		BandwidthLimitMbps: limit,
	}}
}

// violationFieldsOf — имена полей из BadRequest-деталей отказа. Отказ обязан
// НАЗЫВАТЬ поле машинно, а не только прозой: вызывающему нужно понять, что чинить.
func violationFieldsOf(t *testing.T, err error) []string {
	t.Helper()
	st, ok := status.FromError(err)
	require.True(t, ok, "ошибка обязана нести gRPC-статус: %v", err)
	var out []string
	for _, d := range st.Details() {
		br, ok := d.(*errdetails.BadRequest)
		if !ok {
			continue
		}
		for _, fv := range br.GetFieldViolations() {
			out = append(out, fv.GetField())
		}
	}
	return out
}

// TestNICCreate_BandwidthLimit_RejectedWithoutCapability — стенд без умения
// отвергает величину СИНХРОННО, с именем поля; тот же вход на стенде с умением
// проходит и величина СОХРАНЯЕТСЯ.
func TestNICCreate_BandwidthLimit_RejectedWithoutCapability(t *testing.T) {
	const limit = domain.GuaranteedInterfaceBandwidthFloorMbps + 500

	noCap := newBWFixture(t, false)
	_, err := noCap.create.Execute(context.Background(), noCap.createInput(limit))
	require.Error(t, err, "молчаливое принятие — запрещённый исход, а не мягкость")
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Contains(t, violationFieldsOf(t, err), "bandwidth_limit_mbps",
		"отказ обязан назвать поле — иначе вызывающий не узнает, что именно снято")
	ops, _, lerr := noCap.ops.List(context.Background(), operations.ListFilter{PageSize: 10})
	require.NoError(t, lerr)
	assert.Empty(t, ops,
		"отказ синхронный: операция создаваться не должна, иначе вызывающий получит "+
			"успешную операцию на то, что не принято")

	// Положительный контроль — тот же вход на стенде, объявившем умение.
	withCap := newBWFixture(t, true)
	op, cerr := withCap.create.Execute(context.Background(), withCap.createInput(limit))
	require.NoError(t, cerr, "с объявленным умением величина обязана проходить")
	saved := repomock.AwaitOpDone(t, withCap.ops, op.ID)
	require.Nil(t, saved.Error, "операция создания не должна падать: %v", saved.Error)

	var pb vpcv1.NetworkInterface
	require.NoError(t, saved.Response.UnmarshalTo(&pb))
	assert.Equal(t, int64(limit), pb.BandwidthLimitMbps,
		"принятая величина обязана быть ВИДНА в ресурсе: принять и не показать — "+
			"то же самое принято-и-проигнорировано, только тише")
}

// TestNICCreate_BandwidthLimit_UnsetPassesOnAStandWithoutCapability — отсутствие
// просьбы не есть просьба.
//
// Без этой пробы отказ «умения нет» ловил бы КАЖДОЕ создание интерфейса на стенде
// без умения — то есть чинил бы поле ценой ресурса.
func TestNICCreate_BandwidthLimit_UnsetPassesOnAStandWithoutCapability(t *testing.T) {
	f := newBWFixture(t, false)
	op, err := f.create.Execute(context.Background(), f.createInput(domain.TenantBandwidthLimitUnset))
	require.NoError(t, err)
	saved := repomock.AwaitOpDone(t, f.ops, op.ID)
	require.Nil(t, saved.Error, "%v", saved.Error)
}

// TestNICCreate_BandwidthLimit_Bounds — обе границы промежутка на пути запроса,
// каждая с обеих сторон.
func TestNICCreate_BandwidthLimit_Bounds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		v     int64
		valid bool
	}{
		{"ровно пол продукта", domain.GuaranteedInterfaceBandwidthFloorMbps, false},
		{"на единицу выше пола", domain.GuaranteedInterfaceBandwidthFloorMbps + 1, true},
		{"ровно гарантия стенда", bwStandCeilingMbps, true},
		{"на единицу выше гарантии стенда", bwStandCeilingMbps + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBWFixture(t, true)
			op, err := f.create.Execute(context.Background(), f.createInput(tc.v))
			if tc.valid {
				require.NoError(t, err)
				saved := repomock.AwaitOpDone(t, f.ops, op.ID)
				require.Nil(t, saved.Error, "%v", saved.Error)
				return
			}
			require.Error(t, err)
			require.Equal(t, codes.InvalidArgument, status.Code(err))
			assert.Contains(t, violationFieldsOf(t, err), "bandwidth_limit_mbps")
		})
	}
}

// TestNICUpdate_BandwidthLimit_IsMutable — величина ИЗМЕНЯЕМА (настройка, а не
// идентичность) и на стенде без умения по-прежнему отвергается.
func TestNICUpdate_BandwidthLimit_IsMutable(t *testing.T) {
	const first = domain.GuaranteedInterfaceBandwidthFloorMbps + 500
	const second = domain.GuaranteedInterfaceBandwidthFloorMbps + 1500

	f := newBWFixture(t, true)
	op, err := f.create.Execute(context.Background(), f.createInput(first))
	require.NoError(t, err)
	created := repomock.AwaitOpDone(t, f.ops, op.ID)
	require.Nil(t, created.Error, "%v", created.Error)
	var pb vpcv1.NetworkInterface
	require.NoError(t, created.Response.UnmarshalTo(&pb))

	upOp, uerr := f.update.Execute(context.Background(), UpdateInput{
		NetworkInterfaceID: pb.Id,
		NetworkInterface:   domain.NetworkInterface{BandwidthLimitMbps: second},
		UpdateMask:         []string{"bandwidth_limit_mbps"},
	})
	require.NoError(t, uerr, "величина изменяема — это настройка, а не идентичность")
	saved := repomock.AwaitOpDone(t, f.ops, upOp.ID)
	require.Nil(t, saved.Error, "%v", saved.Error)
	var updated vpcv1.NetworkInterface
	require.NoError(t, saved.Response.UnmarshalTo(&updated))
	assert.Equal(t, int64(second), updated.BandwidthLimitMbps)

	// Снятие ограничения — тем же путём, канонический ноль.
	clearOp, cerr := f.update.Execute(context.Background(), UpdateInput{
		NetworkInterfaceID: pb.Id,
		NetworkInterface:   domain.NetworkInterface{BandwidthLimitMbps: domain.TenantBandwidthLimitUnset},
		UpdateMask:         []string{"bandwidth_limit_mbps"},
	})
	require.NoError(t, cerr)
	cleared := repomock.AwaitOpDone(t, f.ops, clearOp.ID)
	require.Nil(t, cleared.Error, "%v", cleared.Error)
	var afterClear vpcv1.NetworkInterface
	require.NoError(t, cleared.Response.UnmarshalTo(&afterClear))
	assert.Equal(t, domain.TenantBandwidthLimitUnset, afterClear.BandwidthLimitMbps)
}

// TestNICUpdate_BandwidthLimit_RejectedWithoutCapability — тот же отказ на пути
// изменения; рядом положительный контроль, что ДРУГОЕ поле на этом же стенде
// по-прежнему меняется.
//
// Второй половиной проверяется, что отказ прицелен: правило, отвергающее любое
// изменение на стенде без умения, прошло бы отрицательную половину целиком.
func TestNICUpdate_BandwidthLimit_RejectedWithoutCapability(t *testing.T) {
	f := newBWFixture(t, false)
	op, err := f.create.Execute(context.Background(), f.createInput(domain.TenantBandwidthLimitUnset))
	require.NoError(t, err)
	created := repomock.AwaitOpDone(t, f.ops, op.ID)
	require.Nil(t, created.Error, "%v", created.Error)
	var pb vpcv1.NetworkInterface
	require.NoError(t, created.Response.UnmarshalTo(&pb))

	_, uerr := f.update.Execute(context.Background(), UpdateInput{
		NetworkInterfaceID: pb.Id,
		NetworkInterface:   domain.NetworkInterface{BandwidthLimitMbps: bwStandCeilingMbps},
		UpdateMask:         []string{"bandwidth_limit_mbps"},
	})
	require.Error(t, uerr)
	require.Equal(t, codes.InvalidArgument, status.Code(uerr))
	assert.Contains(t, violationFieldsOf(t, uerr), "bandwidth_limit_mbps")

	// Положительный контроль — соседнее поле на том же стенде меняется.
	okOp, oerr := f.update.Execute(context.Background(), UpdateInput{
		NetworkInterfaceID: pb.Id,
		NetworkInterface:   domain.NetworkInterface{Description: domain.RcDescription("still mutable")},
		UpdateMask:         []string{"description"},
	})
	require.NoError(t, oerr, "отказ обязан быть прицельным, а не глушить путь изменения целиком")
	require.Nil(t, repomock.AwaitOpDone(t, f.ops, okOp.ID).Error)
}

// TestNICUpdate_BandwidthLimit_MaskPathIsKnown — путь маски объявлен известным.
//
// Отдельная проба, потому что известный набор путей маски — ДРУГОЕ место, чем
// применение: поле, применяемое, но не объявленное, отвергается generic-отказом
// «поле не распознано», и вызывающий видит его вместо контрактного отказа по
// существу.
func TestNICUpdate_BandwidthLimit_MaskPathIsKnown(t *testing.T) {
	f := newBWFixture(t, true)
	op, err := f.create.Execute(context.Background(), f.createInput(domain.TenantBandwidthLimitUnset))
	require.NoError(t, err)
	created := repomock.AwaitOpDone(t, f.ops, op.ID)
	require.Nil(t, created.Error, "%v", created.Error)
	var pb vpcv1.NetworkInterface
	require.NoError(t, created.Response.UnmarshalTo(&pb))

	_, uerr := f.update.Execute(context.Background(), UpdateInput{
		NetworkInterfaceID: pb.Id,
		NetworkInterface:   domain.NetworkInterface{BandwidthLimitMbps: bwStandCeilingMbps},
		UpdateMask:         []string{"bandwidth_limit_mbps"},
	})
	require.NoError(t, uerr)

	// Отрицательный контроль — выдуманный путь по-прежнему отвергается, то есть
	// набор не стал принимать всё подряд.
	_, berr := f.update.Execute(context.Background(), UpdateInput{
		NetworkInterfaceID: pb.Id,
		UpdateMask:         []string{"made_up_field"},
	})
	require.Error(t, berr)
	require.Equal(t, codes.InvalidArgument, status.Code(berr))
}
