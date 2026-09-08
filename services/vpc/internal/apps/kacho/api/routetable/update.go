// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package routetable

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

// UpdateInput — параметры для UpdateRouteTableUseCase.Execute.
type UpdateInput struct {
	RouteTableID string
	RouteTable   domain.RouteTable // несет Name/Description/Labels/StaticRoutes
	UpdateMask   []string
}

// UpdateRouteTableUseCase — sync-валидация update_mask и значений, затем создание
// Operation и async-апдейт в worker'е. Writer-TX явный: DML и outbox-emit атомарны.
type UpdateRouteTableUseCase struct {
	repo      Repo
	opsRepo   operations.Repo
	registrar fgaregister.Registrar
}

// NewUpdateRouteTableUseCase создает UpdateRouteTableUseCase.
func NewUpdateRouteTableUseCase(r Repo, opsRepo operations.Repo) *UpdateRouteTableUseCase {
	return &UpdateRouteTableUseCase{repo: r, opsRepo: opsRepo}
}

// WithRegistrar подключает синхронный owner-tuple registrar — тот же, что у
// create-пути. Смена меток меняет проекцию, которую читает селектор владельца
// прав, поэтому она обязана доезжать на пути запроса: durable-intent остаётся
// at-least-once backstop'ом, но ждать его — значит отдать ОТЗЫВ по снятию метки
// глубине очереди (замер стенда 2026-08-05: 188–365 с при клиентском бюджете
// 15 с). nil (dev/no-iam) → остаётся только async-путь.
func (u *UpdateRouteTableUseCase) WithRegistrar(r fgaregister.Registrar) *UpdateRouteTableUseCase {
	u.registrar = r
	return u
}

// Execute — sync-проверки и запуск Update в worker'е.
func (u *UpdateRouteTableUseCase) Execute(ctx context.Context, in UpdateInput) (*operations.Operation, error) {
	if err := corevalidate.ResourceID("route table", ids.PrefixRouteTable, in.RouteTableID); err != nil {
		return nil, err
	}
	if in.RouteTableID == "" {
		return nil, status.Error(codes.InvalidArgument, "route_table_id required")
	}
	if err := serviceerr.FromValidation(validateRouteTableUpdate(in)); err != nil {
		return nil, err
	}

	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Update route table %s", in.RouteTableID),
		&vpcv1.UpdateRouteTableMetadata{RouteTableId: in.RouteTableID},
	)
	if err != nil {
		return nil, err
	}
	if err := u.opsRepo.Create(ctx, op); err != nil {
		return nil, err
	}

	operations.Run(ctx, u.opsRepo, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		return u.doUpdate(ctx, in)
	})

	return &op, nil
}

func (u *UpdateRouteTableUseCase) doUpdate(ctx context.Context, in UpdateInput) (*anypb.Any, error) {
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer w.Abort()

	// GetForUpdate (SELECT … FOR UPDATE) + Update в одной writer-TX: row-lock
	// сериализует read-modify-write. Конкурентный Update с disjoint update_mask
	// блокируется на GetForUpdate до commit первого, затем читает уже обновленный
	// row и применяет свою маску поверх — lost-update исключен. Plain Get здесь был
	// бы race-prone (second-writer-wins).
	rec, err := w.RouteTables().GetForUpdate(ctx, in.RouteTableID)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	applyRouteTableMask(&rec.RouteTable, in)
	updated, err := w.RouteTables().Update(ctx, &rec.RouteTable)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	if err := w.Outbox().Emit(ctx, "RouteTable", updated.ID, updated.ProjectID, "UPDATED", helpers.RouteTablePayload(updated)); err != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, err))
	}
	// Если labels попали в update_mask (или это full-object PATCH), переэмитим
	// register-intent с обновленными метками в ТОЙ ЖЕ writer-TX, чтобы kaname
	// держал resource_mirror в актуальном виде для ARM_LABELS-селектора (revoke
	// при снятии метки). Update без labels → переэмита нет. Полное снятие labels →
	// upsert с пустыми метками (НЕ Unregister: RouteTable все еще существует,
	// mirror-row остается, просто перестает матчиться селектором). Эталон —
	// network/subnet/securitygroup update.
	var syncItems []fgaregister.Item
	var intentVersion time.Time
	if labelsInMask(in.UpdateMask) {
		syncItems = []fgaregister.Item{
			fgaregister.ProjectHierarchyItem(string(updated.ProjectID), "vpc_route_table", updated.ID,
				domain.LabelsToMap(updated.Labels)),
		}
		var err error
		if intentVersion, err = w.FGARegister().EmitRegister(ctx, fgaregister.RegisterItems(syncItems...)); err != nil {
			return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: fga register intent: %v", repo.ErrInternal, err))
		}
	}
	if err := w.Commit(); err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	// Синхронная доставка ПОСЛЕ durable-коммита — симметрия с create-путём.
	fgaregister.DeliverAfterCommit(ctx, u.registrar, syncItems, intentVersion, "RouteTable", updated.ID)
	return marshalRouteTableRecord(updated)
}

// labelsInMask — затрагивает ли update_mask поле `labels`: пустая маска значит
// full-object PATCH (labels применяются), явная маска матчится, если содержит
// "labels". Управляет переэмитом register-intent — держать в синхроне с
// full-PATCH-набором полей в applyRouteTableMask.
func labelsInMask(updateMask []string) bool {
	if len(updateMask) == 0 {
		return true // full-object PATCH writes labels
	}
	for _, f := range updateMask {
		if f == "labels" {
			return true
		}
	}
	return false
}

// validateRouteTableUpdate проверяет name/description/labels/static_routes в Update.
func validateRouteTableUpdate(in UpdateInput) error {
	// Hard-immutable поля.
	for _, field := range in.UpdateMask {
		switch field {
		case "network_id", "project_id":
			return serviceerr.InvalidArg(field, field+" is immutable after RouteTable.Create")
		}
	}
	known := map[string]struct{}{
		"name": {}, "description": {}, "labels": {}, "static_routes": {},
	}
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
			// Решение об имени принимает ЕДИНСТВЕННАЯ функция дерева: она читает
			// форму запроса (маска × значение) и отвечает сразу на два вопроса —
			// законен ли ввод и следует ли имя применять. Ту же функцию зовёт
			// применение маски, поэтому проверка и запись разойтись не могут.
			//
			// Пять исходов и их причина — в godoc `validate.NameOnUpdate`; здесь
			// они не пересказываются, иначе завелось бы два места об одном
			// предмете. Коротко о том, что здесь неочевидно: при ПУСТОЙ маске
			// пустое имя законно и означает «не прислано» — в proto3 это
			// неотличимо от отсутствия поля.
			if _, err := corevalidate.NameOnUpdate("name", in.UpdateMask, string(in.RouteTable.Name)); err != nil {
				return err
			}
		case "description":
			if err := in.RouteTable.Description.Validate(); err != nil {
				return err
			}
		case "labels":
			if err := domain.ValidateLabels(in.RouteTable.Labels); err != nil {
				return err
			}
		case "static_routes":
			if err := validateStaticRoutes(in.RouteTable.StaticRoutes); err != nil {
				return err
			}
		}
	}
	// Полный апдейт без mask тоже валидирует static_routes, если они есть.
	if len(in.UpdateMask) == 0 && len(in.RouteTable.StaticRoutes) > 0 {
		if err := validateStaticRoutes(in.RouteTable.StaticRoutes); err != nil {
			return err
		}
	}
	return nil
}

// applyRouteTableMask — применяет subset полей к существующему domain.RouteTable.
func applyRouteTableMask(rt *domain.RouteTable, in UpdateInput) {
	// Применять ли имя, решает ТА ЖЕ функция, что вынесла приговор на проверке
	// входа, — поэтому проверка и запись разойтись не могут by construction.
	// Здесь читается только её булева половина: отказ уже случился бы синхронно,
	// до создания операции.
	//
	// Решение вынесено ИЗ ОБЕИХ ветвей маски намеренно. Прежде ветвь полной
	// правки присваивала имя безусловно, и пустое значение уезжало в строку: в
	// proto3 «поле не прислано» неотличимо от «поле пусто», поэтому полная
	// правка, НЕ ТРОГАВШАЯ имя, имя стирала. После миграции 715001, поставившей
	// на столбец ограничение формы, это перестало быть «странным именем» и стало
	// отказом БАЗЫ на пути, где вызывающий не сделал ничего неверного.
	//
	// Ошибка здесь отбрасывается сознательно: на ней функция возвращает false,
	// то есть путь, почему-либо миновавший проверку входа, тоже НЕ запишет
	// негодное имя — отказ направлен в безопасную сторону.
	applyName, _ := corevalidate.NameOnUpdate("name", in.UpdateMask, string(in.RouteTable.Name))
	if applyName {
		rt.Name = in.RouteTable.Name
	}
	if len(in.UpdateMask) == 0 {
		rt.Description = in.RouteTable.Description
		rt.Labels = in.RouteTable.Labels
		rt.StaticRoutes = in.RouteTable.StaticRoutes
		return
	}
	for _, field := range in.UpdateMask {
		switch field {
		case "description":
			rt.Description = in.RouteTable.Description
		case "labels":
			rt.Labels = in.RouteTable.Labels
		case "static_routes":
			rt.StaticRoutes = in.RouteTable.StaticRoutes
		}
	}
}
