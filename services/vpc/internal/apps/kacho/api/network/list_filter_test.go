// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/pbconv"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
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
//
// Полярность сменилась: теперь это ОТКАЗ, а не пустая страница. «Пусто» неотличимо
// от «личность потеряна по дороге», и именно этим неразличением класс живёт годами.
func TestNetworkList_DeclaredSystemTypeIsRefused(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNetworksLabeled(t, kr, "prj_1", "net_a", "net_b")

	filter, peer := narrowtest.Recording()
	uc := NewListNetworksUseCase(kr, filter)

	ctx := operations.WithPrincipal(context.Background(),
		operations.Principal{Type: "system", ID: "anonymous"})
	nets, _, err := uc.Execute(ctx, NetworkFilter{ProjectID: "prj_1"}, Pagination{})

	require.Error(t, err, "объявленный тип принципала не открывает чужие сети")
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.Empty(t, nets)
	assert.Zero(t, peer.Calls, "спрашивать не о ком — модель не тревожат")
}

// Парный положительный: НАЗВАННЫЙ вызывающий доходит до модели и получает строки.
// Без него отказ выше зеленел бы на списке, отвергающем всех.
func TestNetworkList_NamedCallerReachesTheModel(t *testing.T) {
	kr := kachomock.NewRepository()
	seedNetworksLabeled(t, kr, "prj_1", "net_a", "net_b")

	filter, peer := narrowtest.Recording("net_a")
	uc := NewListNetworksUseCase(kr, filter)

	nets, _, err := uc.Execute(narrowtest.Caller(), NetworkFilter{ProjectID: "prj_1"}, Pagination{})

	require.NoError(t, err)
	require.Len(t, nets, 1)
	assert.Equal(t, "net_a", nets[0].ID)
	assert.Equal(t, 1, peer.Calls)
	assert.Equal(t, []string{"v_get"}, peer.Relations,
		"страница спрашивается тем же отношением, которым гейтится чтение")
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
