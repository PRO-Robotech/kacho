// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// АРЕНДА ИЗ ОГРАНИЧЕННОГО ПУЛА ВОЗВРАЩАЕТСЯ НА КАЖДОМ ПУТИ ВЫСВОБОЖДЕНИЯ
// (`data-integrity.md` §Lease-recycle-on-delete).
//
// Признак, ради которого написан файл: подсеть, чей адрес не вернулся, не
// удаляется НИКОГДА — `Subnet.Delete` синхронно отвечает
// `FAILED_PRECONDITION "Subnet has allocated internal addresses"`, и повтор её
// не спасает, потому что освобождать уже некому. Реконсайлер аренд идёт ОТ
// строки балансировщика (`free_ip_runner` выбирает `load_balancers` в
// DELETING/CREATING и берёт `address_id` оттуда); обратной развёртки со стороны
// vpc в системе нет. Значит любой путь, который снимает строку, НЕ освободив
// адрес, теряет аренду безвозвратно.
//
// Ниже — два таких пути. Оба проверяются вместе с законными близнецами: без них
// «адрес не освобождён» было бы неотличимо от «освобождение не работает вовсе».

// TestDelete_UnsetVipOriginStillReleases — ПУТЬ 1: неизвестный дискриминатор.
//
// Колонка допускает пустое значение (`vip_origin_v4 text NOT NULL DEFAULT ”`),
// и ни одно ограничение не связывает непустой `address_id_v4` с непустым
// `vip_origin_v4`. Удержание адреса обязано требовать ЯВНОГО `linked`; всё
// остальное освобождается. Прежняя редакция спрашивала «это auto?» и на пустом
// значении молча уходила в ветку «адрес арендатора» — то есть не освобождала.
func TestDelete_UnsetVipOriginStillReleases(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		origin    domain.VipOrigin
		wantFreed bool
	}{
		// Предмет пробы: пустой дискриминатор.
		{"unset origin frees (lease must never be stranded)", domain.VipOrigin(""), true},
		// Законные близнецы — на них поведение меняться НЕ должно.
		{"auto frees (positive control)", domain.VipOriginAuto, true},
		{"linked keeps the tenant address (negative control)", domain.VipOriginLinked, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := newFakeRepo()
			lbID := seedLB(t, repo, "prj-a", "edge")
			repo.lbs[lbID].AddressIDV4 = "adr-v4"
			repo.lbs[lbID].VipOriginV4 = tc.origin
			opsRepo := newFakeOpsRepo()
			addr := &fakeAddressClient{}

			uc := NewDeleteLoadBalancerUseCase(repo, opsRepo, addr, slog.Default())
			op, err := uc.Execute(context.Background(), &lbv1.DeleteNetworkLoadBalancerRequest{
				NetworkLoadBalancerId: lbID,
			})
			require.NoError(t, err)
			require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)

			// Референс снимается ВСЕГДА — и у адреса арендатора тоже.
			require.Equal(t, []string{"adr-v4"}, addr.cleared,
				"референс снимается на любом origin")
			if tc.wantFreed {
				require.Equal(t, []string{"adr-v4"}, addr.freed,
					"аренда обязана вернуться в пул: после сноса строки её не найдёт никто")
			} else {
				require.Empty(t, addr.freed,
					"адрес арендатора переживает балансировщик")
			}
		})
	}
}

// TestCompensateCreate_KeepsHandleWhenReleaseFailed — ПУТЬ 2: откат создания,
// на котором освобождение НЕ подтвердилось.
//
// Строка балансировщика — единственная координата, по которой реконсайлер
// способен найти аренду. Снос строки при неосвобождённом адресе превращает
// ВРЕМЕННЫЙ отказ соседа в ВЕЧНУЮ утечку. Проба держит именно это: при отказе
// освобождения handle обязан ОСТАТЬСЯ.
func TestCompensateCreate_KeepsHandleWhenReleaseFailed(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	addr := &fakeAddressClient{freeErr: errors.New("vpc unavailable")}

	uc := newCreateUC(repo, newFakeOpsRepo(), createDeps{addr: addr})
	uc.compensateCreate(context.Background(), lbID, map[domain.IPVersion]vipAllocResult{
		domain.IPVersionV4: {addressID: "adr-v4", origin: domain.VipOriginAuto},
	})

	require.Contains(t, repo.lbs, lbID,
		"handle обязан остаться: он единственная координата к неосвобождённой аренде")
	require.Equal(t, []string{"adr-v4"}, addr.cleared,
		"освобождение всё-таки пробовалось — иначе проба зеленела бы на бездействии")
}

// TestCompensateCreate_DropsHandleWhenReleaseSucceeded — положительный контроль
// к пробе выше. Без него «handle остался» было бы неотличимо от «откат вообще
// не сносит строку», и проба зеленела бы на сломанном откате.
func TestCompensateCreate_DropsHandleWhenReleaseSucceeded(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	addr := &fakeAddressClient{}

	uc := newCreateUC(repo, newFakeOpsRepo(), createDeps{addr: addr})
	uc.compensateCreate(context.Background(), lbID, map[domain.IPVersion]vipAllocResult{
		domain.IPVersionV4: {addressID: "adr-v4", origin: domain.VipOriginAuto},
	})

	require.Equal(t, []string{"adr-v4"}, addr.freed)
	require.NotContains(t, repo.lbs, lbID,
		"освобождение подтверждено — handle больше ничего не держит и сносится")
}
