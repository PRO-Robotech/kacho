// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/pbconv"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
)

// seedNetworks помещает N networks в репозиторий через writer-TX. Общий helper
// для list-фильтр-тестов (project-level authz).
func seedNetworks(t *testing.T, kr *kachomock.Repository, projectID string, ids ...string) []*kacho.NetworkRecord {
	t.Helper()
	w, err := kr.Writer(context.Background())
	require.NoError(t, err)
	defer w.Abort()
	var out []*kacho.NetworkRecord
	for _, id := range ids {
		n := &domain.Network{ID: id, ProjectID: projectID, Name: domain.RcNameVPC("net-" + id)}
		rec, ierr := w.Networks().Insert(context.Background(), n)
		require.NoError(t, ierr)
		out = append(out, rec)
	}
	require.NoError(t, w.Commit())
	return out
}

// Тесты маппинга principal-ctx → FGA-subject, который используют все List-handler'ы.
func TestSubjectFromCtx_UserPrincipal(t *testing.T) {
	ctx := operations.WithPrincipal(context.Background(), operations.Principal{
		Type:        "user",
		ID:          "usr_alice",
		DisplayName: "alice@example.com",
	})
	got := pbconv.SubjectFromContext(ctx)
	assert.Equal(t, "user:usr_alice", got)
}

func TestSubjectFromCtx_ServiceAccountPrincipal(t *testing.T) {
	ctx := operations.WithPrincipal(context.Background(), operations.Principal{
		Type: "service_account",
		ID:   "sva_bot",
	})
	got := pbconv.SubjectFromContext(ctx)
	assert.Equal(t, "service_account:sva_bot", got)
}

// Тип принципала пишет себе сам вызывающий, и `system` на платформе служит
// ЯРЛЫКОМ АНОНИМНОСТИ (`{system, anonymous}` край ставит запросу без
// удостоверения). Поэтому он обязан означать «никого не названо», а не
// «доверенный вызов»: иначе полное доверие получают и подделавший заголовок, и
// вообще неаутентифицированный вызывающий. Тот же предикат применяют compute и
// storage; vpc был единственным, кто читал его иначе.
func TestSubjectFromCtx_DeclaredSystemTypeNamesNobody(t *testing.T) {
	for name, p := range map[string]operations.Principal{
		"fallback без auth":  operations.SystemPrincipal(),
		"метка анонимности":  {Type: "system", ID: "anonymous"},
		"подделанный system": {Type: "system", ID: "sva_attacker"},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := operations.WithPrincipal(context.Background(), p)
			assert.Empty(t, pbconv.SubjectFromContext(ctx),
				"объявленный вызывающим тип не может быть основанием доверять ему всё")
		})
	}
}

// Сквозной замок на том же свойстве, но на НАБЛЮДАЕМОМ уровне use-case'а: субъект
// не подставляется тестом, а выводится из ctx, как в проде. Вызывающий, объявивший
// себя `system`, не получает ни строки — и модель о нём не спрашивают, потому что
// спрашивать не о ком.
func TestNetworkList_DeclaredSystemTypeSeesNothing(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNetworksLabeled(t, kr, "prj_1", "net_a", "net_b")

	filter := &fakeListFilter{allowAll: true}
	uc := NewListNetworksUseCase(kr, filter)

	ctx := operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "system", ID: "anonymous"})
	nets, _, err := uc.Execute(ctx, pbconv.SubjectFromContext(ctx),
		NetworkFilter{ProjectID: "prj_1"}, Pagination{})

	require.NoError(t, err)
	assert.Empty(t, nets, "объявленный тип принципала не открывает чужие сети")
	assert.Zero(t, filter.calls)
}

func TestSubjectFromCtx_NoPrincipalReturnsEmpty(t *testing.T) {
	got := pbconv.SubjectFromContext(context.Background())
	assert.Empty(t, got)
}

// Извлечение principal через grpcsrv-interceptor (production-flow) → subject проходит насквозь.
func TestSubjectFromCtx_ViaGrpcMetadata(t *testing.T) {
	md := metadata.New(map[string]string{
		grpcsrv.MDKeyPrincipalType:    "user",
		grpcsrv.MDKeyPrincipalID:      "usr_alice",
		grpcsrv.MDKeyPrincipalDisplay: "alice@example.com",
	})
	ctx := metadata.NewIncomingContext(context.Background(), md)
	p := operations.Principal{Type: "user", ID: "usr_alice", DisplayName: "alice@example.com"}
	ctx = operations.WithPrincipal(ctx, p)
	assert.Equal(t, "user:usr_alice", pbconv.SubjectFromContext(ctx))
}
