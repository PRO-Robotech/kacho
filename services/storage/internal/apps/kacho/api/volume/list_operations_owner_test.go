// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package volume_test

// Список операций ресурса отдаёт операции САМОГО вызывающего.
//
// Утверждение — на наблюдаемом: строки, которые вернул use-case. Фейк
// repomock.OpsRepo реализует оба пути (несуженный List и суженный ListOwned),
// поэтому тест краснеет ровно тогда, когда вызывается несуженный.

import (
	"context"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/volume"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/repomock"
)

func TestListOperations_ReturnsOnlyCallerOwnRows(t *testing.T) {
	ops := repomock.NewOpsRepo()
	uc := volume.New(&repomock.VolumeReader{}, &repomock.VolumeWriter{}, &repomock.PeerClient{}, &repomock.PeerClient{}, ops, serviceerr.ToStatus).WithInstallPrefix(testInstallPrefix)
	resID := ids.NewID(domain.PrefixVolume)

	me := operations.Principal{Type: "user", ID: "usr-me", DisplayName: "me@kacho.local"}
	other := operations.Principal{Type: "user", ID: "usr-other", DisplayName: "other@kacho.local"}
	bg := context.Background()
	if err := ops.CreateWithPrincipal(bg, operations.Operation{ID: "op-mine", ResourceID: resID}, me); err != nil {
		t.Fatalf("seed own op: %v", err)
	}
	if err := ops.CreateWithPrincipal(bg, operations.Operation{ID: "op-foreign", ResourceID: resID}, other); err != nil {
		t.Fatalf("seed foreign op: %v", err)
	}

	got, _, err := uc.ListOperations(operations.WithPrincipal(bg, me), resID, volume.Pagination{})
	if err != nil {
		t.Fatalf("ListOperations: %v", err)
	}
	seen := map[string]bool{}
	for _, op := range got {
		seen[op.ID] = true
	}
	if !seen["op-mine"] {
		t.Fatalf("своя операция обязана присутствовать, получено %+v", got)
	}
	if seen["op-foreign"] {
		t.Fatalf("чужая операция попала в список: её Response несёт ресурс целиком, " +
			"а Principal — email инициатора")
	}
}

// Без ключа владения выдача пуста: несуженный откат запрещён.
func TestListOperations_UnidentifiedCallerGetsNoRows(t *testing.T) {
	ops := repomock.NewOpsRepo()
	uc := volume.New(&repomock.VolumeReader{}, &repomock.VolumeWriter{}, &repomock.PeerClient{}, &repomock.PeerClient{}, ops, serviceerr.ToStatus).WithInstallPrefix(testInstallPrefix)
	resID := ids.NewID(domain.PrefixVolume)

	other := operations.Principal{Type: "user", ID: "usr-other", DisplayName: "other@kacho.local"}
	if err := ops.CreateWithPrincipal(context.Background(),
		operations.Operation{ID: "op-foreign", ResourceID: resID}, other); err != nil {
		t.Fatalf("seed foreign op: %v", err)
	}

	got, _, err := uc.ListOperations(context.Background(), resID, volume.Pagination{})
	if err != nil {
		t.Fatalf("ListOperations: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("без ключа владения выдача обязана быть пустой, получено %+v", got)
	}
}
