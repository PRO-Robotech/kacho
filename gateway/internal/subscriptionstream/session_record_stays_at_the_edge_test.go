// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionstream_test

// session_record_stays_at_the_edge_test.go — номер записи сессии владельцу
// журнала не уезжает (kacho#3136).
//
// Номер записи текущей сессии читает только служба доступа; владелец журнала —
// прочее звено, и номер ему не нужен. Проекция дозванивается до владельца
// сама, мимо моста, поэтому решение моста по адресату её не покрывает:
// утверждается то, что увидел владелец.
//
// Близнец — личность того же запроса доехала: «номера нет» не означает
// «метаданных нет вовсе».

import (
	"testing"

	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

func TestSessionRecordDoesNotReachTheJournalOwner(t *testing.T) {
	const record = "hss-000000000000003136"
	owner := &ownerStub{script: []*subscriptionv1.SubscriptionMessage{openedMessage("p", false)}}
	serve(t, newHandler(t, owner), request("owner=probe",
		principalmeta.HeaderTokenSessionID, record,
		principalmeta.HeaderGRPCMetaTokenSessionID, record))

	md := owner.receivedMD(t)
	if got := md.Get(principalmeta.MetaPrincipalID); len(got) != 1 || got[0] != "usr-probe" {
		t.Fatalf("контроль: владелец получил личность %v, ожидалась [usr-probe] — канал метаданных не провязан", got)
	}
	if got := md.Get(principalmeta.MetaTokenSessionID); len(got) != 0 {
		t.Errorf("владелец журнала получил %s = %q — номер записи сессии уехал прочему звену",
			principalmeta.MetaTokenSessionID, got)
	}
}
