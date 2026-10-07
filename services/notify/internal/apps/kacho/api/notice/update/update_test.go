// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package update

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestFields_ImmutableIsNamedBeforeTheKnownSet — неизменяемое поле называется
// неизменяемостью даже рядом с неизвестным; неизвестное — по имени; пустая
// маска — полная правка; обе формы имени изменяемого поля приняты.
func TestFields_ImmutableIsNamedBeforeTheKnownSet(t *testing.T) {
	t.Parallel()
	refused := []struct {
		mask []string
		want string
	}{
		{[]string{"colour", "affected_resources"}, "affectedResources is immutable after Notice.Create"},
		{[]string{"affectedResources"}, "affectedResources is immutable after Notice.Create"},
		{[]string{"startsAt", "colour"}, "update_mask: unknown field colour"},
	}
	for _, c := range refused {
		_, _, _, err := fields(c.mask)
		if s, _ := status.FromError(err); err == nil || s.Code() != codes.InvalidArgument || s.Message() != c.want {
			t.Fatalf("маска %v: %v, ожидался %q", c.mask, err, c.want)
		}
	}
	if s, e, full, err := fields(nil); err != nil || !s || !e || !full {
		t.Fatalf("пустая маска: %v %v %v %v", s, e, full, err)
	}
	if s, e, full, err := fields([]string{"starts_at", "endsAt"}); err != nil || !s || !e || full {
		t.Fatalf("обе формы имени: %v %v %v %v", s, e, full, err)
	}
}
