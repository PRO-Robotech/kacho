// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package snapshot_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports/portmock"
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/serviceerr"
)

// TestGetMalformedID — malformed snp-id первым стейтментом → sync InvalidArgument
// "invalid snapshot id '<X>'" (api-conventions.md), repo не вызывается.
func TestGetMalformedID(t *testing.T) {
	repo := &portmock.SnapshotRepo{
		GetFunc: func(context.Context, string) (*domain.Snapshot, error) {
			t.Fatal("repo.Get must not be called on malformed id")
			return nil, nil
		},
	}
	uc := snapshot.New(repo, &portmock.PeerClient{}, nil, serviceerr.ToStatus)
	_, err := uc.Get(context.Background(), "not-a-snp-id")
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Get malformed code = %v, want InvalidArgument", status.Code(err))
	}
	if got := status.Convert(err).Message(); got != "invalid snapshot id 'not-a-snp-id'" {
		t.Fatalf("Get malformed message = %q", got)
	}
}

// TestGetWellFormedDelegates — well-formed id проходит в repo.
func TestGetWellFormedDelegates(t *testing.T) {
	const wantID = "snp00000000000000000"
	want := &domain.Snapshot{ID: wantID, ProjectID: "prj-1"}
	repo := &portmock.SnapshotRepo{
		GetFunc: func(_ context.Context, id string) (*domain.Snapshot, error) {
			if id != wantID {
				t.Fatalf("repo got id %q", id)
			}
			return want, nil
		},
	}
	uc := snapshot.New(repo, &portmock.PeerClient{}, nil, serviceerr.ToStatus)
	got, err := uc.Get(context.Background(), wantID)
	if err != nil || got != want {
		t.Fatalf("Get = (%+v, %v)", got, err)
	}
}

// TestCreatePeerValidatesProject — Create валидирует project_id через IAMClient на
// request-path (fail-closed). Peer-ошибка пробрасывается (мутация не создаётся).
func TestCreatePeerValidatesProject(t *testing.T) {
	sentinel := errors.New("iam unavailable")
	iam := &portmock.PeerClient{
		EnsureProjectFunc: func(_ context.Context, projectID string) error {
			if projectID != "prj-1" {
				t.Fatalf("iam got project %q", projectID)
			}
			return sentinel
		},
	}
	uc := snapshot.New(&portmock.SnapshotRepo{}, iam, nil, nil)
	s := &domain.Snapshot{ProjectID: "prj-1", SourceVolumeID: "vol00000000000000000"}
	_, err := uc.Create(context.Background(), s)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Create err = %v, want iam sentinel", err)
	}
}

// TestCreateRejectsMissingSource — domain-инвариант: source_volume_id обязателен →
// sync InvalidArgument (iam не вызывается).
func TestCreateRejectsMissingSource(t *testing.T) {
	iam := &portmock.PeerClient{
		EnsureProjectFunc: func(context.Context, string) error {
			t.Fatal("iam must not be called before domain validation")
			return nil
		},
	}
	uc := snapshot.New(&portmock.SnapshotRepo{}, iam, nil, serviceerr.ToStatus)
	_, err := uc.Create(context.Background(), &domain.Snapshot{ProjectID: "prj-1"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Create missing source code = %v, want InvalidArgument", status.Code(err))
	}
}

// TestListRequiresProjectID — публичный List без projectId → sync InvalidArgument
// "projectId is required" (in-service backstop к gateway scope_extractor
// {project,project_id}). Пустой projectId вернул бы строки ВСЕХ проектов (repo
// сужает лишь при ProjectID!=""), поэтому отвергаем СИНХРОННО первым стейтментом —
// кросс-проектной утечки нет by construction (INV-10, CS1-S3-07). repo.List не зовётся.
func TestListRequiresProjectID(t *testing.T) {
	repo := &portmock.SnapshotRepo{
		ListFunc: func(context.Context, snapshot.Pagination) ([]*domain.Snapshot, string, error) {
			t.Fatal("repo.List must not be called when projectId is empty")
			return nil, "", nil
		},
	}
	uc := snapshot.New(repo, &portmock.PeerClient{}, nil, serviceerr.ToStatus)
	_, _, err := uc.List(context.Background(), snapshot.Pagination{PageSize: 50})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("List empty projectId code = %v, want InvalidArgument", status.Code(err))
	}
	if got := status.Convert(err).Message(); got != "projectId is required" {
		t.Fatalf("List empty projectId message = %q, want %q", got, "projectId is required")
	}
}

// TestListWithProjectIDDelegates — непустой projectId проходит в repo.List
// (guard не ложно-положителен); passed-through Pagination несёт тот же projectId.
func TestListWithProjectIDDelegates(t *testing.T) {
	var gotProject string
	repo := &portmock.SnapshotRepo{
		ListFunc: func(_ context.Context, p snapshot.Pagination) ([]*domain.Snapshot, string, error) {
			gotProject = p.ProjectID
			return []*domain.Snapshot{{ID: "snp00000000000000000", ProjectID: p.ProjectID}}, "", nil
		},
	}
	uc := snapshot.New(repo, &portmock.PeerClient{}, nil, serviceerr.ToStatus)
	got, _, err := uc.List(context.Background(), snapshot.Pagination{PageSize: 50, ProjectID: "prj-1"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if gotProject != "prj-1" || len(got) != 1 {
		t.Fatalf("List delegated project=%q results=%d, want prj-1/1", gotProject, len(got))
	}
}

// TestUpdateImmutableField — immutable-поле в маске → sync InvalidArgument
// "<field> is immutable after Snapshot.Create" (immutable-switch ДО UpdateMask).
func TestUpdateImmutableField(t *testing.T) {
	uc := snapshot.New(&portmock.SnapshotRepo{}, &portmock.PeerClient{}, nil, serviceerr.ToStatus)
	for _, f := range []string{"source_volume_id", "project_id", "size_bytes"} {
		_, err := uc.Update(context.Background(), "snp00000000000000000", []string{f}, "", "", nil)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("Update mask=%s code=%v, want InvalidArgument", f, status.Code(err))
		}
		want := f + " is immutable after Snapshot.Create"
		if got := status.Convert(err).Message(); got != want {
			t.Fatalf("Update mask=%s message=%q, want %q", f, got, want)
		}
	}
}

// TestUpdateMalformedID — malformed snp-id первым стейтментом → sync InvalidArgument.
func TestUpdateMalformedID(t *testing.T) {
	uc := snapshot.New(&portmock.SnapshotRepo{}, &portmock.PeerClient{}, nil, serviceerr.ToStatus)
	_, err := uc.Update(context.Background(), "bad-snp", nil, "x", "", nil)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Update malformed code = %v, want InvalidArgument", status.Code(err))
	}
	if got := status.Convert(err).Message(); got != "invalid snapshot id 'bad-snp'" {
		t.Fatalf("Update malformed message = %q", got)
	}
}

// TestDeleteMalformedID — malformed snp-id → sync InvalidArgument (repo не вызывается).
func TestDeleteMalformedID(t *testing.T) {
	uc := snapshot.New(&portmock.SnapshotRepo{}, &portmock.PeerClient{}, nil, serviceerr.ToStatus)
	_, err := uc.Delete(context.Background(), "nope")
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Delete malformed code = %v, want InvalidArgument", status.Code(err))
	}
}

// ── async LRO worker-слой (детерминированно через in-memory ops-repo + AwaitOpDone,
// не time.Sleep) ──────────────────────────────────────────────────────────────

// TestCreateLROInsertsAndMarksDone — happy async Create: worker вызывает repo.Insert,
// маршалит Snapshot в Operation.response, done=true (без error).
func TestCreateLROInsertsAndMarksDone(t *testing.T) {
	var insertedID string
	repo := &portmock.SnapshotRepo{
		InsertFunc: func(_ context.Context, s *domain.Snapshot) (*domain.Snapshot, error) {
			insertedID = s.ID // ids.NewID(prefix), присвоен use-case'ом до Run
			out := *s
			out.Status = domain.SnapshotStatusReady
			return &out, nil
		},
	}
	iam := &portmock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }}
	ops := portmock.NewOpsRepo()
	uc := snapshot.New(repo, iam, ops, serviceerr.ToStatus)

	op, err := uc.Create(context.Background(), &domain.Snapshot{
		ProjectID: "prj-1", Name: "snap-a", SourceVolumeID: "vol00000000000000000",
	})
	if err != nil {
		t.Fatalf("Create sync err = %v", err)
	}
	done := portmock.AwaitOpDone(t, ops, op.ID)
	if done.Error != nil {
		t.Fatalf("op error = %v, want success terminal", done.Error)
	}
	if done.Response == nil {
		t.Fatalf("op response nil, want marshalled Snapshot")
	}
	var got storagev1.Snapshot
	if uerr := done.Response.UnmarshalTo(&got); uerr != nil {
		t.Fatalf("unmarshal response: %v", uerr)
	}
	if got.GetId() == "" || got.GetId() != insertedID {
		t.Fatalf("response snapshot id = %q, want repo-inserted %q", got.GetId(), insertedID)
	}
}

// TestDeleteLROMarksDoneEmpty — happy async Delete: worker вызывает repo.Delete,
// response = Empty, done=true.
func TestDeleteLROMarksDoneEmpty(t *testing.T) {
	repo := &portmock.SnapshotRepo{
		DeleteFunc: func(context.Context, string) error { return nil },
	}
	ops := portmock.NewOpsRepo()
	uc := snapshot.New(repo, &portmock.PeerClient{}, ops, serviceerr.ToStatus)

	op, err := uc.Delete(context.Background(), "snp00000000000000000")
	if err != nil {
		t.Fatalf("Delete sync err = %v", err)
	}
	done := portmock.AwaitOpDone(t, ops, op.ID)
	if done.Error != nil {
		t.Fatalf("op error = %v, want success terminal", done.Error)
	}
	if done.Response == nil {
		t.Fatalf("op response nil, want Empty")
	}
}

// TestCreateLRORepoErrorMarksError — error async Create: repo.Insert возвращает
// FailedPrecondition-sentinel (source volume не READY) → worker пишет его в
// Operation.error (не response), done=true.
func TestCreateLRORepoErrorMarksError(t *testing.T) {
	sentinel := fmt.Errorf("%w: source volume is not READY", ports.ErrFailedPrecondition)
	repo := &portmock.SnapshotRepo{
		InsertFunc: func(context.Context, *domain.Snapshot) (*domain.Snapshot, error) { return nil, sentinel },
	}
	iam := &portmock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }}
	ops := portmock.NewOpsRepo()
	uc := snapshot.New(repo, iam, ops, serviceerr.ToStatus)

	op, err := uc.Create(context.Background(), &domain.Snapshot{
		ProjectID: "prj-1", SourceVolumeID: "vol00000000000000000",
	})
	if err != nil {
		t.Fatalf("Create sync err = %v", err)
	}
	done := portmock.AwaitOpDone(t, ops, op.ID)
	if done.Response != nil {
		t.Fatalf("op response = %v, want error terminal", done.Response)
	}
	if done.Error == nil || done.Error.GetCode() != int32(codes.FailedPrecondition) {
		t.Fatalf("op error = %v, want FailedPrecondition", done.Error)
	}
}
