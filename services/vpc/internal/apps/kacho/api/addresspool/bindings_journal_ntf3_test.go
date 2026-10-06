// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package addresspool

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
)

// bindingFixture — пул и сеть, заведённые до глагола привязки; журнал до
// глагола запомнен, проба судит только то, что глагол добавил.
func bindingFixture(t *testing.T) (f *useCasesFixture, poolID, netID string, before int) {
	t.Helper()
	f = newUseCases(t)
	pool, err := f.create.Execute(context.Background(), CreatePoolReq{
		Name: "apl-default", Kind: domain.AddressPoolKindExternalPublic, ZoneID: "zone-c",
		V4CIDRBlocks: []string{"198.51.100.0/24"},
	})
	require.NoError(t, err, "фикстура: пул не создан")
	netID = ids.NewID(ids.PrefixNetwork)
	_, err = f.netRepo.NetworkRepo.Insert(context.Background(), &domain.Network{
		ID: netID, ProjectID: "prj-ntf3", Name: domain.RcNameVPC("net-bind"),
	})
	require.NoError(t, err, "фикстура: сеть не создана")
	return f, pool.ID, netID, len(f.kr.inner.Outbox())
}

// assertPoolUpdatedOnly — глагол добавил в журнал ровно одну строку:
// `AddressPool` `UPDATED` с id пула, без якоря, и ни одной строки
// `AddressPoolNetworkDefault`. Форму нагрузки проба не судит: она — состояние
// пула той же формы, что у прочих строк `AddressPool`, и держит её проба
// ветви состояния журнала vpc.
func assertPoolUpdatedOnly(t *testing.T, added []kachomock.OutboxEvent, poolID string) {
	t.Helper()
	for _, ev := range added {
		if ev.Resource == "AddressPoolNetworkDefault" {
			t.Errorf("записана строка снятого вида AddressPoolNetworkDefault (id %s, род %s)", ev.ID, ev.Action)
		}
	}
	if len(added) != 1 {
		t.Fatalf("глагол добавил строк журнала %d, ожидалась 1 (AddressPool UPDATED %s): %+v", len(added), poolID, added)
	}
	ev := added[0]
	if ev.Resource != "AddressPool" || ev.Action != "UPDATED" || ev.ID != poolID {
		t.Errorf("строка журнала %s %s %s, ожидалась AddressPool UPDATED %s", ev.Resource, ev.Action, ev.ID, poolID)
	}
	if ev.ProjectID != "" {
		t.Errorf("у пула уровня кластера якорь %q", ev.ProjectID)
	}
}

// TestAddressPool_NTF362_BindAsNetworkDefaultPublishesPoolUpdated — NTF3-62
// (часть AddressPool), Р2: назначение пула по умолчанию сети даёт событие
// `AddressPool` `UPDATED` этого пула и ни одной строки `AddressPoolNetworkDefault`.
func TestAddressPool_NTF362_BindAsNetworkDefaultPublishesPoolUpdated(t *testing.T) {
	f, poolID, netID, before := bindingFixture(t)

	require.NoError(t, f.bindNet.Execute(context.Background(), netID, poolID))

	assertPoolUpdatedOnly(t, f.kr.inner.Outbox()[before:], poolID)
}

// TestAddressPool_NTF362_UnbindNetworkDefaultPublishesPoolUpdated — NTF3-62,
// Р2: снятие пула по умолчанию сети — то же событие `AddressPool` `UPDATED`
// пула, который был назначен.
func TestAddressPool_NTF362_UnbindNetworkDefaultPublishesPoolUpdated(t *testing.T) {
	f, poolID, netID, _ := bindingFixture(t)
	require.NoError(t, f.bindNet.Execute(context.Background(), netID, poolID), "фикстура: привязка не прошла")
	before := len(f.kr.inner.Outbox())

	require.NoError(t, f.unbindNet.Execute(context.Background(), netID))

	assertPoolUpdatedOnly(t, f.kr.inner.Outbox()[before:], poolID)
}

// TestAddressPool_NTF362_FailedBindWritesNoJournalRow — отрицательный близнец:
// привязка к несуществующей сети отвергнута, и журнал не получил ни одной
// строки. Единственный изменённый факт против положительной пробы — сети нет.
func TestAddressPool_NTF362_FailedBindWritesNoJournalRow(t *testing.T) {
	f, poolID, _, before := bindingFixture(t)

	err := f.bindNet.Execute(context.Background(), ids.NewID(ids.PrefixNetwork), poolID)
	require.Error(t, err, "привязка к несуществующей сети прошла")

	if added := f.kr.inner.Outbox()[before:]; len(added) != 0 {
		t.Fatalf("отвергнутая привязка оставила строк журнала %d: %+v", len(added), added)
	}
}
