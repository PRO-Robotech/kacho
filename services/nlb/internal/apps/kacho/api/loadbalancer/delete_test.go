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

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"

	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

func TestDelete_HappyPath(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	opsRepo := newFakeOpsRepo()
	uc := NewDeleteLoadBalancerUseCase(repo, opsRepo, &fakeAddressClient{}, slog.Default())
	op, err := uc.Execute(context.Background(), &lbv1.DeleteNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
	})
	require.NoError(t, err)
	final := awaitOpDone(t, opsRepo, op.ID)
	require.Nil(t, final.Error)
	require.NotContains(t, repo.lbs, lbID)
	// outbox emitted DELETED.
	evts := repo.outboxEvents()
	require.Len(t, evts, 1)
	require.Equal(t, "DELETED", evts[0].Action)
}

func TestDelete_DeletionProtection(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	repo.lbs[lbID].DeletionProtection = true
	uc := NewDeleteLoadBalancerUseCase(repo, newFakeOpsRepo(), &fakeAddressClient{}, nil)
	_, err := uc.Execute(context.Background(), &lbv1.DeleteNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Equal(t, "load balancer has deletion protection enabled", status.Convert(err).Message())
}

// Снятие аренды предъявляет ВЛАДЕНИЕ, а решение принимает ВЛАДЕЛЕЦ.
//
// Прежде эта проба стерегла ветку потребителя: `auto` → снять ссылку и удалить
// адрес, `linked` → только снять ссылку. Ветки больше нет — потребитель не
// читает свой дискриминатор, а владелец решает по своей колонке `owned`
// (#439). Здесь стережётся то, что пережило правку: предъявление уходит ровно
// одно на семейство и несёт ту же пару, какой аренда заводилась.
//
// Разведение исходов (удалить адрес модуля / оставить адрес арендатора)
// проверяется там, где оно теперь принимается, — на стороне владельца.
func TestDelete_PresentsOwnershipToTheOwner(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	repo.lbs[lbID].AddressIDV4 = "adr-v4"
	opsRepo := newFakeOpsRepo()
	addr := &fakeAddressClient{}
	uc := NewDeleteLoadBalancerUseCase(repo, opsRepo, addr, slog.Default())
	op, err := uc.Execute(context.Background(), &lbv1.DeleteNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)

	reqs := addr.releaseReqs()
	require.Len(t, reqs, 1, "одно семейство — одно предъявление владения")
	require.Equal(t, "adr-v4", reqs[0].AddressID)
	require.Equal(t, "prj-a", reqs[0].ProjectID)
	require.Equal(t, vpcclient.OwnerKindLoadBalancer, reqs[0].Owner.Kind)
	require.Equal(t, lbID, reqs[0].Owner.ID)
}

func TestDelete_HasListeners(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	// Seed listeners в lists map.
	repo.lists[lbID] = []*kachorepo.ListenerRecord{
		{Listener: domain.Listener{
			ID:             domain.ResourceID(ids.NewID(ids.PrefixListener)),
			LoadBalancerID: domain.ResourceID(lbID),
		}},
	}
	uc := NewDeleteLoadBalancerUseCase(repo, newFakeOpsRepo(), &fakeAddressClient{}, nil)
	_, err := uc.Execute(context.Background(), &lbv1.DeleteNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
	require.Contains(t, status.Convert(err).Message(), "listener")
}

// TestDelete_GuardFailure_DoesNotReleaseVIP — при провале атомарного
// mark-DELETING guard'а (конкурентная re-protection / появившийся ребёнок между
// sync-precheck и async-воркером) необратимый cross-domain release VIP НЕ должен
// выполниться, а строка LB — уцелеть. Иначе живой LB остаётся с уже
// освобождённым VIP, который никакой реконсайлер не лечит (регресс до
// sec-hardening r8b: release шёл ДО guard'ов).
func TestDelete_GuardFailure_DoesNotReleaseVIP(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	repo.lbs[lbID].AddressIDV4 = "adr-v4"
	repo.lbs[lbID].VipOriginV4 = domain.VipOriginAuto
	// Guard отвергает переход в DELETING (симулирует конкурентную защиту/ребёнка,
	// проскочивших мимо sync-precheck).
	repo.failOnMarkDeleting = kachorepo.ErrFailedPrecondition
	opsRepo := newFakeOpsRepo()
	addr := &fakeAddressClient{}
	uc := NewDeleteLoadBalancerUseCase(repo, opsRepo, addr, slog.Default())
	op, err := uc.Execute(context.Background(), &lbv1.DeleteNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
	})
	require.NoError(t, err) // sync precheck passes; guard-miss всплывает в async-воркере
	final := awaitOpDone(t, opsRepo, op.ID)
	require.NotNil(t, final.Error, "guard-miss обязан провалить операцию")
	require.Empty(t, addr.freed, "VIP не должен освобождаться при провале guard'а")
	require.Empty(t, addr.cleared, "reference VIP не должен сниматься при провале guard'а")
	require.Contains(t, repo.lbs, lbID, "строка LB обязана уцелеть при провале guard'а")
}

func TestDelete_NotFound(t *testing.T) {
	t.Parallel()
	uc := NewDeleteLoadBalancerUseCase(newFakeRepo(), newFakeOpsRepo(), &fakeAddressClient{}, nil)
	_, err := uc.Execute(context.Background(), &lbv1.DeleteNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: "nlb-x",
	})
	require.Equal(t, codes.NotFound, status.Code(err))
}
