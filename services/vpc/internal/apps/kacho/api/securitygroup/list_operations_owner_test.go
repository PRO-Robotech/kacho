// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package securitygroup

// Список операций группы отдаёт операции САМОГО вызывающего.
//
// Утверждение — на наблюдаемом: строки, которые вернул use-case. Фейк
// repomock.OpsRepo реализует оба пути (несуженный List и суженный ListOwned),
// поэтому тест краснеет ровно тогда, когда вызывается несуженный.
//
// Группа заводится настоящим Create-путём: use-case списка сначала резолвит
// группу в своей БД и на отсутствующей отвечает NotFound, поэтому «просто взять
// произвольный id» здесь не даёт дойти до самой выдачи.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"
)

// seedSecurityGroup создаёт реальную группу и возвращает её id.
func seedSecurityGroup(t *testing.T, sgr *kachomock.Repository, or *repomock.OpsRepo) string {
	t.Helper()
	nr := repomock.NewNetworkRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	_, err := nr.Insert(context.Background(), &domain.Network{
		ID: netID, ProjectID: "f1", Name: domain.RcNameVPC("net")})
	require.NoError(t, err)

	create := NewCreateSecurityGroupUseCase(sgr, nr, &repomock.ProjectClient{OK: true}, or)
	op, err := create.Execute(context.Background(), domain.SecurityGroup{
		ProjectID: "f1", NetworkID: netID, Name: domain.RcNameVPC("sg-ops")})
	require.NoError(t, err)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.Nil(t, saved.Error, "фикстура обязана быть создана — иначе тест идёт по несозданной группе")
	require.NotNil(t, saved.Response, "успешная операция обязана нести созданный ресурс")
	var sg vpcv1.SecurityGroup
	require.NoError(t, saved.Response.UnmarshalTo(&sg))
	require.NotEmpty(t, sg.GetId())
	return sg.GetId()
}

func TestListOperations_ReturnsOnlyCallerOwnRows(t *testing.T) {
	sgr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	sgID := seedSecurityGroup(t, sgr, or)
	uc := NewListOperationsUseCase(sgr, or)

	me := operations.Principal{Type: "user", ID: "usr-me", DisplayName: "me@kacho.local"}
	other := operations.Principal{Type: "user", ID: "usr-other", DisplayName: "other@kacho.local"}
	bg := context.Background()
	require.NoError(t, or.CreateWithPrincipal(bg,
		operations.Operation{ID: "op-mine", ResourceID: sgID}, me))
	require.NoError(t, or.CreateWithPrincipal(bg,
		operations.Operation{ID: "op-foreign", ResourceID: sgID}, other))

	got, _, err := uc.Execute(operations.WithPrincipal(bg, me), sgID, Pagination{})
	require.NoError(t, err)

	seen := map[string]bool{}
	for _, op := range got {
		seen[op.ID] = true
	}
	require.True(t, seen["op-mine"], "своя операция обязана присутствовать")
	require.False(t, seen["op-foreign"],
		"чужая операция попала в список: её Response несёт ресурс целиком, а Principal — email инициатора")
}

// Без ключа владения выдача пуста: несуженный откат запрещён.
func TestListOperations_UnidentifiedCallerGetsNoRows(t *testing.T) {
	sgr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	sgID := seedSecurityGroup(t, sgr, or)
	uc := NewListOperationsUseCase(sgr, or)

	require.NoError(t, or.CreateWithPrincipal(context.Background(),
		operations.Operation{ID: "op-foreign", ResourceID: sgID},
		operations.Principal{Type: "user", ID: "usr-other", DisplayName: "other@kacho.local"}))

	got, _, err := uc.Execute(context.Background(), sgID, Pagination{})
	require.NoError(t, err)
	require.Empty(t, got, "без ключа владения выдача обязана быть пустой, а не полной")
}
