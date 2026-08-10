// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package image_test

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/repomock"
)

func aliceCtx() context.Context {
	return operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: "usr_alice"})
}

func imgPage() []*domain.Image {
	return []*domain.Image{
		{ID: "img00000000000000001", ProjectID: "prj-1", Name: "granted"},
		{ID: "img00000000000000002", ProjectID: "prj-1", Name: "not-granted"},
	}
}

func newListUC(reader image.Reader, f *listnarrow.Narrower) *image.UseCase {
	return image.New(reader, &repomock.ImageWriter{}, &repomock.PeerClient{}, &repomock.PeerClient{},
		nil, serviceerr.ToStatus).WithListFilter(f)
}

func readerReturning(page []*domain.Image, next string) *repomock.ImageReader {
	return &repomock.ImageReader{
		ListFunc: func(context.Context, image.Pagination) ([]*domain.Image, string, error) {
			return page, next, nil
		},
	}
}

// TestList_HidesImagesWithoutPerObjectGrant — член проекта не имеет права видеть
// образ, на который у него нет per-object гранта (дыра видимости storage).
func TestList_HidesImagesWithoutPerObjectGrant(t *testing.T) {
	f, peer := narrowtest.Recording("img00000000000000001")
	uc := newListUC(readerReturning(imgPage(), "next-tok"), f)

	got, next, err := uc.List(aliceCtx(), image.Pagination{ProjectID: "prj-1", PageSize: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != "img00000000000000001" {
		ids := make([]string, 0, len(got))
		for _, i := range got {
			ids = append(ids, i.ID)
		}
		t.Fatalf("List returned %v, want only [img00000000000000001]", ids)
	}
	if next != "next-tok" {
		t.Fatalf("next page token = %q, want preserved cursor", next)
	}
	if peer.Calls != 1 || peer.Subject != "user:usr_alice" ||
		peer.ResourceType != authzfilter.ResourceTypeImage || peer.Action != authzfilter.ActionImageList {
		t.Fatalf("filter asked calls=%d subject=%q (%q,%q); want 1 batched call for (%q,%q) as user:usr_alice",
			peer.Calls, peer.Subject, peer.ResourceType, peer.Action,
			authzfilter.ResourceTypeImage, authzfilter.ActionImageList)
	}
}

// TestList_FilterErrorIsFailClosed — ошибка iam не отдаёт нефильтрованную страницу.
func TestList_FilterErrorIsFailClosed(t *testing.T) {
	f := narrowtest.Failing(status.Error(codes.Unavailable, "list filter: iam unreachable"))
	uc := newListUC(readerReturning(imgPage(), ""), f)

	got, _, err := uc.List(aliceCtx(), image.Pagination{ProjectID: "prj-1", PageSize: 50})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("List code = %v (rows=%d), want Unavailable fail-closed", status.Code(err), len(got))
	}
	if got != nil {
		t.Fatalf("List must not return rows on filter error, got %d", len(got))
	}
}

// TestList_NoPrincipalIsRefused — запрос без caller-identity получает ОТКАЗ.
//
// Полярность сменилась: прежде здесь была пустая страница. «Пусто» неотличимо от
// «личность потеряна по дороге», и именно этим неразличением класс живёт годами.
// Сужатель тут разрешает всё — значит отказ приходит именно с линии личности, а не
// «потому что всё сломано».
func TestList_NoPrincipalIsRefused(t *testing.T) {
	f, peer := narrowtest.Recording("img00000000000000001", "img00000000000000002")
	uc := newListUC(readerReturning(imgPage(), ""), f)

	got, _, err := uc.List(context.Background(), image.Pagination{ProjectID: "prj-1", PageSize: 50})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("List without a caller principal: code = %v, want Unauthenticated", status.Code(err))
	}
	if len(got) != 0 {
		t.Fatalf("List without a caller principal returned %d rows, want 0", len(got))
	}
	if peer.Calls != 0 {
		t.Fatalf("no identity means there is nothing to ask the model about (calls=%d)", peer.Calls)
	}
}

// TestList_PaginationValidatedBeforeVisibilityShortCircuit — формат страничных
// параметров отвергается ДО любого authz-решения (даже без principal'а).
func TestList_PaginationValidatedBeforeVisibilityShortCircuit(t *testing.T) {
	f, peer := narrowtest.Recording()
	reader := &repomock.ImageReader{
		ListFunc: func(_ context.Context, p image.Pagination) ([]*domain.Image, string, error) {
			if p.PageToken == "" {
				t.Fatal("page token must reach the repo")
			}
			return nil, "", fmt.Errorf("%w: invalid page_token", storageerr.ErrInvalidArg)
		},
	}
	uc := newListUC(reader, f)

	if _, _, err := uc.List(context.Background(), image.Pagination{
		ProjectID: "prj-1", PageSize: corevalidate.MaxPageSize + 1,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("page_size over max = %v, want InvalidArgument", status.Code(err))
	}
	if _, _, err := uc.List(context.Background(), image.Pagination{
		ProjectID: "prj-1", PageSize: 50, PageToken: "!!!garbage!!!",
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("garbage page_token = %v, want InvalidArgument", status.Code(err))
	}
	if peer.Calls != 0 {
		t.Fatalf("filter must not be consulted for a malformed request (calls=%d)", peer.Calls)
	}
}
