// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package disktype_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/disktype"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/repomock"
)

// recordingRepo — дублёр репозитория, запоминающий НАБОР изменений, который до него
// доехал, и факт вызова. Утверждать надо именно набор: «правка вернула класс»
// остаётся зелёным и тогда, когда репозиторий переписал поля, которых маска не
// называла, — то есть ровно на том дефекте, ради которого маска и заводится.
type recordingRepo struct {
	*repomock.DiskTypeRepo
	got    disktype.DiskTypeUpdate
	called bool
}

func newRecordingRepo() *recordingRepo {
	r := &recordingRepo{}
	r.DiskTypeRepo = &repomock.DiskTypeRepo{
		UpdateFunc: func(_ context.Context, id string, u disktype.DiskTypeUpdate) (*domain.DiskType, error) {
			r.called, r.got = true, u
			return &domain.DiskType{ID: id, Name: "любой", Lifecycle: domain.LifecycleActive}, nil
		},
	}
	return r
}

// setFields — какие поля набора названы. Имена — пути маски, чтобы падение называло
// то же слово, которым поле зовут в запросе.
func setFields(u disktype.DiskTypeUpdate) map[string]bool {
	return map[string]bool{
		"name":        u.Name != nil,
		"description": u.Description != nil,
		"zone_ids":    u.ZoneIDs != nil,
		"tier":        u.PerformanceTier != nil,
		"limits":      u.MinSizeBytes != nil || u.MaxSizeBytes != nil || u.SizeStepBytes != nil,
		"lifecycle":   u.Lifecycle != nil,
	}
}

// TestUpdateAdminAppliesOnlyMaskedFields (STOR-P-06) — поле, которого маска не
// назвала, до репозитория не доезжает, даже когда тело его несёт. Раньше правка была
// полным замещением: запрос, менявший одно имя, снимал класс со ВСЕХ зон — молча, а
// от списка зон зависит размещение данных.
//
// Каждое отрицание в паре с положительным контролем: та же зона, названная маской,
// доезжает — иначе проба зеленела бы на правке, не делающей вообще ничего.
func TestUpdateAdminAppliesOnlyMaskedFields(t *testing.T) {
	const (
		newName  = "переименованный"
		bodyDesc = "тело несёт описание, маска его не называет"
	)
	bodyZones := []string{"ru-central1-c"}

	repo := newRecordingRepo()
	if _, err := disktype.New(repo).UpdateAdmin(context.Background(), "block-mask",
		[]string{"name"}, newName, bodyDesc, bodyZones,
		domain.TierFast, domain.SizeLimits{MinSizeBytes: 1 << 30}); err != nil {
		t.Fatalf("UpdateAdmin mask=[name] err = %v", err)
	}
	if !repo.called {
		t.Fatal("репозиторий не вызван — правка не дошла")
	}
	if repo.got.Name == nil || *repo.got.Name != newName {
		t.Fatalf("названное маской имя не доехало: %v", repo.got.Name)
	}
	for field, isSet := range setFields(repo.got) {
		if field == "name" {
			continue
		}
		if isSet {
			t.Errorf("поле %q маской не названо, но уехало в репозиторий", field)
		}
	}

	// Положительный контроль: то же поле зон, названное маской, доезжает целиком, а
	// имя из тела — нет.
	repo = newRecordingRepo()
	if _, err := disktype.New(repo).UpdateAdmin(context.Background(), "block-mask",
		[]string{"zone_ids"}, newName, bodyDesc, bodyZones,
		domain.TierFast, domain.SizeLimits{MinSizeBytes: 1 << 30}); err != nil {
		t.Fatalf("UpdateAdmin mask=[zone_ids] err = %v", err)
	}
	if repo.got.ZoneIDs == nil || len(*repo.got.ZoneIDs) != 1 || (*repo.got.ZoneIDs)[0] != bodyZones[0] {
		t.Fatalf("названные маской зоны не доехали: %v", repo.got.ZoneIDs)
	}
	if repo.got.Name != nil {
		t.Errorf("имя маской не названо, но уехало в репозиторий: %q", *repo.got.Name)
	}
}

// TestUpdateAdminEmptyMaskPatchesAllMutable — пустая маска остаётся полной заменой
// ИЗМЕНЯЕМЫХ полей (единая дисциплина платформы, api-conventions.md). Состояние
// обращения в их число не входит: его меняет глагол SetLifecycle, иначе правка
// описания вернула бы выведенный класс в обращение — исход, которого вызывающий не
// заявлял.
func TestUpdateAdminEmptyMaskPatchesAllMutable(t *testing.T) {
	repo := newRecordingRepo()
	if _, err := disktype.New(repo).UpdateAdmin(context.Background(), "block-mask",
		nil, "имя", "описание", []string{"ru-central1-a"},
		domain.TierBalanced, domain.SizeLimits{MinSizeBytes: 1 << 30, MaxSizeBytes: 16 << 40, SizeStepBytes: 1 << 30}); err != nil {
		t.Fatalf("UpdateAdmin mask=[] err = %v", err)
	}
	for field, isSet := range setFields(repo.got) {
		switch field {
		case "lifecycle":
			if isSet {
				t.Error("состояние обращения не изменяется правкой — только глаголом SetLifecycle")
			}
		default:
			if !isSet {
				t.Errorf("пустая маска обязана заместить изменяемое поле %q", field)
			}
		}
	}
}

// TestUpdateAdminRejectsUnknownMaskField — неизвестное поле маски отвергается
// синхронно, до репозитория. Взяты два входа: снятое с контракта имя
// `performance_tier` (под ним лежала другая природа значения — свободная строка) и
// просто чужое слово. Пара — положительный контроль: действующее имя `tier`
// проходит, иначе проба зеленела бы на маске, отвергающей вообще всё.
func TestUpdateAdminRejectsUnknownMaskField(t *testing.T) {
	for _, field := range []string{"performance_tier", "zones", "capacity"} {
		repo := newRecordingRepo()
		_, err := disktype.New(repo).UpdateAdmin(context.Background(), "block-mask",
			[]string{field}, "имя", "", nil, domain.TierFast, domain.SizeLimits{})
		if code := status.Code(serviceerr.ToStatus(err)); code != codes.InvalidArgument {
			t.Fatalf("маска=%q код = %v, want InvalidArgument", field, code)
		}
		if repo.called {
			t.Errorf("маска=%q дошла до репозитория — отказ обязан быть до записи", field)
		}
	}

	repo := newRecordingRepo()
	if _, err := disktype.New(repo).UpdateAdmin(context.Background(), "block-mask",
		[]string{"tier"}, "имя", "", nil, domain.TierFast, domain.SizeLimits{}); err != nil {
		t.Fatalf("действующее имя поля маски отвергнуто: %v", err)
	}
	if repo.got.PerformanceTier == nil || *repo.got.PerformanceTier != domain.TierFast {
		t.Fatalf("названный маской ярус не доехал: %v", repo.got.PerformanceTier)
	}
}

// TestUpdateAdminRejectsImmutableMaskField — неизменяемое поле в маске отвергается
// КОНВЕНЦИОННЫМ текстом, а не общим «unknown field»: проверка стоит ДО сверки со
// множеством известных полей, потому что известными считаются только изменяемые.
//
// Три поля — три разные причины неизменяемости, и все три должны звучать одинаково
// для вызывающего: id адресует ресурс; lifecycle меняется глаголом SetLifecycle;
// capabilities ВЫВОДЯТСЯ из действующих ревизий привязки и на вход не принимаются.
func TestUpdateAdminRejectsImmutableMaskField(t *testing.T) {
	for _, field := range []string{"id", "lifecycle", "capabilities"} {
		repo := newRecordingRepo()
		_, err := disktype.New(repo).UpdateAdmin(context.Background(), "block-mask",
			[]string{field}, "имя", "", nil, domain.TierFast, domain.SizeLimits{})
		st := serviceerr.ToStatus(err)
		if status.Code(st) != codes.InvalidArgument {
			t.Fatalf("маска=%q код = %v, want InvalidArgument", field, status.Code(st))
		}
		want := field + " is immutable after DiskType.Create"
		if got := status.Convert(st).Message(); got != want {
			t.Fatalf("маска=%q текст = %q, want %q", field, got, want)
		}
		if repo.called {
			t.Errorf("маска=%q дошла до репозитория — отказ обязан быть до записи", field)
		}
	}
}
