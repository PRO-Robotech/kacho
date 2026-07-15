// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package volume_test

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
	"github.com/PRO-Robotech/kacho/services/storage/internal/service/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/serviceerr"
)

// TestGetDelegatesToReader — read-путь handler→use-case→reader-порт прошит
// сквозняком (скелет: adapter вернёт результат, use-case пробросит).
func TestGetDelegatesToReader(t *testing.T) {
	const wantID = "vol00000000000000000"
	want := &domain.Volume{ID: wantID, ProjectID: "prj-1"}
	reader := &portmock.VolumeReader{
		GetFunc: func(_ context.Context, id string) (*domain.Volume, error) {
			if id != wantID {
				t.Fatalf("reader got id %q, want %s", id, wantID)
			}
			return want, nil
		},
	}
	uc := volume.New(reader, &portmock.VolumeWriter{}, &portmock.PeerClient{}, &portmock.PeerClient{}, nil, nil)
	got, err := uc.Get(context.Background(), wantID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != want {
		t.Fatalf("Get returned %+v, want %+v", got, want)
	}
}

// TestGetMalformedID — malformed vol-id первым стейтментом → sync InvalidArgument
// "invalid volume id '<X>'" (api-conventions.md), repo не вызывается.
func TestGetMalformedID(t *testing.T) {
	reader := &portmock.VolumeReader{
		GetFunc: func(context.Context, string) (*domain.Volume, error) {
			t.Fatal("reader.Get must not be called on malformed id")
			return nil, nil
		},
	}
	uc := volume.New(reader, &portmock.VolumeWriter{}, &portmock.PeerClient{}, &portmock.PeerClient{}, nil, serviceerr.ToStatus)
	_, err := uc.Get(context.Background(), "not-a-vol-id")
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Get malformed code = %v, want InvalidArgument", status.Code(err))
	}
	if got := status.Convert(err).Message(); got != "invalid volume id 'not-a-vol-id'" {
		t.Fatalf("Get malformed message = %q", got)
	}
}

// TestListRequiresProjectID — публичный List без projectId → sync InvalidArgument
// "projectId is required" (in-service backstop к gateway scope_extractor
// {project,project_id}). Пустой projectId вернул бы строки ВСЕХ проектов (repo
// сужает лишь при ProjectID!=""), поэтому отвергаем СИНХРОННО первым стейтментом —
// кросс-проектной утечки нет by construction (INV-10, CS1-S1-13). reader.List не зовётся.
func TestListRequiresProjectID(t *testing.T) {
	reader := &portmock.VolumeReader{
		ListFunc: func(context.Context, volume.Pagination) ([]*domain.Volume, string, error) {
			t.Fatal("reader.List must not be called when projectId is empty")
			return nil, "", nil
		},
	}
	uc := volume.New(reader, &portmock.VolumeWriter{}, &portmock.PeerClient{}, &portmock.PeerClient{}, nil, serviceerr.ToStatus)
	_, _, err := uc.List(context.Background(), volume.Pagination{PageSize: 50})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("List empty projectId code = %v, want InvalidArgument", status.Code(err))
	}
	if got := status.Convert(err).Message(); got != "projectId is required" {
		t.Fatalf("List empty projectId message = %q, want %q", got, "projectId is required")
	}
}

// TestListWithProjectIDDelegates — непустой projectId проходит в reader.List
// (guard не ложно-положителен); passed-through Pagination несёт тот же projectId.
func TestListWithProjectIDDelegates(t *testing.T) {
	var gotProject string
	reader := &portmock.VolumeReader{
		ListFunc: func(_ context.Context, p volume.Pagination) ([]*domain.Volume, string, error) {
			gotProject = p.ProjectID
			return []*domain.Volume{{ID: "vol00000000000000000", ProjectID: p.ProjectID}}, "", nil
		},
	}
	uc := volume.New(reader, &portmock.VolumeWriter{}, &portmock.PeerClient{}, &portmock.PeerClient{}, nil, serviceerr.ToStatus)
	got, _, err := uc.List(context.Background(), volume.Pagination{PageSize: 50, ProjectID: "prj-1"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if gotProject != "prj-1" || len(got) != 1 {
		t.Fatalf("List delegated project=%q results=%d, want prj-1/1", gotProject, len(got))
	}
}

// TestUpdateImmutableField — immutable-поле в маске → sync InvalidArgument
// "<field> is immutable after Volume.Create" (immutable-switch ДО UpdateMask, S1-05).
func TestUpdateImmutableField(t *testing.T) {
	uc := volume.New(&portmock.VolumeReader{}, &portmock.VolumeWriter{}, &portmock.PeerClient{}, &portmock.PeerClient{}, nil, serviceerr.ToStatus)
	for _, f := range []string{"zone_id", "disk_type_id", "block_size", "source_snapshot_id", "used_by"} {
		_, err := uc.Update(context.Background(), "vol00000000000000000", []string{f}, "", "", nil, 0)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("Update mask=%s code=%v, want InvalidArgument", f, status.Code(err))
		}
		want := f + " is immutable after Volume.Create"
		if got := status.Convert(err).Message(); got != want {
			t.Fatalf("Update mask=%s message=%q, want %q", f, got, want)
		}
	}
}

// TestCreatePeerValidatesZone — Create валидирует zone_id через GeoClient на
// request-path (cross-domain ref, fail-closed). Анкер: rpc-implementer заменит
// заглушку реальным ZoneService.Get.
func TestCreatePeerValidatesZone(t *testing.T) {
	sentinel := errors.New("geo unavailable")
	geo := &portmock.PeerClient{
		EnsureZoneFunc: func(_ context.Context, zoneID string) error {
			if zoneID != "region-1-a" {
				t.Fatalf("geo got zone %q", zoneID)
			}
			return sentinel
		},
	}
	iam := &portmock.PeerClient{
		EnsureProjectFunc: func(context.Context, string) error { return nil },
	}
	uc := volume.New(&portmock.VolumeReader{}, &portmock.VolumeWriter{}, geo, iam, nil, nil)
	v := &domain.Volume{ProjectID: "prj-1", ZoneID: "region-1-a", DiskTypeID: "network-ssd", SizeBytes: 1}
	_, err := uc.Create(context.Background(), v)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Create err = %v, want geo sentinel", err)
	}
}

// ── async LRO worker-слой (детерминированно через in-memory ops-repo + AwaitOpDone,
// не time.Sleep) ──────────────────────────────────────────────────────────────

// TestCreateLROInsertsAndMarksDone — happy-путь async-мутации: sync-фаза создаёт
// LRO-строку, worker вызывает writer.Insert, маршалит Volume в Operation.response и
// переводит op в done=true (без error). Проверяем терминал + response-id.
func TestCreateLROInsertsAndMarksDone(t *testing.T) {
	var insertedID string
	writer := &portmock.VolumeWriter{
		InsertFunc: func(_ context.Context, v *domain.Volume) (*domain.Volume, error) {
			insertedID = v.ID // ids.NewID(prefix), присвоен use-case'ом до Run
			out := *v
			out.Status = domain.VolumeStatusAvailable
			return &out, nil
		},
	}
	geo := &portmock.PeerClient{EnsureZoneFunc: func(context.Context, string) error { return nil }}
	iam := &portmock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }}
	ops := portmock.NewOpsRepo()
	uc := volume.New(&portmock.VolumeReader{}, writer, geo, iam, ops, serviceerr.ToStatus)

	op, err := uc.Create(context.Background(), &domain.Volume{
		ProjectID: "prj-1", Name: "vol-a", ZoneID: "region-1-a", DiskTypeID: "network-ssd", SizeBytes: 1 << 30,
	})
	if err != nil {
		t.Fatalf("Create sync err = %v", err)
	}
	done := portmock.AwaitOpDone(t, ops, op.ID)
	if done.Error != nil {
		t.Fatalf("op error = %v, want success terminal", done.Error)
	}
	if done.Response == nil {
		t.Fatalf("op response nil, want marshalled Volume")
	}
	var got storagev1.Volume
	if uerr := done.Response.UnmarshalTo(&got); uerr != nil {
		t.Fatalf("unmarshal response: %v", uerr)
	}
	if got.GetId() == "" || got.GetId() != insertedID {
		t.Fatalf("response volume id = %q, want writer-inserted %q", got.GetId(), insertedID)
	}
}

// TestUpdateLROAppliesAndMarksDone — happy async Update: worker вызывает
// writer.Update, маршалит результат в Operation.response, done=true.
func TestUpdateLROAppliesAndMarksDone(t *testing.T) {
	const id = "vol00000000000000000"
	writer := &portmock.VolumeWriter{
		UpdateFunc: func(_ context.Context, gotID string, _ volume.VolumeUpdate) (*domain.Volume, error) {
			if gotID != id {
				t.Fatalf("writer.Update id = %q, want %s", gotID, id)
			}
			return &domain.Volume{ID: id, Name: "renamed"}, nil
		},
	}
	ops := portmock.NewOpsRepo()
	uc := volume.New(&portmock.VolumeReader{}, writer, &portmock.PeerClient{}, &portmock.PeerClient{}, ops, serviceerr.ToStatus)

	op, err := uc.Update(context.Background(), id, []string{"name"}, "renamed", "", nil, 0)
	if err != nil {
		t.Fatalf("Update sync err = %v", err)
	}
	done := portmock.AwaitOpDone(t, ops, op.ID)
	if done.Error != nil {
		t.Fatalf("op error = %v, want success terminal", done.Error)
	}
	var got storagev1.Volume
	if uerr := done.Response.UnmarshalTo(&got); uerr != nil {
		t.Fatalf("unmarshal response: %v", uerr)
	}
	if got.GetName() != "renamed" {
		t.Fatalf("response name = %q, want renamed", got.GetName())
	}
}

// TestDeleteLROMarksDoneEmpty — happy async Delete: worker вызывает writer.Delete,
// response = Empty, done=true.
func TestDeleteLROMarksDoneEmpty(t *testing.T) {
	writer := &portmock.VolumeWriter{
		DeleteFunc: func(context.Context, string) error { return nil },
	}
	ops := portmock.NewOpsRepo()
	uc := volume.New(&portmock.VolumeReader{}, writer, &portmock.PeerClient{}, &portmock.PeerClient{}, ops, serviceerr.ToStatus)

	op, err := uc.Delete(context.Background(), "vol00000000000000000")
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

// TestCreateLROWriterErrorMarksError — error-путь async-мутации: writer.Insert
// возвращает FailedPrecondition-sentinel → worker пишет его в Operation.error
// (не response), done=true. Проверяем код терминальной ошибки.
func TestCreateLROWriterErrorMarksError(t *testing.T) {
	sentinel := fmt.Errorf("%w: DiskType network-ssd not found", ports.ErrFailedPrecondition)
	writer := &portmock.VolumeWriter{
		InsertFunc: func(context.Context, *domain.Volume) (*domain.Volume, error) { return nil, sentinel },
	}
	geo := &portmock.PeerClient{EnsureZoneFunc: func(context.Context, string) error { return nil }}
	iam := &portmock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }}
	ops := portmock.NewOpsRepo()
	uc := volume.New(&portmock.VolumeReader{}, writer, geo, iam, ops, serviceerr.ToStatus)

	op, err := uc.Create(context.Background(), &domain.Volume{
		ProjectID: "prj-1", ZoneID: "region-1-a", DiskTypeID: "network-ssd", SizeBytes: 1 << 30,
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
