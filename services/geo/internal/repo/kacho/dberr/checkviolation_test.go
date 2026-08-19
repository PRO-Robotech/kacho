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

// Здесь стояла проба «23514 на ограничении формы имени = наш дефект, не ввод».
// Её предмет снят вместе с полем (#716): формы имени у каталога размещения нет,
// `<t>_name_check` снят миграцией 716001, и полосы «наш дефект против ввода» в
// отображении больше не существует. Осталось единственное утверждение — ниже.

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
