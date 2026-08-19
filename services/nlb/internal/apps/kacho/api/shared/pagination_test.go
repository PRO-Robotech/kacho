// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package shared

import (
	"time"

	"github.com/PRO-Robotech/kacho/pkg/pagetoken"

	"encoding/base64"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestValidatePagination locks WHAT this guard answers: a malformed page_token /
// out-of-range page_size is InvalidArgument, a well-formed cursor and an in-range size
// are not. It says nothing about WHEN the guard runs — it calls the function directly,
// with no caller and no short-circuit anywhere in the picture.
//
// The earlier wording claimed this case locked the guard "running BEFORE the listauthz
// empty-grant short-circuit". It did not, and could not: an assertion that never
// exercises the ordering stays green whatever the order becomes. The ordering is a
// property of the tree, and it is locked as one — internal/repohygiene
// TestEmptyPageNeverPrecedesPaginationValidation walks every List-shaped function and
// requires the format check to precede any empty page returned on the caller's identity.
func TestValidatePagination(t *testing.T) {
	// Токен берётся У ПРОИЗВОДИТЕЛЯ, а не выписывается литералом: собранный руками,
	// он был бы ещё одним объявлением формата и зеленел бы ровно до тех пор, пока
	// копия совпадает с кодеком.
	validToken := pagetoken.EncodeKeysetTime(
		pagetoken.DefaultOrder,
		time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC),
		"nlb0000000000000000",
	)
	cases := []struct {
		name      string
		pageToken string
		pageSize  int64
		wantErr   bool
	}{
		{"empty token + default size", "", 0, false},
		{"size within range", "", 1000, false},
		{"valid token", validToken, 10, false},
		{"size over max", "", 1001, true},
		{"negative size", "", -1, true},
		{"garbage token (not base64)", "not-a-real-token!!", 0, true},
		{"валидный base64 без метки формата", base64.RawURLEncoding.EncodeToString([]byte("noseparator")), 0, true},
		// Токен прежней формы этого же сервиса обязан быть ОТВЕРГНУТ, а не истолкован:
		// курсор опаковый и живёт один сеанс обхода, поэтому вызывающий начинает обход
		// заново — это лучше тихо неверной страницы.
		{"токен прежней формы", base64.RawURLEncoding.EncodeToString([]byte("2026-07-17T00:00:00Z\x00nlb0000000000000000")), 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePagination(tc.pageToken, tc.pageSize)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if got := status.Code(err); got != codes.InvalidArgument {
					t.Fatalf("expected InvalidArgument, got %v", got)
				}
			} else if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
