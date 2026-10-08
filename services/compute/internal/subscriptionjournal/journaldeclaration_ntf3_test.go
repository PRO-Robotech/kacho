// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subscriptionjournal_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/feedjournal/feedjournaltest"
	"github.com/PRO-Robotech/kacho/services/compute/internal/subscriptionjournal"
)

// feedModule — имя модуля compute в ленте: объект строки сигнала
// `notification_feed:compute` и поле `module` объявления журнала.
const feedModule = "compute"

// TestJournalDeclaration_AgreesWithTheJournal — `services/compute/journal.yaml`,
// вход `notifygen init -journal` о журнале модуля (NTF-3 З10), несёт ровно то,
// что объявляет журнал на Go при включённом флаге ленты: таблицу, колонки,
// виды с формой имени и якорем, словарь рода изменения. Функция базы
// `resource-event` переводит строку журнала тем же словарём, что сервер
// потока.
func TestJournalDeclaration_AgreesWithTheJournal(t *testing.T) {
	t.Parallel()
	feedjournaltest.RequireJournalDeclaration(t, "../../journal.yaml", feedModule, subscriptionjournal.Journal(true))
}
