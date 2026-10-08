// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// transitions_integration_test.go — глаголы Start/Complete/Cancel/Update
// (приёмка NTF-5 группа C, Р5; полоса N2): одна запись с условием на состояние
// в транзакции запроса, завершённая Operation в ней же (Ф2), синхронный отказ
// без Operation. «Дано» — извещение в нужном состоянии, поставленное посевом
// строк, которые судит схема (fixture_test.go).

import (
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	operationv1 "github.com/PRO-Robotech/corelib/api/corelib/operation"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"
)

func start(t *testing.T, e edge, id string) (*operationv1.Operation, error) {
	return e.internal().Start(as(t, usrOp, "2"), &notifyv1.StartNoticeRequest{NoticeId: id})
}

func complete(t *testing.T, e edge, id string) (*operationv1.Operation, error) {
	return e.internal().Complete(as(t, usrOp, "2"), &notifyv1.CompleteNoticeRequest{NoticeId: id})
}

func cancelNotice(t *testing.T, e edge, id string) (*operationv1.Operation, error) {
	return e.internal().Cancel(as(t, usrOp, "2"), &notifyv1.CancelNoticeRequest{NoticeId: id})
}

func update(t *testing.T, e edge, r *notifyv1.UpdateNoticeRequest) (*operationv1.Operation, error) {
	return e.internal().Update(as(t, usrOp, "2"), r)
}

func mask(paths ...string) *fieldmaskpb.FieldMask { return &fieldmaskpb.FieldMask{Paths: paths} }

func requireReminders(t *testing.T, n *notifyv1.InternalNotice, at ...string) {
	t.Helper()
	got := n.GetReminders()
	if len(got) != len(at) {
		t.Fatalf("reminders = %v, ожидалось %v", got, at)
	}
	for i, a := range at {
		requireMoment(t, "reminders["+string(rune('0'+i))+"].at", got[i].GetAt(), a)
		if got[i].GetStage() != "reminder" {
			t.Fatalf("reminders[%d].stage = %q, ожидалось reminder", i, got[i].GetStage())
		}
	}
}

func TestNoticeStart_NTF527_StartsScheduledMaintenance(t *testing.T) {
	w, e := newAPI(t)
	n := seed(t, w.pool, maintenance01())
	w.clock.Set(at("2026-10-03T02:05:00Z"))
	op, err := start(t, e, n.ID)
	requireDoneOK(t, "Start", op, err, n.ID)
	got := getInternal(t, e, n.ID)
	if got.GetState() != notifyv1.Notice_IN_PROGRESS {
		t.Fatalf("state = %s, ожидалось IN_PROGRESS", got.GetState())
	}
	requireMoment(t, "startedAt", got.GetStartedAt(), "2026-10-03T02:05:00Z")
	requireReminders(t, got)
}

func TestNoticeComplete_NTF528_CompletesStartedMaintenance(t *testing.T) {
	w, e := newAPI(t)
	n := seed(t, w.pool, started27())
	w.clock.Set(at("2026-10-03T03:40:00Z"))
	op, err := complete(t, e, n.ID)
	requireDoneOK(t, "Complete", op, err, n.ID)
	got := getInternal(t, e, n.ID)
	if got.GetState() != notifyv1.Notice_COMPLETED {
		t.Fatalf("state = %s, ожидалось COMPLETED", got.GetState())
	}
	requireMoment(t, "completedAt", got.GetCompletedAt(), "2026-10-03T03:40:00Z")
	requireMoment(t, "startedAt", got.GetStartedAt(), "2026-10-03T02:05:00Z")
}

func TestNoticeCancel_NTF529_CancelsScheduled(t *testing.T) {
	w, e := newAPI(t)
	n := seed(t, w.pool, maintenance01())
	w.clock.Set(at("2026-10-02T10:00:00Z"))
	op, err := cancelNotice(t, e, n.ID)
	requireDoneOK(t, "Cancel", op, err, n.ID)
	got := getInternal(t, e, n.ID)
	if got.GetState() != notifyv1.Notice_CANCELLED {
		t.Fatalf("state = %s, ожидалось CANCELLED", got.GetState())
	}
	requireMoment(t, "cancelledAt", got.GetCancelledAt(), "2026-10-02T10:00:00Z")
	requireMoment(t, "startedAt", got.GetStartedAt(), "")
	requireMoment(t, "completedAt", got.GetCompletedAt(), "")
}

func TestNoticeCancel_NTF530_InProgressIsRefusedSynchronously(t *testing.T) {
	w, e := newAPI(t)
	n := seed(t, w.pool, started27())
	requireTransitionRefused(t, w, e, "NTF5-30 Cancel",
		func() (*operationv1.Operation, error) { return cancelNotice(t, e, n.ID) },
		n.ID, "Notice "+n.ID+" is IN_PROGRESS, expected SCHEDULED")
}

func TestNoticeComplete_NTF531_ScheduledIsRefusedSynchronously(t *testing.T) {
	w, e := newAPI(t)
	n := seed(t, w.pool, maintenance01())
	requireTransitionRefused(t, w, e, "NTF5-31 Complete",
		func() (*operationv1.Operation, error) { return complete(t, e, n.ID) },
		n.ID, "Notice "+n.ID+" is SCHEDULED, expected IN_PROGRESS")
}

func TestNoticeStart_NTF532_OutageDoesNotSupportStart(t *testing.T) {
	w, e := newAPI(t)
	n := seed(t, w.pool, outage02())
	requireTransitionRefused(t, w, e, "NTF5-32 Start",
		func() (*operationv1.Operation, error) { return start(t, e, n.ID) },
		n.ID, "Notice "+n.ID+" of kind OUTAGE does not support Start")
}

// TestNoticeComplete_NTF533_TwoConcurrentCompletesExactlyOnePasses — 20
// повторов, в каждом новое извещение и две горутины Complete на одной базе.
func TestNoticeComplete_NTF533_TwoConcurrentCompletesExactlyOnePasses(t *testing.T) {
	w, e := newAPI(t)
	for rep := 0; rep < 20; rep++ {
		n := seed(t, w.pool, outage02())
		type outcome struct {
			op  *operationv1.Operation
			err error
		}
		var (
			wg  sync.WaitGroup
			got [2]outcome
		)
		gate := make(chan struct{})
		for i := range got {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-gate
				got[i].op, got[i].err = complete(t, e, n.ID)
			}(i)
		}
		close(gate)
		wg.Wait()
		passed, refused := 0, 0
		for _, o := range got {
			switch {
			case o.err == nil && o.op.GetDone() && o.op.GetError() == nil:
				passed++
			case o.err != nil:
				r := refusalOf(o.err)
				want := "Notice " + n.ID + " is COMPLETED, expected IN_PROGRESS"
				if r.Code != codes.FailedPrecondition || r.Message != want {
					t.Fatalf("повтор %d: проигравший Complete — %s %q, ожидался FAILED_PRECONDITION %q", rep, r.Code, r.Message, want)
				}
				refused++
			default:
				t.Fatalf("повтор %d: исход Complete вне двух законных: done=%v error=%v", rep, o.op.GetDone(), o.op.GetError())
			}
		}
		if passed != 1 || refused != 1 {
			t.Fatalf("повтор %d: прошло %d, отвергнуто %d — ожидалось ровно 1 и 1", rep, passed, refused)
		}
		if k := stageEvents(t, w.pool, n.ID, "resolved"); k != 1 {
			t.Fatalf("повтор %d: этап resolved зафиксирован %d раз, ожидалось ровно 1", rep, k)
		}
	}
}

func reschedule34(id string, m ...string) *notifyv1.UpdateNoticeRequest {
	return &notifyv1.UpdateNoticeRequest{NoticeId: id, UpdateMask: mask(m...),
		StartsAt: ts("2026-10-05T02:00:00Z"), EndsAt: ts("2026-10-05T05:00:00Z")}
}

func TestNoticeUpdate_NTF534_ReschedulesTheWindow(t *testing.T) {
	w, e := newAPI(t)
	n := seed(t, w.pool, maintenance01())
	op, err := update(t, e, reschedule34(n.ID, "startsAt", "endsAt"))
	requireDoneOK(t, "Update", op, err, n.ID)
	got := getInternal(t, e, n.ID)
	requireMoment(t, "startsAt", got.GetStartsAt(), "2026-10-05T02:00:00Z")
	requireMoment(t, "endsAt", got.GetEndsAt(), "2026-10-05T05:00:00Z")
	if got.GetRevision() != 2 || got.GetState() != notifyv1.Notice_SCHEDULED {
		t.Fatalf("revision=%d state=%s, ожидалось 2 и SCHEDULED", got.GetRevision(), got.GetState())
	}
	requireReminders(t, got, "2026-10-04T02:00:00Z")
}

func TestNoticeUpdate_NTF535_InProgressIsRefusedSynchronously(t *testing.T) {
	w, e := newAPI(t)
	n := seed(t, w.pool, started27())
	requireTransitionRefused(t, w, e, "NTF5-35 Update",
		func() (*operationv1.Operation, error) { return update(t, e, reschedule34(n.ID, "startsAt", "endsAt")) },
		n.ID, "Notice "+n.ID+" is IN_PROGRESS, expected SCHEDULED")
}

func requireUpdateInvalid(t *testing.T, w *world, e edge, what, id string, r *notifyv1.UpdateNoticeRequest, message string) {
	t.Helper()
	ops := notifyOperations(t, w.pool)
	_, err := update(t, e, r)
	requireRefusal(t, what, err, codes.InvalidArgument, message, "")
	if got := getInternal(t, e, id); got.GetRevision() != 1 {
		t.Fatalf("%s: отказ изменил revision: %d", what, got.GetRevision())
	}
	if o := notifyOperations(t, w.pool); o != ops {
		t.Fatalf("%s: отказ создал операцию", what)
	}
}

func TestNoticeUpdate_NTF536_KindIsImmutable(t *testing.T) {
	w, e := newAPI(t)
	n := seed(t, w.pool, maintenance01())
	requireUpdateInvalid(t, w, e, "NTF5-36", n.ID,
		&notifyv1.UpdateNoticeRequest{NoticeId: n.ID, UpdateMask: mask("kind")},
		"kind is immutable after Notice.Create")
}

func TestNoticeUpdate_NTF537_AudienceIsImmutable(t *testing.T) {
	w, e := newAPI(t)
	n := seed(t, w.pool, maintenance01())
	requireUpdateInvalid(t, w, e, "NTF5-37", n.ID,
		&notifyv1.UpdateNoticeRequest{NoticeId: n.ID, UpdateMask: mask("audience")},
		"audience is immutable after Notice.Create")
}

func TestNoticeUpdate_NTF538_UnknownMaskFieldIsNamed(t *testing.T) {
	w, e := newAPI(t)
	n := seed(t, w.pool, maintenance01())
	_, err := update(t, e, reschedule34(n.ID, "startsAt", "colour"))
	r := refusalOf(err)
	if err == nil || r.Code != codes.InvalidArgument || !strings.Contains(r.Message, "colour") {
		t.Fatalf("NTF5-38: отказ %s %q, ожидался INVALID_ARGUMENT с именем поля colour", r.Code, r.Message)
	}
	if got := getInternal(t, e, n.ID); got.GetRevision() != 1 {
		t.Fatalf("NTF5-38: revision изменился: %d", got.GetRevision())
	}
}

func TestNoticeUpdate_NTF541_SameValuesAreNotANewRevision(t *testing.T) {
	w, e := newAPI(t)
	n := seed(t, w.pool, maintenance01())
	op, err := update(t, e, &notifyv1.UpdateNoticeRequest{NoticeId: n.ID, UpdateMask: mask("startsAt", "endsAt"),
		StartsAt: ts("2026-10-03T02:00:00Z"), EndsAt: ts("2026-10-03T04:00:00Z")})
	requireDoneOK(t, "Update", op, err, n.ID)
	if got := getInternal(t, e, n.ID); got.GetRevision() != 1 {
		t.Fatalf("правка без изменения повысила revision до %d", got.GetRevision())
	}
	if k := stageEvents(t, w.pool, n.ID, "rescheduled"); k != 0 {
		t.Fatalf("правка без изменения зафиксировала этап rescheduled (%d)", k)
	}
}

func TestNoticeUpdate_NTF586_OutageDoesNotSupportUpdate(t *testing.T) {
	w, e := newAPI(t)
	n := seed(t, w.pool, outage02())
	requireTransitionRefused(t, w, e, "NTF5-86 Update",
		func() (*operationv1.Operation, error) {
			return update(t, e, &notifyv1.UpdateNoticeRequest{NoticeId: n.ID, UpdateMask: mask("startsAt"),
				StartsAt: ts("2026-10-05T00:00:00Z")})
		},
		n.ID, "Notice "+n.ID+" of kind OUTAGE does not support Update")
}

func TestNoticeUpdate_NTF587_MovingIntoThePastIsRefused(t *testing.T) {
	w, e := newAPI(t)
	n := seed(t, w.pool, maintenance01())
	w.clock.Set(at("2026-10-02T00:00:00Z"))
	requireUpdateInvalid(t, w, e, "NTF5-87", n.ID,
		&notifyv1.UpdateNoticeRequest{NoticeId: n.ID, UpdateMask: mask("startsAt"), StartsAt: ts("2026-10-01T12:00:00Z")},
		"startsAt: must be in the future")
}

// TestNoticeUpdate_NTF588_EmptyMaskIsAFullPatch — пустая маска применяет все
// изменяемые поля тела. Буква «kind=OUTAGE в теле игнорируется» в gRPC-форме
// невыразима: в UpdateNoticeRequest поля kind нет (вопрос к приёмке в
// возврате полосы); вид после правки — прежний.
func TestNoticeUpdate_NTF588_EmptyMaskIsAFullPatch(t *testing.T) {
	w, e := newAPI(t)
	n := seed(t, w.pool, maintenance01())
	op, err := update(t, e, reschedule34(n.ID))
	requireDoneOK(t, "Update", op, err, n.ID)
	got := getInternal(t, e, n.ID)
	requireMoment(t, "startsAt", got.GetStartsAt(), "2026-10-05T02:00:00Z")
	requireMoment(t, "endsAt", got.GetEndsAt(), "2026-10-05T05:00:00Z")
	if got.GetRevision() != 2 || got.GetKind() != notifyv1.Notice_MAINTENANCE {
		t.Fatalf("revision=%d kind=%s, ожидалось 2 и MAINTENANCE", got.GetRevision(), got.GetKind())
	}
}

func TestInternalNoticeGet_NTF539_MissingNoticeIsNotFound(t *testing.T) {
	_, e := newAPI(t)
	const id = "ntc-00000000000000000"
	_, err := e.internal().Get(as(t, usrOp, "2"), &notifyv1.GetInternalNoticeRequest{NoticeId: id})
	requireRefusal(t, "NTF5-39", err, codes.NotFound, "Notice "+id+" not found", "RESOURCE_NOT_FOUND")
}

func TestInternalNoticeGet_NTF540_MalformedIDIsInvalid(t *testing.T) {
	_, e := newAPI(t)
	_, err := e.internal().Get(as(t, usrOp, "2"), &notifyv1.GetInternalNoticeRequest{NoticeId: "abc"})
	requireRefusal(t, "NTF5-40", err, codes.InvalidArgument, "invalid notice id 'abc'", "INVALID_RESOURCE_ID")
}
