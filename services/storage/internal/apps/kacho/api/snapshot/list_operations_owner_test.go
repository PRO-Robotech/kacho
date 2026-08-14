// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package snapshot_test

// Журнал операций снимка отдаёт операции САМОГО вызывающего.
//
// Утверждение — на наблюдаемом: строки, которые вернул use-case. Фейк
// repomock.OpsRepo реализует оба пути (несуженный List и суженный ListOwned),
// поэтому проба краснеет ровно тогда, когда вызывается несуженный.
//
// Пара к volume: у тома и образа журнал был, у снимка — нет, хотя операции у него
// те же три. Отсутствие журнала означает, что об отказе создания снимка вызывающий
// узнаёт только из ответа на сам запрос: потерял идентификатор операции — потерял
// причину.

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/api/snapshot"
	"github.com/PRO-Robotech/kacho/services/storage/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/repo/repomock"
)

func TestSnapshotListOperations_ReturnsOnlyCallerOwnRows(t *testing.T) {
	ops := repomock.NewOpsRepo()
	uc := snapshot.New(&repomock.SnapshotRepo{}, &repomock.PeerClient{}, ops, serviceerr.ToStatus).
		WithInstallPrefix(testInstallPrefix)
	resID := ids.NewID(domain.PrefixSnapshot)

	me := operations.Principal{Type: "user", ID: "usr-me", DisplayName: "me@kacho.local"}
	other := operations.Principal{Type: "user", ID: "usr-other", DisplayName: "other@kacho.local"}
	bg := context.Background()
	if err := ops.CreateWithPrincipal(bg, operations.Operation{ID: "op-mine", ResourceID: resID}, me); err != nil {
		t.Fatalf("seed own op: %v", err)
	}
	if err := ops.CreateWithPrincipal(bg, operations.Operation{ID: "op-foreign", ResourceID: resID}, other); err != nil {
		t.Fatalf("seed foreign op: %v", err)
	}

	got, _, err := uc.ListOperations(operations.WithPrincipal(bg, me), resID, snapshot.Pagination{})
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
func TestSnapshotListOperations_UnidentifiedCallerGetsNoRows(t *testing.T) {
	ops := repomock.NewOpsRepo()
	uc := snapshot.New(&repomock.SnapshotRepo{}, &repomock.PeerClient{}, ops, serviceerr.ToStatus).
		WithInstallPrefix(testInstallPrefix)
	resID := ids.NewID(domain.PrefixSnapshot)

	other := operations.Principal{Type: "user", ID: "usr-other", DisplayName: "other@kacho.local"}
	if err := ops.CreateWithPrincipal(context.Background(),
		operations.Operation{ID: "op-foreign", ResourceID: resID}, other); err != nil {
		t.Fatalf("seed foreign op: %v", err)
	}

	got, _, err := uc.ListOperations(context.Background(), resID, snapshot.Pagination{})
	if err != nil {
		t.Fatalf("ListOperations: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("без ключа владения выдача обязана быть пустой, получено %+v", got)
	}
}

// TestSnapshotListOperationsRejectsMalformedID — журнал спрашивают по идентификатору
// снимка, и малформ отвергается ПЕРВЫМ стейтментом, как у Get: иначе мусорная строка
// уезжает в общий журнал операций и возвращает пустую страницу, то есть ответ
// «операций нет» на вопрос, который вообще не про снимок.
//
// Положительный контроль — пробы выше: корректный идентификатор доходит до журнала.
func TestSnapshotListOperationsRejectsMalformedID(t *testing.T) {
	uc := snapshot.New(&repomock.SnapshotRepo{}, &repomock.PeerClient{}, repomock.NewOpsRepo(), serviceerr.ToStatus).
		WithInstallPrefix(testInstallPrefix)

	_, _, err := uc.ListOperations(context.Background(), "not-a-snp-id", snapshot.Pagination{})
	if err == nil {
		t.Fatal("малформ обязан отвергаться первым стейтментом")
	}
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Errorf("код %s, ожидался InvalidArgument", got)
	}
	if got, want := status.Convert(err).Message(), "invalid snapshot id 'not-a-snp-id'"; got != want {
		t.Errorf("сообщение %q, контракт требует %q", got, want)
	}
}
