// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package image_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/image"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/repomock"
)

// ── Пустое имя не доживает до записи (#715) ──────────────────────────────────
//
// У образа ТРИ пути появления строки — заведение, регистрация уже лежащего у
// бэкенда объекта и копия в другой регион, — и каждый минтит свой
// идентификатор. Проверяются все три: подстановка на одном пути ничего не
// говорит о других, а копия к тому же не проверяла имя ВОВСЕ — малформ доезжал
// до DB-CHECK и возвращался асинхронно в ошибке операции.

func imageGeo() *repomock.PeerClient {
	return &repomock.PeerClient{
		EnsureRegionFunc:  func(context.Context, string) error { return nil },
		ZonesOfRegionFunc: func(context.Context, string) ([]string, error) { return []string{"region-1-a"}, nil },
	}
}

func imageIAM() *repomock.PeerClient {
	return &repomock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }}
}

// createImage — заведение образа; возвращает имя, дошедшее до записи.
func createImage(t *testing.T, name string) (error, string) { //nolint:revive // порядок возвратов удобен пробе
	t.Helper()
	var got domain.Image
	writer := &repomock.ImageWriter{
		InsertFunc: func(_ context.Context, i *domain.Image, _ []string) (*domain.Image, error) {
			got = *i
			out := *i
			return &out, nil
		},
	}
	ops := repomock.NewOpsRepo()
	uc := image.New(&repomock.ImageReader{}, writer, imageGeo(), imageIAM(), ops, serviceerr.ToStatus).
		WithInstallPrefix(testInstallPrefix)
	op, err := uc.Create(context.Background(), &domain.Image{
		ProjectID: "prj-1", RegionID: "region-1", Name: name,
		SourceSnapshot: "snp00000000000000000",
	})
	if err != nil {
		return err, ""
	}
	if done := repomock.AwaitOpDone(t, ops, op.ID); done.Error != nil {
		t.Fatalf("Create(name=%q) op error = %v", name, done.Error)
	}
	return nil, got.Name
}

// TestCreateEmptyNameIsSubstitutedBeforeWrite — заведение без имени доходит до
// записи с непустым именем.
func TestCreateEmptyNameIsSubstitutedBeforeWrite(t *testing.T) {
	err, got := createImage(t, "")
	if err != nil {
		t.Fatalf("Create без имени отвергнут: %v", err)
	}
	if got == "" {
		t.Fatal("образ дошёл до записи с ПУСТЫМ именем")
	}
}

// TestCreateKeepsTheNameTheCallerGave — положительный контроль.
func TestCreateKeepsTheNameTheCallerGave(t *testing.T) {
	const want = "img-a"
	err, got := createImage(t, want)
	if err != nil {
		t.Fatalf("Create(name=%q): %v", want, err)
	}
	if got != want {
		t.Fatalf("имя после создания = %q, want %q", got, want)
	}
}

// TestTwoUnnamedImagesGetDifferentNames — два безымянных заведения в одном
// проекте получают РАЗНЫЕ имена.
func TestTwoUnnamedImagesGetDifferentNames(t *testing.T) {
	_, first := createImage(t, "")
	_, second := createImage(t, "")
	if first == "" || second == "" {
		t.Fatalf("имена = (%q, %q), оба обязаны быть непусты", first, second)
	}
	if first == second {
		t.Fatalf("два безымянных образа получили ОДНО имя %q", first)
	}
}

// registerImage — регистрация уже лежащего объекта; возвращает имя, дошедшее до записи.
func registerImage(t *testing.T, name string) (error, string) { //nolint:revive // порядок возвратов удобен пробе
	t.Helper()
	var got domain.Image
	writer := &repomock.ImageWriter{
		RegisterFunc: func(_ context.Context, i *domain.Image) (*domain.Image, error) {
			got = *i
			out := *i
			return &out, nil
		},
	}
	uc := image.New(&repomock.ImageReader{}, writer, imageGeo(), imageIAM(),
		repomock.NewOpsRepo(), serviceerr.ToStatus).WithInstallPrefix(testInstallPrefix)
	_, err := uc.Register(context.Background(), image.RegisterInput{
		ProjectID: "prj-1", RegionID: "region-1", Name: name,
		BackendObject: "obj-1", SizeBytes: 1 << 30, MinDiskBytes: 1 << 30,
	})
	if err != nil {
		return err, ""
	}
	return nil, got.Name
}

// TestRegisterEmptyNameIsSubstitutedBeforeWrite — регистрация без имени тоже
// доходит до записи с непустым именем.
func TestRegisterEmptyNameIsSubstitutedBeforeWrite(t *testing.T) {
	err, got := registerImage(t, "")
	if err != nil {
		t.Fatalf("Register без имени отвергнут: %v", err)
	}
	if got == "" {
		t.Fatal("регистрация дошла до записи с ПУСТЫМ именем")
	}
}

// TestRegisterKeepsTheNameTheCallerGave — положительный контроль.
func TestRegisterKeepsTheNameTheCallerGave(t *testing.T) {
	const want = "img-registered"
	err, got := registerImage(t, want)
	if err != nil {
		t.Fatalf("Register(name=%q): %v", want, err)
	}
	if got != want {
		t.Fatalf("имя после регистрации = %q, want %q", got, want)
	}
}

// copyImage — копия образа в другой регион; возвращает имя, дошедшее до записи.
func copyImage(t *testing.T, name string) (error, string) { //nolint:revive // порядок возвратов удобен пробе
	t.Helper()
	var got domain.Image
	reader := &repomock.ImageReader{
		GetFunc: func(context.Context, string) (*domain.Image, error) {
			return &domain.Image{ID: "img00000000000000000", ProjectID: "prj-1",
				RegionID: "region-1", Name: "source", SourceSnapshot: "snp00000000000000000"}, nil
		},
	}
	writer := &repomock.ImageWriter{
		CopyFn: func(_ context.Context, i *domain.Image, _ string, _ []string) (*domain.Image, error) {
			got = *i
			out := *i
			return &out, nil
		},
	}
	ops := repomock.NewOpsRepo()
	uc := image.New(reader, writer, imageGeo(), imageIAM(), ops, serviceerr.ToStatus).
		WithInstallPrefix(testInstallPrefix)
	op, err := uc.Copy(context.Background(), image.CopyInput{
		ProjectID: "prj-1", ImageID: "img00000000000000000",
		TargetRegionID: "region-2", Name: name,
	})
	if err != nil {
		return err, ""
	}
	if done := repomock.AwaitOpDone(t, ops, op.ID); done.Error != nil {
		t.Fatalf("Copy(name=%q) op error = %v", name, done.Error)
	}
	return nil, got.Name
}

// TestCopyRejectsMalformedNameSynchronously — ДЕФЕКТ: копия не проверяла имя
// вовсе. Малформ доезжал до DB-CHECK и возвращался АСИНХРОННО в ошибке
// операции, тогда как копия снимка отвергает его синхронно. Один контракт, два
// исполнения — ровно то, что снимает единая форма.
func TestCopyRejectsMalformedNameSynchronously(t *testing.T) {
	err, _ := copyImage(t, "Bad_Name")
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Copy(name=%q) code = %v, want синхронный InvalidArgument", "Bad_Name", status.Code(err))
	}
}

// TestCopyEmptyNameIsSubstitutedBeforeWrite — копия без имени доходит до записи
// с непустым именем.
func TestCopyEmptyNameIsSubstitutedBeforeWrite(t *testing.T) {
	err, got := copyImage(t, "")
	if err != nil {
		t.Fatalf("Copy без имени отвергнута: %v", err)
	}
	if got == "" {
		t.Fatal("копия дошла до записи с ПУСТЫМ именем")
	}
}

// TestCopyKeepsTheNameTheCallerGave — положительный контроль к обеим пробам
// выше: законное имя копия принимает и доносит до записи.
func TestCopyKeepsTheNameTheCallerGave(t *testing.T) {
	const want = "img-copy"
	err, got := copyImage(t, want)
	if err != nil {
		t.Fatalf("Copy(name=%q): %v", want, err)
	}
	if got != want {
		t.Fatalf("имя после копии = %q, want %q", got, want)
	}
}

// ── Update: снять имя нельзя, полный PATCH этим не ломается ──────────────────

func updateImageName(t *testing.T, mask []string, name string) (error, *image.ImageUpdate) { //nolint:revive // порядок возвратов удобен пробе
	t.Helper()
	var applied image.ImageUpdate
	writer := &repomock.ImageWriter{
		UpdateFunc: func(_ context.Context, _ string, u image.ImageUpdate) (*domain.Image, error) {
			applied = u
			return &domain.Image{ID: imgUpdID, Name: "kept"}, nil
		},
	}
	ops := repomock.NewOpsRepo()
	uc := image.New(&repomock.ImageReader{}, writer, imageGeo(), imageIAM(), ops, serviceerr.ToStatus).
		WithInstallPrefix(testInstallPrefix)
	op, err := uc.Update(context.Background(), imgUpdID, mask, name, "", nil)
	if err != nil {
		return err, nil
	}
	if done := repomock.AwaitOpDone(t, ops, op.ID); done.Error != nil {
		t.Fatalf("Update mask=%v name=%q op error = %v", mask, name, done.Error)
	}
	return nil, &applied
}

// TestUpdateMaskNamesNameAndValueIsEmpty — маска НАЗЫВАЕТ имя, значение пусто:
// это «сними имя», а снять его нельзя → синхронный INVALID_ARGUMENT.
//
// Утверждается МЕСТО и МЕСТО ТОЛЬКО: у имени контрактный текст сам называет поле
// («Illegal argument name», §1.7) — тот же, что отдают Create и отказ по форме.
// Он закреплён кейсами чёрного ящика (snapshot.py SNP-UPD-VAL-NAME-UPPERCASE), и
// второй тон на одном поле сделал бы «снять имя» и «имя не той формы»
// различимыми по строке, а не по смыслу.
func TestUpdateMaskNamesNameAndValueIsEmpty(t *testing.T) {
	err, _ := updateImageName(t, []string{"name"}, "")
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Update mask=[name] name=\"\" code = %v, want InvalidArgument", status.Code(err))
	}
	if got := status.Convert(err).Message(); got != "Illegal argument name" {
		t.Fatalf("сообщение отказа = %q, want контрактный текст, называющий поле", got)
	}
}

// TestUpdateMaskNamesNameAndValueIsLegal — положительный контроль.
func TestUpdateMaskNamesNameAndValueIsLegal(t *testing.T) {
	err, applied := updateImageName(t, []string{"name"}, "renamed")
	if err != nil {
		t.Fatalf("Update mask=[name] name=%q: %v", "renamed", err)
	}
	if applied.Name == nil || *applied.Name != "renamed" {
		t.Fatalf("до записи дошло имя %v, want %q", applied.Name, "renamed")
	}
}

// TestFullPatchWithEmptyNameLeavesTheNameAlone — пустая маска: пустое имя
// означает «не прислали», поэтому проходит и НЕ пишется.
func TestFullPatchWithEmptyNameLeavesTheNameAlone(t *testing.T) {
	err, applied := updateImageName(t, nil, "")
	if err != nil {
		t.Fatalf("полный PATCH с пустым именем отвергнут: %v", err)
	}
	if applied.Name != nil {
		t.Fatalf("полный PATCH записал имя %q", *applied.Name)
	}
}

// TestFullPatchWithNamedValueStillApplies — положительный контроль к пробе выше.
func TestFullPatchWithNamedValueStillApplies(t *testing.T) {
	err, applied := updateImageName(t, nil, "renamed")
	if err != nil {
		t.Fatalf("полный PATCH с именем отвергнут: %v", err)
	}
	if applied.Name == nil || *applied.Name != "renamed" {
		t.Fatalf("полный PATCH не применил имя: %v", applied.Name)
	}
}
