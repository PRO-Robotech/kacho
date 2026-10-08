// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package create

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/PRO-Robotech/corelib/operations"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"

	"github.com/PRO-Robotech/kacho/services/notify/internal/apps/kacho/api/notice"
)

var t0 = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func ts(s string) *timestamppb.Timestamp {
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return timestamppb.New(v)
}

func maintenance() *notifyv1.CreateNoticeRequest {
	return &notifyv1.CreateNoticeRequest{
		Kind: notifyv1.Notice_MAINTENANCE, StartsAt: ts("2026-10-03T02:00:00Z"), EndsAt: ts("2026-10-03T04:00:00Z"),
		Audience: &notifyv1.NoticeAudience{AccountIds: []string{"acc-1"}},
	}
}

// TestValidate_FirstBrokenItemInP4OrderAnswers — при нескольких нарушениях
// отвечает первое в порядке Р4: вид → поля вида → аудитория → ссылки.
// Близнец каждого — тот же запрос без нарушения — принимается.
func TestValidate_FirstBrokenItemInP4OrderAnswers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		edit func(r *notifyv1.CreateNoticeRequest)
		want string
	}{
		{"вид раньше аудитории", func(r *notifyv1.CreateNoticeRequest) {
			r.Kind, r.Audience = notifyv1.Notice_KIND_UNSPECIFIED, nil
		}, "kind: required"},
		{"вид вне перечня", func(r *notifyv1.CreateNoticeRequest) { r.Kind = 99 }, "kind: required"},
		{"поле вида раньше аудитории", func(r *notifyv1.CreateNoticeRequest) {
			r.EndsAt, r.Audience = nil, nil
		}, "endsAt: required"},
		{"момент в прошлом", func(r *notifyv1.CreateNoticeRequest) { r.StartsAt = ts("2026-09-30T00:00:00Z") },
			"startsAt: must be in the future"},
		{"ссылка у вида без ссылок раньше аудитории", func(r *notifyv1.CreateNoticeRequest) {
			r.Kind, r.StartsAt, r.EndsAt = notifyv1.Notice_TERMS_CHANGE, ts("2026-11-01T00:00:00Z"), nil
			r.Audience = nil
			r.AffectedResources = []*notifyv1.AffectedResource{{Type: "compute_instance", Id: "ins-1"}}
		}, "affectedResources: not allowed for kind TERMS_CHANGE"},
		{"пустая аудитория", func(r *notifyv1.CreateNoticeRequest) { r.Audience = &notifyv1.NoticeAudience{} },
			"audience: required"},
		{"пустой элемент раньше формы соседнего", func(r *notifyv1.CreateNoticeRequest) {
			r.Audience.AccountIds = []string{"bad-1", ""}
		}, "audience.accountIds[1]: required"},
		{"аудитория раньше ссылок", func(r *notifyv1.CreateNoticeRequest) {
			r.Audience.AccountIds = []string{"bad-1"}
			r.AffectedResources = []*notifyv1.AffectedResource{{Type: "colour", Id: "x"}}
		}, "invalid account id 'bad-1'"},
		{"тип раньше обязательности id", func(r *notifyv1.CreateNoticeRequest) {
			r.AffectedResources = []*notifyv1.AffectedResource{{Type: "compute_instance", Id: ""}, {Type: "colour", Id: "x"}}
		}, "affectedResources[1].type: colour is not a tenant resource type"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r := maintenance()
			c.edit(r)
			_, err := Validate(r, t0)
			if st, _ := status.FromError(err); err == nil || st.Code() != codes.InvalidArgument || st.Message() != c.want {
				t.Fatalf("отказ %v, ожидался INVALID_ARGUMENT %q", err, c.want)
			}
		})
	}
	if _, err := Validate(maintenance(), t0); err != nil {
		t.Fatalf("близнец: годный запрос отвергнут: %v", err)
	}
}

// TestValidate_MomentsAreSeconds — моменты входа усечены до секунды до записи (З25).
func TestValidate_MomentsAreSeconds(t *testing.T) {
	t.Parallel()
	r := maintenance()
	r.StartsAt = timestamppb.New(time.Date(2026, 10, 3, 2, 0, 0, 999_000_000, time.UTC))
	got, err := Validate(r, t0)
	if err != nil {
		t.Fatal(err)
	}
	if !got.StartsAt.Equal(time.Date(2026, 10, 3, 2, 0, 0, 0, time.UTC)) {
		t.Fatalf("startsAt = %s, ожидалось усечение до секунды", got.StartsAt)
	}
}

type fakeStore struct {
	notice.Store
	err      error
	accepted []notice.CreateRequest
	ops      []operations.Operation
}

func (f *fakeStore) Accept(_ context.Context, r notice.CreateRequest, op operations.Operation, _ operations.Principal) error {
	if f.err != nil {
		return f.err
	}
	f.accepted, f.ops = append(f.accepted, r), append(f.ops, op)
	return nil
}

func operator() context.Context {
	return operations.WithPrincipal(context.Background(), operations.Principal{Type: "user", ID: "usr-op"})
}

// TestExecute_WritesRequestWithAcceptMomentAndPendingOperation — заявка несёт
// момент приёма часов notify и субъекта оператора; операция — done=false,
// приставка nop, metadata — id извещения заявки.
func TestExecute_WritesRequestWithAcceptMomentAndPendingOperation(t *testing.T) {
	t.Parallel()
	st := &fakeStore{}
	now := func() time.Time { return t0.Add(700 * time.Millisecond) }
	op, err := New(st, now).Execute(operator(), maintenance())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.accepted) != 1 {
		t.Fatalf("заявок записано %d", len(st.accepted))
	}
	r := st.accepted[0]
	if !r.AcceptedAt.Equal(t0) || r.CreatedBy != "user:usr-op" || r.OperationID != op.GetId() || op.GetDone() {
		t.Fatalf("заявка %+v, операция %s done=%v", r, op.GetId(), op.GetDone())
	}
	md := &notifyv1.CreateNoticeMetadata{}
	if err := op.GetMetadata().UnmarshalTo(md); err != nil || md.GetNoticeId() != r.NoticeID {
		t.Fatalf("metadata %v, ожидался noticeId %s", op.GetMetadata(), r.NoticeID)
	}
}

// TestExecute_NoForwardedPrincipalIsUnauthenticated — без принципала пары
// записи нет и запасного системного принципала нет (З17).
func TestExecute_NoForwardedPrincipalIsUnauthenticated(t *testing.T) {
	t.Parallel()
	st := &fakeStore{}
	_, err := New(st, func() time.Time { return t0 }).Execute(context.Background(), maintenance())
	if status.Code(err) != codes.Unauthenticated || len(st.accepted) != 0 {
		t.Fatalf("отказ %v, записано %d — ожидался UNAUTHENTICATED без записи", err, len(st.accepted))
	}
}

// TestExecute_StorageFailureIsAFixedText — текст драйвера наружу не выходит.
func TestExecute_StorageFailureIsAFixedText(t *testing.T) {
	t.Parallel()
	st := &fakeStore{err: errors.New(`pq: duplicate key value violates unique constraint "x"`)}
	_, err := New(st, func() time.Time { return t0 }).Execute(operator(), maintenance())
	if s, _ := status.FromError(err); s.Code() != codes.Internal || s.Message() != "notice storage failed" {
		t.Fatalf("отказ %v, ожидался INTERNAL с фиксированным текстом", err)
	}
}
