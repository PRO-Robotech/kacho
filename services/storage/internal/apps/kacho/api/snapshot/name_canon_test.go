// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package snapshot_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/repomock"
)

// ── Пустое имя не доживает до записи (#715) ──────────────────────────────────
//
// Тот же предмет, что у тома, и обе полосы появления снимка проверяются
// раздельно: снятие с тома и КОПИЯ другого снимка. Копия — отдельный путь со
// своим минтом идентификатора, и подстановка на одном пути ничего не говорит о
// другом.

// createSnapshot — снятие снимка с заданным именем; возвращает имя, дошедшее до записи.
func createSnapshot(t *testing.T, name string) string {
	t.Helper()
	var got domain.Snapshot
	repo := &repomock.SnapshotRepo{
		InsertFunc: func(_ context.Context, s *domain.Snapshot) (*domain.Snapshot, error) {
			got = *s
			out := *s
			return &out, nil
		},
	}
	iam := &repomock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }}
	ops := repomock.NewOpsRepo()
	uc := snapshot.New(repo, iam, ops, serviceerr.ToStatus).WithInstallPrefix(testInstallPrefix)
	op, err := uc.Create(context.Background(), &domain.Snapshot{
		ProjectID: "prj-1", SourceVolumeID: "vol00000000000000000", Name: name,
	})
	if err != nil {
		t.Fatalf("Create(name=%q) sync err = %v", name, err)
	}
	if done := repomock.AwaitOpDone(t, ops, op.ID); done.Error != nil {
		t.Fatalf("Create(name=%q) op error = %v", name, done.Error)
	}
	return got.Name
}

// TestCreateEmptyNameIsSubstitutedBeforeWrite — снятие без имени доходит до
// записи с непустым именем.
func TestCreateEmptyNameIsSubstitutedBeforeWrite(t *testing.T) {
	if got := createSnapshot(t, ""); got == "" {
		t.Fatal("снимок дошёл до записи с ПУСТЫМ именем — подстановки не произошло")
	}
}

// TestCreateKeepsTheNameTheCallerGave — положительный контроль: названное имя
// подстановка не трогает.
func TestCreateKeepsTheNameTheCallerGave(t *testing.T) {
	const want = "snap-01"
	if got := createSnapshot(t, want); got != want {
		t.Fatalf("имя после создания = %q, want %q", got, want)
	}
}

// TestTwoUnnamedSnapshotsGetDifferentNames — два безымянных снятия в одном
// проекте получают РАЗНЫЕ имена (иначе второе заняло бы слот первого).
func TestTwoUnnamedSnapshotsGetDifferentNames(t *testing.T) {
	first, second := createSnapshot(t, ""), createSnapshot(t, "")
	if first == "" || second == "" {
		t.Fatalf("имена = (%q, %q), оба обязаны быть непусты", first, second)
	}
	if first == second {
		t.Fatalf("два безымянных снимка получили ОДНО имя %q", first)
	}
}

// copySnapshot — копия снимка с заданным именем; возвращает ошибку и имя,
// дошедшее до записи.
func copySnapshot(t *testing.T, name string) (error, string) { //nolint:revive // порядок возвратов удобен пробе
	t.Helper()
	var got domain.Snapshot
	repo := &repomock.SnapshotRepo{
		GetFunc: func(context.Context, string) (*domain.Snapshot, error) {
			return &domain.Snapshot{ID: "snp00000000000000000", ProjectID: "prj-1",
				SourceVolumeID: "vol00000000000000000", Name: "source"}, nil
		},
		CopyFn: func(_ context.Context, s *domain.Snapshot, _, _ string) (*domain.Snapshot, error) {
			got = *s
			out := *s
			return &out, nil
		},
	}
	geo := &repomock.PeerClient{EnsureZoneFunc: func(context.Context, string) error { return nil }}
	iam := &repomock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }}
	ops := repomock.NewOpsRepo()
	uc := snapshot.New(repo, iam, ops, serviceerr.ToStatus).WithGeo(geo).WithInstallPrefix(testInstallPrefix)
	op, err := uc.Copy(context.Background(), snapshot.CopyInput{
		ProjectID: "prj-1", SnapshotID: "snp00000000000000000",
		TargetZoneID: "region-1-a", Name: name,
	})
	if err != nil {
		return err, ""
	}
	if done := repomock.AwaitOpDone(t, ops, op.ID); done.Error != nil {
		t.Fatalf("Copy(name=%q) op error = %v", name, done.Error)
	}
	return nil, got.Name
}

// TestCopyEmptyNameIsSubstitutedBeforeWrite — копия без имени тоже доходит до
// записи с непустым именем: путь другой, правило одно.
func TestCopyEmptyNameIsSubstitutedBeforeWrite(t *testing.T) {
	err, got := copySnapshot(t, "")
	if err != nil {
		t.Fatalf("Copy без имени отвергнута: %v", err)
	}
	if got == "" {
		t.Fatal("копия дошла до записи с ПУСТЫМ именем")
	}
}

// TestCopyRejectsMalformedName — отрицание в паре с положительным выше.
func TestCopyRejectsMalformedName(t *testing.T) {
	err, _ := copySnapshot(t, "Bad_Name")
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Copy(name=%q) code = %v, want InvalidArgument", "Bad_Name", status.Code(err))
	}
}

// ── Update: снять имя нельзя, полный PATCH этим не ломается ──────────────────

func updateSnapshotName(t *testing.T, mask []string, name string) (error, *snapshot.SnapshotUpdate) { //nolint:revive // порядок возвратов удобен пробе
	t.Helper()
	var applied snapshot.SnapshotUpdate
	repo := &repomock.SnapshotRepo{
		UpdateFunc: func(_ context.Context, _ string, u snapshot.SnapshotUpdate) (*domain.Snapshot, error) {
			applied = u
			return &domain.Snapshot{ID: snapUpdID, Name: "kept"}, nil
		},
	}
	ops := repomock.NewOpsRepo()
	uc := snapshot.New(repo, &repomock.PeerClient{}, ops, serviceerr.ToStatus).WithInstallPrefix(testInstallPrefix)
	op, err := uc.Update(context.Background(), snapUpdID, mask, name, "", nil)
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
	err, _ := updateSnapshotName(t, []string{"name"}, "")
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Update mask=[name] name=\"\" code = %v, want InvalidArgument", status.Code(err))
	}
	if got := status.Convert(err).Message(); got != "Illegal argument name" {
		t.Fatalf("сообщение отказа = %q, want контрактный текст, называющий поле", got)
	}
}

// TestUpdateMaskNamesNameAndValueIsLegal — положительный контроль.
func TestUpdateMaskNamesNameAndValueIsLegal(t *testing.T) {
	err, applied := updateSnapshotName(t, []string{"name"}, "renamed")
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
	err, applied := updateSnapshotName(t, nil, "")
	if err != nil {
		t.Fatalf("полный PATCH с пустым именем отвергнут: %v", err)
	}
	if applied.Name != nil {
		t.Fatalf("полный PATCH записал имя %q", *applied.Name)
	}
}

// TestFullPatchWithNamedValueStillApplies — положительный контроль к пробе выше.
func TestFullPatchWithNamedValueStillApplies(t *testing.T) {
	err, applied := updateSnapshotName(t, nil, "renamed")
	if err != nil {
		t.Fatalf("полный PATCH с именем отвергнут: %v", err)
	}
	if applied.Name == nil || *applied.Name != "renamed" {
		t.Fatalf("полный PATCH не применил имя: %v", applied.Name)
	}
}
