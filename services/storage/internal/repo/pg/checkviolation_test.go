// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
)

// TestCheckViolation_NameFormBackstopIsInternal — форму имени storage проверяет
// сам (`validate.Name` / `NameOrDefault` на всех трёх ресурсах: том, снимок,
// образ), поэтому ограничение таблицы — защита последнего рубежа, а его
// срабатывание есть дефект сервиса, а не ввода (задача #718).
//
// Проба идёт по ВСЕМ ТРЁМ отображениям сразу: полоса, починенная у одного
// ресурса и забытая у двух других, — ровно тот класс, ради которого перепись и
// делалась.
func TestCheckViolation_NameFormBackstopIsInternal(t *testing.T) {
	cases := []struct {
		name  string
		table string
		mapFn func(error) error
	}{
		{"volume", "volumes", func(e error) error { return mapVolumeErr(e, volErrCtx{volumeID: "vol-1"}) }},
		{"snapshot", "snapshots", func(e error) error { return mapSnapshotErr(e, snapErrCtx{snapshotID: "snp-1"}) }},
		{"image", "images", func(e error) error { return mapImageErr(e, imgErrCtx{imageID: "img-1"}) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.mapFn(&pgconn.PgError{
				Code:           "23514",
				TableName:      c.table,
				ConstraintName: c.table + "_name_check",
				Message:        `new row for relation "` + c.table + `" violates check constraint`,
			})
			if !errors.Is(got, storageerr.ErrInternal) {
				t.Fatalf("23514 на ограничении формы имени = %v; ожидался ErrInternal", got)
			}
			if errors.Is(got, storageerr.ErrInvalidArg) {
				t.Fatalf("23514 на ограничении формы имени = %v; ErrInvalidArg обвиняет вызывающего", got)
			}
		})
	}
}

// TestCheckViolation_OtherCheckStaysInvalidArg — положительный контроль:
// ограничение, формой имени НЕ являющееся, остаётся отказом по вводу. Без него
// проба выше зеленела бы и на «схлопнули весь 23514 в Internal».
func TestCheckViolation_OtherCheckStaysInvalidArg(t *testing.T) {
	cases := []struct {
		name       string
		table      string
		constraint string
		mapFn      func(error) error
	}{
		{"volume", "volumes", "volumes_size_bytes_check", func(e error) error { return mapVolumeErr(e, volErrCtx{volumeID: "vol-1"}) }},
		{"snapshot", "snapshots", "snapshots_description_check", func(e error) error { return mapSnapshotErr(e, snapErrCtx{snapshotID: "snp-1"}) }},
		{"image", "images", "images_format_check", func(e error) error { return mapImageErr(e, imgErrCtx{imageID: "img-1"}) }},
		{"diskType", "disk_types", "disk_types_tier_known", func(e error) error { return mapDiskTypeErr(e, dtErrCtx{diskTypeID: "dt-1"}) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.mapFn(&pgconn.PgError{
				Code: "23514", TableName: c.table, ConstraintName: c.constraint,
			})
			if !errors.Is(got, storageerr.ErrInvalidArg) {
				t.Fatalf("23514 на прочем ограничении = %v; ожидался ErrInvalidArg", got)
			}
			if strings.Contains(got.Error(), c.constraint) {
				t.Errorf("имя ограничения базы утекло вызывающему: %q", got.Error())
			}
		})
	}
}
