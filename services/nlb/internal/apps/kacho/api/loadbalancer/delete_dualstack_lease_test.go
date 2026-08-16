// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	lbv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/loadbalancer/v1"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Аренда адреса возвращается на КАЖДОМ семействе, а семейство без вернувшейся
// аренды не даёт удалить строку (#467).
//
// ПРЕДМЕТ. Удаление двустекового балансировщика проходило успехом (`done:true`,
// пустой ответ), а аренда v6 оставалась выделенной: подсеть после этого
// отказывалась удаляться — `Subnet has allocated internal addresses`. Обе ветви
// освобождения при этом вернули успех.
//
// МЕХАНИКА. Освобождение принимает идентификатор адреса и на ПУСТОМ возвращает
// успех — «этому семейству освобождать нечего». Отличить это от «аренда есть, а
// её идентификатор потерян» оно не может, потому что смотрит только на
// идентификатор. Дальше шаг 3 удаляет строку, и с этого момента аренду в системе
// не видит НИКТО: реконсайлер выбирает только строки в DELETING/CREATING, а
// обратного поиска «что принадлежит этому балансировщику» на стороне vpc нет.
// Ошибка необратима ровно в тот момент, когда перестаёт быть заметной.
//
// ПОЧЕМУ ЭТО НЕ ЛОВИЛОСЬ. Все пробы удаления — v4-only: ветвь v6 не имела
// покрытия вовсе, а дублёр адресного клиента принимал пустую строку, тогда как
// боевой клиент отвергает её (`address_id is empty`). Дублёр был снисходительнее
// продукта ровно в том месте, ради которого его подставляют.
func TestDelete_DualStack_ReleasesBothFamilies(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	rec := repo.lbs[lbID]
	rec.IPFamilies = []domain.IPVersion{domain.IPVersionV4, domain.IPVersionV6}
	rec.AddressV4, rec.AddressIDV4, rec.VipOriginV4 = "10.0.0.7", "adr-v4", domain.VipOriginAuto
	rec.AddressV6, rec.AddressIDV6, rec.VipOriginV6 = "2a02::7", "adr-v6", domain.VipOriginAuto

	opsRepo := newFakeOpsRepo()
	addr := &fakeAddressClient{}
	uc := NewDeleteLoadBalancerUseCase(repo, opsRepo, addr, slog.Default())
	op, err := uc.Execute(context.Background(), &lbv1.DeleteNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)

	require.ElementsMatch(t, []string{"adr-v4", "adr-v6"}, addr.freed,
		"обе аренды обязаны вернуться в пул — иначе подсеть не удалить никогда")
	require.ElementsMatch(t, []string{"adr-v4", "adr-v6"}, addr.cleared)
	require.NotContains(t, repo.lbs, lbID)
}

// Семейство, у которого ЕСТЬ адрес и НЕТ его идентификатора, — это потерянная
// аренда, а не отсутствующая. Удаление обязано отказать и СОХРАНИТЬ строку.
//
// Строка в DELETING остаётся зацепкой: её видит реконсайлер и её видит человек.
// Удалить её значит превратить поправимое расхождение в вечную утечку адреса, а
// вместе с ним — заблокировать подсеть навсегда.
func TestDelete_FamilyWithAddressButNoLeaseID_RefusesAndKeepsRow(t *testing.T) {
	t.Parallel()
	for _, family := range []string{"v4", "v6"} {
		t.Run(family, func(t *testing.T) {
			repo := newFakeRepo()
			lbID := seedLB(t, repo, "prj-a", "edge")
			rec := repo.lbs[lbID]
			rec.IPFamilies = []domain.IPVersion{domain.IPVersionV4, domain.IPVersionV6}
			// Одно семейство здорово, у второго идентификатор аренды пуст.
			rec.AddressV4, rec.AddressIDV4, rec.VipOriginV4 = "10.0.0.7", "adr-v4", domain.VipOriginAuto
			rec.AddressV6, rec.AddressIDV6, rec.VipOriginV6 = "2a02::7", "adr-v6", domain.VipOriginAuto
			if family == "v4" {
				rec.AddressIDV4 = ""
			} else {
				rec.AddressIDV6 = ""
			}

			opsRepo := newFakeOpsRepo()
			addr := &fakeAddressClient{}
			uc := NewDeleteLoadBalancerUseCase(repo, opsRepo, addr, slog.Default())
			op, err := uc.Execute(context.Background(), &lbv1.DeleteNetworkLoadBalancerRequest{
				NetworkLoadBalancerId: lbID,
			})
			require.NoError(t, err)
			final := awaitOpDone(t, opsRepo, op.ID)
			require.NotNil(t, final.Error,
				"потерянный идентификатор аренды обязан провалить операцию, а не пройти успехом")
			require.Contains(t, repo.lbs, lbID,
				"строка обязана уцелеть: удалив её, аренду уже не найдёт ни реконсайлер, ни человек")
		})
	}
}

// Положительный контроль к предыдущей пробе: семейство, которого у
// балансировщика НЕТ вовсе (ни адреса, ни идентификатора), освобождать нечего —
// удаление обязано пройти. Без этой пробы отказ выше мог бы означать «отказываем
// всегда», и односемейный балансировщик перестал бы удаляться.
func TestDelete_SingleFamily_NoLeaseForAbsentFamily_Succeeds(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	lbID := seedLB(t, repo, "prj-a", "edge")
	rec := repo.lbs[lbID]
	rec.IPFamilies = []domain.IPVersion{domain.IPVersionV4}
	rec.AddressV4, rec.AddressIDV4, rec.VipOriginV4 = "10.0.0.7", "adr-v4", domain.VipOriginAuto
	// v6 пусто целиком — семейства нет.

	opsRepo := newFakeOpsRepo()
	addr := &fakeAddressClient{}
	uc := NewDeleteLoadBalancerUseCase(repo, opsRepo, addr, slog.Default())
	op, err := uc.Execute(context.Background(), &lbv1.DeleteNetworkLoadBalancerRequest{
		NetworkLoadBalancerId: lbID,
	})
	require.NoError(t, err)
	require.Nil(t, awaitOpDone(t, opsRepo, op.ID).Error)
	require.Equal(t, []string{"adr-v4"}, addr.freed)
	require.NotContains(t, repo.lbs, lbID)
}
