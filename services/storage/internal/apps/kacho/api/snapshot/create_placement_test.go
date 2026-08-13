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

// testInstallPrefix — префикс установки для проб снимка.
//
// Имя объекта у бэкенда выводится из него и неизменяемого идентификатора СНИМКА:
// снимок адресуется самостоятельно, потому что он переживает свой том. Пробы
// задают префикс явно, а не получают умолчанием, — иначе его отсутствие перестало
// бы быть наблюдаемым отказом (см. TestSnapshotCreateWithoutInstallPrefixIsRefused).
const testInstallPrefix = "kctest"

// TestSnapshotCreateWithoutInstallPrefixIsRefused — посадка без префикса установки
// не создаёт снимков, и отказ приходит ДО обращения к соседям.
//
// Код именно UNAVAILABLE: арендатор не сделал ничего неверного — сервис в этой
// посадке неспособен. FAILED_PRECONDITION или INVALID_ARGUMENT отправили бы его
// чинить собственный ввод, которого чинить нечего.
//
// Порядок несущий и проверяется отдельно: сосед, опрошенный ради запроса, который
// не может быть исполнен ни при каком ответе, — это трата чужого бюджета и лишняя
// точка отказа. Поэтому оба фейка роняют пробу самим фактом вызова.
func TestSnapshotCreateWithoutInstallPrefixIsRefused(t *testing.T) {
	iam := &repomock.PeerClient{
		EnsureProjectFunc: func(context.Context, string) error {
			t.Fatal("владелец проекта опрошен на посадке, которая снимок не создаст ни при каком ответе")
			return nil
		},
	}
	repo := &repomock.SnapshotRepo{
		InsertFunc: func(context.Context, *domain.Snapshot) (*domain.Snapshot, error) {
			t.Fatal("вставка вызвана без префикса установки: имя объекта выводить не из чего")
			return nil, nil
		},
	}
	uc := snapshot.New(repo, iam, nil, serviceerr.ToStatus).WithDataPlane(true)

	_, err := uc.Create(context.Background(), &domain.Snapshot{
		ProjectID: "prj-1", SourceVolumeID: "vol00000000000000000",
	})
	if err == nil {
		t.Fatal("посадка без префикса установки обязана отказывать в создании снимка")
	}
	if got := status.Code(err); got != codes.Unavailable {
		t.Errorf("код %s: отказ обязан говорить о неспособности сервиса, а не о неверном вводе", got)
	}
}

// TestSnapshotCreateDerivesBackendObjectFromOwnID — положительный контроль к пробе
// выше: с префиксом создание доходит до вставки, и имя объекта выведено из
// идентификатора СНИМКА, а не тома.
//
// Без этой пары отказ выше зеленел бы на реализации, которая не создаёт снимков
// вовсе, а имя объекта — на реализации, которая выводит его из источника: такое имя
// пережило бы свой том и указывало бы на объект, которого нет.
func TestSnapshotCreateDerivesBackendObjectFromOwnID(t *testing.T) {
	ops := repomock.NewOpsRepo()
	seen := make(chan *domain.Snapshot, 1)
	repo := &repomock.SnapshotRepo{
		InsertFunc: func(_ context.Context, s *domain.Snapshot) (*domain.Snapshot, error) {
			cp := *s
			seen <- &cp
			return &cp, nil
		},
	}
	iam := &repomock.PeerClient{EnsureProjectFunc: func(context.Context, string) error { return nil }}
	uc := snapshot.New(repo, iam, ops, serviceerr.ToStatus).WithInstallPrefix(testInstallPrefix)

	op, err := uc.Create(context.Background(), &domain.Snapshot{
		ProjectID: "prj-1", SourceVolumeID: "vol00000000000000000",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	repomock.AwaitOpDone(t, ops, op.ID)

	var got *domain.Snapshot
	select {
	case got = <-seen:
	default:
		t.Fatal("вставка не вызвана: положительный контроль обязан доходить до репозитория")
	}
	if got.ID == "" {
		t.Fatal("идентификатор снимка не выделен до вставки")
	}
	if want := testInstallPrefix + "-" + got.ID; got.Backend.BackendObject != want {
		t.Errorf("имя объекта = %q, ожидалось %q (префикс установки + идентификатор снимка)",
			got.Backend.BackendObject, want)
	}
}
