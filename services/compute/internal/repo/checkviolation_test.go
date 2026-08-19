// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repo

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// TestWrapPgErr_NameFormBackstopIsInternal — форму имени compute проверяет сам
// (`corevalidate.Name` / `NameOrDefault` на всех четырёх ресурсах: машина, тип
// машины, группа размещения, гостевой ключ), поэтому ограничение таблицы —
// защита последнего рубежа, и его срабатывание есть дефект сервиса, а не ввода.
func TestWrapPgErr_NameFormBackstopIsInternal(t *testing.T) {
	for _, table := range []string{"instances", "machine_types", "placement_groups", "guest_access_keys"} {
		t.Run(table, func(t *testing.T) {
			got := wrapPgErr(&pgconn.PgError{
				Code:           "23514",
				TableName:      table,
				ConstraintName: table + "_name_check",
			}, "Instance", "ins-1")
			if !errors.Is(got, ports.ErrInternal) {
				t.Fatalf("23514 на ограничении формы имени = %v; ожидался ErrInternal", got)
			}
			if errors.Is(got, ports.ErrInvalidArg) {
				t.Fatalf("23514 на ограничении формы имени = %v; ErrInvalidArg обвиняет вызывающего", got)
			}
		})
	}
}

// TestWrapPgErr_OtherCheckStaysInvalidArg — положительный контроль: ограничение,
// формой имени НЕ являющееся, остаётся отказом по вводу.
func TestWrapPgErr_OtherCheckStaysInvalidArg(t *testing.T) {
	got := wrapPgErr(&pgconn.PgError{
		Code:           "23514",
		TableName:      "instances",
		ConstraintName: "instances_cpu_guarantee_percent_check",
	}, "Instance", "ins-1")

	if !errors.Is(got, ports.ErrInvalidArg) {
		t.Fatalf("23514 на прочем ограничении = %v; ожидался ErrInvalidArg", got)
	}
	if strings.Contains(got.Error(), "instances_cpu_guarantee_percent_check") {
		t.Errorf("имя ограничения базы утекло вызывающему: %q", got.Error())
	}
}
