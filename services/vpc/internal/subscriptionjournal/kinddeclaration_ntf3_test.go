// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"sort"
	"testing"

	"github.com/PRO-Robotech/corelib/subscription"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/subscriptionjournal"
)

// addressPoolObjectType — тип модели прав пула адресов (приёмка NTF-3, раздел
// 1.2: `vpc_address_pool`, объявлен в канонической модели службы доступа).
// Написано литералом приёмки намеренно: проба сверяет производителя
// (`authzfilter.ResourceTypeAddressPool`) с контрактом, а не с самим собой.
const addressPoolObjectType = "vpc_address_pool"

// TestVpcJournal_NTF360_AddressPoolIsPublishedAsAClusterKind — NTF3-60 (половина
// vpc), NTF3-61 (объявление), З2: пул адресов стоит в закрытом словаре видов
// типом `vpc_address_pool`, якорь — уровня кластера, имя — DNS-метка; каждый
// прочий вид vpc объявляет NameFormDNS и ScopeProject.
func TestVpcJournal_NTF360_AddressPoolIsPublishedAsAClusterKind(t *testing.T) {
	kinds := subscriptionjournal.Journal().Mapping.Kinds
	if len(kinds) == 0 {
		t.Fatal("словарь видов vpc пуст — судить нечего")
	}
	pool, ok := kinds["AddressPool"]
	if !ok {
		t.Errorf("вид AddressPool (тип модели %s) не опубликован: его нет в Mapping.Kinds vpc", addressPoolObjectType)
	} else {
		if pool.ObjectType != addressPoolObjectType {
			t.Errorf("AddressPool едет типом %q, ожидался %q", pool.ObjectType, addressPoolObjectType)
		}
		if pool.Action == "" {
			t.Error("AddressPool без действия — унаследовал бы чужой глагол")
		}
	}
	if _, ok := kinds["AddressPoolNetworkDefault"]; ok {
		t.Error("AddressPoolNetworkDefault объявлен видом: у него нет типа модели, его строки сняты (Р2)")
	}

	for word, k := range kinds {
		wantScope := subscription.ScopeProject
		if word == "AddressPool" {
			wantScope = subscription.ScopeCluster
		}
		if k.Scope != wantScope {
			t.Errorf("вид %s: Scope = %d, ожидался %d", word, k.Scope, wantScope)
		}
		if k.NameForm != subscription.NameFormDNS {
			t.Errorf("вид %s: NameForm = %d, ожидалась NameFormDNS (%d) — снятие обязано нести снимок имени",
				word, k.NameForm, subscription.NameFormDNS)
		}
	}

	dict := subscriptionjournal.Journal().KindDictionary()
	found := false
	for _, d := range dict {
		found = found || d == addressPoolObjectType
	}
	if !found {
		t.Errorf("клиент не может назвать пул: типа %q нет в словаре видов %v", addressPoolObjectType, dict)
	}
	words := make([]string, 0, len(kinds))
	for w := range kinds {
		words = append(words, w)
	}
	sort.Strings(words)
	t.Logf("объявлено видов %d: %v", len(kinds), words)
}
