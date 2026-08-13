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

	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	storageerr "github.com/PRO-Robotech/kacho/services/storage/internal/errors"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/repomock"
)

// TestGetDelegatesToReader — read-путь handler→use-case→reader-порт прошит
// сквозняком (скелет: adapter вернёт результат, use-case пробросит).
func TestGetDelegatesToReader(t *testing.T) {
	const wantID = "vol00000000000000000"
	want := &domain.Volume{ID: wantID, ProjectID: "prj-1"}
	reader := &repomock.VolumeReader{
		GetFunc: func(_ context.Context, id string) (*domain.Volume, error) {
			if id != wantID {
				t.Fatalf("reader got id %q, want %s", id, wantID)
			}
			return want, nil
		},
	}
	uc := volume.New(reader, &repomock.VolumeWriter{}, &repomock.PeerClient{}, &repomock.PeerClient{}, nil, nil).WithInstallPrefix(testInstallPrefix)
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
	reader := &repomock.VolumeReader{
		GetFunc: func(context.Context, string) (*domain.Volume, error) {
			t.Fatal("reader.Get must not be called on malformed id")
			return nil, nil
		},
	}
	uc := volume.New(reader, &repomock.VolumeWriter{}, &repomock.PeerClient{}, &repomock.PeerClient{}, nil, serviceerr.ToStatus).WithInstallPrefix(testInstallPrefix)
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
	reader := &repomock.VolumeReader{
		ListFunc: func(context.Context, volume.Pagination) ([]*domain.Volume, string, error) {
			t.Fatal("reader.List must not be called when projectId is empty")
			return nil, "", nil
		},
	}
	uc := volume.New(reader, &repomock.VolumeWriter{}, &repomock.PeerClient{}, &repomock.PeerClient{}, nil, serviceerr.ToStatus).WithInstallPrefix(testInstallPrefix)
	_, _, err := uc.List(narrowtest.Caller(), volume.Pagination{PageSize: 50})
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
	reader := &repomock.VolumeReader{
		ListFunc: func(_ context.Context, p volume.Pagination) ([]*domain.Volume, string, error) {
			gotProject = p.ProjectID
			return []*domain.Volume{{ID: "vol00000000000000000", ProjectID: p.ProjectID}}, "", nil
		},
	}
	uc := volume.New(reader, &repomock.VolumeWriter{}, &repomock.PeerClient{}, &repomock.PeerClient{},
		nil, serviceerr.ToStatus).WithInstallPrefix(testInstallPrefix).WithListFilter(narrowtest.AllowingAll())
	got, _, err := uc.List(narrowtest.Caller(), volume.Pagination{PageSize: 50, ProjectID: "prj-1"})
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
	uc := volume.New(&repomock.VolumeReader{}, &repomock.VolumeWriter{}, &repomock.PeerClient{}, &repomock.PeerClient{}, nil, serviceerr.ToStatus).WithInstallPrefix(testInstallPrefix)
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
	geo := &repomock.PeerClient{
		EnsureZoneFunc: func(_ context.Context, zoneID string) error {
			if zoneID != "region-1-a" {
				t.Fatalf("geo got zone %q", zoneID)
			}
			return sentinel
		},
	}
	iam := &repomock.PeerClient{
		EnsureProjectFunc: func(context.Context, string) error { return nil },
	}
	uc := volume.New(&repomock.VolumeReader{}, &repomock.VolumeWriter{}, geo, iam, nil, nil).WithInstallPrefix(testInstallPrefix)
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
	writer := &repomock.VolumeWriter{
		InsertFunc: func(_ context.Context, v *domain.Volume) (*domain.Volume, error) {
			insertedID = v.ID // ids.NewID(prefix), присвоен use-case'ом до Run
			out := *v
			out.Status = domain.VolumeStatusAvailable
			return &out, nil
		},
	}
	geo := &repomock.PeerClient{EnsureZoneFunc: func(context.Context, string) error { return nil }}
	iam := &repomock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }}
	ops := repomock.NewOpsRepo()
	uc := volume.New(&repomock.VolumeReader{}, writer, geo, iam, ops, serviceerr.ToStatus).WithInstallPrefix(testInstallPrefix)

	op, err := uc.Create(context.Background(), &domain.Volume{
		ProjectID: "prj-1", Name: "vol-a", ZoneID: "region-1-a", DiskTypeID: "network-ssd", SizeBytes: 1 << 30,
	})
	if err != nil {
		t.Fatalf("Create sync err = %v", err)
	}
	done := repomock.AwaitOpDone(t, ops, op.ID)
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
	writer := &repomock.VolumeWriter{
		UpdateFunc: func(_ context.Context, gotID string, _ volume.VolumeUpdate) (*domain.Volume, error) {
			if gotID != id {
				t.Fatalf("writer.Update id = %q, want %s", gotID, id)
			}
			return &domain.Volume{ID: id, Name: "renamed"}, nil
		},
	}
	ops := repomock.NewOpsRepo()
	uc := volume.New(&repomock.VolumeReader{}, writer, &repomock.PeerClient{}, &repomock.PeerClient{}, ops, serviceerr.ToStatus).WithInstallPrefix(testInstallPrefix)

	op, err := uc.Update(context.Background(), id, []string{"name"}, "renamed", "", nil, 0)
	if err != nil {
		t.Fatalf("Update sync err = %v", err)
	}
	done := repomock.AwaitOpDone(t, ops, op.ID)
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
	writer := &repomock.VolumeWriter{
		DeleteFunc: func(context.Context, string) error { return nil },
	}
	ops := repomock.NewOpsRepo()
	uc := volume.New(&repomock.VolumeReader{}, writer, &repomock.PeerClient{}, &repomock.PeerClient{}, ops, serviceerr.ToStatus).WithInstallPrefix(testInstallPrefix)

	op, err := uc.Delete(context.Background(), "vol00000000000000000")
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

// TestCreateLROWriterErrorMarksError — error-путь async-мутации: writer.Insert
// возвращает FailedPrecondition-sentinel → worker пишет его в Operation.error
// (не response), done=true. Проверяем код терминальной ошибки.
func TestCreateLROWriterErrorMarksError(t *testing.T) {
	sentinel := fmt.Errorf("%w: DiskType network-ssd not found", storageerr.ErrFailedPrecondition)
	writer := &repomock.VolumeWriter{
		InsertFunc: func(context.Context, *domain.Volume) (*domain.Volume, error) { return nil, sentinel },
	}
	geo := &repomock.PeerClient{EnsureZoneFunc: func(context.Context, string) error { return nil }}
	iam := &repomock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }}
	ops := repomock.NewOpsRepo()
	uc := volume.New(&repomock.VolumeReader{}, writer, geo, iam, ops, serviceerr.ToStatus).WithInstallPrefix(testInstallPrefix)

	op, err := uc.Create(context.Background(), &domain.Volume{
		ProjectID: "prj-1", ZoneID: "region-1-a", DiskTypeID: "network-ssd", SizeBytes: 1 << 30,
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

// testInstallPrefix — префикс установки для проб.
//
// Он обязателен на пути создания: имя объекта у бэкенда выводится из него и
// неизменяемого идентификатора тома, и без префикса два развёртывания на одном
// кластере хранилища усыновили бы объекты друг друга. Пробы задают его явно, а не
// получают умолчанием, — чтобы отсутствие префикса оставалось наблюдаемым отказом
// (см. TestCreateWithoutInstallPrefixIsRefused).
const testInstallPrefix = "kctest"

// TestCreateWithoutInstallPrefixIsRefused — посадка без префикса установки не
// создаёт томов, и отказ говорит о СЕРВИСЕ, а не о запросе.
//
// Арендатор не сделал ничего неверного: сервис в этой посадке не способен исполнить
// запрос. Код FAILED_PRECONDITION или INVALID_ARGUMENT отправил бы его чинить свой
// ввод, которого чинить нечего.
// TestCreateWithoutDataPlaneNeedsNoInstallPrefix — обратная сторона того же
// правила, и без неё отрицание выше означало бы «отказываем всегда».
//
// Префикс даёт ИМЯ объекту у бэкенда. Плоскости данных нет — объекта не будет,
// имя выводить не для чего, и требовать префикс беспредметно. Отказ Unavailable
// в такой посадке говорил бы «сервис недоступен» там, где он исправен: именно
// это и роняло сквозные прогоны на стенде без кластера хранения.
func TestCreateWithoutDataPlaneNeedsNoInstallPrefix(t *testing.T) {
	uc := volume.New(&repomock.VolumeReader{}, &repomock.VolumeWriter{},
		&repomock.PeerClient{}, &repomock.PeerClient{}, nil, serviceerr.ToStatus)
	// dataPlane НЕ объявлена и префикса нет — сочетание, штатное для платформы
	// только управляющей плоскости.
	// Предмет пробы — ОТСУТСТВИЕ отказа по неспособности сервиса, а не успех
	// создания: успех потребовал бы поднять весь путь записи, и проба стала бы
	// о другом. Поэтому паника на неполном дублёре ловится и не засчитывается
	// за отказ — нас интересует ровно код Unavailable.
	defer func() { _ = recover() }()

	_, err := uc.Create(context.Background(), &domain.Volume{
		ID: "vol00000000000000000", ProjectID: "prj-1", Name: "v",
		ZoneID: "region-1-a", DiskTypeID: "block-balanced", SizeBytes: 1 << 30,
	})
	if err != nil && status.Code(err) == codes.Unavailable {
		t.Fatalf("посадка без плоскости данных НЕ должна отвечать «сервис недоступен»: %v", err)
	}
}

func TestCreateWithoutInstallPrefixIsRefused(t *testing.T) {
	uc := volume.New(&repomock.VolumeReader{}, &repomock.VolumeWriter{},
		&repomock.PeerClient{}, &repomock.PeerClient{}, nil, serviceerr.ToStatus).
		// Отказ ждут ТАМ, ГДЕ плоскость данных объявлена: без неё имя объекта
		// выводить не для чего, и требование префикса беспредметно.
		WithDataPlane(true)

	_, err := uc.Create(context.Background(), &domain.Volume{
		ProjectID: "prj-1", ZoneID: "ru-central1-a", DiskTypeID: "block-balanced", SizeBytes: 1 << 30,
	})
	if err == nil {
		t.Fatal("посадка без префикса установки обязана отказывать в создании тома")
	}
	if got := status.Code(err); got != codes.Unavailable {
		t.Errorf("код %s: отказ обязан говорить о неспособности сервиса, а не о неверном вводе", got)
	}
}
