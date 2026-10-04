// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package journal_test

import (
	"testing"

	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/corelib/subscription"

	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/journal"
)

// TestJournalCarriesOnlyTheFeedKey — у пробы других видов журнала нет: словарь
// видов — ровно ключ ленты, и он уровня кластера без имени. Поэтому при
// выключенной доставке журнал не собирается вовсе (NTF1-N07 (б)), а не
// собирается пустым.
func TestJournalCarriesOnlyTheFeedKey(t *testing.T) {
	j := journal.Journal()
	if err := j.Validate(); err != nil {
		t.Fatalf("объявление журнала отвергнуто фундаментом: %v", err)
	}
	if len(j.Mapping.Kinds) != 1 {
		t.Fatalf("видов журнала %d, ожидался один — ключ ленты %q", len(j.Mapping.Kinds), feed.JournalKey)
	}
	k, ok := j.Mapping.Kinds[feed.JournalKey]
	if !ok {
		t.Fatalf("ключа ленты %q в словаре нет: %v", feed.JournalKey, j.Mapping.Kinds)
	}
	if k.ObjectType != string(feed.FeedObjectType) {
		t.Fatalf("тип объекта вида %q, ожидался %q", k.ObjectType, feed.FeedObjectType)
	}
	if k.Scope != subscription.ScopeCluster || k.NameForm != subscription.NameFormNone {
		t.Fatalf("ключ ленты обязан быть уровня кластера без имени: %+v", k)
	}
	if j.Storage.Project != subscription.ProjectAbsent {
		t.Fatalf("у журнала ленты нет проектного измерения, объявлено %v", j.Storage.Project)
	}
}

// TestFeedSignalAcceptsTheJournal — писатель сигнала ленты строится над этим
// журналом: объявление судится при сборке корня, а не первой постановкой.
func TestFeedSignalAcceptsTheJournal(t *testing.T) {
	if _, err := feed.JournalSignal(journal.Journal(), journal.Module, journal.ChangeUpdated); err != nil {
		t.Fatalf("сигнал ленты над журналом пробы не собирается: %v", err)
	}
}
