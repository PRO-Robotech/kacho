// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package cidrgroup

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/fgaregister"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
)

// UpdateInput — параметры UpdateCidrGroupUseCase.Execute: доменная сущность плюс
// ортогональная ей маска.
type UpdateInput struct {
	CidrGroupID string
	CidrGroup   domain.CidrGroup // несёт Name/Description/Labels
	UpdateMask  []string
}

// UpdateCidrGroupUseCase — правка КОСМЕТИЧЕСКИХ полей набора.
//
// Состав через Update не меняется, и это решение, а не пропуск: полная замена
// набора дала бы «победил последний» двум редакторам, каждый из которых прислал
// свой полный список, а потолок пришлось бы судить по разнице двух множеств
// вместо дельты. Состав правят глаголы.
type UpdateCidrGroupUseCase struct {
	repo      Repo
	opsRepo   operations.Repo
	registrar fgaregister.Registrar
}

// NewUpdateCidrGroupUseCase создаёт UpdateCidrGroupUseCase.
func NewUpdateCidrGroupUseCase(r Repo, opsRepo operations.Repo) *UpdateCidrGroupUseCase {
	return &UpdateCidrGroupUseCase{repo: r, opsRepo: opsRepo}
}

// WithRegistrar подключает синхронный registrar — тот же, что у пути создания.
// Смена меток меняет проекцию, которую читает селектор владельца прав, поэтому
// она обязана доезжать на пути запроса; durable-intent остаётся backstop'ом.
func (u *UpdateCidrGroupUseCase) WithRegistrar(r fgaregister.Registrar) *UpdateCidrGroupUseCase {
	u.registrar = r
	return u
}

// Execute — sync-проверки и запуск Update в worker'е.
func (u *UpdateCidrGroupUseCase) Execute(ctx context.Context, in UpdateInput) (*operations.Operation, error) {
	if err := corevalidate.ResourceID("cidr_group", ids.PrefixCidrGroupHyphen, in.CidrGroupID); err != nil {
		return nil, err
	}
	if in.CidrGroupID == "" {
		return nil, status.Error(codes.InvalidArgument, "cidr_group_id required")
	}
	// Immutable-switch ДО проверки маски по известному набору.
	//
	// Порядок несущий: известный набор маски НЕ содержит неизменяемых полей,
	// поэтому без этого switch они отверглись бы родовым «unknown field» вместо
	// конвенционного текста, который называет предмет.
	//
	//   - id / project_id / created_at — неизменяемые: id адресует ресурс всю его
	//     жизнь, проект — его владение;
	//   - v4/v6_cidr_blocks — состав, у него свои глаголы (см. отдельный текст:
	//     «непригодно для Update» — не то же самое, что «неизменяемо»);
	//   - cidr_block_count / used_by — производные на чтении, на вход не
	//     принимаются вовсе.
	for _, field := range in.UpdateMask {
		switch field {
		case "v4_cidr_blocks", "v6_cidr_blocks":
			return nil, serviceerr.InvalidArg(field,
				field+" is not updatable via Update; use AddCidrBlocks/RemoveCidrBlocks")
		case "id", "project_id", "created_at", "cidr_block_count", "used_by":
			return nil, serviceerr.InvalidArg(field, field+" is immutable after CidrGroup.Create")
		}
	}
	if err := serviceerr.FromValidation(validateCidrGroupUpdate(in)); err != nil {
		return nil, err
	}

	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Update cidr group %s", in.CidrGroupID),
		&vpcv1.UpdateCidrGroupMetadata{CidrGroupId: in.CidrGroupID},
	)
	if err != nil {
		return nil, err
	}
	if err := u.opsRepo.Create(ctx, op); err != nil {
		return nil, err
	}
	if err := operations.RunSync(ctx, u.opsRepo, &op, func(ctx context.Context) (*anypb.Any, error) {
		return u.doUpdate(ctx, in)
	}); err != nil {
		return nil, err
	}
	return &op, nil
}

func (u *UpdateCidrGroupUseCase) doUpdate(ctx context.Context, in UpdateInput) (*anypb.Any, error) {
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer w.Abort()

	// GetForUpdate + Update в одной writer-TX: блокировка строки сериализует
	// read-modify-write. Обычный Get здесь дал бы «победил последний» на двух
	// правках с непересекающимися масками.
	rec, err := w.CidrGroups().GetForUpdate(ctx, in.CidrGroupID)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	applyCidrGroupMask(&rec.CidrGroup, in)
	updated, err := w.CidrGroups().Update(ctx, &rec.CidrGroup)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	if err := w.Outbox().Emit(ctx, "CidrGroup", updated.ID, "UPDATED", helpers.DomainToMap(updated)); err != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, err))
	}

	var syncItems []fgaregister.Item
	var intentVersion time.Time
	if labelsInMask(in.UpdateMask) {
		syncItems = []fgaregister.Item{
			fgaregister.ProjectHierarchyItem(updated.ProjectID, "vpc_cidr_group", updated.ID,
				domain.LabelsToMap(updated.Labels)),
		}
		if intentVersion, err = w.FGARegister().EmitRegister(ctx, fgaregister.RegisterItems(syncItems...)); err != nil {
			return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: fga register intent: %v", repo.ErrInternal, err))
		}
	}
	if err := w.Commit(); err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	fgaregister.DeliverAfterCommit(ctx, u.registrar, syncItems, intentVersion, "CidrGroup", updated.ID)
	return marshalCidrGroupRecord(updated)
}

// validateCidrGroupUpdate — проверка маски и заявленных значений.
func validateCidrGroupUpdate(in UpdateInput) error {
	known := map[string]struct{}{"name": {}, "description": {}, "labels": {}}
	if err := corevalidate.UpdateMask("update_mask", in.UpdateMask, known); err != nil {
		return err
	}
	updates := in.UpdateMask
	if len(updates) == 0 {
		updates = []string{"name", "description", "labels"}
	}
	for _, f := range updates {
		switch f {
		case "name":
			if err := in.CidrGroup.Name.Validate(); err != nil {
				return err
			}
		case "description":
			if err := in.CidrGroup.Description.Validate(); err != nil {
				return err
			}
		case "labels":
			if err := domain.ValidateLabels(in.CidrGroup.Labels); err != nil {
				return err
			}
		}
	}
	return nil
}

// labelsInMask — затрагивает ли маска метки: пустая маска значит полную правку
// (метки применяются), явная — матчится по имени. Управляет переэмитом
// register-intent.
//
// Хелпер намеренно co-located с applyCidrGroupMask: набор полей полной правки и
// условие переэмита обязаны не разъезжаться, а вынесенный «общий помощник масок»
// связал бы несвязанные пакеты без выгоды.
func labelsInMask(updateMask []string) bool {
	if len(updateMask) == 0 {
		return true
	}
	for _, f := range updateMask {
		if f == "labels" {
			return true
		}
	}
	return false
}

// applyCidrGroupMask применяет подмножество полей. Пустая маска = полная правка
// всех изменяемых полей; состав не входит в набор ни при какой маске.
func applyCidrGroupMask(g *domain.CidrGroup, in UpdateInput) {
	if len(in.UpdateMask) == 0 {
		g.Name = in.CidrGroup.Name
		g.Description = in.CidrGroup.Description
		g.Labels = in.CidrGroup.Labels
		return
	}
	for _, field := range in.UpdateMask {
		switch field {
		case "name":
			g.Name = in.CidrGroup.Name
		case "description":
			g.Description = in.CidrGroup.Description
		case "labels":
			g.Labels = in.CidrGroup.Labels
		}
	}
}
