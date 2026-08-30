// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package cidrgroup

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/fgaregister"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// DeleteCidrGroupUseCase — удаление набора.
//
// Набор, на который ссылается живое правило, не удаляется. Инвариант держит
// внешний ключ RESTRICT с проекции ссылок (миграция 0035) — он отвечает В МОМЕНТ
// удаления, поэтому правило, созданное между вопросом и удалением, не остаётся с
// висячей ссылкой. Синхронная проверка ниже существует не вместо ключа, а ради
// ТЕКСТА: ключ говорит «нарушено ограничение», а вызывающему нужен радиус —
// сколько групп правил и сколько правил держат набор.
type DeleteCidrGroupUseCase struct {
	repo    Repo
	opsRepo operations.Repo
}

// NewDeleteCidrGroupUseCase создаёт DeleteCidrGroupUseCase.
func NewDeleteCidrGroupUseCase(r Repo, opsRepo operations.Repo) *DeleteCidrGroupUseCase {
	return &DeleteCidrGroupUseCase{repo: r, opsRepo: opsRepo}
}

// Execute — формат id, синхронный отказ по живым ссылкам, затем Operation.
func (u *DeleteCidrGroupUseCase) Execute(ctx context.Context, id string) (*operations.Operation, error) {
	if err := corevalidate.ResourceID("cidr_group", ids.PrefixCidrGroupHyphen, id); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "cidr_group_id required")
	}
	if err := u.checkUnreferenced(ctx, id); err != nil {
		return nil, err
	}

	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Delete cidr group %s", id),
		&vpcv1.DeleteCidrGroupMetadata{CidrGroupId: id},
	)
	if err != nil {
		return nil, err
	}
	if err := u.opsRepo.Create(ctx, op); err != nil {
		return nil, err
	}
	if err := operations.RunSync(ctx, u.opsRepo, &op, func(ctx context.Context) (*anypb.Any, error) {
		return u.doDelete(ctx, id)
	}); err != nil {
		return nil, err
	}
	return &op, nil
}

func (u *DeleteCidrGroupUseCase) doDelete(ctx context.Context, id string) (*anypb.Any, error) {
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer w.Abort()

	// projectID читается ДО удаления: он нужен как субъект снимаемого кортежа
	// иерархии. Ресурс исчезает — его место в иерархии прав тоже.
	var unregTuples []fgaregister.Tuple
	var projectID string
	if rec, gerr := w.CidrGroups().Get(ctx, id); gerr == nil {
		projectID = rec.ProjectID
		unregTuples = append(unregTuples,
			fgaregister.ProjectHierarchy(rec.ProjectID, "vpc_cidr_group", id))
	}

	if err := w.CidrGroups().Delete(ctx, id); err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	if err := w.Outbox().Emit(ctx, "CidrGroup", id, projectID, "DELETED", map[string]any{"id": id}); err != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, err))
	}
	if len(unregTuples) > 0 {
		if err := w.FGARegister().EmitUnregister(ctx, fgaregister.RegisterIntent(unregTuples...)); err != nil {
			return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: fga unregister intent: %v", repo.ErrInternal, err))
		}
	}
	if err := w.Commit(); err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	return anypb.New(&emptypb.Empty{})
}

// checkUnreferenced — синхронный отказ, ПЕРЕЧИСЛЯЮЩИЙ мешающее по видам и
// числам.
//
// Числа, а не идентификаторы: число координатой не является, перечень чужих
// идентификаторов ею становится. Форма и причина — те же, что у отказа на
// непустой сети: без перечня арендатор выясняет радиус перебором.
func (u *DeleteCidrGroupUseCase) checkUnreferenced(ctx context.Context, id string) error {
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return serviceerr.MapRepoErr(err)
	}
	defer func() { _ = rd.Close() }()

	rec, err := rd.CidrGroups().Get(ctx, id)
	if err != nil {
		return serviceerr.MapRepoErr(err)
	}
	if len(rec.UsedBy) == 0 {
		return nil
	}
	return status.Errorf(codes.FailedPrecondition,
		"CidrGroup %s is in use (%s)", id, blockersText(rec.UsedBy))
}

// blockersText — «security groups: 2, rules: 3». Класс с нулём в перечень не
// попадает: «rules: 0» не сообщает ничего, а перечень с нулями нельзя прочитать
// как радиус.
func blockersText(refs []kachorepo.CidrGroupReferrer) string {
	rules := 0
	for _, ref := range refs {
		rules += ref.Rules
	}
	var parts []string
	if len(refs) > 0 {
		parts = append(parts, fmt.Sprintf("security groups: %d", len(refs)))
	}
	if rules > 0 {
		parts = append(parts, fmt.Sprintf("rules: %d", rules))
	}
	return strings.Join(parts, ", ")
}
