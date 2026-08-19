// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package handler

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/disktype"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/repomock"
)

// Провязка админ-каталога проверяется НА ХЕНДЛЕРЕ, а не только на use-case: маска
// живёт в запросе, и потерять её можно ровно здесь — передать в use-case пустую,
// после чего дисциплина маски исполняется безупречно над пустым списком, то есть
// правка снова становится полной заменой. Дефект был бы невидим обеим сторонам:
// use-case честно делает полный PATCH, хендлер честно зовёт use-case.

// diskTypeUpdateRecorder — репозиторий-дублёр, запоминающий набор изменений, дошедший
// до записи. Судим по набору: возвращённый ресурс одинаков и когда маска прочитана, и
// когда потеряна.
type diskTypeUpdateRecorder struct {
	*repomock.DiskTypeRepo
	got disktype.DiskTypeUpdate
}

func newDiskTypeUpdateRecorder() *diskTypeUpdateRecorder {
	r := &diskTypeUpdateRecorder{}
	r.DiskTypeRepo = &repomock.DiskTypeRepo{
		UpdateFunc: func(_ context.Context, id string, u disktype.DiskTypeUpdate) (*domain.DiskType, error) {
			r.got = u
			return &domain.DiskType{ID: id, Name: "любой", Lifecycle: domain.LifecycleActive}, nil
		},
	}
	return r
}

// TestInternalDiskTypeUpdateForwardsMask — маска запроса доезжает до записи: тело
// несёт зоны, маска называет только имя, значит зоны не пишутся.
//
// Пара — положительный контроль: названное маской имя записывается, иначе проба
// зеленела бы на хендлере, который не вызывает use-case вовсе.
func TestInternalDiskTypeUpdateForwardsMask(t *testing.T) {
	repo := newDiskTypeUpdateRecorder()
	h := NewInternalDiskTypeHandler(disktype.New(repo))

	if _, err := h.Update(context.Background(), &storagev1.UpdateDiskTypeRequest{
		DiskTypeId: "block-mask",
		Name:       "block-renamed",
		ZoneIds:    []string{"ru-central1-c"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}},
	}); err != nil {
		t.Fatalf("Update err = %v", err)
	}
	if repo.got.Name == nil || *repo.got.Name != "block-renamed" {
		t.Fatalf("названное маской имя не доехало: %v", repo.got.Name)
	}
	if repo.got.ZoneIDs != nil {
		t.Fatalf("зоны маской не названы, но уехали в запись: %v", *repo.got.ZoneIDs)
	}
}

// TestInternalDiskTypeUpdateForwardsLimits — границы размера доезжают из запроса, а
// не теряются по дороге: класс объявляет ими предел тома, и молча потерянное поле
// означало бы предел, которого администратор не ставил.
func TestInternalDiskTypeUpdateForwardsLimits(t *testing.T) {
	repo := newDiskTypeUpdateRecorder()
	h := NewInternalDiskTypeHandler(disktype.New(repo))

	if _, err := h.Update(context.Background(), &storagev1.UpdateDiskTypeRequest{
		DiskTypeId: "block-mask",
		Limits: &storagev1.DiskType_SizeLimits{
			MinSizeBytes: 1 << 30, MaxSizeBytes: 16 << 40, SizeStepBytes: 1 << 30,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"limits"}},
	}); err != nil {
		t.Fatalf("Update err = %v", err)
	}
	if repo.got.MinSizeBytes == nil || *repo.got.MinSizeBytes != 1<<30 {
		t.Fatalf("минимум не доехал: %v", repo.got.MinSizeBytes)
	}
	if repo.got.MaxSizeBytes == nil || *repo.got.MaxSizeBytes != 16<<40 {
		t.Fatalf("максимум не доехал: %v", repo.got.MaxSizeBytes)
	}
	if repo.got.Name != nil {
		t.Fatalf("имя маской не названо, но уехало в запись: %q", *repo.got.Name)
	}
}

// TestInternalDiskTypeUpdateImmutableFieldStatus — неизменяемое поле маски приходит
// вызывающему конвенционным текстом и кодом INVALID_ARGUMENT (а не общим «unknown
// field»): маппер sentinel→статус обязан быть провязан в этом хендлере.
func TestInternalDiskTypeUpdateImmutableFieldStatus(t *testing.T) {
	repo := newDiskTypeUpdateRecorder()
	h := NewInternalDiskTypeHandler(disktype.New(repo))

	_, err := h.Update(context.Background(), &storagev1.UpdateDiskTypeRequest{
		DiskTypeId: "block-mask",
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"lifecycle"}},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("код = %v, want InvalidArgument", status.Code(err))
	}
	const want = "lifecycle is immutable after DiskType.Create"
	if got := status.Convert(err).Message(); got != want {
		t.Fatalf("текст = %q, want %q", got, want)
	}
}

// TestInternalDiskTypeSetLifecycleForwardsState — глагол вывода из обращения доезжает
// до записи именно тем состоянием, которое названо в запросе.
//
// Отрицание в паре: неназванное состояние (UNSPECIFIED) отвергается — умолчания у
// этого глагола нет, «не указано» неотличимо от «верни в обращение», а такой исход
// обязан быть выбран, а не получен по недосмотру.
func TestInternalDiskTypeSetLifecycleForwardsState(t *testing.T) {
	repo := newDiskTypeUpdateRecorder()
	h := NewInternalDiskTypeHandler(disktype.New(repo))

	if _, err := h.SetLifecycle(context.Background(), &storagev1.SetDiskTypeLifecycleRequest{
		DiskTypeId: "block-x",
		Lifecycle:  storagev1.DiskType_DEPRECATED,
	}); err != nil {
		t.Fatalf("SetLifecycle err = %v", err)
	}
	if repo.got.Lifecycle == nil || *repo.got.Lifecycle != domain.LifecycleDeprecated {
		t.Fatalf("состояние не доехало: %v", repo.got.Lifecycle)
	}
	if repo.got.Name != nil || repo.got.ZoneIDs != nil || repo.got.MinSizeBytes != nil {
		t.Fatal("глагол переписал поля, которых не касается")
	}

	repo = newDiskTypeUpdateRecorder()
	h = NewInternalDiskTypeHandler(disktype.New(repo))
	_, err := h.SetLifecycle(context.Background(), &storagev1.SetDiskTypeLifecycleRequest{
		DiskTypeId: "block-x",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("неназванное состояние: код = %v, want InvalidArgument", status.Code(err))
	}
	if repo.got.Lifecycle != nil {
		t.Fatal("неназванное состояние дошло до записи")
	}
}
