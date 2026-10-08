// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// probes_common_test.go — общие шаги проб N2: «Дано» G0 с поднятым notify-api,
// запросы сценариев NTF-5 и чтение исхода. Имя пробы несёт ID сценария
// (`Test<Предмет>_NTF5<NN>_<Суть>`), буквы сценария — подпробы.

import (
	"sort"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/timestamppb"

	operationv1 "github.com/PRO-Robotech/corelib/api/corelib/operation"
	"github.com/PRO-Robotech/corelib/ids"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"
)

// newAPI — G0 с notify-api, поднятым на ручках G0.
func newAPI(t *testing.T) (*world, edge) {
	t.Helper()
	w := g0(t)
	return w, raise(t, w, g0Knobs)
}

func audienceAccounts(idsList ...string) *notifyv1.NoticeAudience {
	return &notifyv1.NoticeAudience{AccountIds: idsList}
}

// req01 — payload NTF5-01: плановые работы acc-1, окно 2026-10-03T02:00–04:00.
func req01() *notifyv1.CreateNoticeRequest {
	return &notifyv1.CreateNoticeRequest{
		Kind:     notifyv1.Notice_MAINTENANCE,
		StartsAt: ts("2026-10-03T02:00:00Z"),
		EndsAt:   ts("2026-10-03T04:00:00Z"),
		Audience: audienceAccounts("acc-1"),
	}
}

// req02 — payload NTF5-02: авария acc-1.
func req02() *notifyv1.CreateNoticeRequest {
	return &notifyv1.CreateNoticeRequest{Kind: notifyv1.Notice_OUTAGE, Audience: audienceAccounts("acc-1")}
}

// req03 — payload NTF5-03: вывод из эксплуатации со сроком 2026-11-10.
func req03() *notifyv1.CreateNoticeRequest {
	return &notifyv1.CreateNoticeRequest{
		Kind: notifyv1.Notice_DECOMMISSION, StartsAt: ts("2026-11-10T00:00:00Z"), Audience: audienceAccounts("acc-1"),
	}
}

// req05 — payload NTF5-05: инцидент безопасности acc-1.
func req05() *notifyv1.CreateNoticeRequest {
	return &notifyv1.CreateNoticeRequest{Kind: notifyv1.Notice_SECURITY_INCIDENT, Audience: audienceAccounts("acc-1")}
}

// machines — n ссылок `compute_instance` формы `ins-…` (NTF5-20).
func machines(n int) []*notifyv1.AffectedResource {
	out := make([]*notifyv1.AffectedResource, n)
	for i := range out {
		out[i] = &notifyv1.AffectedResource{Type: "compute_instance", Id: ids.NewHyphenID("ins")}
	}
	return out
}

// requireAccepted — ответ Create: операция notify (`nop`), `done=false`,
// метаданные с id извещения формы `ntc-` (Р2, Р3, Р10). Отдаёт id извещения.
func requireAccepted(t *testing.T, op *operationv1.Operation, err error) string {
	t.Helper()
	if err != nil {
		t.Fatalf("Create отвергнут: %v", err)
	}
	if !opIDForm.MatchString(op.GetId()) {
		t.Fatalf("id операции %q — не приставка %s и 17 символов crockford", op.GetId(), ids.PrefixOperationNotify)
	}
	if op.GetDone() {
		t.Fatalf("операция Create родилась завершённой (done=true): заявка не прошла проверку у владельца")
	}
	md := &notifyv1.CreateNoticeMetadata{}
	if op.GetMetadata() == nil || op.GetMetadata().UnmarshalTo(md) != nil {
		t.Fatalf("метаданные операции — не CreateNoticeMetadata: %v", op.GetMetadata())
	}
	if !noticeIDForm.MatchString(md.GetNoticeId()) {
		t.Fatalf("metadata.noticeId %q — не `ntc-` и 17 символов crockford", md.GetNoticeId())
	}
	return md.GetNoticeId()
}

// requireDoneOK — ответ перехода: операция завершена без ошибки, ответ —
// InternalNotice этого извещения (Р5, Ф2). Отдаёт извещение из ответа.
func requireDoneOK(t *testing.T, what string, op *operationv1.Operation, err error, noticeID string) *notifyv1.InternalNotice {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: ответ вызова — отказ %v, ожидалась Operation done=true", what, err)
	}
	if !op.GetDone() || op.GetError() != nil {
		t.Fatalf("%s: операция done=%v error=%v, ожидалась done=true без ошибки", what, op.GetDone(), op.GetError())
	}
	n := &notifyv1.InternalNotice{}
	if op.GetResponse() == nil || op.GetResponse().UnmarshalTo(n) != nil || n.GetId() != noticeID {
		t.Fatalf("%s: result.response — не InternalNotice %s: %v", what, noticeID, op.GetResponse())
	}
	return n
}

// getInternal — внутренняя проекция извещения глазами usr-op.
func getInternal(t *testing.T, e edge, id string) *notifyv1.InternalNotice {
	t.Helper()
	n, err := e.internal().Get(as(t, usrOp, "2"), &notifyv1.GetInternalNoticeRequest{NoticeId: id})
	if err != nil {
		t.Fatalf("InternalNoticeService.Get(%s): %v", id, err)
	}
	return n
}

func requireMoment(t *testing.T, field string, got *timestamppb.Timestamp, want string) {
	t.Helper()
	if want == "" {
		if got != nil {
			t.Fatalf("%s задан (%s), ожидалось «не задан»", field, got.AsTime().Format("2006-01-02T15:04:05.999999999Z07:00"))
		}
		return
	}
	if got == nil || !got.AsTime().Equal(at(want)) {
		t.Fatalf("%s = %v, ожидалось %s", field, got, want)
	}
}

// requireTransitionRefused — синхронный FAILED_PRECONDITION перехода: тело —
// статус, Operation не создана, состояние и ревизия прежние (Р5).
func requireTransitionRefused(t *testing.T, w *world, e edge, what string, call func() (*operationv1.Operation, error),
	id, message string) {
	t.Helper()
	before := getInternal(t, e, id)
	ops := notifyOperations(t, w.pool)
	_, err := call()
	requireRefusal(t, what, err, codes.FailedPrecondition, message, "")
	if n := notifyOperations(t, w.pool); n != ops {
		t.Fatalf("%s: отказ перехода создал операцию (было %d, стало %d)", what, ops, n)
	}
	after := getInternal(t, e, id)
	if after.GetState() != before.GetState() || after.GetRevision() != before.GetRevision() {
		t.Fatalf("%s: отказ изменил извещение: %s/%d → %s/%d", what,
			before.GetState(), before.GetRevision(), after.GetState(), after.GetRevision())
	}
}

// byCreated — id извещений в порядке (createdAt, id) (Р16).
func byCreated(seeds ...noticeSeed) []string {
	s := append([]noticeSeed(nil), seeds...)
	sort.Slice(s, func(i, j int) bool {
		if !s[i].CreatedAt.Equal(s[j].CreatedAt) {
			return s[i].CreatedAt.Before(s[j].CreatedAt)
		}
		return s[i].ID < s[j].ID
	})
	out := make([]string, len(s))
	for i, n := range s {
		out[i] = n.ID
	}
	return out
}

func idsOf[N interface{ GetId() string }](ns []N) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.GetId()
	}
	return out
}

func equalIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func opGet(id string) *operationv1.GetOperationRequest {
	return &operationv1.GetOperationRequest{OperationId: id}
}
