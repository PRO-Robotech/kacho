// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dberr_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	geoerrors "github.com/PRO-Robotech/kacho/services/geo/internal/errors"
	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/dberr"
)

// TestWrap_NameFormBackstop_IsInternal — форму имени Region/Zone geo теперь
// судит сам, на пути запроса (`validate.Name` в use-case, задача #718). Значит
// ограничение таблицы — защита последнего рубежа, и его срабатывание означает,
// что негодное значение прошло МИМО проверки: дефект сервиса, а не ввода.
func TestWrap_NameFormBackstop_IsInternal(t *testing.T) {
	for _, table := range []string{"regions", "zones"} {
		t.Run(table, func(t *testing.T) {
			got := dberr.Wrap(&pgconn.PgError{
				Code:           "23514",
				TableName:      table,
				ConstraintName: table + "_name_check",
			}, "Region", "ru-central1")
			if !errors.Is(got, geoerrors.ErrInternal) {
				t.Fatalf("23514 на ограничении формы имени = %v; ожидался ErrInternal", got)
			}
			if errors.Is(got, geoerrors.ErrInvalidArg) {
				t.Fatalf("23514 на ограничении формы имени = %v; ErrInvalidArg обвиняет вызывающего", got)
			}
		})
	}
}

// TestWrap_OtherCheck_StaysInvalidArg — положительный контроль: ограничение,
// формой имени НЕ являющееся, остаётся отказом по вводу.
func TestWrap_OtherCheck_StaysInvalidArg(t *testing.T) {
	got := dberr.Wrap(&pgconn.PgError{
		Code:           "23514",
		TableName:      "regions",
		ConstraintName: "regions_status_check",
	}, "Region", "ru-central1")

	if !errors.Is(got, geoerrors.ErrInvalidArg) {
		t.Fatalf("23514 на прочем ограничении = %v; ожидался ErrInvalidArg", got)
	}
	if strings.Contains(got.Error(), "regions_status_check") {
		t.Errorf("имя ограничения базы утекло вызывающему: %q", got.Error())
	}
}
