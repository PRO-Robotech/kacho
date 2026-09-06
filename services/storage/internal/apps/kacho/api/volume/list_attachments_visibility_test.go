// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package volume_test

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/repomock"
)

// ListAttachments отвечает на вопрос «какие тома привязаны к этим инстансам» — это
// зеркало, которое kacho-compute показывает на своей машине. Инстансы называет
// ВЫЗЫВАЮЩИЙ, а ответ несёт id тома, id и имя инстанса, имя устройства и признак
// загрузочного диска.
//
// Единственный вопрос, который задавался перед вызовом, — `viewer` на синглтоне
// `cluster:cluster_root`. Это отношение ГЛОБАЛЬНОГО СПРАВОЧНИКА (регионы,
// зоны, типы дисков), и bootstrap кластера намеренно пишет
// `cluster:<root>#viewer@user:*`, чтобы справочник читал любой аутентифицированный
// субъект. Значит проверка пропускала всех и отдавала привязки любых названных
// инстансов — из чужих проектов и чужих аккаунтов. Привязки томов справочными
// данными не являются.
//
// Решение переезжает на данные: страница читается из своей БД, и модель
// спрашивается про id ЭТОЙ страницы (`viewer` на `storage_volume:<id>`, батчами) —
// ровно тот предикат, которым энфорсится `Volume.Get`, и ровно та дисциплина, что
// уже стоит на публичных List этого сервиса.

// attPair — две привязки: одна к тому, который субъекту виден, вторая — к чужому.
func attPair() []*domain.VolumeAttachment {
	return []*domain.VolumeAttachment{
		{
			VolumeID:     "vol00000000000000001",
			InstanceID:   "ins-mine",
			InstanceName: "mine",
			ProjectID:    "prj-mine",
			DeviceName:   "vda",
			IsBoot:       true,
		},
		{
			VolumeID:     "vol00000000000000002",
			InstanceID:   "ins-theirs",
			InstanceName: "theirs",
			ProjectID:    "prj-theirs",
			DeviceName:   "vdb",
		},
	}
}

// readerAttachments — Reader, отдающий заданную страницу привязок.
func readerAttachments(att []*domain.VolumeAttachment) *repomock.VolumeReader {
	return &repomock.VolumeReader{
		ListAttachmentsFunc: func(_ context.Context, ids []string) ([]*domain.VolumeAttachment, error) {
			return rowsForInstances(att, ids), nil
		},
	}
}

// rowsForInstances воспроизводит то, что делает настоящий репозиторий:
// `WHERE instance_id = ANY($1)`. Фейк, игнорирующий переданные id, отдавал бы
// строки инстансов, о которых его не спрашивали, — и тест на всё-или-ничего по
// инстансу проходил бы или падал по причине, не связанной с предметом.
func rowsForInstances(att []*domain.VolumeAttachment, ids []string) []*domain.VolumeAttachment {
	want := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		want[id] = struct{}{}
	}
	out := make([]*domain.VolumeAttachment, 0, len(att))
	for _, a := range att {
		if _, ok := want[a.InstanceID]; ok {
			out = append(out, a)
		}
	}
	return out
}

// countingReader — Reader, отдающий полную страницу и считающий обращения. Отдаёт
// именно ПОЛНУЮ страницу нарочно: тест обязан утверждать, что вызывающему не уехало
// ничего, а не что читать было нечего.
type countingReader struct {
	repomock.VolumeReader
	calls int
}

func newCountingReader(att []*domain.VolumeAttachment) *countingReader {
	r := &countingReader{}
	r.ListAttachmentsFunc = func(_ context.Context, ids []string) ([]*domain.VolumeAttachment, error) {
		r.calls++
		return rowsForInstances(att, ids), nil
	}
	return r
}

func volumeIDsOf(att []*domain.VolumeAttachment) []string {
	out := make([]string, 0, len(att))
	for _, a := range att {
		out = append(out, a.VolumeID)
	}
	return out
}

// Сужение по видимости ТОМА было первой попыткой и здесь БОЛЬШЕ НЕ ПРОВЕРЯЕТСЯ: оно
// ломало снос — привязку, которую сервис отказывался вернуть, цикл отцепления
// пропускал, а строку инстанса удалял безусловно, так что том оставался занят навсегда,
// а операция рапортовала успех. Вопрос задаётся про ИНСТАНС, всё-или-ничего.
//
// Здесь лежала функция с этой снятой проверкой, переименованная так, что `go test` её
// не исполнял, — и при этом её же заголовок называл её ГЛАВНОЙ регрессией. Два
// утверждения в одном комментарии противоречили друг другу, а исполнялось из них ноль.
// Возврат исполнения показал ровно это: она падает, потому что спрашивает фильтр про
// идентификаторы ТОМОВ, тогда как сужение спрашивает про инстансы. Тот же сценарий (свой
// инстанс рядом с чужим) исполняется и проходит ниже, в
// TestListAttachments_ForeignInstanceReturnsNothing — с действующим вопросом и более
// строгим ожиданием. Снятую проверку не восстанавливают: восстановленная, она
// утверждала бы отменённое решение.

// TestListAttachments_NoGrantReturnsNothing — грантов нет вообще → не возвращается
// ничего (и по форме ответа тоже ничего не узнать).
// Код отказа безымянному вызывающему приведён к общему: `Unauthenticated`.
//
// Прежде storage отвечал здесь `PermissionDenied "permission denied"`, схлопывая
// «личность не предъявлена» и «прав нет» в один ответ. Отличие НЕ было записано
// решением ни в docs/architecture, ни где-либо ещё — его держали только эти пробы, —
// поэтому при сведении реализаций оно приведено к эталонной клетке (nlb): ответ
// обязан говорить о ЛИЧНОСТИ, а не о правах, иначе оператор ищет отсутствующую
// выдачу вместо потерянного по дороге принципала.
//
// Сужение при этом не ослаблено ни на шаг: обе линии по-прежнему ОТКАЗЫВАЮТ, ни одна
// строка не отдаётся, и модель на безымянном запросе не тревожится вовсе.

func TestListAttachments_NoGrantReturnsNothing(t *testing.T) {
	f := narrowtest.DenyingAll()
	uc := newListUC(readerAttachments(attPair()), f)

	got, err := uc.ListAttachments(aliceCtx(), []string{"ins-mine", "ins-theirs"})
	if err != nil {
		t.Fatalf("ListAttachments: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListAttachments returned %v, want nothing for a subject with no grant", volumeIDsOf(got))
	}
}

// TestListAttachments_EmptySubjectFailsClosed — не извлечённая identity значит «не
// знаю, кто ты», а не «доверенный»: страницу не отдаём и модель не спрашиваем.
func TestListAttachments_EmptySubjectFailsClosed(t *testing.T) {
	f, peer := narrowtest.Recording("vol00000000000000001", "vol00000000000000002")
	reader := newCountingReader(attPair())
	uc := newListUC(reader, f)

	got, err := uc.ListAttachments(context.Background(), []string{"ins-mine", "ins-theirs"})
	// Пустой ответ здесь означал бы «у этих инстансов привязок нет», и снос,
	// поверив, удалил бы инстанс, оставив привязку сиротой. Отказ, не пустота.
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("err = %v, want Unauthenticated — an empty answer would read as \"no attachments\"", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListAttachments without a caller identity returned %v, want nothing", volumeIDsOf(got))
	}
	if peer.Calls != 0 {
		t.Fatalf("filter calls = %d, want 0 — with no identity there is nothing to ask the model about", peer.Calls)
	}
	if reader.calls != 0 {
		t.Fatalf("rows were read (%d calls) for a caller whose identity was not extracted", reader.calls)
	}
}

// TestListAttachments_EmptySubjectFailsClosedEvenWithoutFilter — отсечка пустого
// субъекта БЕЗУСЛОВНА, а не «когда фильтр подключён».
//
// За этим RPC нет per-RPC Check (он ScopeFiltered), поэтому вызывающий без
// извлечённой identity доходит сюда. Привяжи мы fail-closed к наличию фильтра —
// осталась бы посадка, в которой RPC отдаёт привязки всего кластера кому угодно,
// то есть ровно исходная дыра.
func TestListAttachments_EmptySubjectFailsClosedEvenWithoutFilter(t *testing.T) {
	reader := newCountingReader(attPair())
	uc := newListUC(reader, nil)

	got, err := uc.ListAttachments(context.Background(), []string{"ins-mine", "ins-theirs"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("err = %v, want Unauthenticated", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListAttachments without a caller identity returned %v with no filter configured, "+
			"want nothing — the only gate of this RPC lives here", volumeIDsOf(got))
	}
	if reader.calls != 0 {
		t.Fatalf("rows were read (%d calls) for a caller whose identity was not extracted", reader.calls)
	}
}

// TestListAttachments_SystemPrincipalFailsClosed — у storage нет понятия
// «доверенный system-субъект» на этом пути: system-принципал не резолвится в
// FGA-субъекта (authzfilter.SubjectFromPrincipal → ""), поэтому он попадает в ту же
// fail-closed ветку. Зафиксировано, чтобы passthrough нельзя было ввести молча.
func TestListAttachments_SystemPrincipalFailsClosed(t *testing.T) {
	f, peer := narrowtest.Recording("vol00000000000000001")
	reader := newCountingReader(attPair())
	uc := newListUC(reader, f)

	ctx := operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "system", ID: "bootstrap"})
	got, err := uc.ListAttachments(ctx, []string{"ins-mine"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("err = %v, want Unauthenticated", err)
	}
	if len(got) != 0 || peer.Calls != 0 || reader.calls != 0 {
		t.Fatalf("system principal: rows=%v filter-calls=%d reader-calls=%d, want none/0/0",
			volumeIDsOf(got), peer.Calls, reader.calls)
	}
}

// TestListAttachments_FilterErrorFailsClosed — недоступный ответ модели не есть
// ответ «да»: страница не отдаётся, отказ доезжает как есть.
func TestListAttachments_FilterErrorFailsClosed(t *testing.T) {
	f := narrowtest.Failing(status.Error(codes.Unavailable, "list filter: iam unreachable"))
	uc := newListUC(readerAttachments(attPair()), f)

	got, err := uc.ListAttachments(aliceCtx(), []string{"ins-mine", "ins-theirs"})
	if err == nil {
		t.Fatalf("ListAttachments must fail closed on a filter error, returned %v", volumeIDsOf(got))
	}
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("error code = %v, want Unavailable", status.Code(err))
	}
	if msg := status.Convert(err).Message(); !strings.Contains(msg, "list filter") {
		t.Fatalf("error message = %q, want the filter's own message preserved", msg)
	}
	if len(got) != 0 {
		t.Fatalf("ListAttachments must not return rows on a filter error, got %v", volumeIDsOf(got))
	}
}

// TestListAttachments_NoInstancesNamedAsksNothingAndReadsNothing — вызывающий не
// назвал ни одного инстанса, значит спрашивать модель не о чем и читать нечего.
//
// Прежняя версия этого теста утверждала, что пустой ОТВЕТ БД не стоит round-trip'а
// в iam. С вопросом про инстансы такой формулировки больше нет: строки читаются
// уже ПОСЛЕ сужения, поэтому «пустой ответ БД» наступает только когда у видимых
// инстансов и правда нет привязок — утверждать там нечего. Осмысленный остаток —
// пустой вход.
func TestListAttachments_NoInstancesNamedAsksNothingAndReadsNothing(t *testing.T) {
	f, peer := narrowtest.Recording()
	reader := newCountingReader(attPair())
	uc := newListUC(reader, f)

	got, err := uc.ListAttachments(aliceCtx(), nil)
	if err != nil {
		t.Fatalf("ListAttachments: %v", err)
	}
	if len(got) != 0 || peer.Calls != 0 || reader.calls != 0 {
		t.Fatalf("no instances named: rows=%v filter-calls=%d reader-calls=%d, want none/0/0",
			volumeIDsOf(got), peer.Calls, reader.calls)
	}
}

// TestListAttachments_TeardownSeesEveryAttachmentOfAnInstanceItMayDelete is the
// completeness half, and it is the one that was missing.
//
// The only consumers of this listing act destructively on the answer. compute's
// instance teardown enumerates the attachments, detaches each returned one, and
// deletes the instance row LAST — unconditionally. So a row this service declines
// to return is indistinguishable from "this instance has no attachment": the loop
// skips it, the instance disappears, and the attachment row survives pointing at
// an instance that no longer exists. The volume stays IN USE forever, cannot be
// attached and cannot be deleted, and the operation reports success.
//
// Narrowing the page by VOLUME visibility produced exactly that. The caller's
// right to a volume and their right to the instance it hangs on are granted
// separately: revoke the volume grant while it stays attached, or let an account
// administrator delete somebody else's instance, or simply delete an instance
// whose fresh volume tuples have not been materialised yet — and the teardown
// silently leaves an orphan. Before the narrowing existed, the row came back, the
// per-object gate on Detach refused, and the operation failed CLOSED with the
// instance still alive and recoverable. A loud refusal became quiet corruption.
//
// The question this RPC must ask is the one the caller actually named: may you
// see this INSTANCE. Answer it and the page is all-or-nothing per instance —
// complete for anyone entitled to tear it down (delete implies admin implies
// viewer in the model), empty for anyone naming an instance that is not theirs.
func TestListAttachments_TeardownSeesEveryAttachmentOfAnInstanceItMayDelete(t *testing.T) {
	// The subject may see the instance. One of its two volumes is not separately
	// visible to them — irrelevant to whether the instance can be torn down.
	f, _ := narrowtest.Recording("ins-mine")
	uc := newListUC(readerAttachments([]*domain.VolumeAttachment{
		{VolumeID: "vol00000000000000001", InstanceID: "ins-mine", ProjectID: "prj-mine", DeviceName: "vda", IsBoot: true},
		{VolumeID: "vol00000000000000009", InstanceID: "ins-mine", ProjectID: "prj-mine", DeviceName: "vdb"},
	}), f)

	got, err := uc.ListAttachments(aliceCtx(), []string{"ins-mine"})
	if err != nil {
		t.Fatalf("ListAttachments: %v", err)
	}
	if len(got) != 2 {
		var ids []string
		for _, a := range got {
			ids = append(ids, a.VolumeID)
		}
		t.Fatalf("teardown got %v (%d rows), want both attachments of an instance the caller may delete — "+
			"a withheld row is skipped by the detach loop and orphaned when the instance row is deleted", ids, len(got))
	}
}

// TestListAttachments_ForeignInstanceReturnsNothing is the disclosure half: the
// caller names an instance that is not theirs and learns nothing about it. This
// is the defect the narrowing was introduced for, restated against the instance.
func TestListAttachments_ForeignInstanceReturnsNothing(t *testing.T) {
	f, _ := narrowtest.Recording("ins-mine")
	uc := newListUC(readerAttachments(attPair()), f) // ins-mine + ins-theirs

	got, err := uc.ListAttachments(aliceCtx(), []string{"ins-mine", "ins-theirs"})
	if err != nil {
		t.Fatalf("ListAttachments: %v", err)
	}
	for _, a := range got {
		if a.InstanceID == "ins-theirs" {
			t.Fatalf("returned a binding of an instance the caller may not see: %+v", a)
		}
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, want the single binding of the instance the caller may see", len(got))
	}
}
