// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"testing"

	"github.com/PRO-Robotech/corelib/subscription"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	"github.com/PRO-Robotech/kacho/services/registry/internal/subscriptionjournal"
)

// TestRegistryJournal_NTF360_RepositoryIsPublishedWithoutADNSName — NTF3-60
// (половина registry), NTF3-61 (объявление), З2 (CX3B-30): репозиторий стоит в
// закрытом словаре типом `registry_repository`, якорь — проект, формы имени
// DNS-метки у него нет (`NameFormNone`: грамматика имени OCI допускает `/`);
// реестр — `NameFormDNS`, проектный.
func TestRegistryJournal_NTF360_RepositoryIsPublishedWithoutADNSName(t *testing.T) {
	kinds := subscriptionjournal.Journal(probeEndpointBase, false).Mapping.Kinds
	if len(kinds) == 0 {
		t.Fatal("словарь видов registry пуст — судить нечего")
	}
	want := map[string]struct {
		objectType string
		nameForm   subscription.NameForm
	}{
		subscriptionjournal.JournalWordRegistry: {domain.FGAObjectTypeRegistry, subscription.NameFormDNS},
		"Repository":                            {domain.FGAObjectTypeRepository, subscription.NameFormNone},
	}
	for word, w := range want {
		k, ok := kinds[word]
		if !ok {
			t.Errorf("вид %s (тип модели %s) не опубликован: его нет в Mapping.Kinds registry", word, w.objectType)
			continue
		}
		if k.ObjectType != w.objectType {
			t.Errorf("вид %s едет типом %q, ожидался %q", word, k.ObjectType, w.objectType)
		}
		if k.Action == "" {
			t.Errorf("вид %s без действия", word)
		}
		if k.NameForm != w.nameForm {
			t.Errorf("вид %s: NameForm = %d, ожидалась %d", word, k.NameForm, w.nameForm)
		}
		if k.Scope != subscription.ScopeProject {
			t.Errorf("вид %s: Scope = %d, ожидался ScopeProject (%d)", word, k.Scope, subscription.ScopeProject)
		}
	}
	if len(kinds) != len(want) {
		t.Errorf("объявлено видов %d, ожидалось %d", len(kinds), len(want))
	}
	t.Logf("объявлено видов %d; словарь клиента %v", len(kinds), subscriptionjournal.Journal(probeEndpointBase, false).KindDictionary())
}
