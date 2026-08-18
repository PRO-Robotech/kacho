// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package securitygroup

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/kachomock"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/repomock"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowtest"
)

// Тесты SecurityGroup use-case'ов и handler'а (против `*securitygroup.Handler`).
//
// Mock-port'ы — переиспользуем `internal/repo/repomock` (уже реализует
// `internal/repo.SecurityGroupRepoIface`, который ⊇ нашему локальному
// SecurityGroupRepo).

// ---- builders ----

// newSGReaderMock — порт чтения групп для тестов. Здесь НЕ своя реализация: это
// ровно тот порт, который собирает конструктор use-case'а (`sgTargetReader`
// поверх `Repo`). Собственная копия была бы дублёром, способным оказаться
// снисходительнее продукта именно там, где расхождение не видно, — а прод-порт
// в тестовом окружении выразим как есть, потому что `kachomock.Repository`
// реализует тот же `Repo`.
func newSGReaderMock(r *kachomock.Repository) SecurityGroupReader { return sgTargetReader(nil, r) }

func makeHandler(
	t *testing.T,
	sgr *kachomock.Repository,
	nr *repomock.NetworkRepo,
	or *repomock.OpsRepo,
	fc *repomock.ProjectClient,
) *Handler {
	t.Helper()
	sgReader := newSGReaderMock(sgr)
	create := NewCreateSecurityGroupUseCase(sgr, nr, fc, or).WithSGReader(sgReader)
	update := NewUpdateSecurityGroupUseCase(sgr, or)
	updateRules := NewUpdateRulesUseCase(sgr, or, sgReader)
	updateRule := NewUpdateRuleUseCase(sgr, or, sgReader)
	deleteUC := NewDeleteSecurityGroupUseCase(sgr, or)
	get := NewGetSecurityGroupUseCase(sgr)
	list := NewListSecurityGroupsUseCase(sgr, narrowtest.AllowingAll())
	listOps := NewListOperationsUseCase(sgr, or)
	return NewHandler(create, update, updateRules, updateRule, deleteUC, get, list, listOps)
}

// minimalHandler — wiring с дефолтными mock'ами; project=true.
func minimalHandler(t *testing.T) (*Handler, *repomock.OpsRepo, *kachomock.Repository) {
	t.Helper()
	sgr := kachomock.NewRepository()
	nr := repomock.NewNetworkRepo()
	or := repomock.NewOpsRepo()
	fc := &repomock.ProjectClient{OK: true}
	return makeHandler(t, sgr, nr, or, fc), or, sgr
}

// ---- Handler — sync paths ----

func TestHandler_Get_InvalidArg(t *testing.T) {
	h, _, _ := minimalHandler(t)
	_, err := h.Get(context.Background(), &vpcv1.GetSecurityGroupRequest{SecurityGroupId: ""})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestHandler_Get_NotFound(t *testing.T) {
	h, _, _ := minimalHandler(t)
	_, err := h.Get(context.Background(), &vpcv1.GetSecurityGroupRequest{SecurityGroupId: ids.NewID(ids.PrefixSecurityGroup)})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestHandler_List_Empty(t *testing.T) {
	h, _, _ := minimalHandler(t)
	resp, err := h.List(narrowtest.Caller(), &vpcv1.ListSecurityGroupsRequest{ProjectId: "f1"})
	require.NoError(t, err)
	assert.Empty(t, resp.SecurityGroups)
}

func TestHandler_Create_Validates(t *testing.T) {
	// project_id отсутствует → InvalidArgument.
	h, _, _ := minimalHandler(t)
	_, err := h.Create(context.Background(), &vpcv1.CreateSecurityGroupRequest{Name: "sg"})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestHandler_Update_InvalidArg(t *testing.T) {
	h, _, _ := minimalHandler(t)
	_, err := h.Update(context.Background(), &vpcv1.UpdateSecurityGroupRequest{SecurityGroupId: ""})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestHandler_UpdateRules_InvalidArg(t *testing.T) {
	h, _, _ := minimalHandler(t)
	_, err := h.UpdateRules(context.Background(), &vpcv1.UpdateSecurityGroupRulesRequest{SecurityGroupId: ""})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestHandler_UpdateRule_InvalidArg(t *testing.T) {
	h, _, _ := minimalHandler(t)
	_, err := h.UpdateRule(context.Background(), &vpcv1.UpdateSecurityGroupRuleRequest{SecurityGroupId: ""})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestHandler_Delete_InvalidArg(t *testing.T) {
	h, _, _ := minimalHandler(t)
	_, err := h.Delete(context.Background(), &vpcv1.DeleteSecurityGroupRequest{SecurityGroupId: ""})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestHandler_ListOperations_RequiresID(t *testing.T) {
	h, _, _ := minimalHandler(t)
	_, err := h.ListOperations(context.Background(), &vpcv1.ListSecurityGroupOperationsRequest{SecurityGroupId: ""})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// ---- use-case-level (без handler'а) ----

func TestCreateUseCase_ValidationError(t *testing.T) {
	sgr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	nr := repomock.NewNetworkRepo()
	uc := NewCreateSecurityGroupUseCase(sgr, nr, &repomock.ProjectClient{OK: true}, or)

	// project_id обязателен.
	_, err := uc.Execute(context.Background(), domain.SecurityGroup{Name: "test"})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())

	// network_id обязателен: пустой network_id → sync InvalidArgument.
	_, err = uc.Execute(context.Background(), domain.SecurityGroup{ProjectID: "f1", Name: "test"})
	require.Error(t, err)
	st, _ = status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, "network_id required", st.Message())

	// Негодное имя. Здесь стояло "1bad" — ведущая цифра; единственная форма дерева
	// (RFC 1123 DNS label, #715) её ДОПУСКАЕТ, поэтому вход стал законным и проба
	// перестала бы проверять хоть что-нибудь. Взято то, что форма отвергает.
	// Сидим валидную Network, чтобы пройти gate обязательности/существования network_id.
	netID := ids.NewID(ids.PrefixNetwork)
	_, _ = nr.Insert(context.Background(), &domain.Network{ID: netID, ProjectID: "f1", Name: domain.RcNameVPC("net")})
	_, err = uc.Execute(context.Background(), domain.SecurityGroup{
		ProjectID: "f1",
		NetworkID: netID,
		Name:      domain.RcNameVPC("Bad_Name"),
	})
	require.Error(t, err)
	st, _ = status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// TestCreateUseCase_ProjectNotFound — sync-precheck project.Exists убран как
// race-prone; NotFound теперь приходит через `operation.error` из async
// `doCreate`, а не sync-статусом. Поэтому: Execute → без ошибки; AwaitOpDone →
// Operation.Done=true с Error.Code == NotFound.
func TestCreateUseCase_ProjectNotFound(t *testing.T) {
	sgr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	nr := repomock.NewNetworkRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	_, _ = nr.Insert(context.Background(), &domain.Network{ID: netID, ProjectID: "f1", Name: domain.RcNameVPC("net")})
	uc := NewCreateSecurityGroupUseCase(sgr, nr, &repomock.ProjectClient{OK: false}, or)

	op, err := uc.Execute(context.Background(), domain.SecurityGroup{
		ProjectID: "f1",
		NetworkID: netID,
		Name:      domain.RcNameVPC("sg1"),
	})
	require.NoError(t, err)
	require.NotEmpty(t, op.ID)

	saved := repomock.AwaitOpDone(t, or, op.ID)
	require.True(t, saved.Done)
	require.NotNil(t, saved.Error, "operation should fail in worker — project missing")
	assert.Equal(t, int32(codes.NotFound), saved.Error.Code)
	// Канонический контракт сообщения: "<Resource> %s not found".
	assert.Equal(t, "Project f1 not found", saved.Error.Message)
}

// TestCreateUseCase_CrossProjectNetwork_Denied — BOLA-guard: SG в проекте "f1"
// НЕ может ссылаться на Network чужого проекта ("other"). Cross-project → NotFound
// (тот же ответ, что для несуществующей сети — без existence-oracle). RED до фикса:
// reference на чужую сеть проходил (networkReader.Get не сверял project).
func TestCreateUseCase_CrossProjectNetwork_Denied(t *testing.T) {
	sgr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	nr := repomock.NewNetworkRepo()
	foreignNet := ids.NewID(ids.PrefixNetwork)
	_, _ = nr.Insert(context.Background(), &domain.Network{ID: foreignNet, ProjectID: "other", Name: domain.RcNameVPC("net")})
	uc := NewCreateSecurityGroupUseCase(sgr, nr, &repomock.ProjectClient{OK: true}, or)

	_, err := uc.Execute(context.Background(), domain.SecurityGroup{
		ProjectID: "f1", NetworkID: foreignNet, Name: domain.RcNameVPC("sg-bola"),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
	assert.Equal(t, "Network "+foreignNet+" not found", st.Message())
}

// TestCreateUseCase_OK — happy-path Create с валидной существующей Network
// (network_id обязателен).
func TestCreateUseCase_OK(t *testing.T) {
	sgr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	nr := repomock.NewNetworkRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	_, _ = nr.Insert(context.Background(), &domain.Network{ID: netID, ProjectID: "f1", Name: domain.RcNameVPC("net")})
	uc := NewCreateSecurityGroupUseCase(sgr, nr, &repomock.ProjectClient{OK: true}, or)

	op, err := uc.Execute(context.Background(), domain.SecurityGroup{
		ProjectID:   "f1",
		NetworkID:   netID,
		Name:        domain.RcNameVPC("sg1"),
		Description: domain.RcDescription("desc"),
	})
	require.NoError(t, err)
	require.NotEmpty(t, op.ID)

	saved := repomock.AwaitOpDone(t, or, op.ID)
	assert.True(t, saved.Done)
	assert.Nil(t, saved.Error)
}

func TestDeleteUseCase_InvalidArg(t *testing.T) {
	uc := NewDeleteSecurityGroupUseCase(kachomock.NewRepository(), repomock.NewOpsRepo())
	_, err := uc.Execute(context.Background(), "")
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestListUseCase_RequiresProject(t *testing.T) {
	uc := NewListSecurityGroupsUseCase(kachomock.NewRepository(), nil)
	_, _, err := uc.Execute(narrowtest.Caller(), SecurityGroupFilter{}, Pagination{})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUpdateRulesUseCase_InvalidArg(t *testing.T) {
	uc := NewUpdateRulesUseCase(kachomock.NewRepository(), repomock.NewOpsRepo(), nil)
	// security_group_id обязателен (валидация resource-id).
	_, err := uc.Execute(context.Background(), UpdateRulesInput{SecurityGroupID: "bad"})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// ---- валидация same-network SG-target правил (быстрые, на моках) ----

// seedMockSG вставляет SecurityGroup в kachomock через committed writer-TX.
func seedMockSG(t *testing.T, sgr *kachomock.Repository, projectID, networkID, name string) string {
	t.Helper()
	id := ids.NewID(ids.PrefixSecurityGroup)
	w, err := sgr.Writer(context.Background())
	require.NoError(t, err)
	_, err = w.SecurityGroups().Insert(context.Background(), &domain.SecurityGroup{
		ID: id, ProjectID: projectID, NetworkID: networkID,
		Name: domain.RcNameVPC(name),
	})
	require.NoError(t, err)
	require.NoError(t, w.Commit())
	return id
}

func sgTargetRule(targetSGID string) domain.SecurityGroupRule {
	return domain.SecurityGroupRule{
		Direction: domain.SecurityGroupRuleDirectionIngress, FromPort: -1, ToPort: -1,
		SecurityGroupID: targetSGID,
	}
}

// Create с правилом, ссылающимся на SG из другой сети → sync InvalidArgument.
//
// Текст отказа — контракт-тон промаха SecurityGroup, ОДИН и тот же, что у
// «цели нет вовсе»: различимые тексты сообщали бы о существовании чужой группы.
// Побайтовое равенство обоих исходов держит
// `TestSGRuleTarget_MissingAndCrossNetworkAreIndistinguishable`.
func TestCreateUseCase_CrossNetworkRule_InvalidArgument(t *testing.T) {
	sgr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	nr := repomock.NewNetworkRepo()
	netA := ids.NewID(ids.PrefixNetwork)
	netB := ids.NewID(ids.PrefixNetwork)
	_, _ = nr.Insert(context.Background(), &domain.Network{ID: netA, ProjectID: "P", Name: "net-a"})
	_, _ = nr.Insert(context.Background(), &domain.Network{ID: netB, ProjectID: "P", Name: "net-b"})
	sgB := seedMockSG(t, sgr, "P", netB, "sg-target-B")

	uc := NewCreateSecurityGroupUseCase(sgr, nr, &repomock.ProjectClient{OK: true}, or).WithSGReader(newSGReaderMock(sgr))
	_, err := uc.Execute(context.Background(), domain.SecurityGroup{
		ProjectID: "P", NetworkID: netA, Name: domain.RcNameVPC("sg-7"),
		Rules: []domain.SecurityGroupRule{sgTargetRule(sgB)},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, fmt.Sprintf(sgTargetNotFoundForm(t), sgB), st.Message())
}

// Create с правилом, ссылающимся на SG из той же сети → OK (без sync-ошибки).
func TestCreateUseCase_SameNetworkRule_OK(t *testing.T) {
	sgr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	nr := repomock.NewNetworkRepo()
	netA := ids.NewID(ids.PrefixNetwork)
	_, _ = nr.Insert(context.Background(), &domain.Network{ID: netA, ProjectID: "P", Name: "net-a"})
	sgA := seedMockSG(t, sgr, "P", netA, "sg-target-A")

	uc := NewCreateSecurityGroupUseCase(sgr, nr, &repomock.ProjectClient{OK: true}, or).WithSGReader(newSGReaderMock(sgr))
	op, err := uc.Execute(context.Background(), domain.SecurityGroup{
		ProjectID: "P", NetworkID: netA, Name: domain.RcNameVPC("sg-8"),
		Rules: []domain.SecurityGroupRule{sgTargetRule(sgA)},
	})
	require.NoError(t, err)
	saved := repomock.AwaitOpDone(t, or, op.ID)
	assert.True(t, saved.Done)
	assert.Nil(t, saved.Error)
}

// Create с правилом на несуществующий SG-target → sync InvalidArgument, тем же
// текстом, что и цель из чужой сети (см. пробу неотличимости).
func TestCreateUseCase_TargetNotFound_InvalidArgument(t *testing.T) {
	sgr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	nr := repomock.NewNetworkRepo()
	netA := ids.NewID(ids.PrefixNetwork)
	_, _ = nr.Insert(context.Background(), &domain.Network{ID: netA, ProjectID: "P", Name: "net-a"})

	const absentTarget = "enp11111111111111111"
	uc := NewCreateSecurityGroupUseCase(sgr, nr, &repomock.ProjectClient{OK: true}, or).WithSGReader(newSGReaderMock(sgr))
	_, err := uc.Execute(context.Background(), domain.SecurityGroup{
		ProjectID: "P", NetworkID: netA, Name: domain.RcNameVPC("sg-x"),
		Rules: []domain.SecurityGroupRule{sgTargetRule(absentTarget)},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, fmt.Sprintf(sgTargetNotFoundForm(t), absentTarget), st.Message())
}

// UpdateRules с правилом на SG из другой сети → sync InvalidArgument.
func TestUpdateRulesUseCase_CrossNetworkRule_InvalidArgument(t *testing.T) {
	sgr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netA := ids.NewID(ids.PrefixNetwork)
	netB := ids.NewID(ids.PrefixNetwork)
	sg8 := seedMockSG(t, sgr, "P", netA, "sg-8")
	sgB := seedMockSG(t, sgr, "P", netB, "sg-target-B")

	uc := NewUpdateRulesUseCase(sgr, or, newSGReaderMock(sgr))
	_, err := uc.Execute(context.Background(), UpdateRulesInput{
		SecurityGroupID:   sg8,
		AdditionRuleSpecs: []domain.SecurityGroupRule{sgTargetRule(sgB)},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Equal(t, fmt.Sprintf(sgTargetNotFoundForm(t), sgB), st.Message())
}

func TestUpdateRuleUseCase_InvalidArg(t *testing.T) {
	uc := NewUpdateRuleUseCase(kachomock.NewRepository(), repomock.NewOpsRepo(), nil)
	_, err := uc.Execute(context.Background(), UpdateRuleInput{SecurityGroupID: "bad"})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())

	// SG-id ok, rule-id пустой → InvalidArgument.
	_, err = uc.Execute(context.Background(), UpdateRuleInput{SecurityGroupID: ids.NewID(ids.PrefixSecurityGroup), RuleID: ""})
	require.Error(t, err)
	st, _ = status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGetUseCase_InvalidArg(t *testing.T) {
	uc := NewGetSecurityGroupUseCase(kachomock.NewRepository())
	_, err := uc.Execute(context.Background(), "bad-id")
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// ---- helpers — pure converter functions ----

func TestSGToProto_Fields(t *testing.T) {
	rec := &kacho.SecurityGroupRecord{
		SecurityGroup: domain.SecurityGroup{
			ID:                "sg-1",
			ProjectID:         "f1",
			NetworkID:         "net-1",
			Name:              domain.RcNameVPC("sg"),
			Description:       domain.RcDescription("desc"),
			Labels:            domain.LabelsFromMap(map[string]string{"k": "v"}),
			DefaultForNetwork: false,
			Rules: []domain.SecurityGroupRule{
				{
					ID:           "r1",
					Direction:    domain.SecurityGroupRuleDirectionIngress,
					Description:  domain.RcDescription("in"),
					ProtocolName: "tcp",
					FromPort:     22, ToPort: 22,
					V4CidrBlocks: []string{"10.0.0.0/24"},
				},
				{
					ID:             "r2",
					Direction:      domain.SecurityGroupRuleDirectionEgress,
					ProtocolNumber: 17,
				},
			},
		},
	}
	p, err := securityGroupToPb(rec)
	require.NoError(t, err)
	assert.Equal(t, "sg-1", p.Id)
	assert.Len(t, p.Rules, 2)
	assert.Equal(t, vpcv1.SecurityGroupRule_INGRESS, p.Rules[0].Direction)
	require.NotNil(t, p.Rules[0].Ports)
	assert.Equal(t, int64(22), p.Rules[0].Ports.FromPort)
	assert.Nil(t, p.Rules[1].Ports, "ports nil for any")
}

func TestRuleSpecFromProto_Fields(t *testing.T) {
	rs := &vpcv1.SecurityGroupRuleSpec{
		Description: "desc",
		Labels:      map[string]string{"k": "v"},
		Direction:   vpcv1.SecurityGroupRule_INGRESS,
		Ports:       &vpcv1.PortRange{FromPort: 80, ToPort: 80},
		Protocol:    &vpcv1.SecurityGroupRuleSpec_ProtocolName{ProtocolName: "tcp"},
		Target: &vpcv1.SecurityGroupRuleSpec_CidrBlocks{
			CidrBlocks: &vpcv1.CidrBlocks{V4CidrBlocks: []string{"0.0.0.0/0"}},
		},
	}
	r, err := ruleSpecFromProto("rule_specs[0]", rs)
	require.NoError(t, err)
	assert.Equal(t, domain.SecurityGroupRuleDirectionIngress, r.Direction)
	assert.Equal(t, int64(80), r.FromPort)
	assert.Equal(t, "tcp", r.ProtocolName)
	assert.Equal(t, []string{"0.0.0.0/0"}, r.V4CidrBlocks)
}

func TestRuleSpecFromProto_ProtocolNumber(t *testing.T) {
	rs := &vpcv1.SecurityGroupRuleSpec{
		Direction: vpcv1.SecurityGroupRule_EGRESS,
		Protocol:  &vpcv1.SecurityGroupRuleSpec_ProtocolNumber{ProtocolNumber: 17},
		Target:    &vpcv1.SecurityGroupRuleSpec_SecurityGroupId{SecurityGroupId: "sg-2"},
	}
	r, err := ruleSpecFromProto("rule_specs[0]", rs)
	require.NoError(t, err)
	assert.Equal(t, domain.SecurityGroupRuleDirectionEgress, r.Direction)
	assert.Equal(t, int64(17), r.ProtocolNumber)
	assert.Equal(t, "sg-2", r.SecurityGroupID)
}

// Прежде здесь стояла TestRuleSpecFromProto_Predefined — разбор ветви
// предопределённой цели. Ветвь СНЯТА с контракта: свободная строка без словаря, на
// которой сервис не ветвился, а вызывающий не мог узнать, что в неё написать.
// Проба удалена, а не переписана: разбирать нечего.

// Набор правил аддитивен: серия формально законных добавлений не имеет права
// растить его без предела. Потолок на НАКОПЛЕННОМ наборе проверяет тот, кто
// пишет итог, — иначе каждый отдельный запрос проходит, а состояние выходит за
// объявленный предел.
func TestSecurityGroup_UpdateRules_AccumulatedCapEnforced(t *testing.T) {
	kr := kachomock.NewRepository()
	or := repomock.NewOpsRepo()
	netID := ids.NewID(ids.PrefixNetwork)
	sgID := seedMockSG(t, kr, "prj-cap", netID, "cap-sg")

	uc := NewUpdateRulesUseCase(kr, or, newSGReaderMock(kr))
	// Каждый запрос — в пределах потолка одного запроса, но их сумма его
	// превышает.
	batch := func(n int) []domain.SecurityGroupRule {
		out := make([]domain.SecurityGroupRule, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, domain.SecurityGroupRule{
				Direction: domain.SecurityGroupRuleDirectionIngress,
				FromPort:  -1, ToPort: -1,
				V4CidrBlocks: []string{"10.0.0.0/8"},
			})
		}
		return out
	}
	half := domain.MaxSecurityGroupRules/2 + 1
	op1, err := uc.Execute(context.Background(), UpdateRulesInput{
		SecurityGroupID: sgID, AdditionRuleSpecs: batch(half),
	})
	require.NoError(t, err)
	done1 := repomock.AwaitOpDone(t, or, op1.ID)
	require.Nil(t, done1.Error, "первый запрос в пределах потолка обязан пройти")

	op2, err := uc.Execute(context.Background(), UpdateRulesInput{
		SecurityGroupID: sgID, AdditionRuleSpecs: batch(half),
	})
	require.NoError(t, err)
	done2 := repomock.AwaitOpDone(t, or, op2.ID)
	require.NotNil(t, done2.Error, "накопленный набор сверх потолка обязан быть отвергнут")
	assert.Equal(t, codes.FailedPrecondition, status.FromProto(done2.Error).Code())
}
