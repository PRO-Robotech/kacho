// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package securitygroup

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	operationpb "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/operation"
	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/applystate"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/pbconv"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/dto"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"

	// Blank-import регистрирует SecurityGroup/time DTO-трансферы через init().
	_ "github.com/PRO-Robotech/kacho/services/vpc/internal/dto/toproto"
)

// Handler — реализация vpcv1.SecurityGroupServiceServer на основе use-case'ов.
// Тонкий transport-слой: proto-request → domain → use-case → proto-response.
// Никакой бизнес-логики.
//
// SG-специфика: split-endpoint UpdateRules / UpdateRule — каждый идет в свой
// use-case (UpdateRulesUseCase / UpdateRuleUseCase), а не в обычный
// UpdateSecurityGroupUseCase. Обычный Update — name/description/labels, плюс
// full-replace всего набора правил через update_mask=rule_specs (альтернатива
// инкрементальным UpdateRules/UpdateRule).
type Handler struct {
	vpcv1.UnimplementedSecurityGroupServiceServer

	create         *CreateSecurityGroupUseCase
	update         *UpdateSecurityGroupUseCase
	updateRules    *UpdateRulesUseCase
	updateRule     *UpdateRuleUseCase
	delete         *DeleteSecurityGroupUseCase
	get            *GetSecurityGroupUseCase
	list           *ListSecurityGroupsUseCase
	listOperations *ListOperationsUseCase
	// applyState — заполнитель публичного поля состояния применения.
	// Провязывается композиционным корнем; нулевое значение означает
	// «утверждения нет» и к базе не ходит.
	applyState *applystate.Filler
}

// NewHandler собирает Handler из готовых use-case'ов. Конструктор намеренно
// принимает все use-case'ы — composition-root (cmd/vpc/main.go) собирает их
// с одинаковыми зависимостями (repo / networkReader / projectClient / opsRepo).
func NewHandler(
	create *CreateSecurityGroupUseCase,
	update *UpdateSecurityGroupUseCase,
	updateRules *UpdateRulesUseCase,
	updateRule *UpdateRuleUseCase,
	deleteUC *DeleteSecurityGroupUseCase,
	get *GetSecurityGroupUseCase,
	list *ListSecurityGroupsUseCase,
	listOps *ListOperationsUseCase,
) *Handler {
	return &Handler{
		create:         create,
		update:         update,
		updateRules:    updateRules,
		updateRule:     updateRule,
		delete:         deleteUC,
		get:            get,
		list:           list,
		listOperations: listOps,
	}
}

// WithApplyState провязывает заполнитель состояния применения.
//
// Отдельным методом, а не аргументом конструктора: у семи ресурсов
// конструкторы разной формы, и добавление восьмого позиционного аргумента
// сделало бы каждую их правку правкой всех вызывающих. Провязку боевой
// сборки держит гейт по дереву — он же ловит забытый вызов.
func (h *Handler) WithApplyState(f *applystate.Filler) *Handler {
	h.applyState = f
	return h
}

// Get — sync read. Per-object AuthZ (включая existence-hiding на deny) энфорсит
// per-RPC authz-interceptor прямым Check'ом — см. GetSecurityGroupUseCase.
func (h *Handler) Get(ctx context.Context, req *vpcv1.GetSecurityGroupRequest) (*vpcv1.SecurityGroup, error) {
	if req.SecurityGroupId == "" {
		return nil, status.Error(codes.InvalidArgument, "security_group_id required")
	}
	sg, err := h.get.Execute(ctx, req.SecurityGroupId)
	if err != nil {
		return nil, err
	}
	pb, err := securityGroupToPb(sg)
	if err != nil {
		return nil, err
	}
	// Состояние применения — ОТДЕЛЬНЫМ вопросом к проекции подтверждений, а не
	// полем строки ресурса: оно выводится сравнением ревизий и живёт в другой
	// таблице. Незаполненное поле означает «утверждения нет».
	if pb.ApplyState, err = h.applyState.One(ctx, pb.GetId()); err != nil {
		return nil, err
	}
	return pb, nil
}

// List — project_id required + FGA list-filter. Project-scope AuthZ (`viewer @
// project:<project_id>`) энфорсит per-RPC authz-interceptor.
func (h *Handler) List(ctx context.Context, req *vpcv1.ListSecurityGroupsRequest) (*vpcv1.ListSecurityGroupsResponse, error) {
	sgs, nextToken, err := h.list.Execute(ctx, SecurityGroupFilter{
		ProjectID: req.ProjectId,
		Filter:    req.Filter,
	}, Pagination{
		PageToken: req.PageToken,
		PageSize:  req.PageSize,
	})
	if err != nil {
		return nil, err
	}
	resp := &vpcv1.ListSecurityGroupsResponse{NextPageToken: nextToken}
	for _, sg := range sgs {
		pb, err := securityGroupToPb(sg)
		if err != nil {
			return nil, err
		}
		resp.SecurityGroups = append(resp.SecurityGroups, pb)
	}
	// Состояние применения СТРАНИЦЫ — одним обращением к проекции: стоимость
	// принадлежит запросу, а не популяции проекта. Спрашивается ПОСЛЕ того, как
	// страница отобрана и сужена правами, то есть по идентификаторам, которые
	// вызывающий и так увидит.
	if ferr := applystate.FillPage(ctx, h.applyState, resp.SecurityGroups,
		func(p *vpcv1.SecurityGroup) string { return p.GetId() },
		func(p *vpcv1.SecurityGroup, st *vpcv1.ApplyState) { p.ApplyState = st },
	); ferr != nil {
		return nil, ferr
	}
	return resp, nil
}

// Create — proto → domain → use-case. Project-scope AuthZ (`editor @
// project:<project_id>`) энфорсит per-RPC authz-interceptor.
func (h *Handler) Create(ctx context.Context, req *vpcv1.CreateSecurityGroupRequest) (*operationpb.Operation, error) {
	sg := domain.SecurityGroup{
		ProjectID:   req.ProjectId,
		NetworkID:   req.NetworkId,
		Name:        domain.RcNameVPC(req.Name),
		Description: domain.RcDescription(req.Description),
		Labels:      domain.LabelsFromMap(req.Labels),
	}
	rules, err := ruleSpecsFromProto("rule_specs", req.RuleSpecs)
	if err != nil {
		return nil, err
	}
	sg.Rules = rules
	op, err := h.create.Execute(ctx, sg)
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// Update — sync repo.Get (existence → NotFound) + use-case; per-object AuthZ
// энфорсит per-RPC authz-interceptor. Весь набор правил можно заменить целиком
// (full-replace) через update_mask=rule_specs; инкрементальная правка — через
// split-endpoint UpdateRules.
func (h *Handler) Update(ctx context.Context, req *vpcv1.UpdateSecurityGroupRequest) (*operationpb.Operation, error) {
	if req.SecurityGroupId == "" {
		return nil, status.Error(codes.InvalidArgument, "security_group_id required")
	}
	if _, err := h.get.Execute(ctx, req.SecurityGroupId); err != nil {
		return nil, err
	}
	var mask []string
	if req.UpdateMask != nil {
		mask = req.UpdateMask.Paths
	}
	dsg := domain.SecurityGroup{
		Name:        domain.RcNameVPC(req.Name),
		Description: domain.RcDescription(req.Description),
		Labels:      domain.LabelsFromMap(req.Labels),
	}
	rules, err := ruleSpecsFromProto("rule_specs", req.RuleSpecs)
	if err != nil {
		return nil, err
	}
	dsg.Rules = rules
	op, err := h.update.Execute(ctx, UpdateInput{
		SecurityGroupID: req.SecurityGroupId,
		SecurityGroup:   dsg,
		UpdateMask:      mask,
	})
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// UpdateRules — split-endpoint: атомарно удалить deletion_rule_ids + добавить
// addition_rule_specs. Response — parent SG.
func (h *Handler) UpdateRules(ctx context.Context, req *vpcv1.UpdateSecurityGroupRulesRequest) (*operationpb.Operation, error) {
	if req.SecurityGroupId == "" {
		return nil, status.Error(codes.InvalidArgument, "security_group_id required")
	}
	if _, err := h.get.Execute(ctx, req.SecurityGroupId); err != nil {
		return nil, err
	}
	in := UpdateRulesInput{
		SecurityGroupID: req.SecurityGroupId,
		DeletionRuleIDs: req.DeletionRuleIds,
	}
	additions, err := ruleSpecsFromProto("addition_rule_specs", req.AdditionRuleSpecs)
	if err != nil {
		return nil, err
	}
	in.AdditionRuleSpecs = additions
	op, err := h.updateRules.Execute(ctx, in)
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// UpdateRule — изменение одного правила (description / labels). Response —
// parent SG.
func (h *Handler) UpdateRule(ctx context.Context, req *vpcv1.UpdateSecurityGroupRuleRequest) (*operationpb.Operation, error) {
	if req.SecurityGroupId == "" {
		return nil, status.Error(codes.InvalidArgument, "security_group_id required")
	}
	if _, err := h.get.Execute(ctx, req.SecurityGroupId); err != nil {
		return nil, err
	}
	var mask []string
	if req.UpdateMask != nil {
		mask = req.UpdateMask.Paths
	}
	op, err := h.updateRule.Execute(ctx, UpdateRuleInput{
		SecurityGroupID: req.SecurityGroupId,
		RuleID:          req.RuleId,
		Description:     req.Description,
		Labels:          req.Labels,
		UpdateMask:      mask,
	})
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// Delete — sync repo.Get (existence → NotFound) + default-SG-protected, затем
// use-case. Per-object AuthZ энфорсит per-RPC authz-interceptor.
func (h *Handler) Delete(ctx context.Context, req *vpcv1.DeleteSecurityGroupRequest) (*operationpb.Operation, error) {
	if req.SecurityGroupId == "" {
		return nil, status.Error(codes.InvalidArgument, "security_group_id required")
	}
	if _, err := h.get.Execute(ctx, req.SecurityGroupId); err != nil {
		return nil, err
	}
	op, err := h.delete.Execute(ctx, req.SecurityGroupId)
	if err != nil {
		return nil, err
	}
	return pbconv.OperationToProto(op), nil
}

// ListOperations — SG обязан существовать (Get → NotFound) → list operations.
// Per-object AuthZ энфорсит per-RPC authz-interceptor.
func (h *Handler) ListOperations(ctx context.Context, req *vpcv1.ListSecurityGroupOperationsRequest) (*vpcv1.ListSecurityGroupOperationsResponse, error) {
	if req.SecurityGroupId == "" {
		return nil, status.Error(codes.InvalidArgument, "security_group_id required")
	}
	if _, err := h.get.Execute(ctx, req.SecurityGroupId); err != nil {
		return nil, err
	}
	ops, nextToken, err := h.listOperations.Execute(ctx, req.SecurityGroupId, Pagination{
		PageToken: req.PageToken,
		PageSize:  req.PageSize,
	})
	if err != nil {
		return nil, err
	}
	resp := &vpcv1.ListSecurityGroupOperationsResponse{NextPageToken: nextToken}
	for i := range ops {
		resp.Operations = append(resp.Operations, pbconv.OperationToProto(&ops[i]))
	}
	return resp, nil
}

// securityGroupToPb — repo-entity SecurityGroup → proto SecurityGroup через
// DTO-реестр.
func securityGroupToPb(rec *kacho.SecurityGroupRecord) (*vpcv1.SecurityGroup, error) {
	var dst *vpcv1.SecurityGroup
	if err := dto.Transfer(dto.FromTo(*rec, &dst)); err != nil {
		return nil, status.Error(codes.Internal, "dto.Transfer SecurityGroup failed")
	}
	return dst, nil
}
