// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// create_integration_test.go — приём `InternalNoticeService.Create` в
// notify-api (приёмка NTF-5 группа A, Р4, Р10; полоса N2): синхронные
// проверки до заявки и ответ приёма `done=false`. Фиксацию заявки (шаги 2–6
// Р10) исполняет notify-sender — она предмет полосы N3 и здесь не
// утверждается: пробы этого файла судят то, что производит notify-api один.

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"
)

// requireCreateRefused — синхронный INVALID_ARGUMENT с точным текстом; ни
// заявки, ни извещения; ни одного вызова справочника (Р4, Р10 шаг 1).
func requireCreateRefused(t *testing.T, w *world, e edge, what string, req *notifyv1.CreateNoticeRequest, message string) {
	t.Helper()
	pending, notices := pendingCreates(t, w.pool), noticeRows(t, w.pool)
	mark := w.kaname.mark()
	op, err := e.internal().Create(as(t, usrOp, "2"), req)
	if err == nil {
		t.Fatalf("%s: ответ — Operation %s, ожидался синхронный INVALID_ARGUMENT %q", what, op.GetId(), message)
	}
	requireRefusal(t, what, err, codes.InvalidArgument, message, "")
	if n := pendingCreates(t, w.pool); n != pending {
		t.Fatalf("%s: заявок создания было %d, стало %d", what, pending, n)
	}
	if n := noticeRows(t, w.pool); n != notices {
		t.Fatalf("%s: извещений было %d, стало %d", what, notices, n)
	}
	if dc := directoryCalls(w.kaname.since(mark)); len(dc) != 0 {
		t.Fatalf("%s: отвергнутый вызов дошёл до справочника: %+v", what, dc)
	}
}

func TestInternalNoticeCreate_NTF507_FieldForbiddenByKindIsRefused(t *testing.T) {
	w, e := newAPI(t)
	req := req03()
	req.EndsAt = ts("2026-11-11T00:00:00Z")
	requireCreateRefused(t, w, e, "NTF5-07", req, "endsAt: not allowed for kind DECOMMISSION")
}

func TestInternalNoticeCreate_NTF508_RequiredFieldOfKindIsMissing(t *testing.T) {
	w, e := newAPI(t)
	req := req01()
	req.EndsAt = nil
	requireCreateRefused(t, w, e, "NTF5-08", req, "endsAt: required")
}

func TestInternalNoticeCreate_NTF509_StartIsNotInTheFuture(t *testing.T) {
	w, e := newAPI(t)
	req := req01()
	req.StartsAt = ts("2026-10-01T00:00:00Z")
	requireCreateRefused(t, w, e, "NTF5-09", req, "startsAt: must be in the future")
}

func TestInternalNoticeCreate_NTF510_EndIsNotAfterStart(t *testing.T) {
	w, e := newAPI(t)
	req := req01()
	req.EndsAt = ts("2026-10-03T02:00:00Z")
	requireCreateRefused(t, w, e, "NTF5-10", req, "endsAt: must be after startsAt")
}

func TestInternalNoticeCreate_NTF511_KindIsRequired(t *testing.T) {
	w, e := newAPI(t)
	req := req01()
	req.Kind = notifyv1.Notice_KIND_UNSPECIFIED
	requireCreateRefused(t, w, e, "NTF5-11", req, "kind: required")
}

func TestInternalNoticeCreate_NTF512_AudienceIsRequired(t *testing.T) {
	w, e := newAPI(t)
	req := req01()
	req.Audience = nil
	requireCreateRefused(t, w, e, "NTF5-12", req, "audience: required")
}

func hundredAndOneAccounts() []string {
	out := make([]string, 101)
	for i := range out {
		out[i] = fmt.Sprintf("acc-%d", i+1)
	}
	return out
}

func TestInternalNoticeCreate_NTF513_HundredAndOneAccountsAreRefused(t *testing.T) {
	w, e := newAPI(t)
	req := req01()
	req.Audience = audienceAccounts(hundredAndOneAccounts()...)
	requireCreateRefused(t, w, e, "NTF5-13", req, "audience.accountIds: at most 100 items")
}

func TestInternalNoticeCreate_NTF515_AllAccountsForbiddenForSuspension(t *testing.T) {
	w, e := newAPI(t)
	req := &notifyv1.CreateNoticeRequest{Kind: notifyv1.Notice_SUSPENSION,
		Audience: &notifyv1.NoticeAudience{AllAccounts: true}}
	requireCreateRefused(t, w, e, "NTF5-15", req, "audience.allAccounts: not allowed for kind SUSPENSION")
}

func TestInternalNoticeCreate_NTF516_TwoAudienceFormsAreRefused(t *testing.T) {
	w, e := newAPI(t)
	req := req01()
	req.Audience.ProjectIds = []string{"prj-1"}
	requireCreateRefused(t, w, e, "NTF5-16", req, "audience: exactly one of allAccounts, accountIds, projectIds")
}

func TestInternalNoticeCreate_NTF519_ReferencesForbiddenForSecurityIncident(t *testing.T) {
	w, e := newAPI(t)
	req := req05()
	req.AffectedResources = machines(1)
	requireCreateRefused(t, w, e, "NTF5-19", req, "affectedResources: not allowed for kind SECURITY_INCIDENT")
}

func TestInternalNoticeCreate_NTF521_TooManyReferencesOrForeignType(t *testing.T) {
	w, e := newAPI(t)
	t.Run("(а) 51 ссылка", func(t *testing.T) {
		req := req02()
		req.AffectedResources = machines(51)
		requireCreateRefused(t, w, e, "NTF5-21 (а)", req, "affectedResources: at most 50 items")
	})
	for _, c := range []struct{ letter, typ string }{{"(б)", "account"}, {"(в)", "vpc_address_pool"}} {
		t.Run(c.letter+" тип "+c.typ, func(t *testing.T) {
			req := req02()
			req.AffectedResources = machines(50)
			req.AffectedResources[17].Type = c.typ
			requireCreateRefused(t, w, e, "NTF5-21 "+c.letter, req,
				"affectedResources[17].type: "+c.typ+" is not a tenant resource type")
		})
	}
}

func TestInternalNoticeCreate_NTF5109_ForeignIDsAreCheckedBeforeTheRequest(t *testing.T) {
	w, e := newAPI(t)
	cases := []struct {
		letter, message string
		req             func() *notifyv1.CreateNoticeRequest
	}{
		{"(а)", "invalid account id 'bad-1'", func() *notifyv1.CreateNoticeRequest {
			r := req01()
			r.Audience = audienceAccounts("bad-1")
			return r
		}},
		{"(б)", "audience.accountIds[0]: required", func() *notifyv1.CreateNoticeRequest {
			r := req01()
			r.Audience = audienceAccounts("")
			return r
		}},
		{"(в)", "invalid project id 'bad-1'", func() *notifyv1.CreateNoticeRequest {
			r := req01()
			r.Audience = &notifyv1.NoticeAudience{ProjectIds: []string{"bad-1"}}
			return r
		}},
		{"(г)", "invalid compute_instance id 'bad-1'", func() *notifyv1.CreateNoticeRequest {
			r := req02()
			r.AffectedResources = machines(50)
			r.AffectedResources[3].Id = "bad-1"
			return r
		}},
		{"(д)", "affectedResources[3].id: required", func() *notifyv1.CreateNoticeRequest {
			r := req02()
			r.AffectedResources = machines(50)
			r.AffectedResources[3].Id = ""
			return r
		}},
	}
	for _, c := range cases {
		t.Run(c.letter, func(t *testing.T) {
			mark := w.kaname.mark()
			requireCreateRefused(t, w, e, "NTF5-109 "+c.letter, c.req(), c.message)
			// «подмена службы доступа вызовов не получала» — ни справочника, ни
			// пакетной проверки; вопрос звена прав о вызывающем — до use-case.
			for _, call := range w.kaname.since(mark) {
				if call.Method != methodCheck || call.Question != question(usrOp, "system_admin", "cluster:cluster_root") {
					t.Fatalf("NTF5-109 %s: проверка формы отдала вызов службе доступа: %+v", c.letter, call)
				}
			}
		})
	}
}

// requireRequestRow — заявка создания записана notify-api: одна строка с этим
// id извещения и этой операцией, без аренды, момент приёма — часы notify.
func requireRequestRow(t *testing.T, w *world, noticeID, opID string) {
	t.Helper()
	n := count(t, w.pool, `SELECT count(*) FROM notice_create_requests
		WHERE notice_id = $1 AND operation_id = $2 AND lease_token IS NULL AND accepted_at = $3`, noticeID, opID, t0)
	if n != 1 {
		t.Fatalf("заявки создания (%s, %s) без аренды с моментом приёма T0 — %d строк, ожидалась 1", noticeID, opID, n)
	}
}

// TestInternalNoticeCreate_NTF501_AcceptReturnsPendingOperation — часть
// «Тогда» NTF5-01, которую производит notify-api: операция `nop` с
// `done=false`, `metadata.noticeId` формы `ntc-`, заявка и операция одной
// записью; notify-api справочника не зовёт. Доведение до `done=true` — шаг
// фиксации notify-sender (полоса N3, проба той же буквы в её наборе).
// Близнец NTF5-109 (а) — тот же вызов с формой id `acc-1`.
func TestInternalNoticeCreate_NTF501_AcceptReturnsPendingOperation(t *testing.T) {
	w, e := newAPI(t)
	mark := w.kaname.mark()
	op, err := e.internal().Create(as(t, usrOp, "2"), req01())
	noticeID := requireAccepted(t, op, err)
	requireRequestRow(t, w, noticeID, op.GetId())
	if n := noticeRows(t, w.pool); n != 0 {
		t.Fatalf("приём записал извещение (%d строк) до проверки областей у владельца", n)
	}
	if dc := directoryCalls(w.kaname.since(mark)); len(dc) != 0 {
		t.Fatalf("notify-api позвал справочник при приёме: %+v", dc)
	}
	got, err := e.operations().Get(as(t, usrOp, "2"), opGet(op.GetId()))
	if err != nil || got.GetDone() || got.GetId() != op.GetId() {
		t.Fatalf("OperationService.Get на слушателе notify-api: %v done=%v err=%v", got.GetId(), got.GetDone(), err)
	}
}

// TestInternalNoticeCreate_NTF518_PendingRequestIsNotANotice — «до истечения
// окна» NTF5-18: заявка не видна как извещение (Get → NOT_FOUND, List пуст).
func TestInternalNoticeCreate_NTF518_PendingRequestIsNotANotice(t *testing.T) {
	_, e := newAPI(t)
	op, err := e.internal().Create(as(t, usrOp, "2"), req01())
	noticeID := requireAccepted(t, op, err)
	_, err = e.internal().Get(as(t, usrOp, "2"), &notifyv1.GetInternalNoticeRequest{NoticeId: noticeID})
	requireRefusal(t, "Get заявки", err, codes.NotFound, "Notice "+noticeID+" not found", "RESOURCE_NOT_FOUND")
	l, err := e.internal().List(as(t, usrOp, "2"), &notifyv1.ListInternalNoticesRequest{})
	if err != nil || len(l.GetNotices()) != 0 {
		t.Fatalf("внутренний List при одной заявке: %v err=%v", idsOf(l.GetNotices()), err)
	}
}

// TestInternalNoticeCreate_NTF514_HundredAccountsAccepted — граница 100
// принимается (близнец NTF5-13 — 101 отвергается); заявка несёт все 100 в
// порядке запроса.
func TestInternalNoticeCreate_NTF514_HundredAccountsAccepted(t *testing.T) {
	w, e := newAPI(t)
	req := req01()
	req.Audience = audienceAccounts(hundredAndOneAccounts()[:100]...)
	op, err := e.internal().Create(as(t, usrOp, "2"), req)
	noticeID := requireAccepted(t, op, err)
	var got []string
	if err := w.pool.QueryRow(context.Background(),
		`SELECT audience_account_ids FROM notice_create_requests WHERE notice_id = $1`, noticeID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if !equalIDs(got, req.GetAudience().GetAccountIds()) {
		t.Fatalf("заявка несёт %d аккаунтов (%v…), ожидались 100 в порядке запроса", len(got), got[:min(3, len(got))])
	}
}

// TestInternalNoticeCreate_NTF520_FiftyReferencesAccepted — 50 ссылок, одна —
// на несуществующую машину, принимаются без проверки существования; порядок
// хранится как прислан (близнец NTF5-21 (а), NTF5-109 (г), (д)).
func TestInternalNoticeCreate_NTF520_FiftyReferencesAccepted(t *testing.T) {
	w, e := newAPI(t)
	req := req02()
	req.AffectedResources = machines(50)
	op, err := e.internal().Create(as(t, usrOp, "2"), req)
	noticeID := requireAccepted(t, op, err)
	var types, refIDs []string
	if err := w.pool.QueryRow(context.Background(),
		`SELECT affected_types, affected_ids FROM notice_create_requests WHERE notice_id = $1`, noticeID).Scan(&types, &refIDs); err != nil {
		t.Fatal(err)
	}
	if !equalIDs(refIDs, idsOf(req.GetAffectedResources())) || len(types) != 50 {
		t.Fatalf("заявка несёт %d ссылок, ожидались 50 в порядке запроса", len(refIDs))
	}
}
