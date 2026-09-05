// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package gateway

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

// UpdateInput — параметры для UpdateGatewayUseCase.Execute. Для Update нужны и
// domain.Gateway (с заявленными полями), и ортогональный ему UpdateMask.
type UpdateInput struct {
	GatewayID  string
	Gateway    domain.Gateway // несет Name/Description/Labels/GatewayType; остальные поля не используются
	UpdateMask []string
}

// UpdateGatewayUseCase — sync-валидация update_mask и значений, затем создание
// Operation и async-update в worker'е. doUpdate открывает Writer-TX и делает
// Get + apply mask + Update + outbox emit в одной транзакции.
type UpdateGatewayUseCase struct {
	repo      Repo
	opsRepo   operations.Repo
	registrar fgaregister.Registrar
}

// NewUpdateGatewayUseCase создает UpdateGatewayUseCase.
func NewUpdateGatewayUseCase(r Repo, opsRepo operations.Repo) *UpdateGatewayUseCase {
	return &UpdateGatewayUseCase{repo: r, opsRepo: opsRepo}
}

// WithRegistrar подключает синхронный owner-tuple registrar — тот же, что у
// create-пути. Смена меток меняет проекцию, которую читает селектор владельца
// прав, поэтому она обязана доезжать на пути запроса: durable-intent остаётся
// at-least-once backstop'ом, но ждать его — значит отдать ОТЗЫВ по снятию метки
// глубине очереди (замер стенда 2026-08-05: 188–365 с при клиентском бюджете
// 15 с). nil (dev/no-iam) → остаётся только async-путь.
func (u *UpdateGatewayUseCase) WithRegistrar(r fgaregister.Registrar) *UpdateGatewayUseCase {
	u.registrar = r
	return u
}

// Execute — sync-проверки и запуск Update в worker'е.
func (u *UpdateGatewayUseCase) Execute(ctx context.Context, in UpdateInput) (*operations.Operation, error) {
	if err := corevalidate.ResourceID("gateway", ids.PrefixGateway, in.GatewayID); err != nil {
		return nil, err
	}
	if in.GatewayID == "" {
		return nil, status.Error(codes.InvalidArgument, "gateway_id required")
	}
	// Immutable-switch ДО corevalidate.UpdateMask (api-conventions §gotcha'и):
	// известный набор маски immutable-полей НЕ содержит, поэтому без этого switch
	// они отверглись бы generic'ом «unknown field in update_mask» вместо
	// конвенционного тона. Эталон формы — subnet/update.go.
	for _, field := range in.UpdateMask {
		switch field {
		case gatewayVariantMaskNat, gatewayVariantMaskEgressOnly, "subnet_id":
			return nil, serviceerr.InvalidArg(field, field+" is immutable after Gateway.Create")
		}
	}
	if err := serviceerr.FromValidation(validateGatewayUpdate(in)); err != nil {
		return nil, err
	}

	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Update gateway %s", in.GatewayID),
		&vpcv1.UpdateGatewayMetadata{GatewayId: in.GatewayID},
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

func (u *UpdateGatewayUseCase) doUpdate(ctx context.Context, in UpdateInput) (*anypb.Any, error) {
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer w.Abort()

	// GetForUpdate (SELECT … FOR UPDATE) + Update в одной writer-TX: row-lock
	// сериализует read-modify-write. Конкурентный Update с disjoint update_mask
	// блокируется на GetForUpdate до commit первого, затем читает уже обновленный
	// row и применяет свою маску поверх — lost-update исключен. Plain Get здесь
	// был бы race-prone (second-writer-wins).
	rec, err := w.Gateways().GetForUpdate(ctx, in.GatewayID)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	applyGatewayMask(&rec.Gateway, in)
	updated, err := w.Gateways().Update(ctx, &rec.Gateway)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	if oerr := w.Outbox().Emit(ctx, "Gateway", updated.ID, updated.ProjectID, "UPDATED", helpers.DomainToMap(updated)); oerr != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, oerr))
	}
	// Если labels попали в update_mask (или это full-object PATCH), переэмитим
	// register-intent с обновленными метками в ТОЙ ЖЕ writer-TX, чтобы kaname
	// держал resource_mirror в актуальном виде для ARM_LABELS-селектора (revoke
	// при снятии метки). Update без labels → переэмита нет. Полное снятие labels →
	// upsert с пустыми метками (НЕ Unregister: Gateway все еще существует). Эталон —
	// network/subnet/securitygroup update.
	var syncItems []fgaregister.Item
	var intentVersion time.Time
	if labelsInMask(in.UpdateMask) {
		syncItems = []fgaregister.Item{
			fgaregister.ProjectHierarchyItem(string(updated.ProjectID), "vpc_gateway", updated.ID,
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
	fgaregister.DeliverAfterCommit(ctx, u.registrar, syncItems, intentVersion, "Gateway", updated.ID)
	return marshalGatewayRecord(updated)
}

// labelsInMask — затрагивает ли update_mask поле `labels`: пустая маска значит
// full-object PATCH (labels применяются), явная маска матчится, если содержит
// "labels". Управляет переэмитом register-intent — держать в синхроне с
// full-PATCH-набором полей в applyGatewayMask.
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

// validateGatewayUpdate — sync-проверка update_mask и значений: description/labels
// валидируются через domain newtype.Validate(), name — единственной формой дерева
// (`corevalidate.Name`, #715).
func validateGatewayUpdate(in UpdateInput) error {
	// Известный набор — ИМЕНА ПОЛЕЙ КОНТРАКТА, и только изменяемых. Здесь стояло
	// `gateway_type` — имя СТОЛБЦА БД (`services/vpc/internal/migrations/0001_initial.sql`),
	// которого нет ни в одном сообщении proto. Расхождение работало в обе стороны:
	// имя столбца принималось и меняло вид шлюза, а законное имя поля контракта
	// отвергалось как «unknown field». Вид шлюза выбирается на Create и
	// неизменяем — его место в immutable-switch выше, а не здесь.
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
			if _, err := corevalidate.NameOnUpdate("name", in.UpdateMask, string(in.Gateway.Name)); err != nil {
				return err
			}
		case "description":
			if err := in.Gateway.Description.Validate(); err != nil {
				return err
			}
		case "labels":
			if err := domain.ValidateLabels(in.Gateway.Labels); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyGatewayMask — применяет subset полей к существующему domain.Gateway.
// Пустой mask = full-object PATCH по ИЗМЕНЯЕМЫМ полям; вид шлюза и его привязка
// в набор не входят — они неизменяемы после Create, и присланное в теле без маски
// по конвенции игнорируется молча (явное указание в маске отвергается выше).
func applyGatewayMask(g *domain.Gateway, in UpdateInput) {
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
	applyName, _ := corevalidate.NameOnUpdate("name", in.UpdateMask, string(in.Gateway.Name))
	if applyName {
		g.Name = in.Gateway.Name
	}
	if len(in.UpdateMask) == 0 {
		g.Description = in.Gateway.Description
		g.Labels = in.Gateway.Labels
		return
	}
	for _, field := range in.UpdateMask {
		switch field {
		case "description":
			g.Description = in.Gateway.Description
		case "labels":
			g.Labels = in.Gateway.Labels
		}
	}
}
