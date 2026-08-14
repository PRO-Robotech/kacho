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
)

// TestSetLifecycleWritesOnlyLifecycle — вывод класса из обращения меняет ТОЛЬКО
// состояние обращения. Отдельный глагол заведён именно ради этого: в теле правки
// состояние возвращало бы выведенный класс в обращение при пустой маске, молча.
//
// Утверждается набор изменений, а не возвращённый класс: «вернул класс» остаётся
// зелёным и тогда, когда вместе с состоянием переписаны имя, зоны и границы.
func TestSetLifecycleWritesOnlyLifecycle(t *testing.T) {
	for _, want := range []domain.DiskTypeLifecycle{
		domain.LifecycleActive, domain.LifecycleDeprecated, domain.LifecycleRetired,
	} {
		repo := newRecordingRepo()
		got, err := disktype.New(repo).SetLifecycle(context.Background(), "block-x", want)
		if err != nil {
			t.Fatalf("SetLifecycle(%q) err = %v", want, err)
		}
		if got == nil {
			t.Fatalf("SetLifecycle(%q) вернул пустой класс", want)
		}
		if repo.got.Lifecycle == nil || *repo.got.Lifecycle != want {
			t.Fatalf("SetLifecycle(%q): в репозиторий уехало %v", want, repo.got.Lifecycle)
		}
		for field, isSet := range setFields(repo.got) {
			if field != "lifecycle" && isSet {
				t.Errorf("SetLifecycle(%q) переписал поле %q, которого не касается", want, field)
			}
		}
	}
}

// TestSetLifecycleRejectsUnknownState — состояние вне словаря отвергается до записи.
// Пустое значение сюда приходит и от НЕНАЗВАННОГО состояния в запросе (конверсия
// отдаёт пустое, а не ACTIVE намеренно): умолчания у этого глагола нет, потому что
// «не указано» неотличимо от «верни в обращение», а такой исход обязан быть выбран.
//
// Отрицание в паре с положительным контролем выше: три известных состояния проходят.
func TestSetLifecycleRejectsUnknownState(t *testing.T) {
	for _, bad := range []domain.DiskTypeLifecycle{"", "DISABLED", "active", "DELETED"} {
		repo := newRecordingRepo()
		_, err := disktype.New(repo).SetLifecycle(context.Background(), "block-x", bad)
		if code := status.Code(serviceerr.ToStatus(err)); code != codes.InvalidArgument {
			t.Fatalf("SetLifecycle(%q) код = %v, want InvalidArgument", bad, code)
		}
		if repo.called {
			t.Errorf("SetLifecycle(%q) дошёл до репозитория — отказ обязан быть до записи", bad)
		}
	}
}

// TestSetLifecycleRequiresID — класс адресуется id, и пустой id отвергается здесь, а
// не превращается в промах репозитория: NotFound утверждал бы, что класс с таким
// именем искали и не нашли.
func TestSetLifecycleRequiresID(t *testing.T) {
	repo := newRecordingRepo()
	_, err := disktype.New(repo).SetLifecycle(context.Background(), "", domain.LifecycleRetired)
	if code := status.Code(serviceerr.ToStatus(err)); code != codes.InvalidArgument {
		t.Fatalf("SetLifecycle без id код = %v, want InvalidArgument", code)
	}
	if repo.called {
		t.Error("SetLifecycle без id дошёл до репозитория")
	}
}
