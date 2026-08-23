// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// transportMessageAllow — послабления: транспортные сообщения, у которых глагола
// сегодня нет по НАЗВАННОЙ причине, заведённой отдельной задачей.
//
// Здесь нет ни одного сообщения из тех двенадцати, что осиротели после снятия
// восьми методов vpc: они сняты вместе со своим предметом. Перечень ниже — два
// класса ДРУГОГО происхождения, найденные той же переписью и разведённые по
// задачам именно потому, что решения у них разные.
//
// Запись живёт, пока у неё есть предмет: послабление, которому больше нечего
// исключать, роняет гейт (`stale-allowance`).
var transportMessageAllow = []TransportMessageAllowance{
	// ПУСТО, И ЭТО ИСХОД, А НЕ УПУЩЕНИЕ.
	//
	// Здесь стояли 26 записей двух задач, и обе истекли ровно тем способом, ради
	// которого механизм и написан: их предмет закрыли, гейт назвал каждую
	// поимённо (`stale-allowance`), и снимает их та линия, что догоняет ствол
	// второй, — тем же изменением, которым догоняет.
	//
	// Двумя РАЗНЫМИ способами, и различие видно в самом выводе гейта:
	//   * kacho#580 (15 записей) и пять метаданных kacho#581 — «сообщения нет в
	//     дереве»: сняты с контракта, разрывы объявлены в proto/declared-breaks.yaml;
	//   * шесть метаданных kacho#581 — гейт называет их КООРДИНАТОЙ ФАЙЛА: сами
	//     сообщения на месте, у них появился глагол. Их писал прод-код, а контракт
	//     о них молчал, поэтому исход был обратный — не снять, а объявить.
	//
	// Пустой перечень означает «сообщений без глагола в дереве нет». Появится
	// класс, который нельзя закрыть сразу, — его имена встанут сюда с причиной и
	// задачей, и та же механика снимет их снова.

	// ── Фаза Ф1 эпика kacho#1016: форма подписки объявлена ДО своего глагола ──
	//
	// Перечень перестал быть пустым, и запись ниже — не исключение из правила
	// выше, а его применение: класс есть, закрыть его сразу нельзя, причина и
	// задача названы.
	{
		Message: "SubscriptionRequest",
		Issue:   "kacho#1018",
		Reason: "единая форма подписки объявлена фазой, которая объявляет ТОЛЬКО типы " +
			"сообщений: запрос, служебное первое сообщение потока и оболочку события. " +
			"Глагол вместе со своим сервером заводит следующая фаза — форма обязана быть " +
			"утверждена до того, как под неё пишут сервер. Объявить глагол здесь стоило бы " +
			"трёх вещей сразу: гейту монтирования понадобилось бы самоистекающее послабление " +
			"на сервис, который не монтирует ни один композиционный корень; каталог прав " +
			"получил бы запись под метод, которого никто не обслуживает, и обе вшитые копии " +
			"обязаны были бы остаться байт-идентичными; а предикат потоковых методов ушёл бы " +
			"с нуля на единицу, не отличая «поток есть» от «поток объявлен и мёртв». " +
			"ИСТЕКАЕТ САМА: как только следующая фаза объявит метод подписки, сообщение " +
			"окажется названо глаголом, и эта запись уронит прогон как истёкшая",
	},
}

func transportMessageOptions(t *testing.T, allow []TransportMessageAllowance) TransportMessageOptions {
	t.Helper()
	return TransportMessageOptions{Root: repoRoot(t), ProtoRoot: "proto", Allow: allow}
}

// TestTransportMessageIsTouchedByAVerb — главный гейт: у каждого транспортного
// сообщения контракта есть глагол, который его называет.
func TestTransportMessageIsTouchedByAVerb(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditTransportMessageReach(
		transportMessageOptions(t, transportMessageAllow), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(log.String())

	if census.ProtoFiles == 0 || census.TransportMsgs == 0 {
		t.Fatalf("предпосылка гейта не выполнена: файлов %d, транспортных сообщений %d",
			census.ProtoFiles, census.TransportMsgs)
	}
	for _, f := range findings {
		t.Errorf("%s", f)
	}
}

// Гейт обязан УМЕТЬ находить: сообщение без глагола называется поимённо.
func TestTransportMessageReach_RedOnAnUntouchedMessage(t *testing.T) {
	dir := transportFixture(t, `
service S { rpc Get (GetThingRequest) returns (Thing); }
message GetThingRequest { string id = 1; }
message Thing { string id = 1; }
message OrphanedThingRequest { string id = 1; }
`)
	findings, _, err := AuditTransportMessageReach(
		TransportMessageOptions{Root: dir, ProtoRoot: "proto"}, nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 1 || findings[0].Message != "OrphanedThingRequest" {
		t.Fatalf("ожидалась одна находка OrphanedThingRequest, получено: %v", findings)
	}
}

// …и обязан МОЛЧАТЬ на законной форме. Без этой половины гейт ловил бы имя, а
// не существо, и первый же ложный срабат его отключил бы.
func TestTransportMessageReach_SilentOnTouchedMessages(t *testing.T) {
	dir := transportFixture(t, `
service S {
  rpc Get (GetThingRequest) returns (GetThingResponse);
  rpc Create (CreateThingRequest) returns (Operation) {
    option (kacho.cloud.api.operation) = { metadata: "CreateThingMetadata" response: "Thing" };
  }
}
message GetThingRequest { string id = 1; }
message GetThingResponse { Thing thing = 1; }
message CreateThingRequest { NestedPayloadRequest payload = 1; }
message CreateThingMetadata { string id = 1; }
message NestedPayloadRequest { string v = 1; }
message Thing { string id = 1; }
message Operation { string id = 1; }
`)
	findings, census, err := AuditTransportMessageReach(
		TransportMessageOptions{Root: dir, ProtoRoot: "proto"}, nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("законная форма не должна давать находок, получено: %v", findings)
	}
	// Перепись обязана подтвердить, что рассматривала предмет, а не молчала на пустом.
	if census.TransportMsgs != 5 {
		t.Fatalf("ожидалось 5 транспортных сообщений, перепись насчитала %d", census.TransportMsgs)
	}
}

// Послабление извиняет свой предмет — и истекает, когда предмета не стало.
func TestTransportMessageReach_AllowanceExcusesAndThenExpires(t *testing.T) {
	dir := transportFixture(t, `
service S { rpc Get (GetThingRequest) returns (Thing); }
message GetThingRequest { string id = 1; }
message Thing { string id = 1; }
message OrphanedThingRequest { string id = 1; }
`)
	allow := []TransportMessageAllowance{
		{Message: "OrphanedThingRequest", Issue: "kacho#1", Reason: "проба"},
	}
	findings, _, err := AuditTransportMessageReach(
		TransportMessageOptions{Root: dir, ProtoRoot: "proto", Allow: allow}, nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("послабление обязано извинять свой предмет, получено: %v", findings)
	}

	// Тот же перечень на дереве, где предмета больше нет.
	clean := transportFixture(t, `
service S { rpc Get (GetThingRequest) returns (Thing); }
message GetThingRequest { string id = 1; }
message Thing { string id = 1; }
`)
	findings, _, err = AuditTransportMessageReach(
		TransportMessageOptions{Root: clean, ProtoRoot: "proto", Allow: allow}, nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 1 || findings[0].Kind != "stale-allowance" {
		t.Fatalf("послабление без предмета обязано быть находкой, получено: %v", findings)
	}
}

// Регрессия на две формы объявления глагола, которые СТОИЛИ ложных находок при
// первом прогоне этого гейта: потоковый ответ несёт `stream` внутри скобок, а
// объявление переносится на вторую строку. Обе живут в дереве (`Watch`,
// `Subscribe`, `InvalidateSubject`), и обе читались как «сообщение осиротело».
//
// Проба стоит здесь, а не в дереве, потому что дерево умеет ПЕРЕСТАТЬ нести
// такую форму: тогда исчезла бы не находка, а доказательство, что гейт её
// понимает.
func TestTransportMessageReach_StreamingAndWrappedRPCDeclarationsAreCounted(t *testing.T) {
	dir := transportFixture(t, `
service S {
  rpc Watch (WatchRequest) returns (stream Event) {
    option (kacho.iam.authz.v1.permission) = "x.y.watch";
  }
  rpc Invalidate (InvalidateSubjectRequest)
    returns (InvalidateSubjectResponse) {
    option (kacho.iam.authz.v1.permission) = "x.y.invalidate";
  }
  rpc Upload (stream UploadChunkRequest) returns (UploadResponse);
}
message WatchRequest { string id = 1; }
message Event { string id = 1; }
message InvalidateSubjectRequest { string id = 1; }
message InvalidateSubjectResponse { string id = 1; }
message UploadChunkRequest { string id = 1; }
message UploadResponse { string id = 1; }
`)
	findings, census, err := AuditTransportMessageReach(
		TransportMessageOptions{Root: dir, ProtoRoot: "proto"}, nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("живые сообщения потоковых и перенесённых объявлений не должны быть находками, "+
			"получено: %v", findings)
	}
	if census.TransportMsgs != 5 {
		t.Fatalf("ожидалось 5 транспортных сообщений, перепись насчитала %d", census.TransportMsgs)
	}
}

// Имя, упомянутое только в ПРОЗЕ, ссылкой не является: иначе комментарий,
// объясняющий снятие сообщения, сам бы это снятие и прятал.
func TestTransportMessageReach_MentionInProseIsNotAReference(t *testing.T) {
	dir := transportFixture(t, `
service S { rpc Get (GetThingRequest) returns (Thing); }
message GetThingRequest { string id = 1; }
message Thing { string id = 1; }
// Когда-то здесь был глагол, принимавший OrphanedThingRequest.
message OrphanedThingRequest { string id = 1; }
`)
	findings, _, err := AuditTransportMessageReach(
		TransportMessageOptions{Root: dir, ProtoRoot: "proto"}, nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if len(findings) != 1 || findings[0].Message != "OrphanedThingRequest" {
		t.Fatalf("упоминание в прозе не должно засчитываться за ссылку, получено: %v", findings)
	}
}

// Гейт проверяет СВОЮ предпосылку: на пустом дереве он обязан отказать, а не
// отчитаться нулём находок.
func TestTransportMessageReach_EmptyTreeIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "proto"), 0o755); err != nil {
		t.Fatalf("подготовка: %v", err)
	}
	if _, _, err := AuditTransportMessageReach(
		TransportMessageOptions{Root: dir, ProtoRoot: "proto"}, nil); err == nil {
		t.Fatal("пустое дерево обязано быть ошибкой, а не нулём находок")
	}
}

// Скобка в комментарии не смещает уровень вложенности: без вычистки прозы весь
// остаток файла читался бы как вложенный, и сообщения верхнего уровня после неё
// стали бы невидимы гейту.
func TestTransportMessageReach_BraceInProseDoesNotShiftNesting(t *testing.T) {
	dir := transportFixture(t, `
service S { rpc Get (GetThingRequest) returns (Thing); }
// Здесь скобка в прозе: { и она не открывает тела.
message GetThingRequest { string id = 1; }
message Thing { string id = 1; }
message OrphanedThingRequest { string id = 1; }
`)
	findings, census, err := AuditTransportMessageReach(
		TransportMessageOptions{Root: dir, ProtoRoot: "proto"}, nil)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	if census.NestedTransport != 0 {
		t.Fatalf("проза не должна порождать вложенности, насчитано %d", census.NestedTransport)
	}
	if len(findings) != 1 || findings[0].Message != "OrphanedThingRequest" {
		t.Fatalf("ожидалась одна находка, получено: %v", findings)
	}
}

func transportFixture(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	pd := filepath.Join(dir, "proto", "kacho")
	if err := os.MkdirAll(pd, 0o755); err != nil {
		t.Fatalf("подготовка: %v", err)
	}
	src := "syntax = \"proto3\";\npackage kacho.test.v1;\n" + body
	if err := os.WriteFile(filepath.Join(pd, "t.proto"), []byte(src), 0o600); err != nil {
		t.Fatalf("подготовка: %v", err)
	}
	return dir
}
