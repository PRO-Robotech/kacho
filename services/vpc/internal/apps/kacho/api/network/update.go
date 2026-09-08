// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

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

// UpdateInput — параметры для UpdateNetworkUseCase.Execute. Update требует и
// domain.Network (с заявленными полями), и UpdateMask, поэтому отдельный input-тип
// оправдан: он хранит domain плюс ортогональную ему маску.
type UpdateInput struct {
	NetworkID  string
	Network    domain.Network // несет Name/Description/Labels, остальные поля не используются
	UpdateMask []string
}

// UpdateNetworkUseCase — sync-валидация update_mask + значений, затем создание
// Operation + async update в worker'е. Writer-TX явный, DML + outbox атомарны.
type UpdateNetworkUseCase struct {
	repo      Repo
	opsRepo   operations.Repo
	registrar fgaregister.Registrar
}

// NewUpdateNetworkUseCase создает UpdateNetworkUseCase.
func NewUpdateNetworkUseCase(r Repo, opsRepo operations.Repo) *UpdateNetworkUseCase {
	return &UpdateNetworkUseCase{repo: r, opsRepo: opsRepo}
}

// WithRegistrar подключает синхронный owner-tuple registrar — тот же, что у
// create-пути. Смена меток меняет проекцию, которую читает селектор владельца
// прав, поэтому она обязана доезжать на пути запроса: durable-intent остаётся
// at-least-once backstop'ом, но ждать его — значит отдать ОТЗЫВ по снятию метки
// глубине очереди (замер стенда 2026-08-05: 188–365 с при клиентском бюджете
// 15 с). nil (dev/no-iam) → остаётся только async-путь.
func (u *UpdateNetworkUseCase) WithRegistrar(r fgaregister.Registrar) *UpdateNetworkUseCase {
	u.registrar = r
	return u
}

// Execute — sync-проверки и запуск Update в worker'е.
func (u *UpdateNetworkUseCase) Execute(ctx context.Context, in UpdateInput) (*operations.Operation, error) {
	if err := corevalidate.ResourceID("network", ids.PrefixNetwork, in.NetworkID); err != nil {
		return nil, err
	}
	if in.NetworkID == "" {
		return nil, status.Error(codes.InvalidArgument, "network_id required")
	}
	// Immutable-switch ДО corevalidate.UpdateMask (api-conventions gotcha): known-set
	// маски НЕ содержит immutable-полей, поэтому без этого switch они отверглись бы
	// generic "unknown field" вместо конвенционного immutable-текста.
	//   - project_id — Move снят целиком, hard-immutable (VPC-1-20).
	//   - ipv4/ipv6_cidr_blocks — declared супернет: immutable через Update, мутируется
	//     ТОЛЬКО через verb-pair :add-cidr-blocks / :remove-cidr-blocks (VPC-1-07).
	//   - default_security_group_id / default_route_table_id — server-derived °
	//     (system-provisioned), на вход Update не принимаются.
	for _, field := range in.UpdateMask {
		switch field {
		case "project_id", "ipv4_cidr_blocks", "ipv6_cidr_blocks",
			"default_security_group_id", "default_route_table_id":
			return nil, serviceerr.InvalidArg(field, field+" is immutable after Network.Create")
		}
	}
	if err := serviceerr.FromValidation(validateNetworkUpdate(in)); err != nil {
		return nil, err
	}

	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Update network %s", in.NetworkID),
		&vpcv1.UpdateNetworkMetadata{NetworkId: in.NetworkID},
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

func (u *UpdateNetworkUseCase) doUpdate(ctx context.Context, in UpdateInput) (*anypb.Any, error) {
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
	rec, err := w.Networks().GetForUpdate(ctx, in.NetworkID)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	applyNetworkMask(&rec.Network, in)
	updated, err := w.Networks().Update(ctx, &rec.Network)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	if err := w.Outbox().Emit(ctx, "Network", updated.ID, updated.ProjectID, "UPDATED", helpers.DomainToMap(updated)); err != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, err))
	}
	// Если labels попали в update_mask (или это full-object PATCH), переэмитим
	// register-intent с обновленными метками в ТОЙ ЖЕ writer-TX, чтобы kaname
	// держал resource_mirror в актуальном виде для ARM_LABELS-селектора
	// (reconcile / revoke при смене меток). Update без labels → переэмита нет.
	// Полное удаление labels → upsert с пустыми метками (НЕ Unregister: сеть все
	// еще существует, mirror-row остается, просто перестает матчиться селектором).
	var syncItems []fgaregister.Item
	var intentVersion time.Time
	if labelsInMask(in.UpdateMask) {
		syncItems = []fgaregister.Item{
			fgaregister.ProjectHierarchyItem(string(updated.ProjectID), "vpc_network", updated.ID,
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
	fgaregister.DeliverAfterCommit(ctx, u.registrar, syncItems, intentVersion, "Network", updated.ID)
	return marshalNetworkRecord(updated)
}

// validateNetworkUpdate — sync-проверка update_mask и значений: заявленные поля
// преобразуем в domain-newtypes и зовем их `Validate()` напрямую.
func validateNetworkUpdate(in UpdateInput) error {
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
			if _, err := corevalidate.NameOnUpdate("name", in.UpdateMask, string(in.Network.Name)); err != nil {
				return err
			}
		case "description":
			if err := in.Network.Description.Validate(); err != nil {
				return err
			}
		case "labels":
			if err := domain.ValidateLabels(in.Network.Labels); err != nil {
				return err
			}
		}
	}
	return nil
}

// labelsInMask — затрагивает ли update_mask поле `labels`: пустая маска значит
// full-object PATCH (labels применяются), явная маска матчится, если содержит
// "labels". Управляет переэмитом register-intent — держать в синхроне с
// full-PATCH-набором полей в applyNetworkMask.
//
// Хелпер намеренно co-located с applyNetworkMask (а не вынесен в shared-пакет),
// чтобы full-PATCH-набор полей и emit-gate не разъехались; общий «shared mask
// helper» связал бы несвязанные use-case-пакеты без реальной пользы.
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

// applyNetworkMask — применяет subset полей к существующему domain.Network.
// Пустая маска = full PATCH (применяются все mutable-поля).
func applyNetworkMask(n *domain.Network, in UpdateInput) {
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
	applyName, _ := corevalidate.NameOnUpdate("name", in.UpdateMask, string(in.Network.Name))
	if applyName {
		n.Name = in.Network.Name
	}
	if len(in.UpdateMask) == 0 {
		n.Description = in.Network.Description
		n.Labels = in.Network.Labels
		return
	}
	for _, field := range in.UpdateMask {
		switch field {
		case "description":
			n.Description = in.Network.Description
		case "labels":
			n.Labels = in.Network.Labels
		}
	}
}
