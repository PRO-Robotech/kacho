// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// journalInitiatorFaults — оба отказа журнала модуля по инициатору с теми
// координатами, какие отдаёт база (их утверждает интеграционная проба миграции
// журнала), и законный близнец каждого: тот же код на соседнем предмете.
func journalInitiatorFaults() (refused, twins map[string]error) {
	wrap := func(e *pgconn.PgError) error { return fmt.Errorf("writer tx: %w", e) }
	refused = map[string]error{
		"23502 initiator":      wrap(&pgconn.PgError{Code: "23502", TableName: "nlb_outbox", ColumnName: "initiator"}),
		"23514 initiator form": wrap(&pgconn.PgError{Code: "23514", TableName: "nlb_outbox", ConstraintName: "nlb_outbox_initiator_form"}),
	}
	twins = map[string]error{
		"23514 neighbour check": wrap(&pgconn.PgError{Code: "23514", TableName: "nlb_outbox", ConstraintName: "nlb_outbox_kind_check"}),
	}
	return refused, twins
}

// TestJournalInitiatorRefusalMapsToInternal — отказ журнала по инициатору есть
// дефект записи сервиса (значение производит помощник транзакции, вызывающему
// исправлять нечего): каждый маппер, решающий класс 23514, отвечает внутренним
// отказом, а не отказом по вводу; близнец остаётся отказом по вводу.
func TestJournalInitiatorRefusalMapsToInternal(t *testing.T) {
	refused, twins := journalInitiatorFaults()
	for mapper, fn := range map[string]func(error) error{
		"mapPgErr":        func(err error) error { return mapPgErr(err, "TargetGroup", "tg1") },
		"mapAttachVIPErr": mapAttachVIPErr,
	} {
		for name, err := range refused {
			got := fn(err)
			if !errors.Is(got, kacho.ErrInternal) || errors.Is(got, kacho.ErrInvalidArg) {
				t.Errorf("%s(%s) = %v — ждали внутренний отказ, а не отказ по вводу", mapper, name, got)
			}
		}
		for name, err := range twins {
			if got := fn(err); !errors.Is(got, kacho.ErrInvalidArg) {
				t.Errorf("%s(%s) близнец = %v — обязан остаться отказом по вводу", mapper, name, got)
			}
		}
	}
}
