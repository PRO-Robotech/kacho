// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// authz_link_integration_test.go — NTF5-121: звено прав notify-api само судит
// методы InternalNoticeService по их аннотации (Р18, замысел З16 п.1), минуя
// прослойку прав края. Вызов идёт прямо на слушатель notify-api клиентом с SAN
// края и пересланным принципалом; адресат, SAN, ступень, метод и payload у
// букв одни, меняется только принципал (§9 приёмки).

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"
)

func TestInternalNoticeAuthz_NTF5121_ListenerJudgesTheAnnotation(t *testing.T) {
	w, e := newAPI(t)
	w.kaname.grant(question(usrV, "system_viewer", "cluster:cluster_root"))
	n0 := seed(t, w.pool, maintenance01())

	denied := []struct {
		letter, user, q string
		call            func() error
	}{
		{"(а)", usrC, question(usrC, "system_admin", "cluster:cluster_root"), func() error {
			_, err := e.internal().Create(as(t, usrC, "2"), req01())
			return err
		}},
		{"(б)", usrV, question(usrV, "system_admin", "cluster:cluster_root"), func() error {
			_, err := e.internal().Create(as(t, usrV, "2"), req01())
			return err
		}},
		{"(в)", usrC, question(usrC, "system_viewer", "cluster:cluster_root"), func() error {
			_, err := e.internal().Get(as(t, usrC, "2"), &notifyv1.GetInternalNoticeRequest{NoticeId: n0.ID})
			return err
		}},
	}
	for _, d := range denied {
		t.Run(d.letter+" "+d.user, func(t *testing.T) {
			pending, ops := pendingCreates(t, w.pool), notifyOperations(t, w.pool)
			mark := w.kaname.mark()
			requireRefusal(t, "NTF5-121 "+d.letter, d.call(), codes.PermissionDenied, "permission denied", "")
			if p, o := pendingCreates(t, w.pool), notifyOperations(t, w.pool); p != pending || o != ops {
				t.Fatalf("отказ звена прав, а use-case исполнился: заявок %d→%d, операций nop %d→%d", pending, p, ops, o)
			}
			if got := checkQuestions(w.kaname.since(mark)); len(got) != 1 || got[0] != d.q {
				t.Fatalf("вопросы звена прав за вызов: %v, ожидался ровно один %q", got, d.q)
			}
		})
	}

	t.Run("(г) usr-op — близнец (а), (б)", func(t *testing.T) {
		pending := pendingCreates(t, w.pool)
		op, err := e.internal().Create(as(t, usrOp, "2"), req01())
		requireAccepted(t, op, err)
		if p := pendingCreates(t, w.pool); p != pending+1 {
			t.Fatalf("заявок было %d, стало %d — ожидалось +1", pending, p)
		}
		var ptype, pid string
		if err := w.pool.QueryRow(context.Background(),
			`SELECT principal_type, principal_id FROM operations WHERE id = $1`, op.GetId()).Scan(&ptype, &pid); err != nil {
			t.Fatalf("операция %s не прочитана из базы notify: %v", op.GetId(), err)
		}
		if ptype != "user" || pid != usrOp {
			t.Fatalf("операция записана с принципалом %s:%s, ожидался user:%s", ptype, pid, usrOp)
		}
	})

	t.Run("(д) usr-v — близнец (в)", func(t *testing.T) {
		n, err := e.internal().Get(as(t, usrV, "2"), &notifyv1.GetInternalNoticeRequest{NoticeId: n0.ID})
		if err != nil {
			t.Fatalf("system_viewer не прочитал внутреннюю проекцию: %v", err)
		}
		if n.GetId() != n0.ID || n.GetCreatedBy() != "user:"+usrOp || n.GetRevision() != 1 ||
			!equalIDs(n.GetAudience().GetAccountIds(), []string{"acc-1"}) {
			t.Fatalf("внутренняя проекция без полей Р18: id=%s createdBy=%q revision=%d audience=%v",
				n.GetId(), n.GetCreatedBy(), n.GetRevision(), n.GetAudience())
		}
	})
}
