// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

// RED-фаза (строгий TDD, ban #12) под задачу продукта #1473 — зональный
// балансировщик обязан НАЗЫВАТЬ свою зону.
//
// Публичная проекция несла `placement_type ∈ {ZONAL, REGIONAL}` и не несла зоны:
// про INTERNAL_ZONAL можно было сказать «размещение зональное» и нельзя — КАКОЕ.
// Площадка, на которой машина и балансировщик обязаны совпасть
// (data-integrity.md §Placement-coherence), оставалась неназванной, а общий
// якорь размещения консоли вырождался — ветвь ZONAL была недостижима by
// construction.
//
// Зона НЕ выводится разбором имени и НЕ спрашивается заново: она уже резолвится
// на пути запроса у владельца подсети (`resolveOneSource` → `Subnet.ZoneID`) и
// ровно там же сверяется на согласие семейств. Здесь она перестаёт выбрасываться
// после проверки и доезжает до строки.
//
// Вторая половина — БЕЗОПАСНОСТЬ, и она проверяется отрицаниями. Зона у
// REGIONAL/anycast отсутствует by construction, а у EXTERNAL зона подлежащего
// адреса деривится платформой и от публичной поверхности СКРЫТА (тот самый
// placement-leak, из-за которого сняты прежние per-zone-поля, `reserved 15, 18`).
// Поэтому непустая зона допустима РОВНО при ZONAL — и это же требование стоит
// DB-CHECK'ом.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// TestCreate_ZonalLoadBalancerRecordsItsZone — ZONAL: зона резолвнутой подсети
// доезжает до строки. Положительный случай пары.
func TestCreate_ZonalLoadBalancerRecordsItsZone(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	sn := &fakeSubnetClient{getFunc: func(_ context.Context, id string) (*vpcclient.Subnet, error) {
		return zonalSubnet(id, "region-1-a", "region-1", "net-1"), nil
	}}
	uc := newCreateUC(repo, opsRepo, createDeps{subnet: sn})

	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_ZONAL
	req.V4Source = vipSubnet("sub-v4")

	op, err := uc.Execute(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)

	lb := lbByName(t, repo, "lb-1")
	require.Equal(t, domain.PlacementZonal, lb.PlacementType)
	require.Equal(t, domain.ZoneID("region-1-a"), lb.ZoneID,
		"зональный балансировщик обязан назвать площадку, на которой стоит")
}

// TestCreate_ZonalDualstack_RecordsTheSharedZone — dualstack ZONAL: обе семьи уже
// обязаны сойтись в одной зоне, и записывается именно она (а не «зона первой
// попавшейся семьи»).
func TestCreate_ZonalDualstack_RecordsTheSharedZone(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	sn := &fakeSubnetClient{getFunc: func(_ context.Context, id string) (*vpcclient.Subnet, error) {
		return zonalSubnet(id, "region-1-b", "region-1", "net-1"), nil
	}}
	uc := newCreateUC(repo, opsRepo, createDeps{subnet: sn})

	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_ZONAL
	req.V4Source = vipSubnet("sub-v4")
	req.V6Source = vipSubnet("sub-v6")

	op, err := uc.Execute(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
	require.Equal(t, domain.ZoneID("region-1-b"), lbByName(t, repo, "lb-1").ZoneID)
}

// TestCreate_RegionalLoadBalancerHasNoZone — REGIONAL/anycast: зоны нет by
// construction, и придумывать её нельзя. Отрицание в паре с положительным выше.
func TestCreate_RegionalLoadBalancerHasNoZone(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	sn := &fakeSubnetClient{getFunc: func(_ context.Context, id string) (*vpcclient.Subnet, error) {
		return regionalSubnet(id, "region-1", "net-1"), nil
	}}
	uc := newCreateUC(repo, opsRepo, createDeps{subnet: sn})

	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_REGIONAL
	req.V4Source = vipSubnet("sub-v4")

	op, err := uc.Execute(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)

	lb := lbByName(t, repo, "lb-1")
	require.Equal(t, domain.PlacementRegional, lb.PlacementType)
	require.Equal(t, domain.ZoneID(""), lb.ZoneID,
		"у anycast-балансировщика зональной координаты нет — сравнивать не с чем")
}

// TestCreate_ExternalLoadBalancerHidesTheUnderlyingZone — EXTERNAL: зона
// подлежащего адреса деривится платформой из региона и от публичной поверхности
// скрыта. Это не «нечего записать», а НАМЕРЕННОЕ сокрытие (placement-leak), и
// оно проверяется отдельно от anycast-случая: производители у них разные.
func TestCreate_ExternalLoadBalancerHidesTheUnderlyingZone(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	uc := newCreateUC(repo, opsRepo, createDeps{})

	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_EXTERNAL_REGIONAL
	req.V4Source = vipPublic()

	op, err := uc.Execute(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)

	lb := lbByName(t, repo, "lb-1")
	require.Equal(t, domain.ZoneID(""), lb.ZoneID,
		"зона внешнего VIP деривится платформой и наружу не выходит")
}

// TestUpdate_ZoneIsImmutable — площадка не меняется правкой (#1473).
//
// Смена площадки — не правка поля, а перестановка балансировщика на другую
// площадку, то есть другой ресурс. Отказ обязан быть КОНВЕНЦИОННЫМ: поле, не
// внесённое в таблицу неизменяемых, отвергается маской как «неизвестное», и
// вызывающий читает «такого поля нет» вместо «его нельзя менять».
func TestUpdate_ZoneIsImmutable(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	uc := NewUpdateLoadBalancerUseCase(repo, newFakeOpsRepo(), nil, nil)

	_, err := uc.Execute(context.Background(), &lbv1.UpdateNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		UpdateMask:            &fieldmaskpb.FieldMask{Paths: []string{"zone_id"}},
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Equal(t, "zone_id is immutable after NetworkLoadBalancer.Create", status.Convert(err).Message())
}
