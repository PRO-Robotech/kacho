// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// peer_unconfigured_failclosed_test.go — «peer не сконфигурирован» на мутации
// обязан быть FAIL-CLOSED, а не «пропустить проверку».
//
// Прежний контракт: `client == nil → return nil` (проверка пропускается
// целиком). Это неверная конфигурация развёрнутого стенда (nil-клиент возникает
// РОВНО когда addr ребра пуст — см. composition root), а не легитимный режим
// работы, и цена ошибки — молча снятый placement-coherence инвариант на
// мутации: LB привязывается к подсети/адресу чужой зоны/региона/проекта, и
// ничто этого не ловит (data-integrity.md §Placement-coherence требует
// энфорса на request-path, security.md — fail-closed на мутациях).
//
// Эталон уже был в этом же файле-соседе: `externalAddressRegionCoherent`
// отдаёт `Unavailable` при nil-резолвере вместо пропуска. Три оставшиеся
// проверки приводятся к нему.
//
// Второй слой (config.Validate) ловит ту же ошибку ещё на старте в
// production-режиме — см. config/validate_test.go.

// ucWithNilPeer собирает Create use-case, где ОДИН peer-клиент намеренно nil
// (остальные — рабочие двойники), чтобы зафиксировать поведение именно на
// несконфигурированном ребре.
func ucWithNilPeer(repo *fakeRepo, opsRepo *fakeOpsRepo, d createDeps) *CreateLoadBalancerUseCase {
	return NewCreateLoadBalancerUseCase(repo, opsRepo,
		&fakeProjectClient{}, &fakeRegionClient{}, d.zone, &fakeZoneRegionClient{},
		d.subnet, d.reader, &fakeAddressClient{}, slog.Default())
}

// Create INTERNAL из subnet-source с несконфигурированным vpc: placement/region
// когерентность подсети проверить нечем → мутация обязана отказать.
func TestCreate_SubnetPeerUnconfigured_FailsClosed(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	uc := ucWithNilPeer(repo, opsRepo, createDeps{
		subnet: nil, reader: &fakeAddressReader{}, zone: &fakeZoneClient{},
	})
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_ZONAL
	req.V4Source = vipSubnet(lbTestSubnetZonal)

	_, err := uc.Execute(context.Background(), req)
	require.Equal(t, codes.Unavailable, status.Code(err),
		"unconfigured vpc peer must fail the mutation closed, not silently skip subnet placement-coherence")
	require.Equal(t, "subnet lookup unavailable", status.Convert(err).Message())
}

// Create из linked-address с несконфигурированным vpc: ownership/family/kind и
// placement связанного адреса проверить нечем → отказ (иначе тенант привязал бы
// ЧУЖОЙ Address — cross-project VIP-hijack).
func TestCreate_AddressReaderUnconfigured_FailsClosed(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	uc := ucWithNilPeer(repo, opsRepo, createDeps{
		subnet: &fakeSubnetClient{}, reader: nil, zone: &fakeZoneClient{},
	})
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_REGIONAL
	req.V4Source = vipAddress(lbTestAddrInternal)

	_, err := uc.Execute(context.Background(), req)
	require.Equal(t, codes.Unavailable, status.Code(err),
		"unconfigured vpc peer must fail the mutation closed, not silently accept an unvalidated linked address")
	require.Equal(t, "address lookup unavailable", status.Convert(err).Message())
}

// INTERNAL linked-address: адрес резолвится, а его ПОДСЕТЬ — нет (вложенный
// nil-skip внутри resolveLinkedAddress). Тот же fail-closed.
func TestCreate_LinkedAddressSubnetPeerUnconfigured_FailsClosed(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	uc := ucWithNilPeer(repo, opsRepo, createDeps{
		subnet: nil, reader: &fakeAddressReader{}, zone: &fakeZoneClient{},
	})
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_REGIONAL
	req.V4Source = vipAddress(lbTestAddrInternal)

	_, err := uc.Execute(context.Background(), req)
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Equal(t, "subnet lookup unavailable", status.Convert(err).Message())
}

// disabled_announce_zones на Create с несконфигурированным geo: «зона ∈ регион»
// и «не все зоны региона» проверить нечем → отказ вместо приёма произвольного
// набора зон (drain всех зон = недостижимый VIP).
func TestCreate_ZonePeerUnconfigured_FailsClosed(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	uc := ucWithNilPeer(repo, opsRepo, createDeps{
		subnet: &fakeSubnetClient{}, reader: &fakeAddressReader{}, zone: nil,
	})
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_REGIONAL
	req.V4Source = vipSubnet(lbTestSubnetRegional)
	req.DisabledAnnounceZones = []string{"region-1-b"}

	_, err := uc.Execute(context.Background(), req)
	require.Equal(t, codes.Unavailable, status.Code(err),
		"unconfigured geo peer must fail the mutation closed, not silently accept an unvalidated drain set")
	require.Equal(t, "zone lookup unavailable", status.Convert(err).Message())
}

// Пустой drain-набор с несконфигурированным geo — проверять нечего, geo не
// нужен: отказ был бы ложным (guard срабатывает ТОЛЬКО когда проверка реально
// требуется).
func TestCreate_ZonePeerUnconfigured_NoDrainSet_Passes(t *testing.T) {
	repo, opsRepo := newFakeRepo(), newFakeOpsRepo()
	uc := ucWithNilPeer(repo, opsRepo, createDeps{
		subnet: &fakeSubnetClient{}, reader: &fakeAddressReader{}, zone: nil,
	})
	req := baseCreateReq()
	req.Placement = lbv1.NetworkLoadBalancer_INTERNAL_REGIONAL
	req.V4Source = vipSubnet(lbTestSubnetRegional)

	op, err := uc.Execute(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
}

// Тот же инвариант на Update: drain-набор меняется — geo обязателен.
func TestUpdate_ZonePeerUnconfigured_FailsClosed(t *testing.T) {
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	repo.lbs[lbID].Type = domain.LBTypeInternal
	repo.lbs[lbID].RegionID = "region-1"
	repo.lbs[lbID].PlacementType = domain.PlacementRegional
	uc := NewUpdateLoadBalancerUseCase(repo, newFakeOpsRepo(), nil, slog.Default())

	_, err := uc.Execute(context.Background(), &lbv1.UpdateNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
		DisabledAnnounceZones: []string{"region-1-b"},
		UpdateMask:            &fieldmaskpb.FieldMask{Paths: []string{"disabled_announce_zones"}},
	})
	require.Equal(t, codes.Unavailable, status.Code(err))
	require.Equal(t, "zone lookup unavailable", status.Convert(err).Message())
}
