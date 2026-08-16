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

	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
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

// TestDelete_LocalDiscriminatorNoLongerDecidesRelease — ПУТЬ 1: неизвестный
// дискриминатор.
//
// ЧТО ЗДЕСЬ ПРОВЕРЯЛОСЬ ПРЕЖДЕ И ПОЧЕМУ ПРОБА ПЕРЕПИСАНА. Раньше решение
// «освободить или удержать» принимал ПОТРЕБИТЕЛЬ по своей колонке
// `vip_origin_*`. Колонка допускает пустое значение, ни одно ограничение не
// связывает её с непустым `address_id`, и три места, принимавшие это решение,
// спрашивали по-разному — поэтому проба стерегла НАПРАВЛЕНИЕ СРАВНЕНИЯ: удержание
// обязано требовать явного `linked`.
//
// Предмет этой стражи снят вместе с самим сравнением (#439): решение переехало к
// ВЛАДЕЛЬЦУ, который читает свою колонку `owned`. Проба, оставленная как была,
// утверждала бы о ветке, которой в коде нет.
//
// Свойство, которое пережило правку и стережётся здесь, СИЛЬНЕЕ прежнего:
// аренда не может быть брошена НИ ПРИ КАКОМ значении локального дискриминатора,
// потому что потребитель его больше не читает вовсе. Для каждого семейства с
// непустым идентификатором аренды владельцу предъявляется владение — ровно один
// раз, с той же парой, какой аренда заводилась.
func TestDelete_LocalDiscriminatorNoLongerDecidesRelease(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		origin domain.VipOrigin
	}{
		{"пустой дискриминатор", domain.VipOrigin("")},
		{"auto", domain.VipOriginAuto},
		{"linked", domain.VipOriginLinked},
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

			reqs := addr.releaseReqs()
			require.Len(t, reqs, 1,
				"владельцу предъявляется владение ровно один раз на семейство — при ЛЮБОМ локальном дискриминаторе")
			require.Equal(t, "adr-v4", reqs[0].AddressID)
			require.Equal(t, "prj-a", reqs[0].ProjectID,
				"якорь права — проект: без него глагол не авторизуем")
			require.Equal(t, vpcclient.OwnerKindLoadBalancer, reqs[0].Owner.Kind,
				"предъявляется ТА ЖЕ пара, какой аренда заводилась — иначе сверка владения не совпадёт ни разу")
			require.Equal(t, lbID, reqs[0].Owner.ID)
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
	uc.compensateCreate(context.Background(), "prj-a", lbID, map[domain.IPVersion]vipAllocResult{
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
	uc.compensateCreate(context.Background(), "prj-a", lbID, map[domain.IPVersion]vipAllocResult{
		domain.IPVersionV4: {addressID: "adr-v4", origin: domain.VipOriginAuto},
	})

	require.Equal(t, []string{"adr-v4"}, addr.freed)
	require.NotContains(t, repo.lbs, lbID,
		"освобождение подтверждено — handle больше ничего не держит и сносится")
}
