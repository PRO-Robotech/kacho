// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package snapshot_test

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/authzfilter"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports/portmock"
)

// fakeListFilter — фейк порта per-object видимости (authzfilter.Filter).
type fakeListFilter struct {
	allow map[string]bool
	err   error

	calls   int
	subject string
	resType string
	action  string
}

func (f *fakeListFilter) FilterVisibleIDs(_ context.Context, subject, resourceType, action string, ids []string) ([]string, error) {
	f.calls++
	f.subject, f.resType, f.action = subject, resourceType, action
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

func aliceCtx() context.Context {
	return operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "user", ID: "usr_alice"})
}

func snapPage() []*domain.Snapshot {
	return []*domain.Snapshot{
		{ID: "snp00000000000000001", ProjectID: "prj-1", Name: "granted"},
		{ID: "snp00000000000000002", ProjectID: "prj-1", Name: "not-granted"},
	}
}

func newListUC(repo snapshot.Repo, f authzfilter.Filter) *snapshot.UseCase {
	return snapshot.New(repo, &portmock.PeerClient{}, nil, serviceerr.ToStatus).WithListFilter(f)
}

func repoReturning(page []*domain.Snapshot, next string) *portmock.SnapshotRepo {
	return &portmock.SnapshotRepo{
		ListFunc: func(context.Context, snapshot.Pagination) ([]*domain.Snapshot, string, error) {
			return page, next, nil
		},
	}
}

// TestList_HidesSnapshotsWithoutPerObjectGrant — член проекта не имеет права видеть
// снимок, на который у него нет per-object гранта (дыра видимости storage).
func TestList_HidesSnapshotsWithoutPerObjectGrant(t *testing.T) {
	f := &fakeListFilter{allow: map[string]bool{"snp00000000000000001": true}}
	uc := newListUC(repoReturning(snapPage(), "next-tok"), f)

	got, next, err := uc.List(aliceCtx(), snapshot.Pagination{ProjectID: "prj-1", PageSize: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != "snp00000000000000001" {
		ids := make([]string, 0, len(got))
		for _, s := range got {
			ids = append(ids, s.ID)
		}
		t.Fatalf("List returned %v, want only [snp00000000000000001]", ids)
	}
	if next != "next-tok" {
		t.Fatalf("next page token = %q, want preserved cursor", next)
	}
	if f.calls != 1 || f.subject != "user:usr_alice" ||
		f.resType != authzfilter.ResourceTypeSnapshot || f.action != authzfilter.ActionSnapshotList {
		t.Fatalf("filter asked calls=%d subject=%q (%q,%q); want 1 batched call for (%q,%q) as user:usr_alice",
			f.calls, f.subject, f.resType, f.action,
			authzfilter.ResourceTypeSnapshot, authzfilter.ActionSnapshotList)
	}
}

// TestList_FilterErrorIsFailClosed — ошибка iam не отдаёт нефильтрованную страницу.
func TestList_FilterErrorIsFailClosed(t *testing.T) {
	f := &fakeListFilter{err: status.Error(codes.Unavailable, "list filter: iam unreachable")}
	uc := newListUC(repoReturning(snapPage(), ""), f)

	got, _, err := uc.List(aliceCtx(), snapshot.Pagination{ProjectID: "prj-1", PageSize: 50})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("List code = %v (rows=%d), want Unavailable fail-closed", status.Code(err), len(got))
	}
	if got != nil {
		t.Fatalf("List must not return rows on filter error, got %d", len(got))
	}
}

// TestList_NoPrincipalYieldsEmptyPage — запрос без caller-identity не обходит фильтр.
func TestList_NoPrincipalYieldsEmptyPage(t *testing.T) {
	f := &fakeListFilter{allow: map[string]bool{
		"snp00000000000000001": true, "snp00000000000000002": true,
	}}
	uc := newListUC(repoReturning(snapPage(), ""), f)

	got, _, err := uc.List(context.Background(), snapshot.Pagination{ProjectID: "prj-1", PageSize: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List without a caller principal returned %d rows, want 0 (fail-closed)", len(got))
	}
}

// TestList_PaginationValidatedBeforeVisibilityShortCircuit — формат страничных
// параметров отвергается ДО любого authz-решения (даже без principal'а).
func TestList_PaginationValidatedBeforeVisibilityShortCircuit(t *testing.T) {
	f := &fakeListFilter{}
	repo := &portmock.SnapshotRepo{
		ListFunc: func(_ context.Context, p snapshot.Pagination) ([]*domain.Snapshot, string, error) {
			if p.PageToken == "" {
				t.Fatal("page token must reach the repo")
			}
			return nil, "", fmt.Errorf("%w: invalid page_token", ports.ErrInvalidArg)
		},
	}
	uc := newListUC(repo, f)

	if _, _, err := uc.List(context.Background(), snapshot.Pagination{
		ProjectID: "prj-1", PageSize: corevalidate.MaxPageSize + 1,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("page_size over max = %v, want InvalidArgument", status.Code(err))
	}
	if _, _, err := uc.List(context.Background(), snapshot.Pagination{
		ProjectID: "prj-1", PageSize: 50, PageToken: "!!!garbage!!!",
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("garbage page_token = %v, want InvalidArgument", status.Code(err))
	}
	if f.calls != 0 {
		t.Fatalf("filter must not be consulted for a malformed request (calls=%d)", f.calls)
	}
}
