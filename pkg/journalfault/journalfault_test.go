// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package journalfault_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"

	"github.com/PRO-Robotech/corelib/db/pgfault"

	"github.com/PRO-Robotech/kacho/pkg/journalfault"
)

// faultOf — отказ сервера с координатами, какие база отдаёт на журнале модуля
// (их же утверждают интеграционные пробы миграции журнала каждого модуля).
func faultOf(code, table, column, constraint string) pgfault.Fault {
	return pgfault.Classify(fmt.Errorf("repo: %w", &pgconn.PgError{
		Code: code, TableName: table, ColumnName: column, ConstraintName: constraint,
		Message: "server words", Detail: "Failing row contains (secret)",
	}))
}

// TestInitiatorRefusalIsRecognisedAndItsTwinsAreNot — оба отказа журнала по
// инициатору опознаются на каждой из пяти таблиц журнала; каждый близнец
// меняет ровно один факт и опознан не будет.
func TestInitiatorRefusalIsRecognisedAndItsTwinsAreNot(t *testing.T) {
	for _, table := range []string{"vpc_outbox", "compute_outbox", "nlb_outbox", "storage_outbox", "registry_resource_journal"} {
		t.Run(table, func(t *testing.T) {
			assert.True(t, journalfault.Initiator(faultOf("23502", table, "initiator", "")),
				"23502 по колонке initiator — отказ журнала по инициатору")
			assert.True(t, journalfault.Initiator(faultOf("23514", table, "", table+"_initiator_form")),
				"23514 от ограничения формы инициатора — отказ журнала по инициатору")

			assert.False(t, journalfault.Initiator(faultOf("23502", table, "created_at", "")),
				"близнец: 23502 по колонке времени — не инициатор")
			assert.False(t, journalfault.Initiator(faultOf("23514", table, "", table+"_kind_check")),
				"близнец: 23514 от соседнего ограничения — не инициатор")
			assert.False(t, journalfault.Initiator(faultOf("23514", "subnets", "", table+"_initiator_form")),
				"близнец: имя ограничения чужой таблицы — не инициатор")
			assert.False(t, journalfault.Initiator(faultOf("23505", table, "initiator", table+"_initiator_form")),
				"близнец: другой класс отказа — не инициатор")
		})
	}
	assert.False(t, journalfault.Initiator(pgfault.Classify(nil)), "нет отказа — не инициатор")
	assert.False(t, journalfault.Initiator(faultOf("23514", "", "", "_initiator_form")),
		"таблица не названа сервером — суждения нет")
}

// TestReportWritesTheJournalCoordinatesAndNotTheRowValues — запись оператору
// называет таблицу, ограничение и код, но не значения строки (DETAIL несёт их).
func TestReportWritesTheJournalCoordinatesAndNotTheRowValues(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	assert.True(t, journalfault.Report(faultOf("23514", "vpc_outbox", "", "vpc_outbox_initiator_form"), "kind", "Subnet"))
	out := buf.String()
	assert.Contains(t, out, "level=ERROR")
	assert.Contains(t, out, "vpc_outbox_initiator_form")
	assert.Contains(t, out, "sqlstate=23514")
	assert.Contains(t, out, "kind=Subnet")
	assert.NotContains(t, out, "secret", "DETAIL несёт значения строки и в журнал не идёт")

	buf.Reset()
	assert.False(t, journalfault.Report(faultOf("23514", "vpc_outbox", "", "vpc_outbox_kind_check"), "kind", "Subnet"))
	assert.Empty(t, buf.String(), "близнец не пишет ничего: запись принадлежит полосе вызывающего")
}
