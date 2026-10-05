// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check_test

// feed_signal_visibility_test.go — SA-3042-01 (kacho#2918, NTF-3): сужатель,
// которым сервер потока подписки реестра судит видимость строк журнала
// (`IAMCheckClient.Narrower`, buildSubscriptionServer), спрашивает о строке
// сигнала ленты (`notification_feed:registry`) отношение, объявленное моделью у
// типа ленты, — `reader`. Умолчание карты реестра (`v_list`) у типа ленты не
// объявлено: служба доступа отвечает «неизвестно» на всю партию, и поток
// подписки обрывается у всякого подписчика реестра, не сузившего виды.

import (
	"slices"
	"testing"

	"github.com/PRO-Robotech/corelib/listnarrow"
	"github.com/PRO-Robotech/corelib/listnarrow/narrowtest"
	"github.com/PRO-Robotech/corelib/notify/feed"
	"github.com/PRO-Robotech/kacho/pkg/feedjournal"
	"github.com/PRO-Robotech/kacho/pkg/feedjournal/feedjournaltest"

	"github.com/PRO-Robotech/kacho/services/registry/internal/check"
)

func TestFeedSignalRow_NotifyReaderSeesItOthersDoNotAndTheBatchHolds(t *testing.T) {
	t.Parallel()
	n := check.NewIAMCheckClient(feedjournaltest.Kaname()).Narrower()
	row := []string{"registry"}

	got, err := listnarrow.IDs(feedjournaltest.NotifyCaller(t), n, string(feed.FeedObjectType), feedjournal.ActionSubscribe, row)
	if err != nil {
		t.Fatalf("SA-3042-01: registry: вопрос о строке сигнала ленты от %s отвергнут: %v", feedjournaltest.NotifySubject, err)
	}
	if !slices.Equal(got, row) {
		t.Fatalf("SA-3042-01: registry: %s с reader на ленте не видит строку сигнала: видимы %v", feedjournaltest.NotifySubject, got)
	}

	// Близнец по одному факту — вызывающий: подписчик без reader строки не
	// видит, и вопрос о ней не рвёт партию (поток подписчика остаётся открыт).
	got, err = listnarrow.IDs(narrowtest.Caller(), n, string(feed.FeedObjectType), feedjournal.ActionSubscribe, row)
	if err != nil {
		t.Fatalf("SA-3042-01: registry: вопрос о строке сигнала ленты от подписчика без reader оборвал партию — поток оборвался бы: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("SA-3042-01: registry: подписчик без reader видит строку сигнала ленты: %v", got)
	}
}
