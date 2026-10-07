// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// public_read_integration_test.go — чтение арендатором NoticeService
// (приёмка NTF-5 группа D, Р16; замысел З13, З14; полоса N2): методы
// освобождены у края, право судит `Check` в use-case (authzcheck.RequireScope),
// форма области — до права, сужение затронутых ресурсов — сужателем на каждый
// тип ссылок, окно положительных вердиктов — из ручки.

import (
	"sort"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"

	notifyv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/notify/v1"
)

// set42 — «Дано» NTF5-42: N1 → prj-1; N2 → все аккаунты; N3 → acc-1;
// N4 → prj-2 (аккаунт acc-2); N5 → acc-2. Все — плановые работы с окном NTF5-01,
// приняты при T0 (createdAt равны, порядок — по id).
type set42 struct{ n1, n2, n3, n4, n5 noticeSeed }

func seed42(t *testing.T, w *world) set42 {
	t.Helper()
	w.kaname.grant(question(usrC, "v_get", "project:prj-1"))
	all := maintenance01()
	all.AllAccounts, all.Audience = true, nil
	return set42{
		n1: seed(t, w.pool, maintenance01(projectScope("prj-1", "acc-1"))),
		n2: seed(t, w.pool, all),
		n3: seed(t, w.pool, maintenance01(accountScope("acc-1"))),
		n4: seed(t, w.pool, maintenance01(projectScope("prj-2", "acc-2"))),
		n5: seed(t, w.pool, maintenance01(accountScope("acc-2"))),
	}
}

func listProject(t *testing.T, e edge, user, project string) (*notifyv1.ListNoticesResponse, error) {
	return e.public().List(as(t, user, "2"), &notifyv1.ListNoticesRequest{ProjectId: project})
}

func listAccount(t *testing.T, e edge, user, account string) (*notifyv1.ListNoticesResponse, error) {
	return e.public().ListByAccount(as(t, user, "2"), &notifyv1.ListNoticesByAccountRequest{AccountId: account})
}

func getProject(t *testing.T, e edge, user, id, project string) (*notifyv1.Notice, error) {
	return e.public().Get(as(t, user, "2"), &notifyv1.GetNoticeRequest{NoticeId: id, ProjectId: project})
}

func getAccount(t *testing.T, e edge, user, id, account string) (*notifyv1.Notice, error) {
	return e.public().GetByAccount(as(t, user, "2"), &notifyv1.GetNoticeByAccountRequest{NoticeId: id, AccountId: account})
}

func requireList(t *testing.T, what string, r interface {
	GetNextPageToken() string
}, got, want []string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: отказ %v, ожидалась страница %v", what, err, want)
	}
	if !equalIDs(got, want) {
		t.Fatalf("%s: notices = %v, ожидалось %v", what, got, want)
	}
	if r.GetNextPageToken() != "" {
		t.Fatalf("%s: nextPageToken %q на единственной странице", what, r.GetNextPageToken())
	}
}

func TestNoticeList_NTF542_ProjectSeesProjectAndAllAccounts(t *testing.T) {
	w, e := newAPI(t)
	s := seed42(t, w)
	r, err := listProject(t, e, usrC, "prj-1")
	requireList(t, "NTF5-42", r, idsOf(r.GetNotices()), byCreated(s.n1, s.n2), err)
	for _, n := range r.GetNotices() {
		if n.GetStartedAt() != nil || n.GetCompletedAt() != nil || n.GetCancelledAt() != nil {
			t.Fatalf("незаданный момент заполнен у %s", n.GetId())
		}
		if len(n.GetAffectedResources()) != 0 {
			t.Fatalf("List заполнил affectedResources у %s", n.GetId())
		}
		if n.GetKind() != notifyv1.Notice_MAINTENANCE || n.GetCategory() != notifyv1.Notice_OPERATIONS ||
			n.GetState() != notifyv1.Notice_SCHEDULED || n.GetCreatedAt() == nil || n.GetUpdatedAt() == nil ||
			n.GetStartsAt() == nil || n.GetEndsAt() == nil {
			t.Fatalf("публичная проекция %s без обязательных полей: %v", n.GetId(), n)
		}
	}
}

func TestNoticeListByAccount_NTF543_AccountSeesItsProjectsAndAllAccounts(t *testing.T) {
	w, e := newAPI(t)
	s := seed42(t, w)
	mark := w.kaname.mark()
	r, err := listAccount(t, e, usrC, "acc-1")
	requireList(t, "NTF5-43", r, idsOf(r.GetNotices()), byCreated(s.n1, s.n2, s.n3), err)
	calls := w.kaname.since(mark)
	if q := checkQuestions(calls); len(q) != 1 || q[0] != question(usrC, "v_get", "account:acc-1") {
		t.Fatalf("вопросы notify-api о праве: %v, ожидался ровно один Check(user:usr-c, v_get, account:acc-1)", q)
	}
	if dc := directoryCalls(calls); len(dc) != 0 {
		t.Fatalf("чтение позвало справочник: %+v", dc)
	}
}

func TestNoticeRead_NTF544_ForeignProjectIsDeniedBeforeTheSelect(t *testing.T) {
	w, e := newAPI(t)
	s := seed42(t, w)
	t.Run("(а) List prj-2", func(t *testing.T) {
		r, err := listProject(t, e, usrC, "prj-2")
		if err == nil || refusalOf(err).Code != codes.PermissionDenied {
			t.Fatalf("NTF5-44 (а): ответ %v err=%v, ожидался PERMISSION_DENIED", idsOf(r.GetNotices()), err)
		}
	})
	t.Run("(б) Get N4 prj-2", func(t *testing.T) {
		mark := w.kaname.mark()
		n, err := getProject(t, e, usrC, s.n4.ID, "prj-2")
		if status := refusalOf(err); err == nil || status.Code != codes.PermissionDenied {
			t.Fatalf("NTF5-44 (б): ответ %v/%v, ожидался PERMISSION_DENIED — не 404 и не тело N4", n.GetId(), err)
		}
		calls := w.kaname.since(mark)
		if q := checkQuestions(calls); len(q) != 1 || q[0] != question(usrC, "v_get", "project:prj-2") {
			t.Fatalf("NTF5-44 (б): вопросы о праве %v, ожидался ровно один Check(user:usr-c, v_get, project:prj-2)", q)
		}
		if b := batchQuestions(calls); len(b) != 0 {
			t.Fatalf("NTF5-44 (б): пакетная проверка ссылок до права: %v", b)
		}
	})
	t.Run("близнец (б): v_get на prj-2", func(t *testing.T) {
		w.kaname.grant(question(usrC, "v_get", "project:prj-2"))
		n, err := getProject(t, e, usrC, s.n4.ID, "prj-2")
		if err != nil || n.GetId() != s.n4.ID {
			t.Fatalf("с правом на prj-2 Get N4: %v err=%v", n.GetId(), err)
		}
	})
}

// batchItems — множество проверок пакета: «тип → перечень объектов».
func batchItems(qs []string) map[string][]string {
	out := map[string][]string{}
	for _, b := range qs {
		var objs []string
		typ := ""
		for _, item := range strings.Split(b, "; ") {
			f := strings.Fields(item)
			obj := f[len(f)-1]
			typ = obj[:strings.IndexByte(obj, ':')]
			objs = append(objs, obj[strings.IndexByte(obj, ':')+1:])
		}
		sort.Strings(objs)
		out[typ] = append(out[typ], strings.Join(objs, ","))
	}
	return out
}

func seed45(t *testing.T, w *world, third affected) noticeSeed {
	t.Helper()
	w.kaname.grant(question(usrC, "v_get", "project:prj-1"),
		"user:"+usrC+" v_get compute_instance:ins-r1")
	n := maintenance01(projectScope("prj-1", "acc-1"))
	n.Affected = []affected{{"compute_instance", "ins-r1"}, {"compute_instance", "ins-r2"}, third}
	return seed(t, w.pool, n)
}

func requireNarrowed(t *testing.T, what string, n *notifyv1.Notice, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	got := n.GetAffectedResources()
	if len(got) != 1 || got[0].GetType() != "compute_instance" || got[0].GetId() != "ins-r1" {
		t.Fatalf("%s: affectedResources = %v, ожидалось ровно [{compute_instance, ins-r1}]", what, got)
	}
}

func TestNoticeGet_NTF545_ReferencesAreNarrowedByVGet(t *testing.T) {
	t.Run("база: один тип — одна пакетная проверка", func(t *testing.T) {
		w, e := newAPI(t)
		n1 := seed45(t, w, affected{"compute_instance", "ins-gone"})
		mark := w.kaname.mark()
		n, err := getProject(t, e, usrC, n1.ID, "prj-1")
		requireNarrowed(t, "NTF5-45", n, err)
		calls := w.kaname.since(mark)
		items := batchItems(batchQuestions(calls))
		if len(items) != 1 || len(items["compute_instance"]) != 1 || items["compute_instance"][0] != "ins-gone,ins-r1,ins-r2" {
			t.Fatalf("NTF5-45: пакетные проверки %v, ожидалась ровно одна о трёх ссылках N1", batchQuestions(calls))
		}
		for _, c := range calls {
			if strings.HasPrefix(c.Question, "service:notify") {
				t.Fatalf("NTF5-45: вопрос от service:notify о ресурсах модулей: %+v", c)
			}
			if c.Method == methodBatchCheck && !strings.Contains(c.Question, "user:"+usrC+" v_get") {
				t.Fatalf("NTF5-45: пакетная проверка не о user:usr-c v_get: %q", c.Question)
			}
		}
	})
	t.Run("(а) две разных типа — по проверке на тип", func(t *testing.T) {
		w, e := newAPI(t)
		n1 := seed45(t, w, affected{"vpc_network", "net-gone"})
		mark := w.kaname.mark()
		n, err := getProject(t, e, usrC, n1.ID, "prj-1")
		requireNarrowed(t, "NTF5-45 (а)", n, err)
		items := batchItems(batchQuestions(w.kaname.since(mark)))
		if len(items) != 2 || len(items["compute_instance"]) != 1 || items["compute_instance"][0] != "ins-r1,ins-r2" ||
			len(items["vpc_network"]) != 1 || items["vpc_network"][0] != "net-gone" {
			t.Fatalf("NTF5-45 (а): пакетные проверки %v, ожидались две — compute_instance {ins-r1, ins-r2} и vpc_network {net-gone}", items)
		}
	})
}

func TestNoticeGet_NTF546_InvisibleNoticeIsIndistinguishableFromMissing(t *testing.T) {
	w, e := newAPI(t)
	s := seed42(t, w)
	var bodies []string
	for _, id := range []string{s.n4.ID, s.n5.ID, s.n3.ID, "ntc-00000000000000000"} {
		_, err := getProject(t, e, usrC, id, "prj-1")
		requireRefusal(t, "NTF5-46 "+id, err, codes.NotFound, "Notice "+id+" not found", "")
		r := refusalOf(err)
		bodies = append(bodies, strings.ReplaceAll(r.Message, id, "<id>")+"|"+strings.Join(r.Reasons, ","))
	}
	for i := 1; i < len(bodies); i++ {
		if bodies[i] != bodies[0] {
			t.Fatalf("NTF5-46: тела отказа после подстановки id различаются: %q vs %q", bodies[0], bodies[i])
		}
	}
}

func TestNoticeLists_NTF547_GarbageTokenAndPageSize(t *testing.T) {
	w, e := newAPI(t)
	s := seed42(t, w)
	type call func(size int64, token string) ([]string, string, error)
	pub := func(size int64, token string) ([]string, string, error) {
		r, err := e.public().List(as(t, usrC, "2"), &notifyv1.ListNoticesRequest{ProjectId: "prj-1", PageSize: size, PageToken: token})
		return idsOf(r.GetNotices()), r.GetNextPageToken(), err
	}
	acc := func(size int64, token string) ([]string, string, error) {
		r, err := e.public().ListByAccount(as(t, usrC, "2"), &notifyv1.ListNoticesByAccountRequest{AccountId: "acc-1", PageSize: size, PageToken: token})
		return idsOf(r.GetNotices()), r.GetNextPageToken(), err
	}
	internal := func(size int64, token string) ([]string, string, error) {
		r, err := e.internal().List(as(t, usrOp, "2"), &notifyv1.ListInternalNoticesRequest{PageSize: size, PageToken: token})
		return idsOf(r.GetNotices()), r.GetNextPageToken(), err
	}
	surfaces := []struct {
		letters [3]string
		c       call
		want    []string
	}{
		{[3]string{"(а)", "(б)", "(в)"}, pub, byCreated(s.n1, s.n2)},
		{[3]string{"(г)", "(д)", "(е)"}, acc, byCreated(s.n1, s.n2, s.n3)},
		{[3]string{"(ж)", "(з)", "(и)"}, internal, byCreated(s.n1, s.n2, s.n3, s.n4, s.n5)},
	}
	for _, sf := range surfaces {
		bad := []struct {
			letter, field string
			size          int64
			token         string
			twinSize      int64
		}{
			{sf.letters[0], "page_size", 1001, "", 1000},
			{sf.letters[1], "page_size", -1, "", 0},
			{sf.letters[2], "page_token", 0, "%%%", 0},
		}
		for _, b := range bad {
			t.Run(b.letter, func(t *testing.T) {
				_, _, err := sf.c(b.size, b.token)
				r := refusalOf(err)
				if err == nil || r.Code != codes.InvalidArgument || !strings.Contains(r.Message, b.field) {
					t.Fatalf("NTF5-47 %s: ответ %s %q, ожидался INVALID_ARGUMENT с полем %s", b.letter, r.Code, r.Message, b.field)
				}
				got, next, err := sf.c(b.twinSize, "")
				if err != nil || !equalIDs(got, sf.want) || next != "" {
					t.Fatalf("NTF5-47 близнец %s: %v next=%q err=%v, ожидалось %v одной страницей", b.letter, got, next, err, sf.want)
				}
			})
		}
	}
}

func TestNoticeList_NTF548_SameSecondIsOrderedByID(t *testing.T) {
	w, e := newAPI(t)
	w.kaname.grant(question(usrC, "v_get", "project:prj-1"))
	var s []noticeSeed
	for i := 0; i < 3; i++ {
		n := maintenance01()
		n.AllAccounts, n.Audience = true, nil
		s = append(s, seed(t, w.pool, n))
	}
	want := byCreated(s...)
	p1, err := e.public().List(as(t, usrC, "2"), &notifyv1.ListNoticesRequest{ProjectId: "prj-1", PageSize: 2})
	if err != nil || !equalIDs(idsOf(p1.GetNotices()), want[:2]) || p1.GetNextPageToken() == "" {
		t.Fatalf("NTF5-48: первая страница %v next=%q err=%v, ожидалось %v и непустой курсор", idsOf(p1.GetNotices()), p1.GetNextPageToken(), err, want[:2])
	}
	p2, err := e.public().List(as(t, usrC, "2"), &notifyv1.ListNoticesRequest{ProjectId: "prj-1", PageSize: 2, PageToken: p1.GetNextPageToken()})
	if err != nil || !equalIDs(idsOf(p2.GetNotices()), want[2:]) || p2.GetNextPageToken() != "" {
		t.Fatalf("NTF5-48: вторая страница %v next=%q err=%v, ожидалось %v и пустой курсор", idsOf(p2.GetNotices()), p2.GetNextPageToken(), err, want[2:])
	}
}

func TestNoticeReadByAccount_NTF589_NoVGetOnAccountIsDenied(t *testing.T) {
	w, e := newAPI(t)
	s := seed42(t, w)
	_, err := listAccount(t, e, usrX, "acc-1")
	if refusalOf(err).Code != codes.PermissionDenied || err == nil {
		t.Fatalf("NTF5-89 ListByAccount: %v, ожидался PERMISSION_DENIED", err)
	}
	_, err = getAccount(t, e, usrX, s.n3.ID, "acc-1")
	if refusalOf(err).Code != codes.PermissionDenied || err == nil {
		t.Fatalf("NTF5-89 GetByAccount: %v, ожидался PERMISSION_DENIED", err)
	}
}

func TestNoticeRead_NTF590_AccessServiceUnreachableRefusesInsteadOfEmptying(t *testing.T) {
	w, e := newAPI(t)
	s := seed42(t, w)
	// Близнец NTF5-42: путь цел — страница.
	r, err := listProject(t, e, usrC, "prj-1")
	requireList(t, "NTF5-90 близнец", r, idsOf(r.GetNotices()), byCreated(s.n1, s.n2), err)

	w.path.Cut()
	calls := map[string]func() error{
		"List prj-1": func() error {
			r, err := listProject(t, e, usrC, "prj-1")
			if err == nil {
				t.Errorf("List при разрыве: страница %v вместо отказа", idsOf(r.GetNotices()))
			}
			return err
		},
		"ListByAccount acc-1": func() error {
			r, err := listAccount(t, e, usrC, "acc-1")
			if err == nil {
				t.Errorf("ListByAccount при разрыве: страница %v вместо отказа", idsOf(r.GetNotices()))
			}
			return err
		},
		"Get N1 prj-1": func() error {
			n, err := getProject(t, e, usrC, s.n1.ID, "prj-1")
			if err == nil {
				t.Errorf("Get при разрыве: тело %s вместо отказа", n.GetId())
			}
			return err
		},
	}
	for name, call := range calls {
		r := refusalOf(call())
		if r.Code != codes.Unavailable || !contains(r.Reasons, "PEER_UNAVAILABLE") {
			t.Errorf("NTF5-90 %s: отказ %s %q reasons=%v, ожидался UNAVAILABLE с reason=PEER_UNAVAILABLE", name, r.Code, r.Message, r.Reasons)
		}
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestNoticeGetByAccount_NTF591_AccountSeesItsNoticeAndItsProjectNotice(t *testing.T) {
	w, e := newAPI(t)
	s := seed42(t, w)
	for _, n := range []noticeSeed{s.n3, s.n1} {
		got, err := getAccount(t, e, usrC, n.ID, "acc-1")
		if err != nil || got.GetId() != n.ID {
			t.Fatalf("NTF5-91 GetByAccount %s: %v err=%v", n.ID, got.GetId(), err)
		}
	}
}

func TestNoticeRead_NTF5110_ScopeFormIsCheckedBeforeTheRight(t *testing.T) {
	w, e := newAPI(t)
	s := seed42(t, w)
	mark := w.kaname.mark()
	_, err := listProject(t, e, usrC, "bad-1")
	requireRefusal(t, "NTF5-110 (а)", err, codes.InvalidArgument, "invalid project id 'bad-1'", "")
	_, err = listAccount(t, e, usrC, "bad-1")
	requireRefusal(t, "NTF5-110 (б)", err, codes.InvalidArgument, "invalid account id 'bad-1'", "")
	_, err = listProject(t, e, usrC, "")
	requireRefusal(t, "NTF5-110 (в)", err, codes.InvalidArgument, "project_id: required", "")
	_, err = getProject(t, e, usrC, s.n1.ID, "bad-1")
	requireRefusal(t, "NTF5-110 (г)", err, codes.InvalidArgument, "invalid project id 'bad-1'", "")
	if calls := w.kaname.since(mark); len(calls) != 0 {
		t.Fatalf("NTF5-110: проверка формы спросила службу доступа: %+v", calls)
	}
	r, err := listProject(t, e, usrC, "prj-1")
	requireList(t, "NTF5-110 близнец", r, idsOf(r.GetNotices()), byCreated(s.n1, s.n2), err)
}

// TestNoticeGet_NTF5120_NarrowerWindowIsTheKnobNotTheDefault — окно сужателя
// 2 с (ручка) против умолчания фундамента 5 с; часы сужателя — часы notify.
func TestNoticeGet_NTF5120_NarrowerWindowIsTheKnobNotTheDefault(t *testing.T) {
	w := g0(t)
	e := raise(t, w, apiKnobs{reminderLead: 24 * time.Hour, listFilterTTL: 2 * time.Second})
	n1 := seed45(t, w, affected{"compute_instance", "ins-gone"})
	tm := t0.Add(time.Hour)
	w.clock.Set(tm)
	n, err := getProject(t, e, usrC, n1.ID, "prj-1")
	requireNarrowed(t, "NTF5-120 в момент T", n, err)
	w.kaname.revoke("user:" + usrC + " v_get compute_instance:ins-r1")

	asksAboutR1 := func(calls []kanameCall) bool {
		for _, c := range calls {
			if c.Method == methodBatchCheck && strings.Contains(c.Question, "user:"+usrC+" v_get compute_instance:ins-r1") {
				return true
			}
		}
		return false
	}
	t.Run("(а) T+1s — в окне", func(t *testing.T) {
		w.clock.Set(tm.Add(time.Second))
		mark := w.kaname.mark()
		n, err := getProject(t, e, usrC, n1.ID, "prj-1")
		requireNarrowed(t, "NTF5-120 (а)", n, err)
		if asksAboutR1(w.kaname.since(mark)) {
			t.Fatalf("NTF5-120 (а): в окне положительного вердикта сужатель спросил об ins-r1")
		}
	})
	t.Run("(б) T+3s — окно ручки истекло", func(t *testing.T) {
		w.clock.Set(tm.Add(3 * time.Second))
		mark := w.kaname.mark()
		n, err := getProject(t, e, usrC, n1.ID, "prj-1")
		if err != nil || len(n.GetAffectedResources()) != 0 {
			t.Fatalf("NTF5-120 (б): affectedResources = %v err=%v, ожидалось [] — окно 2 с истекло", n.GetAffectedResources(), err)
		}
		if !asksAboutR1(w.kaname.since(mark)) {
			t.Fatalf("NTF5-120 (б): сужатель не спросил об ins-r1 после окна ручки")
		}
	})
}
