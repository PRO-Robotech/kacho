// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package source

// claimtimeout_test.go — классификация отказа `Claim` как «отменён по сроку»
// (CX1-72) без настоящих часов и сети.
//
// Срок вызова уходит серверу заголовком `grpc-timeout`, и сервер отвечает
// `DeadlineExceeded` по СВОЕЙ копии того же срока. Его ответ может дойти до
// клиента раньше, чем таймер клиентского контекста проставит `Err()`: у
// клиента срок уже истёк, а `callCtx.Err()` ещё nil. Такой вызов — тоже
// отменённый по сроку, иначе счётчик `notify_claim_call_timeouts_total`
// недосчитывает ровно те вызовы, ради которых заведён (наблюдалось в
// `TestClaimCallTimeoutAfterLeaseCommitIsCounted/late-answer-is-cancelled`:
// журнал «Claim отвергнут … code = DeadlineExceeded», счётчик 0).

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestClaimTimedOutClassifiesTheCallsOwnDeadline(t *testing.T) {
	t.Parallel()
	deadline := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	after, before := deadline.Add(time.Millisecond), deadline.Add(-time.Second)
	serverDeadline := status.Error(codes.DeadlineExceeded, "context deadline exceeded")

	for _, tc := range []struct {
		name    string
		err     error
		callErr error
		now     time.Time
		want    bool
	}{
		{
			// Предмет: ответ сервера по переданному сроку опередил таймер клиента.
			name: "server-reports-the-propagated-deadline-before-the-client-timer",
			err:  serverDeadline, callErr: nil, now: after, want: true,
		},
		{
			// Близнец: тот же ответ ДО срока вызова — собственный срок сервера,
			// то есть отказ источника, а не срок notify.
			name: "server-deadline-before-the-calls-own-deadline-is-a-refusal",
			err:  serverDeadline, callErr: nil, now: before, want: false,
		},
		{
			name: "client-context-deadline",
			err:  status.Error(codes.DeadlineExceeded, "context deadline exceeded"), callErr: context.DeadlineExceeded,
			now: after, want: true,
		},
		{
			// Близнец по коду: иной отказ после срока — не срок вызова.
			name: "other-refusal-after-the-deadline-is-a-refusal",
			err:  status.Error(codes.Unavailable, "connection reset"), callErr: nil, now: after, want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := claimTimedOut(tc.err, tc.callErr, deadline, tc.now); got != tc.want {
				t.Fatalf("claimTimedOut(%v, callErr=%v, now−deadline=%s) = %v, ожидалось %v",
					tc.err, tc.callErr, tc.now.Sub(deadline), got, tc.want)
			}
		})
	}
}
