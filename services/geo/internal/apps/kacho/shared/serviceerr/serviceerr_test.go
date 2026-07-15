// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package serviceerr_test

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/serviceerr"
	geoerrors "github.com/PRO-Robotech/kacho/services/geo/internal/errors"
)

// TestToStatus_canceled — client-cancelled запрос обязан стать codes.Canceled,
// а не codes.Internal (иначе нормальная отмена раздувает server-error budget).
func TestToStatus_canceled(t *testing.T) {
	err := serviceerr.ToStatus(geoerrors.ErrCanceled)
	if status.Code(err) != codes.Canceled {
		t.Fatalf("ToStatus(ErrCanceled) code = %v, want Canceled", status.Code(err))
	}
}

// TestToStatus_deadlineExceeded — истёкший deadline обязан стать
// codes.DeadlineExceeded, а не codes.Internal.
func TestToStatus_deadlineExceeded(t *testing.T) {
	err := serviceerr.ToStatus(geoerrors.ErrDeadlineExceeded)
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("ToStatus(ErrDeadlineExceeded) code = %v, want DeadlineExceeded", status.Code(err))
	}
}
