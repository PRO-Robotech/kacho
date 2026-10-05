// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package feedjournal_test

import (
	"testing"

	"google.golang.org/protobuf/types/known/anypb"

	subscriptionv1 "github.com/PRO-Robotech/corelib/api/corelib/subscription"
	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/corelib/subscription"

	"github.com/PRO-Robotech/kacho/pkg/feedjournal"
)

// probeJournal — журнал модуля с одним ресурсным видом и словарём, собранным
// Declare при флаге on.
func probeJournal(on bool) subscription.Journal {
	kinds := map[string]subscription.Kind{
		"Probe": {ObjectType: "probe_object", Action: "probe.objects.list", Scope: subscription.ScopeProject},
	}
	feedjournal.Declare(kinds, on)
	return subscription.Journal{
		Channel: "probe_outbox",
		Storage: subscription.Storage{
			Table: "kacho_probe.probe_outbox", PositionColumn: "sequence_no", KindColumn: "resource_kind",
			IDColumn: "resource_id", ChangeColumn: "event_type", PayloadColumn: "payload",
			ProjectColumn: "project_id", Project: subscription.ProjectInColumn,
			Retention: subscription.RetainsFromEarliestRow, AgeColumn: "created_at",
		},
		Mapping: subscription.Mapping{
			Kinds: kinds,
			Changes: map[string]subscriptionv1.SubscriptionEvent_Change{
				"UPDATED": subscriptionv1.SubscriptionEvent_UPDATED,
			},
			State: func(subscription.Row) (*anypb.Any, subscription.StateAbsence, error) {
				return nil, subscription.StateNotProduced, nil
			},
		},
	}
}

// TestDeclare_FeedKindFollowsTheFlag — NTF3-65 / NTF3-67: вид ленты в словаре
// ровно при включённом флаге; ресурсный вид от флага не зависит.
func TestDeclare_FeedKindFollowsTheFlag(t *testing.T) {
	on, off := probeJournal(true), probeJournal(false)
	if _, ok := off.Mapping.Kinds[feed.JournalKey]; ok {
		t.Fatalf("флаг false: в словаре объявлен ключ %q", feed.JournalKey)
	}
	if len(off.Mapping.Kinds) != 1 || len(on.Mapping.Kinds) != 2 {
		t.Fatalf("видов при false %d, при true %d; ожидалось 1 и 2", len(off.Mapping.Kinds), len(on.Mapping.Kinds))
	}
	got, ok := on.Mapping.Kinds[feed.JournalKey]
	if !ok {
		t.Fatalf("флаг true: ключа %q в словаре нет", feed.JournalKey)
	}
	if got.ObjectType != "notification_feed" {
		t.Fatalf("вид ленты едет на провод словом %q, ожидалось notification_feed", got.ObjectType)
	}
	if on.Mapping.Kinds["Probe"] != off.Mapping.Kinds["Probe"] {
		t.Fatalf("флаг изменил ресурсный вид: %+v против %+v", on.Mapping.Kinds["Probe"], off.Mapping.Kinds["Probe"])
	}
}

// TestDeclare_FoundationAcceptsTheDeclaredKind — объявление судит сам фундамент
// ленты (feed.JournalSignal): вид уровня кластера без имени, журнал собирается.
// Близнец — журнал без вида (флаг false) фундамент ленты отвергает.
func TestDeclare_FoundationAcceptsTheDeclaredKind(t *testing.T) {
	if _, err := feed.JournalSignal(probeJournal(true), "probe", "UPDATED"); err != nil {
		t.Fatalf("фундамент ленты отверг объявленный вид: %v", err)
	}
	if _, err := feed.JournalSignal(probeJournal(false), "probe", "UPDATED"); err == nil {
		t.Fatalf("фундамент ленты принял журнал без вида ленты — близнец не различает флаг")
	}
}
