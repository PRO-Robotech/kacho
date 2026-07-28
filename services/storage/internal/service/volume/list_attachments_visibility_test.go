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

	"github.com/PRO-Robotech/kacho/services/storage/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports/portmock"
)

// ListAttachments отвечает на вопрос «какие тома привязаны к этим инстансам» — это
// зеркало, которое kacho-compute показывает на своей машине. Инстансы называет
// ВЫЗЫВАЮЩИЙ, а ответ несёт id тома, id и имя инстанса, имя устройства и признак
// загрузочного диска.
//
// Единственный вопрос, который задавался перед вызовом, — `viewer` на синглтоне
// `cluster:cluster_kacho_root`. Это отношение ГЛОБАЛЬНОГО СПРАВОЧНИКА (регионы,
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
func readerAttachments(att []*domain.VolumeAttachment) *portmock.VolumeReader {
	return &portmock.VolumeReader{
		ListAttachmentsFunc: func(context.Context, []string) ([]*domain.VolumeAttachment, error) {
			return att, nil
		},
	}
}

// countingReader — Reader, отдающий полную страницу и считающий обращения. Отдаёт
// именно ПОЛНУЮ страницу нарочно: тест обязан утверждать, что вызывающему не уехало
// ничего, а не что читать было нечего.
type countingReader struct {
	portmock.VolumeReader
	calls int
}

func newCountingReader(att []*domain.VolumeAttachment) *countingReader {
	r := &countingReader{}
	r.ListAttachmentsFunc = func(context.Context, []string) ([]*domain.VolumeAttachment, error) {
		r.calls++
		return att, nil
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

// TestListAttachments_ReturnsOnlyAttachmentsOfVolumesTheSubjectMaySee — ГЛАВНАЯ
// регрессия. Субъект вправе видеть один том из двух; назвав чужой инстанс, он не
// должен получить привязку чужого тома.
func TestListAttachments_ReturnsOnlyAttachmentsOfVolumesTheSubjectMaySee(t *testing.T) {
	f := &fakeListFilter{allow: map[string]bool{"vol00000000000000001": true}}
	uc := newListUC(readerAttachments(attPair()), f)

	got, err := uc.ListAttachments(aliceCtx(), []string{"ins-mine", "ins-theirs"})
	if err != nil {
		t.Fatalf("ListAttachments: %v", err)
	}

	// Наблюдаемое: что именно уехало вызывающему.
	if ids := volumeIDsOf(got); len(ids) != 1 || ids[0] != "vol00000000000000001" {
		t.Fatalf("ListAttachments returned %v, want only [vol00000000000000001] — a binding of a "+
			"volume the subject may not see must not come back merely because the caller named "+
			"the instance it is attached to", ids)
	}
	for _, a := range got {
		if a.InstanceID == "ins-theirs" || a.InstanceName == "theirs" || a.DeviceName == "vdb" {
			t.Fatalf("the foreign binding leaked through the response: %+v", a)
		}
	}

	// Форма вопроса: один батч по id ПРОЧИТАННОЙ страницы, тем же предикатом, что
	// энфорсит Volume.Get.
	if f.calls != 1 {
		t.Fatalf("filter calls = %d, want exactly 1 batched call per page", f.calls)
	}
	if f.subject != "user:usr_alice" {
		t.Fatalf("filter subject = %q, want %q", f.subject, "user:usr_alice")
	}
	if f.resType != authzfilter.ResourceTypeVolume || f.action != authzfilter.ActionVolumeList {
		t.Fatalf("filter asked (%q,%q), want (%q,%q)", f.resType, f.action,
			authzfilter.ResourceTypeVolume, authzfilter.ActionVolumeList)
	}
	if len(f.gotIDs) != 2 {
		t.Fatalf("filter got %v, want both volume ids of the page", f.gotIDs)
	}
}

// TestListAttachments_NoGrantReturnsNothing — грантов нет вообще → не возвращается
// ничего (и по форме ответа тоже ничего не узнать).
func TestListAttachments_NoGrantReturnsNothing(t *testing.T) {
	f := &fakeListFilter{}
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
	f := &fakeListFilter{allow: map[string]bool{
		"vol00000000000000001": true,
		"vol00000000000000002": true,
	}}
	reader := newCountingReader(attPair())
	uc := newListUC(reader, f)

	got, err := uc.ListAttachments(context.Background(), []string{"ins-mine", "ins-theirs"})
	if err != nil {
		t.Fatalf("ListAttachments: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListAttachments without a caller identity returned %v, want nothing", volumeIDsOf(got))
	}
	if f.calls != 0 {
		t.Fatalf("filter calls = %d, want 0 — with no identity there is nothing to ask the model about", f.calls)
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
	if err != nil {
		t.Fatalf("ListAttachments: %v", err)
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
	f := &fakeListFilter{allow: map[string]bool{"vol00000000000000001": true}}
	reader := newCountingReader(attPair())
	uc := newListUC(reader, f)

	ctx := operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "system", ID: "bootstrap"})
	got, err := uc.ListAttachments(ctx, []string{"ins-mine"})
	if err != nil {
		t.Fatalf("ListAttachments: %v", err)
	}
	if len(got) != 0 || f.calls != 0 || reader.calls != 0 {
		t.Fatalf("system principal: rows=%v filter-calls=%d reader-calls=%d, want none/0/0",
			volumeIDsOf(got), f.calls, reader.calls)
	}
}

// TestListAttachments_FilterErrorFailsClosed — недоступный ответ модели не есть
// ответ «да»: страница не отдаётся, отказ доезжает как есть.
func TestListAttachments_FilterErrorFailsClosed(t *testing.T) {
	f := &fakeListFilter{err: status.Error(codes.Unavailable, "list filter: iam unreachable")}
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

// TestListAttachments_EmptyPageSkipsIAM — пустой ответ БД не стоит round-trip'а.
func TestListAttachments_EmptyPageSkipsIAM(t *testing.T) {
	f := &fakeListFilter{}
	uc := newListUC(readerAttachments(nil), f)

	got, err := uc.ListAttachments(aliceCtx(), []string{"ins-mine"})
	if err != nil {
		t.Fatalf("ListAttachments: %v", err)
	}
	if len(got) != 0 || f.calls != 0 {
		t.Fatalf("empty page: rows=%v filter-calls=%d, want none/0", volumeIDsOf(got), f.calls)
	}
}
