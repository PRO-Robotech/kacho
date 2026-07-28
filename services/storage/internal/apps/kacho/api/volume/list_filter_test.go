// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package volume_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/repomock"
)

// fakeListFilter — фейк порта per-object видимости (authzfilter.Filter). Отвечает
// «видим» только для id из allow; фиксирует аргументы, чтобы тест мог утверждать
// ФОРМУ вопроса (батч по id прочитанной СТРАНИЦЫ), а не только результат.
type fakeListFilter struct {
	allow map[string]bool
	err   error

	calls   int
	subject string
	resType string
	action  string
	gotIDs  []string
}

func (f *fakeListFilter) FilterVisibleIDs(_ context.Context, subject, resourceType, action string, ids []string) ([]string, error) {
	f.calls++
	f.subject, f.resType, f.action = subject, resourceType, action
	f.gotIDs = append([]string(nil), ids...)
	if f.err != nil {
		return nil, f.err
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if f.allow[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

// aliceCtx — ctx с principal'ом реального пользователя (то, что кладёт
// principal-extract-интерсептор обоих листенеров).
func aliceCtx() context.Context {
	return operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: "usr_alice"})
}

// volPage — фикстура страницы: три тома ОДНОГО проекта (member проекта проходит
// gateway project-tier viewer Check и видит эту страницу целиком без per-object
// фильтра — ровно то, что мы закрываем).
func volPage() []*domain.Volume {
	return []*domain.Volume{
		{ID: "vol00000000000000001", ProjectID: "prj-1", Name: "granted"},
		{ID: "vol00000000000000002", ProjectID: "prj-1", Name: "not-granted"},
		{ID: "vol00000000000000003", ProjectID: "prj-1", Name: "also-not-granted"},
	}
}

func readerReturning(page []*domain.Volume, next string) *repomock.VolumeReader {
	return &repomock.VolumeReader{
		ListFunc: func(context.Context, volume.Pagination) ([]*domain.Volume, string, error) {
			return page, next, nil
		},
	}
}

func newListUC(reader volume.Reader, f authzfilter.Filter) *volume.UseCase {
	return volume.New(reader, &repomock.VolumeWriter{}, &repomock.PeerClient{}, &repomock.PeerClient{},
		nil, serviceerr.ToStatus).WithListFilter(f)
}

// TestList_HidesVolumesWithoutPerObjectGrant — ГЛАВНАЯ регрессия дыры видимости.
//
// Субъект — член проекта (gateway пропустил его project-tier `viewer` Check), но
// per-object грант у него есть ТОЛЬКО на один том из трёх. До фикса storage не
// спрашивал про объекты вообще и отдавал все три (любой член проекта видел КАЖДЫЙ
// том/снимок/образ проекта — over-show, CWE-862).
func TestList_HidesVolumesWithoutPerObjectGrant(t *testing.T) {
	page := volPage()
	f := &fakeListFilter{allow: map[string]bool{"vol00000000000000001": true}}
	uc := newListUC(readerReturning(page, "next-tok"), f)

	got, next, err := uc.List(aliceCtx(), volume.Pagination{ProjectID: "prj-1", PageSize: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != "vol00000000000000001" {
		ids := make([]string, 0, len(got))
		for _, v := range got {
			ids = append(ids, v.ID)
		}
		t.Fatalf("List returned %v, want only the granted volume [vol00000000000000001] — "+
			"a project member must NOT see volumes they hold no per-object grant on", ids)
	}
	// next_page_token берётся от последней ПРОСМОТРЕННОЙ строки — фильтрация
	// страницы не ломает курсорный обход.
	if next != "next-tok" {
		t.Fatalf("next page token = %q, want %q (cursor must survive filtering)", next, "next-tok")
	}

	// Форма вопроса: ОДИН батч по id прочитанной страницы (а не «перечисли всё,
	// что субъекту можно» — тот приём даёт усечение сверх предела ListObjects).
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
	if len(f.gotIDs) != len(page) {
		t.Fatalf("filter got %d ids, want the whole page (%d)", len(f.gotIDs), len(page))
	}
}

// TestList_KeepsGrantedVolumesInCursorOrder — обратная сторона: субъект видит те
// тома, на которые грант есть, в исходном порядке курсора.
func TestList_KeepsGrantedVolumesInCursorOrder(t *testing.T) {
	page := volPage()
	f := &fakeListFilter{allow: map[string]bool{
		"vol00000000000000003": true,
		"vol00000000000000001": true,
	}}
	uc := newListUC(readerReturning(page, ""), f)

	got, _, err := uc.List(aliceCtx(), volume.Pagination{ProjectID: "prj-1", PageSize: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"vol00000000000000001", "vol00000000000000003"}
	if len(got) != len(want) {
		t.Fatalf("List returned %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("row %d = %s, want %s (cursor order must be preserved)", i, got[i].ID, want[i])
		}
	}
}

// TestList_FilterErrorIsFailClosed — ошибка iam НИКОГДА не отдаёт нефильтрованную
// страницу (fail-closed, security.md).
func TestList_FilterErrorIsFailClosed(t *testing.T) {
	f := &fakeListFilter{err: status.Error(codes.Unavailable, "list filter: iam unreachable")}
	uc := newListUC(readerReturning(volPage(), ""), f)

	got, _, err := uc.List(aliceCtx(), volume.Pagination{ProjectID: "prj-1", PageSize: 50})
	if err == nil {
		t.Fatalf("List must fail closed on filter error, returned %d rows", len(got))
	}
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("List error code = %v, want Unavailable", status.Code(err))
	}
	// Поведенческий lock, не только код: fail-closed отказ обязан доехать до клиента
	// как есть, а не быть перемолотым в фиксированный INTERNAL «internal error» —
	// иначе «iam недоступен» неотличимо от «сломался storage», и деградация фильтра
	// становится тихой.
	if msg := status.Convert(err).Message(); !strings.Contains(msg, "list filter") {
		t.Fatalf("List error message = %q, want the filter's own message preserved", msg)
	}
	if got != nil {
		t.Fatalf("List must not return rows on filter error, got %d", len(got))
	}
}

// TestList_NoPrincipalYieldsEmptyPage — запрос без caller-identity (principal не
// извлечён / system) не имеет права обойти фильтр: пустой subject = fail-closed
// пустая страница, а НЕ bypass-all.
func TestList_NoPrincipalYieldsEmptyPage(t *testing.T) {
	f := &fakeListFilter{allow: map[string]bool{
		"vol00000000000000001": true,
		"vol00000000000000002": true,
		"vol00000000000000003": true,
	}}
	uc := newListUC(readerReturning(volPage(), ""), f)

	got, _, err := uc.List(context.Background(), volume.Pagination{ProjectID: "prj-1", PageSize: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List without a caller principal returned %d rows, want 0 (fail-closed)", len(got))
	}
}

// TestList_PageSizeValidatedBeforeVisibilityShortCircuit — порядок из
// api-conventions.md: страничные параметры валидируются ДО любого authz-решения,
// поэтому page_size > MaxPageSize даёт InvalidArgument, а не пустую страницу.
func TestList_PageSizeValidatedBeforeVisibilityShortCircuit(t *testing.T) {
	f := &fakeListFilter{}
	reader := &repomock.VolumeReader{
		ListFunc: func(context.Context, volume.Pagination) ([]*domain.Volume, string, error) {
			t.Fatal("repo.List must not run on an invalid page_size")
			return nil, "", nil
		},
	}
	uc := newListUC(reader, f)

	// Даже БЕЗ principal'а (где видимость короткозамкнулась бы в пустую страницу)
	// формат обязан отвергаться первым.
	_, _, err := uc.List(context.Background(), volume.Pagination{
		ProjectID: "prj-1", PageSize: corevalidate.MaxPageSize + 1,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("page_size=%d code = %v, want InvalidArgument (format before authz short-circuit)",
			corevalidate.MaxPageSize+1, status.Code(err))
	}
	if f.calls != 0 {
		t.Fatalf("filter must not be consulted for a malformed request (calls=%d)", f.calls)
	}
}

// TestList_PageTokenValidatedBeforeVisibilityShortCircuit — тот же порядок для
// мусорного page_token: страница читается (и репозиторий отвергает токен) ДО
// короткого замыкания на пустом гранте / пустом субъекте.
func TestList_PageTokenValidatedBeforeVisibilityShortCircuit(t *testing.T) {
	f := &fakeListFilter{}
	reader := &repomock.VolumeReader{
		ListFunc: func(_ context.Context, p volume.Pagination) ([]*domain.Volume, string, error) {
			if p.PageToken == "" {
				t.Fatal("page token must reach the repo")
			}
			return nil, "", fmt.Errorf("%w: invalid page_token", storageerr.ErrInvalidArg)
		},
	}
	uc := newListUC(reader, f)

	_, _, err := uc.List(context.Background(), volume.Pagination{
		ProjectID: "prj-1", PageSize: 50, PageToken: "!!!garbage!!!",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("garbage page_token code = %v, want InvalidArgument even with an empty grant/subject",
			status.Code(err))
	}
}

// TestList_NilFilterIsPassthrough — фильтр не сконфигурирован (dev / list-filter
// disabled) → страница отдаётся как есть; production boot-guard запрещает такую
// посадку на развёрнутом стенде (config.Validate).
func TestList_NilFilterIsPassthrough(t *testing.T) {
	uc := newListUC(readerReturning(volPage(), ""), nil)
	got, _, err := uc.List(aliceCtx(), volume.Pagination{ProjectID: "prj-1", PageSize: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("nil filter must be passthrough, got %d rows want 3", len(got))
	}
}

// TestList_EmptyPageSkipsIAM — пустая страница не стоит round-trip'а в iam.
func TestList_EmptyPageSkipsIAM(t *testing.T) {
	f := &fakeListFilter{}
	uc := newListUC(readerReturning(nil, ""), f)
	got, _, err := uc.List(aliceCtx(), volume.Pagination{ProjectID: "prj-1", PageSize: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 || f.calls != 0 {
		t.Fatalf("empty page: rows=%d filter-calls=%d, want 0/0", len(got), f.calls)
	}
}
