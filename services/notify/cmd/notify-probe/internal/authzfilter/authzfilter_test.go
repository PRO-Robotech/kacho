// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package authzfilter_test

import (
	"testing"

	"github.com/PRO-Robotech/corelib/notify/feed"

	"github.com/PRO-Robotech/kacho/services/notify/cmd/notify-probe/internal/authzfilter"
)

// TestFeedTypeIsTheFoundationWord — тип объекта ленты в словаре пробы равен
// слову фундамента: тот же объект спрашивает Claim и Ack (привязка сервера) и
// сужатель потока. Разойдутся — поток спросит о типе, которого нет у сервера.
func TestFeedTypeIsTheFoundationWord(t *testing.T) {
	if authzfilter.ResourceTypeFeed != string(feed.FeedObjectType) {
		t.Fatalf("тип ленты пробы %q, у фундамента %q", authzfilter.ResourceTypeFeed, feed.FeedObjectType)
	}
	rels, ok := authzfilter.PageRelations[authzfilter.ResourceTypeFeed]
	if !ok || len(rels) != 1 || rels[0] != authzfilter.RelationReader {
		t.Fatalf("видимость ленты: %v, ожидалось ровно [%s]", rels, authzfilter.RelationReader)
	}
	if len(authzfilter.PageRelations) != 1 {
		t.Fatalf("словарь видимости несёт %d типов, у журнала пробы тип один", len(authzfilter.PageRelations))
	}
}
