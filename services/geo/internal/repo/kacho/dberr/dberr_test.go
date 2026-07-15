// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package dberr_test

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/PRO-Robotech/kacho/services/geo/internal/repo/kacho/dberr"

	geoerrors "github.com/PRO-Robotech/kacho/services/geo/internal/errors"
)

func TestWrap_noRows_notFound(t *testing.T) {
	err := dberr.Wrap(pgx.ErrNoRows, "Region", "region-1")
	if !stderrors.Is(err, geoerrors.ErrNotFound) {
		t.Fatalf("Wrap(ErrNoRows) = %v, want ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), "Region region-1 not found") {
		t.Fatalf("Wrap msg = %q, want stable not-found text", err.Error())
	}
}

// TestWrap_fkViolation_directionNeutral — 23503 летит и на parent-delete
// (Region.Delete с зонами), и на child-insert (Zone с несуществующим region_id).
// Сообщение обязано быть direction-neutral (не «referenced by», что верно только
// для parent-delete).
func TestWrap_fkViolation_directionNeutral(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23503"}
	err := dberr.Wrap(pgErr, "Zone", "region-1-a")
	if !stderrors.Is(err, geoerrors.ErrFailedPrecondition) {
		t.Fatalf("Wrap(23503) = %v, want ErrFailedPrecondition", err)
	}
	if !strings.Contains(err.Error(), "Zone region-1-a violates a reference constraint") {
		t.Fatalf("Wrap(23503) msg = %q, want direction-neutral reference-constraint text", err.Error())
	}
	if strings.Contains(err.Error(), "referenced by") {
		t.Fatalf("Wrap(23503) msg = %q, must not contain direction-specific 'referenced by'", err.Error())
	}
}

func TestWrap_checkViolation_invalidArg(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23514"}
	err := dberr.Wrap(pgErr, "Zone", "z-1")
	if !stderrors.Is(err, geoerrors.ErrInvalidArg) {
		t.Fatalf("Wrap(23514) = %v, want ErrInvalidArg", err)
	}
}

// TestWrap_uncategorized_internal — сырой не-pgx-текст не течёт наружу.
func TestWrap_uncategorized_internal(t *testing.T) {
	err := dberr.Wrap(stderrors.New("raw driver text"), "Region", "r-1")
	if !stderrors.Is(err, geoerrors.ErrInternal) {
		t.Fatalf("Wrap(raw) = %v, want ErrInternal", err)
	}
	if strings.Contains(err.Error(), "raw driver text") {
		t.Fatalf("Wrap(raw) leaked driver text: %q", err.Error())
	}
}

// withCapturedDefaultLogger временно подменяет slog.Default() на буфер и
// восстанавливает по завершении теста.
func withCapturedDefaultLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// TestWrap_unhandledSQLSTATE_loggedNotLeaked — некатегоризированный SQLSTATE
// (deadlock 40P01) коллапсирует в ErrInternal (no-leak в err.Error()), НО
// SQLSTATE попадает в server-log для operator-trail (CWE-390: раньше root cause
// выбрасывался без следа).
func TestWrap_unhandledSQLSTATE_loggedNotLeaked(t *testing.T) {
	buf := withCapturedDefaultLogger(t)
	pgErr := &pgconn.PgError{Code: "40P01", Message: "deadlock detected"}
	err := dberr.Wrap(pgErr, "Zone", "region-1-a")

	if !stderrors.Is(err, geoerrors.ErrInternal) {
		t.Fatalf("Wrap(40P01) = %v, want ErrInternal", err)
	}
	if strings.Contains(err.Error(), "deadlock detected") {
		t.Fatalf("Wrap(40P01) leaked pg text into err.Error(): %q", err.Error())
	}
	logged := buf.String()
	if !strings.Contains(logged, "40P01") {
		t.Fatalf("SQLSTATE 40P01 not captured in server log; operator has no trail. log=%q", logged)
	}
}

// TestWrap_uncategorizedNonPg_logged — не-pg ошибка (deadline/conn reset) тоже
// логируется на repo-границе перед коллапсом в sentinel.
func TestWrap_uncategorizedNonPg_logged(t *testing.T) {
	buf := withCapturedDefaultLogger(t)
	err := dberr.Wrap(stderrors.New("connection reset by peer"), "Region", "r-1")
	if !stderrors.Is(err, geoerrors.ErrInternal) {
		t.Fatalf("Wrap(raw) = %v, want ErrInternal", err)
	}
	if !strings.Contains(buf.String(), "connection reset by peer") {
		t.Fatalf("raw db error not captured in server log; log=%q", buf.String())
	}
}

// TestWrap_contextCanceled_notInternal — клиентская отмена (client-cancelled
// Get/List) НЕ должна коллапсировать в ErrInternal: иначе нормальная отмена
// рапортуется как INTERNAL (раздувает server-error budget, ложные «server bug»
// алерты). context.Canceled → ErrCanceled (→ codes.Canceled в serviceerr).
func TestWrap_contextCanceled_notInternal(t *testing.T) {
	buf := withCapturedDefaultLogger(t)
	err := dberr.Wrap(context.Canceled, "Region", "r-1")
	if stderrors.Is(err, geoerrors.ErrInternal) {
		t.Fatalf("Wrap(context.Canceled) = %v, must NOT be ErrInternal", err)
	}
	if !stderrors.Is(err, geoerrors.ErrCanceled) {
		t.Fatalf("Wrap(context.Canceled) = %v, want ErrCanceled", err)
	}
	// Нормальная отмена не должна ERROR-флудить лог «uncategorized».
	if strings.Contains(buf.String(), "uncategorized") {
		t.Fatalf("cancellation flooded ERROR-level uncategorized log: %q", buf.String())
	}
}

// TestWrap_deadlineExceeded_notInternal — истёкший per-call deadline
// (api-gateway timeout) → ErrDeadlineExceeded (→ codes.DeadlineExceeded), не
// INTERNAL. Также обёрнутый deadline (через fmt.Errorf %w) распознаётся.
func TestWrap_deadlineExceeded_notInternal(t *testing.T) {
	buf := withCapturedDefaultLogger(t)
	wrapped := fmt.Errorf("query failed: %w", context.DeadlineExceeded)
	err := dberr.Wrap(wrapped, "Zone", "z-1")
	if stderrors.Is(err, geoerrors.ErrInternal) {
		t.Fatalf("Wrap(DeadlineExceeded) = %v, must NOT be ErrInternal", err)
	}
	if !stderrors.Is(err, geoerrors.ErrDeadlineExceeded) {
		t.Fatalf("Wrap(DeadlineExceeded) = %v, want ErrDeadlineExceeded", err)
	}
	if strings.Contains(buf.String(), "uncategorized") {
		t.Fatalf("deadline flooded ERROR-level uncategorized log: %q", buf.String())
	}
}
