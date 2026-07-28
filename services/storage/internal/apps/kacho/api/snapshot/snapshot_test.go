// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package snapshot_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/repomock"
)

// TestGetMalformedID — malformed snp-id первым стейтментом → sync InvalidArgument
// "invalid snapshot id '<X>'" (api-conventions.md), repo не вызывается.
func TestGetMalformedID(t *testing.T) {
	repo := &repomock.SnapshotRepo{
		GetFunc: func(context.Context, string) (*domain.Snapshot, error) {
			t.Fatal("repo.Get must not be called on malformed id")
			return nil, nil
		},
	}
	uc := snapshot.New(repo, &repomock.PeerClient{}, nil, serviceerr.ToStatus)
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
	repo := &repomock.SnapshotRepo{
		GetFunc: func(_ context.Context, id string) (*domain.Snapshot, error) {
			if id != wantID {
				t.Fatalf("repo got id %q", id)
			}
			return want, nil
		},
	}
	uc := snapshot.New(repo, &repomock.PeerClient{}, nil, serviceerr.ToStatus)
	got, err := uc.Get(context.Background(), wantID)
	if err != nil || got != want {
		t.Fatalf("Get = (%+v, %v)", got, err)
	}
}

// TestCreatePeerValidatesProject — Create валидирует project_id через IAMClient на
// request-path (fail-closed). Peer-ошибка пробрасывается (мутация не создаётся).
func TestCreatePeerValidatesProject(t *testing.T) {
	sentinel := errors.New("iam unavailable")
	iam := &repomock.PeerClient{
		EnsureProjectFunc: func(_ context.Context, projectID string) error {
			if projectID != "prj-1" {
				t.Fatalf("iam got project %q", projectID)
			}
			return sentinel
		},
	}
	uc := snapshot.New(&repomock.SnapshotRepo{}, iam, nil, nil)
	s := &domain.Snapshot{ProjectID: "prj-1", SourceVolumeID: "vol00000000000000000"}
	_, err := uc.Create(context.Background(), s)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Create err = %v, want iam sentinel", err)
	}
}

// TestCreateRejectsMissingSource — domain-инвариант: source_volume_id обязателен →
// sync InvalidArgument (iam не вызывается).
func TestCreateRejectsMissingSource(t *testing.T) {
	iam := &repomock.PeerClient{
		EnsureProjectFunc: func(context.Context, string) error {
			t.Fatal("iam must not be called before domain validation")
			return nil
		},
	}
	uc := snapshot.New(&repomock.SnapshotRepo{}, iam, nil, serviceerr.ToStatus)
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
	repo := &repomock.SnapshotRepo{
		ListFunc: func(context.Context, snapshot.Pagination) ([]*domain.Snapshot, string, error) {
			t.Fatal("repo.List must not be called when projectId is empty")
			return nil, "", nil
		},
	}
	uc := snapshot.New(repo, &repomock.PeerClient{}, nil, serviceerr.ToStatus)
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
	repo := &repomock.SnapshotRepo{
		ListFunc: func(_ context.Context, p snapshot.Pagination) ([]*domain.Snapshot, string, error) {
			gotProject = p.ProjectID
			return []*domain.Snapshot{{ID: "snp00000000000000000", ProjectID: p.ProjectID}}, "", nil
		},
	}
	uc := snapshot.New(repo, &repomock.PeerClient{}, nil, serviceerr.ToStatus)
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
	uc := snapshot.New(&repomock.SnapshotRepo{}, &repomock.PeerClient{}, nil, serviceerr.ToStatus)
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
	uc := snapshot.New(&repomock.SnapshotRepo{}, &repomock.PeerClient{}, nil, serviceerr.ToStatus)
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
	uc := snapshot.New(&repomock.SnapshotRepo{}, &repomock.PeerClient{}, nil, serviceerr.ToStatus)
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
	repo := &repomock.SnapshotRepo{
		InsertFunc: func(_ context.Context, s *domain.Snapshot) (*domain.Snapshot, error) {
			insertedID = s.ID // ids.NewID(prefix), присвоен use-case'ом до Run
			out := *s
			out.Status = domain.SnapshotStatusReady
			return &out, nil
		},
	}
	iam := &repomock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }}
	ops := repomock.NewOpsRepo()
	uc := snapshot.New(repo, iam, ops, serviceerr.ToStatus)

	op, err := uc.Create(context.Background(), &domain.Snapshot{
		ProjectID: "prj-1", Name: "snap-a", SourceVolumeID: "vol00000000000000000",
	})
	if err != nil {
		t.Fatalf("Create sync err = %v", err)
	}
	done := repomock.AwaitOpDone(t, ops, op.ID)
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
	repo := &repomock.SnapshotRepo{
		DeleteFunc: func(context.Context, string) error { return nil },
	}
	ops := repomock.NewOpsRepo()
	uc := snapshot.New(repo, &repomock.PeerClient{}, ops, serviceerr.ToStatus)

	op, err := uc.Delete(context.Background(), "snp00000000000000000")
	if err != nil {
		t.Fatalf("Delete sync err = %v", err)
	}
	done := repomock.AwaitOpDone(t, ops, op.ID)
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
	sentinel := fmt.Errorf("%w: source volume is not READY", storageerr.ErrFailedPrecondition)
	repo := &repomock.SnapshotRepo{
		InsertFunc: func(context.Context, *domain.Snapshot) (*domain.Snapshot, error) { return nil, sentinel },
	}
	iam := &repomock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }}
	ops := repomock.NewOpsRepo()
	uc := snapshot.New(repo, iam, ops, serviceerr.ToStatus)

	op, err := uc.Create(context.Background(), &domain.Snapshot{
		ProjectID: "prj-1", SourceVolumeID: "vol00000000000000000",
	})
	if err != nil {
		t.Fatalf("Create sync err = %v", err)
	}
	done := repomock.AwaitOpDone(t, ops, op.ID)
	if done.Response != nil {
		t.Fatalf("op response = %v, want error terminal", done.Response)
	}
	if done.Error == nil || done.Error.GetCode() != int32(codes.FailedPrecondition) {
		t.Fatalf("op error = %v, want FailedPrecondition", done.Error)
	}
}

// ── description / labels: отвергаются СИНХРОННО, как у тома и образа ─────────
//
// Том и образ прогоняют оба поля через validate.* на кромке запроса. Снимок этого
// не делал: доменная проверка описания и меток не касается вовсе, поэтому
// переразмерное значение доезжало до вставки, ловилось ограничением БД и
// возвращалось АСИНХРОННО в ошибке операции обобщённым текстом — то есть поздно и
// без имени поля. Для вызывающего это разница между «ты прислал слишком длинное
// описание» и «операция почему-то не удалась».
//
// Утверждается наблюдаемое: отказ СИНХРОННЫЙ (ошибка из самого вызова, не из
// операции), код InvalidArgument, и до однорангового узла дело не доходит — то
// есть проверка стоит ДО сетевых вызовов, а не после.
//
// Имя поля здесь НЕ утверждается, и это не послабление. Общий валидатор кладёт
// его в field violation внутри деталей, но слой use-case пересобирает ошибку через
// её текст и детали теряет — одинаково у тома, образа и снимка. Утверждать здесь
// имя поля значило бы держать снимок строже двух соседей по контракту, который
// продукт не выполняет ни для одного из трёх. Потеря деталей заведена отдельно.

func TestCreateRejectsOverLongDescriptionSynchronously(t *testing.T) {
	iam := &repomock.PeerClient{
		EnsureProjectFunc: func(context.Context, string) error {
			t.Fatal("peer must not be called before the request edge rejects the body")
			return nil
		},
	}
	uc := snapshot.New(&repomock.SnapshotRepo{}, iam, nil, serviceerr.ToStatus)

	s := &domain.Snapshot{
		ProjectID:      "prj-1",
		SourceVolumeID: "vol00000000000000000",
		Description:    strings.Repeat("x", 257), // предел 256
	}
	op, err := uc.Create(context.Background(), s)
	if op != nil {
		t.Fatalf("Create returned an operation %v — the refusal must be synchronous", op)
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Create over-long description code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestCreateRejectsTooManyLabelsSynchronously(t *testing.T) {
	iam := &repomock.PeerClient{
		EnsureProjectFunc: func(context.Context, string) error {
			t.Fatal("peer must not be called before the request edge rejects the body")
			return nil
		},
	}
	uc := snapshot.New(&repomock.SnapshotRepo{}, iam, nil, serviceerr.ToStatus)

	labels := make(map[string]string, 65) // предел 64
	for i := 0; i < 65; i++ {
		labels[fmt.Sprintf("k%02d", i)] = "v"
	}
	s := &domain.Snapshot{
		ProjectID:      "prj-1",
		SourceVolumeID: "vol00000000000000000",
		Labels:         labels,
	}
	op, err := uc.Create(context.Background(), s)
	if op != nil {
		t.Fatalf("Create returned an operation %v — the refusal must be synchronous", op)
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Create over-limit labels code = %v, want InvalidArgument", status.Code(err))
	}
}

// Граница остаётся проходимой: ровно 256 символов и ровно 64 метки — не отказ.
func TestCreateAcceptsDescriptionAndLabelsAtTheLimit(t *testing.T) {
	repo := &repomock.SnapshotRepo{
		InsertFunc: func(_ context.Context, s *domain.Snapshot) (*domain.Snapshot, error) {
			out := *s
			out.Status = domain.SnapshotStatusReady
			return &out, nil
		},
	}
	iam := &repomock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }}
	// Валидный вход доходит до создания операции, поэтому здесь нужен настоящий
	// репозиторий операций: с пустым вызов падает ещё до утверждения, и тест
	// краснел бы по причине, к границе отношения не имеющей.
	uc := snapshot.New(repo, iam, repomock.NewOpsRepo(), serviceerr.ToStatus)

	labels := make(map[string]string, 64)
	for i := 0; i < 64; i++ {
		labels[fmt.Sprintf("k%02d", i)] = "v"
	}
	s := &domain.Snapshot{
		ProjectID:      "prj-1",
		SourceVolumeID: "vol00000000000000000",
		Description:    strings.Repeat("x", 256),
		Labels:         labels,
	}
	if _, err := uc.Create(context.Background(), s); status.Code(err) == codes.InvalidArgument {
		t.Fatalf("Create at the limit was refused: %v", err)
	}
}
