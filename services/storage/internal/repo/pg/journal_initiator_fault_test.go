// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
)

// journalInitiatorFaults — оба отказа журнала модуля по инициатору с теми
// координатами, какие отдаёт база (их утверждает интеграционная проба миграции
// журнала), и законный близнец каждого: тот же код на соседнем предмете.
func journalInitiatorFaults() (refused, twins map[string]error) {
	wrap := func(e *pgconn.PgError) error { return fmt.Errorf("writer tx: %w", e) }
	refused = map[string]error{
		"23502 initiator":      wrap(&pgconn.PgError{Code: "23502", TableName: "storage_outbox", ColumnName: "initiator"}),
		"23514 initiator form": wrap(&pgconn.PgError{Code: "23514", TableName: "storage_outbox", ConstraintName: "storage_outbox_initiator_form"}),
	}
	twins = map[string]error{
		"23514 neighbour check": wrap(&pgconn.PgError{Code: "23514", TableName: "storage_outbox", ConstraintName: "storage_outbox_kind_check"}),
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
		"mapVolumeErr":          func(err error) error { return mapVolumeErr(err, volErrCtx{volumeID: "v1"}) },
		"mapSnapshotErr":        func(err error) error { return mapSnapshotErr(err, snapErrCtx{snapshotID: "s1"}) },
		"mapImageErr":           func(err error) error { return mapImageErr(err, imgErrCtx{imageID: "i1"}) },
		"mapDiskTypeErr":        func(err error) error { return mapDiskTypeErr(err, dtErrCtx{diskTypeID: "d1"}) },
		"mapDiskTypeBindingErr": func(err error) error { return mapDiskTypeBindingErr(err, dtbErrCtx{bindingID: "b1"}) },
		"mapStorageBackendErr":  func(err error) error { return mapStorageBackendErr(err, sbErrCtx{backendID: "sb1"}) },
	} {
		for name, err := range refused {
			got := fn(err)
			if !errors.Is(got, storageerr.ErrInternal) || errors.Is(got, storageerr.ErrInvalidArg) {
				t.Errorf("%s(%s) = %v — ждали внутренний отказ, а не отказ по вводу", mapper, name, got)
			}
		}
		for name, err := range twins {
			if got := fn(err); !errors.Is(got, storageerr.ErrInvalidArg) {
				t.Errorf("%s(%s) близнец = %v — обязан остаться отказом по вводу", mapper, name, got)
			}
		}
	}
}
