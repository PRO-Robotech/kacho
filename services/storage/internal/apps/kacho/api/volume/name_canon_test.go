// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package volume_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/repomock"
)

// ── Пустое имя не доживает до записи (#715) ──────────────────────────────────
//
// До этой правки том создавался БЕЗ имени: форма имени пустую строку пропускала,
// а подстановки не было вовсе, поэтому частичный индекс `WHERE name <> ''` нёс
// нагрузку — он существовал ровно затем, чтобы безымянные строки не спорили за
// уникальность. Утверждается наблюдаемое: строка, дошедшая до записи, несёт
// НЕПУСТОЕ имя, и два безымянных создания в одном проекте получают РАЗНЫЕ имена.

// newCreateUC собирает use-case создания, захватывая том, дошедший до записи.
func newCreateUC(t *testing.T, captured *domain.Volume) (*volume.UseCase, *repomock.OpsRepo) {
	t.Helper()
	writer := &repomock.VolumeWriter{
		InsertFunc: func(_ context.Context, v *domain.Volume) (*domain.Volume, error) {
			*captured = *v
			out := *v
			out.Status = domain.VolumeStatusAvailable
			return &out, nil
		},
	}
	geo := &repomock.PeerClient{EnsureZoneFunc: func(context.Context, string) error { return nil }}
	iam := &repomock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }}
	ops := repomock.NewOpsRepo()
	return volume.New(&repomock.VolumeReader{}, writer, geo, iam, ops, serviceerr.ToStatus).
		WithInstallPrefix(testInstallPrefix), ops
}

// createVolume — создание тома с заданным именем; возвращает имя, дошедшее до записи.
func createVolume(t *testing.T, name string) string {
	t.Helper()
	var got domain.Volume
	uc, ops := newCreateUC(t, &got)
	op, err := uc.Create(context.Background(), &domain.Volume{
		ProjectID: "prj-1", Name: name, ZoneID: "region-1-a",
		DiskTypeID: "block-balanced", SizeBytes: 1 << 30,
	})
	if err != nil {
		t.Fatalf("Create(name=%q) sync err = %v", name, err)
	}
	if done := repomock.AwaitOpDone(t, ops, op.ID); done.Error != nil {
		t.Fatalf("Create(name=%q) op error = %v", name, done.Error)
	}
	return got.Name
}

// TestCreateEmptyNameIsSubstitutedBeforeWrite — создание без имени доходит до
// записи с НЕПУСТЫМ именем, производным от идентификатора.
func TestCreateEmptyNameIsSubstitutedBeforeWrite(t *testing.T) {
	if got := createVolume(t, ""); got == "" {
		t.Fatal("том дошёл до записи с ПУСТЫМ именем — подстановки не произошло")
	}
}

// TestCreateKeepsTheNameTheCallerGave — положительный контроль к пробе выше:
// подстановка не трогает имя, которое вызывающий назвал сам. Без него проба
// «имя непусто» зеленела бы и на реализации, подставляющей умолчание ВСЕГДА.
func TestCreateKeepsTheNameTheCallerGave(t *testing.T) {
	const want = "vol-a"
	if got := createVolume(t, want); got != want {
		t.Fatalf("имя после создания = %q, want %q", got, want)
	}
}

// TestTwoUnnamedVolumesGetDifferentNames — два безымянных создания в ОДНОМ
// проекте проходят оба и получают РАЗНЫЕ имена. Умолчание, одинаковое для двух
// строк, заняло бы `UNIQUE(project,name)` и превратило второе создание в
// ALREADY_EXISTS — то есть подстановка сломала бы то, ради чего заведена.
func TestTwoUnnamedVolumesGetDifferentNames(t *testing.T) {
	first, second := createVolume(t, ""), createVolume(t, "")
	if first == "" || second == "" {
		t.Fatalf("имена = (%q, %q), оба обязаны быть непусты", first, second)
	}
	if first == second {
		t.Fatalf("два безымянных тома получили ОДНО имя %q — второй занял бы слот первого", first)
	}
}

// TestCreateRejectsMalformedName — отрицание в паре с положительными выше:
// подстановка не отменяет проверку формы.
func TestCreateRejectsMalformedName(t *testing.T) {
	var got domain.Volume
	uc, _ := newCreateUC(t, &got)
	_, err := uc.Create(context.Background(), &domain.Volume{
		ProjectID: "prj-1", Name: "Bad_Name", ZoneID: "region-1-a",
		DiskTypeID: "block-balanced", SizeBytes: 1 << 30,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Create(name=%q) code = %v, want InvalidArgument", "Bad_Name", status.Code(err))
	}
}

// ── Update: снять имя нельзя, но полный PATCH этим не ломается ───────────────

// updateName зовёт Update с заданной маской и именем; возвращает ошибку и то,
// что дошло до записи.
func updateName(t *testing.T, mask []string, name string) (error, *volume.VolumeUpdate) { //nolint:revive // порядок возвратов удобен пробе
	t.Helper()
	var applied volume.VolumeUpdate
	writer := &repomock.VolumeWriter{
		UpdateFunc: func(_ context.Context, _ string, u volume.VolumeUpdate) (*domain.Volume, error) {
			applied = u
			return &domain.Volume{ID: volUpdID, Name: "kept"}, nil
		},
	}
	ops := repomock.NewOpsRepo()
	uc := volume.New(&repomock.VolumeReader{}, writer,
		&repomock.PeerClient{}, &repomock.PeerClient{}, ops, serviceerr.ToStatus).
		WithInstallPrefix(testInstallPrefix)
	op, err := uc.Update(context.Background(), volUpdID, mask, name, "", nil, 0)
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
	err, _ := updateName(t, []string{"name"}, "")
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("Update mask=[name] name=\"\" code = %v, want InvalidArgument", status.Code(err))
	}
	if got := status.Convert(err).Message(); got != "Illegal argument name" {
		t.Fatalf("сообщение отказа = %q, want контрактный текст, называющий поле", got)
	}
}

// TestUpdateMaskNamesNameAndValueIsLegal — положительный контроль: та же полоса
// на законном имени проходит и доносит его до записи.
func TestUpdateMaskNamesNameAndValueIsLegal(t *testing.T) {
	err, applied := updateName(t, []string{"name"}, "renamed")
	if err != nil {
		t.Fatalf("Update mask=[name] name=%q: %v", "renamed", err)
	}
	if applied.Name == nil || *applied.Name != "renamed" {
		t.Fatalf("до записи дошло имя %v, want %q", applied.Name, "renamed")
	}
}

// TestFullPatchWithEmptyNameLeavesTheNameAlone — пустая маска: в proto3
// пропущенное и пустое имя НЕРАЗЛИЧИМЫ, поэтому пустое здесь означает «не
// прислали». Отказ сломал бы каждого, кто правит описание полным PATCH'ем;
// запись пустой строки вернула бы безымянный ресурс той же дверью, которую
// закрывает отказ выше. Значит: проходит И имя не пишется.
func TestFullPatchWithEmptyNameLeavesTheNameAlone(t *testing.T) {
	err, applied := updateName(t, nil, "")
	if err != nil {
		t.Fatalf("полный PATCH с пустым именем отвергнут: %v", err)
	}
	if applied.Name != nil {
		t.Fatalf("полный PATCH записал имя %q — имя обязано остаться нетронутым", *applied.Name)
	}
}

// TestFullPatchWithNamedValueStillApplies — положительный контроль к пробе выше:
// полный PATCH с НАЗВАННЫМ именем по-прежнему его применяет.
func TestFullPatchWithNamedValueStillApplies(t *testing.T) {
	err, applied := updateName(t, nil, "renamed")
	if err != nil {
		t.Fatalf("полный PATCH с именем отвергнут: %v", err)
	}
	if applied.Name == nil || *applied.Name != "renamed" {
		t.Fatalf("полный PATCH не применил имя: %v", applied.Name)
	}
}
