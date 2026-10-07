// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package notice

import (
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/notify/internal/notice/rules"
)

// TestRefusal_KindBeforeStateAndClosedConstraintTexts — порядок «вид раньше
// состояния» (З5 п.2), закрытый перевод ограничений (З5 п.5), фиксированный
// текст на прочее.
func TestRefusal_KindBeforeStateAndClosedConstraintTexts(t *testing.T) {
	t.Parallel()
	start := rules.TransitionOf(rules.VerbStart)
	cases := []struct {
		err  error
		code codes.Code
		msg  string
	}{
		{ErrNotFound, codes.NotFound, "Notice ntc-1 not found"},
		{&TransitionRefusal{Kind: rules.KindOutage, State: rules.StateInProgress}, codes.FailedPrecondition,
			"Notice ntc-1 of kind OUTAGE does not support Start"},
		{&TransitionRefusal{Kind: rules.KindMaintenance, State: rules.StateCompleted}, codes.FailedPrecondition,
			"Notice ntc-1 is COMPLETED, expected SCHEDULED"},
		{&ConstraintRefusal{Constraint: "notices_ends_at_kind_chk", Kind: rules.KindDecommission}, codes.InvalidArgument,
			"endsAt: not allowed for kind DECOMMISSION"},
		{&ConstraintRefusal{Constraint: "notices_moments_order_chk"}, codes.Internal, "notice storage failed"},
		{errors.New("driver text"), codes.Internal, "notice storage failed"},
	}
	for _, c := range cases {
		st, _ := status.FromError(Refusal("ntc-1", rules.VerbStart, start, c.err))
		if st.Code() != c.code || st.Message() != c.msg {
			t.Fatalf("%v → %s %q, ожидалось %s %q", c.err, st.Code(), st.Message(), c.code, c.msg)
		}
	}
}
