// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PRO-Robotech/corelib/pgtest"
	"github.com/PRO-Robotech/kacho/pkg/feedjournal/feedjournaltest"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/subscriptionjournal"
)

// TestResourceEvent_JournalRowPutsOneFeedRow — функция базы `resource-event`
// (NTF-3 З10), выпущенная `notifygen init -journal` в цепочку миграций vpc,
// ставит на строку журнала одну строку ленты той же транзакцией; при флаге
// `false` и при откате — ни одной (NTF3-65, NTF3-68, NTF3-69 по пути функции
// базы).
func TestResourceEvent_JournalRowPutsOneFeedRow(t *testing.T) {
	dsn := pgtest.NewDB(t)
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("пул не собрался: %v", err)
	}
	pgtest.ClosePoolAtEnd(t, pool)
	feedjournaltest.RequireResourceEventFeedRow(t, feedjournaltest.ResourceEventStand{
		Pool:      pool,
		Journal:   subscriptionjournal.Journal(true),
		FeedTable: "kacho_vpc.vpc_notification_outbox",
		Module:    feedModule,
		Kind:      subscriptionjournal.KindNetwork,
	})
}
