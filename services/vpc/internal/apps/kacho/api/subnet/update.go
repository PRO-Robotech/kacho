// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

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

// UpdateInput — параметры для UpdateSubnetUseCase.Execute. Update'у нужны и
// domain.Subnet (с заявленными полями), и UpdateMask, поэтому используется
// отдельный input-тип.
type UpdateInput struct {
	SubnetID   string
	Subnet     domain.Subnet // несет Name/Description/Labels/RouteTableID; остальные не используются
	UpdateMask []string
}

// UpdateSubnetUseCase — sync-валидация update_mask + значений, затем создание
// Operation + async update в worker'е.
//
// network_id / zone_id — hard-immutable: явное указание в update_mask →
// InvalidArgument; присланное в body без mask — silently игнорируется
// (full-object PATCH из UI). VPC-1 F7: CIDR immutable через Update —
// ipv4_cidr_primary / ipv4_cidr_blocks (и v6) в mask → immutable-reject; правят
// их через :add/:removeCidrBlocks (primary-anchor не меняется вовсе).
//
// Worker открывает Writer-TX и делает Get+Update+outbox атомарно.
type UpdateSubnetUseCase struct {
	repo      Repo
	opsRepo   operations.Repo
	registrar fgaregister.Registrar
}

// NewUpdateSubnetUseCase создает UpdateSubnetUseCase.
func NewUpdateSubnetUseCase(r Repo, opsRepo operations.Repo) *UpdateSubnetUseCase {
	return &UpdateSubnetUseCase{repo: r, opsRepo: opsRepo}
}

// WithRegistrar подключает синхронный owner-tuple registrar — тот же, что у
// create-пути. Смена меток меняет проекцию, которую читает селектор владельца
// прав, поэтому она обязана доезжать на пути запроса: durable-intent остаётся
// at-least-once backstop'ом, но ждать его — значит отдать ОТЗЫВ по снятию метки
// глубине очереди (замер стенда 2026-08-05: 188–365 с при клиентском бюджете
// 15 с). nil (dev/no-iam) → остаётся только async-путь.
func (u *UpdateSubnetUseCase) WithRegistrar(r fgaregister.Registrar) *UpdateSubnetUseCase {
	u.registrar = r
	return u
}

// Execute — sync-проверки и запуск Update в worker'е.
func (u *UpdateSubnetUseCase) Execute(ctx context.Context, in UpdateInput) (*operations.Operation, error) {
	if err := corevalidate.ResourceID("subnet", ids.PrefixSubnet, in.SubnetID); err != nil {
		return nil, err
	}
	if in.SubnetID == "" {
		return nil, status.Error(codes.InvalidArgument, "subnet_id required")
	}
	// Immutable-switch ДО corevalidate.UpdateMask (api-conventions): known-set маски
	// НЕ содержит immutable-полей, поэтому без этого switch они отверглись бы generic
	// "unknown field" вместо конвенционного immutable/derived-текста.
	// network_id/zone_id/region_id — hard-immutable (VRF-scoping + placement-coherence
	// всех размещённых ресурсов). placement_type — server-derived (F6): даже в mask
	// это не «immutable value», а «нельзя писать» → derive-reject текст.
	for _, field := range in.UpdateMask {
		switch field {
		case "network_id", "zone_id", "region_id",
			"ipv4_cidr_primary", "ipv6_cidr_primary", "ipv4_cidr_blocks", "ipv6_cidr_blocks":
			return nil, serviceerr.InvalidArg(field, field+" is immutable after Subnet.Create")
		case "placement_type":
			return nil, status.Error(codes.InvalidArgument,
				"placement_type is server-derived; set zone_id or region_id instead")
		}
	}
	if err := serviceerr.FromValidation(validateSubnetUpdate(in)); err != nil {
		return nil, err
	}

	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Update subnet %s", in.SubnetID),
		&vpcv1.UpdateSubnetMetadata{SubnetId: in.SubnetID},
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

func (u *UpdateSubnetUseCase) doUpdate(ctx context.Context, in UpdateInput) (*anypb.Any, error) {
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
	rec, err := w.Subnets().GetForUpdate(ctx, in.SubnetID)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	prevRT := rec.Subnet.RouteTableID
	applySubnetMask(&rec.Subnet, in)
	// Таблица маршрутов — ссылка от вызывающего. Внешний ключ гарантирует лишь
	// существование строки, а таблица принадлежит СВОЕЙ сети, поэтому без этой
	// проверки подсеть привязывается к таблице чужой сети (и чужого проекта) —
	// состояние, которого по дизайну не бывает и которое база выразить не может.
	// Проверка в той же writer-TX, что и запись.
	if rec.Subnet.RouteTableID != prevRT && rec.Subnet.RouteTableID != "" {
		if rtErr := validateSubnetRouteTableRef(ctx, w.RouteTables(), rec.Subnet.RouteTableID, rec.Subnet.NetworkID); rtErr != nil {
			return nil, rtErr
		}
	}
	updated, err := w.Subnets().Update(ctx, &rec.Subnet)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	if err := w.Outbox().Emit(ctx, "Subnet", updated.ID, updated.ProjectID, "UPDATED", helpers.DomainToMap(updated)); err != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, err))
	}
	// Если labels были в update_mask (или это full-object PATCH), пере-эмитим
	// register-intent с обновленными labels в ТОЙ ЖЕ writer-TX, чтобы kaname
	// держал resource_mirror актуальным для label-селектора (reconcile при смене
	// label'ов). Update без labels → re-emit не делаем.
	var syncItems []fgaregister.Item
	var intentVersion time.Time
	if labelsInMask(in.UpdateMask) {
		syncItems = []fgaregister.Item{
			fgaregister.ProjectHierarchyItem(string(updated.ProjectID), "vpc_subnet", updated.ID,
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
	fgaregister.DeliverAfterCommit(ctx, u.registrar, syncItems, intentVersion, "Subnet", updated.ID)
	return marshalSubnetRecord(updated)
}

// validateSubnetUpdate проверяет name/description/labels в Update.
// Валидация идет через domain-newtypes (self-validating domain).
//
// Immutable-поля (network_id, zone_id, region_id, ipv4/ipv6_cidr_primary/_blocks)
// ловятся раньше в Execute() immutable-switch (до UpdateMask) → сюда не доходят;
// known-set содержит только mutable-поля. VPC-1-43: dhcp_options снят by design —
// в known-set его нет, поэтому dhcp_options в update_mask → InvalidArgument (unknown).
func validateSubnetUpdate(in UpdateInput) error {
	known := map[string]struct{}{
		"name": {}, "description": {}, "labels": {},
		"route_table_id": {},
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
			if _, err := corevalidate.NameOnUpdate("name", in.UpdateMask, string(in.Subnet.Name)); err != nil {
				return err
			}
		case "description":
			if err := in.Subnet.Description.Validate(); err != nil {
				return err
			}
		case "labels":
			if err := domain.ValidateLabels(in.Subnet.Labels); err != nil {
				return err
			}
		}
	}
	return nil
}

// labelsInMask сообщает, затрагивает ли update_mask поле labels: пустая маска —
// это full-object PATCH (labels применяются), явная маска совпадает, если в ней
// есть "labels". Управляет re-emit register-intent — держать в синхроне с набором
// full-PATCH-полей applySubnetMask.
func labelsInMask(updateMask []string) bool {
	if len(updateMask) == 0 {
		return true // full-object PATCH пишет labels
	}
	for _, f := range updateMask {
		if f == "labels" {
			return true
		}
	}
	return false
}

// applySubnetMask применяет mutable-поля из in к sub.
//
// Immutable-поля (ipv4/ipv6_cidr_primary/_blocks, network_id, zone_id, region_id)
// НЕ применяются никогда — даже если клиент прислал их в body без mask; sync-check
// в Execute() отвергает попытку указать их в update_mask. VPC-1-43: dhcp_options
// снят by design — Update его не трогает (mutable-набор: name/description/labels/
// route_table_id).
func applySubnetMask(sub *domain.Subnet, in UpdateInput) {
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
	applyName, _ := corevalidate.NameOnUpdate("name", in.UpdateMask, string(in.Subnet.Name))
	if applyName {
		sub.Name = in.Subnet.Name
	}
	if len(in.UpdateMask) == 0 {
		// Полный update — только mutable-поля.
		sub.Description = in.Subnet.Description
		sub.Labels = in.Subnet.Labels
		sub.RouteTableID = in.Subnet.RouteTableID
		return
	}
	for _, field := range in.UpdateMask {
		switch field {
		case "description":
			sub.Description = in.Subnet.Description
		case "labels":
			sub.Labels = in.Subnet.Labels
		case "route_table_id":
			sub.RouteTableID = in.Subnet.RouteTableID
		}
	}
}
