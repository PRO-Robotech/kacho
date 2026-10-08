// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

// fixture_selfcheck_test.go — положительный контроль оснастки без испытуемого.
// Каждое средство «Дано», которым пробы N2 ставят испытуемому вопрос, здесь
// исполнено и утверждено: сломанная оснастка краснеет сама и своим текстом, а
// не выдаёт себя за отсутствующую возможность notify-api.
//
// Прогон до реализации — с overlay, снимающим harness_test.go и файлы проб
// (`go test -overlay`): этот файл и fixture_test.go не называют испытуемого.

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/PRO-Robotech/corelib/ids"
	"github.com/PRO-Robotech/corelib/principalwire"
	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"
)

func TestFixtureSelfcheck_KanameDoubleAnswersByTuplesAndLogsCallerSAN(t *testing.T) {
	w := g0(t)
	conn := w.kanameConn(t)
	ctx := context.Background()

	cli := iamv1.NewInternalIAMServiceClient(conn)
	granted, err := cli.Check(ctx, &iamv1.CheckRequest{SubjectId: "user:" + usrC, Relation: "v_get", Object: "account:acc-1"})
	if err != nil || !granted.GetAllowed() {
		t.Fatalf("посеянный кортеж G0 не разрешён: allowed=%v err=%v", granted.GetAllowed(), err)
	}
	denied, err := cli.Check(ctx, &iamv1.CheckRequest{SubjectId: "user:" + usrC, Relation: "v_get", Object: "project:prj-2"})
	if err != nil || denied.GetAllowed() {
		t.Fatalf("непосеянный кортеж разрешён: allowed=%v err=%v", denied.GetAllowed(), err)
	}

	batch, err := iamv1.NewAuthorizeServiceClient(conn).BatchCheck(ctx, &iamv1.BatchAuthorizeCheckRequest{
		Checks: []*iamv1.AuthorizeCheckRequest{
			{Subject: "user:" + usrC, RequiredRelation: "v_get", Resource: &iamv1.ResourceRef{Type: "account", Id: "acc-1"}},
			{Subject: "user:" + usrC, RequiredRelation: "v_get", Resource: &iamv1.ResourceRef{Type: "compute_instance", Id: "ins-r2"}},
		},
	})
	if err != nil || len(batch.GetResponses()) != 2 || !batch.GetResponses()[0].GetAllowed() || batch.GetResponses()[1].GetAllowed() {
		t.Fatalf("пакетная проверка подмены ответила не по кортежам: %v err=%v", batch.GetResponses(), err)
	}

	calls := w.kaname.calls()
	if len(calls) != 3 {
		t.Fatalf("журнал подмены — %d строк, ожидалось 3: %+v", len(calls), calls)
	}
	for _, c := range calls {
		if c.PeerSAN != apiSAN {
			t.Fatalf("журнал подмены записал сертификат %q вместо %q", c.PeerSAN, apiSAN)
		}
	}
	if got := checkQuestions(calls); len(got) != 2 || got[0] != question(usrC, "v_get", "account:acc-1") {
		t.Fatalf("вопросы Check в журнале: %v", got)
	}
	if got := batchQuestions(calls); len(got) != 1 ||
		got[0] != "user:usr-c v_get account:acc-1; user:usr-c v_get compute_instance:ins-r2" {
		t.Fatalf("пакетная проверка в журнале: %v", got)
	}
}

func TestFixtureSelfcheck_SelectedRefusalTouchesOnlyItsQuestion(t *testing.T) {
	w := g0(t)
	cli := iamv1.NewInternalIAMServiceClient(w.kanameConn(t))
	selected := question(usrC, "v_get", "account:acc-1")
	w.kaname.refuseOn(selected, codes.Unavailable)

	_, err := cli.Check(context.Background(), &iamv1.CheckRequest{SubjectId: "user:" + usrC, Relation: "v_get", Object: "account:acc-1"})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("отобранный вызов не получил отказ UNAVAILABLE: %v", err)
	}
	// Близнец: вопрос, отличающийся одним фактом (объект), обслужен штатно.
	r, err := cli.Check(context.Background(), &iamv1.CheckRequest{SubjectId: "user:" + usrOwn, Relation: "v_get", Object: "account:acc-1"})
	if err != nil || !r.GetAllowed() {
		t.Fatalf("неотобранный вызов не обслужен штатно: allowed=%v err=%v", r.GetAllowed(), err)
	}
}

func TestFixtureSelfcheck_UnservedMethodIsLoggedWithCallerSAN(t *testing.T) {
	w := g0(t)
	conn := w.kanameConn(t)
	const directory = "/kaname.cloud.iam.v1.InternalRecipientDirectoryService/DescribeScope"
	err := conn.Invoke(context.Background(), directory, &emptypb.Empty{}, &emptypb.Empty{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("непосеянный метод подмены ответил %v, ожидался Unimplemented", err)
	}
	dc := directoryCalls(w.kaname.calls())
	if len(dc) != 1 || dc[0].Method != directory || dc[0].PeerSAN != apiSAN {
		t.Fatalf("вызов справочника не записан журналом с методом и сертификатом: %+v", dc)
	}
}

func TestFixtureSelfcheck_CutPathRefusesAndRestores(t *testing.T) {
	w := g0(t)
	cli := iamv1.NewInternalIAMServiceClient(w.kanameConn(t))
	call := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, err := cli.Check(ctx, &iamv1.CheckRequest{SubjectId: "user:" + usrC, Relation: "v_get", Object: "account:acc-1"})
		return err
	}
	if err := call(); err != nil {
		t.Fatalf("путь цел, а вызов не прошёл: %v", err)
	}
	w.path.Cut()
	before := w.kaname.mark()
	if err := call(); status.Code(err) != codes.Unavailable {
		t.Fatalf("путь разорван, а вызов не получил UNAVAILABLE: %v", err)
	}
	if n := len(w.kaname.since(before)); n != 0 {
		t.Fatalf("через разорванный путь подмена получила %d вызовов", n)
	}
	w.path.Restore()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := call(); err == nil {
			return
		} else if time.Now().After(deadline) {
			t.Fatalf("путь восстановлен, а вызов не проходит: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// echoSide — сырой сервер с удостоверением notify-api: отдаёт пересланные
// метаданные и SAN вызывающего, чтобы проверить клиента края.
type echoSide struct {
	md  metadata.MD
	san string
}

func TestFixtureSelfcheck_EdgeForwardsPrincipalUnderGatewaySAN(t *testing.T) {
	ca := newTestCA(t)
	got := &echoSide{}
	srv := grpc.NewServer(grpc.Creds(ca.serverCreds(t, "notify-api-raw", apiSAN)),
		grpc.UnknownServiceHandler(func(_ any, ss grpc.ServerStream) error {
			got.md, _ = metadata.FromIncomingContext(ss.Context())
			got.san = peerSAN(ss.Context())
			return status.Error(codes.Unimplemented, "echo")
		}))
	addr := "127.0.0.1:" + freePort(t)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)

	e := dialEdge(t, ca, addr)
	_ = e.conn.Invoke(as(t, usrOp, "2"), "/kacho.cloud.notify.v1.InternalNoticeService/Get", &emptypb.Empty{}, &emptypb.Empty{})
	if got.san != gatewaySAN {
		t.Fatalf("клиент края предъявил SAN %q, ожидался %q", got.san, gatewaySAN)
	}
	for key, want := range map[string]string{
		principalwire.MetaPrincipalType: "user",
		principalwire.MetaPrincipalID:   usrOp,
		principalwire.MetaTokenACR:      "2",
	} {
		if v := got.md.Get(key); len(v) != 1 || v[0] != want {
			t.Fatalf("ключ %s пересланного принципала: %v, ожидалось %q", key, v, want)
		}
	}
}

func TestFixtureSelfcheck_SeedsAreJudgedBySchema(t *testing.T) {
	w := g0(t)
	if n := pendingCreates(t, w.pool); n != 0 {
		t.Fatalf("в свежей базе %d заявок создания", n)
	}
	n1 := seed(t, w.pool, maintenance01(projectScope("prj-1", "acc-1")))
	seed(t, w.pool, outage02())
	seed(t, w.pool, started27())
	all := maintenance01()
	all.AllAccounts, all.Audience = true, nil
	seed(t, w.pool, all)
	refs := outage02()
	refs.Affected = []affected{{"compute_instance", "ins-r1"}, {"vpc_network", "net-gone"}}
	seed(t, w.pool, refs)
	if n := noticeRows(t, w.pool); n != 5 {
		t.Fatalf("после посева пяти извещений строк %d", n)
	}
	if n := count(t, w.pool, `SELECT count(*) FROM notice_audience WHERE notice_id = $1 AND account_id = 'acc-1'`, n1.ID); n != 1 {
		t.Fatalf("аккаунт проекта не записан в аудиторию посева: %d", n)
	}
	if n := count(t, w.pool, `SELECT count(*) FROM notice_reminders WHERE notice_id = $1`, n1.ID); n != 1 {
		t.Fatalf("напоминание посева не записано: %d", n)
	}
	if n := stageEvents(t, w.pool, n1.ID, "scheduled"); n != 0 {
		t.Fatalf("посев пишет события этапа: %d", n)
	}

	// Близнец: строка, которой схема не допускает (MAINTENANCE без endsAt;
	// аудитория двух форм) — посев отвергнут, строки нет.
	bad := maintenance01()
	bad.EndsAt = nil
	if err := seedErr(w.pool, bad); violated(err) != "notices_ends_at_kind_chk" {
		t.Fatalf("посев строки вне схемы не отвергнут ограничением: %v", err)
	}
	mixed := maintenance01(accountScope("acc-1"), projectScope("prj-1", "acc-1"))
	if err := seedErr(w.pool, mixed); violated(err) != "notices_audience_form_chk" {
		t.Fatalf("аудитория двух форм не отвергнута триггером формы: %v", err)
	}
	if n := noticeRows(t, w.pool); n != 5 {
		t.Fatalf("отвергнутый посев оставил строки: %d", n)
	}
}

func TestFixtureSelfcheck_IDFormsAcceptGeneratorOutputOnly(t *testing.T) {
	for i := 0; i < 50; i++ {
		if op := ids.NewID(ids.PrefixOperationNotify); !opIDForm.MatchString(op) {
			t.Fatalf("форма id операции не принимает выдачу генератора: %q", op)
		}
		if n := newNoticeID(); !noticeIDForm.MatchString(n) {
			t.Fatalf("форма id извещения не принимает выдачу генератора: %q", n)
		}
	}
	for _, bad := range []string{
		"nop0123456789abcdef", "nop0123456789ABCDEFG", "ntc0123456789abcdefg", "abc0123456789abcdefg",
	} {
		if opIDForm.MatchString(bad) {
			t.Fatalf("форма id операции принимает %q", bad)
		}
	}
	for _, bad := range []string{"ntc-0123456789abcdef", "ntc-0123456789abcdefu", "ntc0123456789abcdefgh", "ins-0123456789abcdefg"} {
		if noticeIDForm.MatchString(bad) {
			t.Fatalf("форма id извещения принимает %q", bad)
		}
	}
}

func TestFixtureSelfcheck_ClockIsControlled(t *testing.T) {
	c := newClock(t0)
	if !c.Now().Equal(t0) {
		t.Fatalf("часы не стоят на T0: %v", c.Now())
	}
	time.Sleep(10 * time.Millisecond)
	if !c.Now().Equal(t0) {
		t.Fatalf("часы идут сами: %v", c.Now())
	}
	c.Set(t0.Add(3 * time.Second))
	if !c.Now().Equal(t0.Add(3 * time.Second)) {
		t.Fatalf("перевод часов не применён: %v", c.Now())
	}
}

// violated — имя ограничения, которым база отвергла запись; "" — не отвергла.
func violated(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.ConstraintName
	}
	return ""
}
