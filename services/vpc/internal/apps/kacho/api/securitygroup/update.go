// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package securitygroup

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

// UpdateInput — параметры для UpdateSecurityGroupUseCase.Execute. Несет
// SecurityGroupID (target) + domain.SecurityGroup (с заявленными полями) +
// UpdateMask. Собственный input-тип оправдан orthogonal mask'ом и target id.
//
// rule_specs в этом use-case'е принимаются как full-replace всего набора правил
// (через mask `"rule_specs"`). Инкрементальная правка — отдельный use-case
// `UpdateRules`.
type UpdateInput struct {
	SecurityGroupID string
	SecurityGroup   domain.SecurityGroup // несет Name/Description/Labels/Rules
	UpdateMask      []string
}

// UpdateSecurityGroupUseCase — sync-валидация update_mask + значений, затем
// создание Operation + async update в worker'е. Get + Update + outbox-emit —
// в одной writer-TX (writer видит свои writes для Get).
type UpdateSecurityGroupUseCase struct {
	repo      Repo
	opsRepo   operations.Repo
	sgReader  SecurityGroupReader // обязателен и не пуст; см. sgTargetReader
	registrar fgaregister.Registrar
}

// NewUpdateSecurityGroupUseCase создает UpdateSecurityGroupUseCase.
//
// Порт чтения групп выводится здесь же из обязательного `Repo` — same-network
// проверка SG-target-правил, приехавших через `rule_specs`, не имеет выключенного
// состояния.
func NewUpdateSecurityGroupUseCase(r Repo, opsRepo operations.Repo) *UpdateSecurityGroupUseCase {
	return &UpdateSecurityGroupUseCase{repo: r, opsRepo: opsRepo, sgReader: sgTargetReader(nil, r)}
}

// WithRegistrar подключает синхронный owner-tuple registrar — тот же, что у
// create-пути. Смена меток меняет проекцию, которую читает селектор владельца
// прав, поэтому она обязана доезжать на пути запроса: durable-intent остаётся
// at-least-once backstop'ом, но ждать его — значит отдать ОТЗЫВ по снятию метки
// глубине очереди (замер стенда 2026-08-05: 188–365 с при клиентском бюджете
// 15 с). nil (dev/no-iam) → остаётся только async-путь.
func (u *UpdateSecurityGroupUseCase) WithRegistrar(r fgaregister.Registrar) *UpdateSecurityGroupUseCase {
	u.registrar = r
	return u
}

// WithSGReader уточняет ИСТОЧНИК чтения групп для same-network-валидации
// SG-target правил, переданных через rule_specs (parity с UpdateRules/UpdateRule).
// Проверку он не включает и не выключает — она уже включена конструктором.
func (u *UpdateSecurityGroupUseCase) WithSGReader(r SecurityGroupReader) *UpdateSecurityGroupUseCase {
	u.sgReader = sgTargetReader(r, u.repo)
	return u
}

// Execute — sync-проверки и запуск Update в worker'е.
func (u *UpdateSecurityGroupUseCase) Execute(ctx context.Context, in UpdateInput) (*operations.Operation, error) {
	if err := corevalidate.ResourceID("security group", ids.PrefixSecurityGroup, in.SecurityGroupID); err != nil {
		return nil, err
	}
	if in.SecurityGroupID == "" {
		return nil, status.Error(codes.InvalidArgument, "security_group_id required")
	}
	if err := serviceerr.FromValidation(validateSGUpdate(in)); err != nil {
		return nil, err
	}

	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Update security group %s", in.SecurityGroupID),
		&vpcv1.UpdateSecurityGroupMetadata{SecurityGroupId: in.SecurityGroupID},
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

func (u *UpdateSecurityGroupUseCase) doUpdate(ctx context.Context, in UpdateInput) (*anypb.Any, error) {
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
	rec, err := w.SecurityGroups().GetForUpdate(ctx, in.SecurityGroupID)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	applySGMask(&rec.SecurityGroup, in)
	// Потолок проверяется на ИТОГОВОМ наборе — до резолва целей.
	if cerr := validateSGRulesCardinality("rule_specs", rec.Rules); cerr != nil {
		return nil, cerr
	}
	// Same-network-валидация SG-target правил (parity с UpdateRules): rule_specs,
	// переданные через Update, не должны указывать на SG из другой сети. network_id
	// берем у самой редактируемой SG (immutable).
	if verr := validateSGTargetSameNetwork(ctx, u.sgReader, rec.NetworkID, rec.Rules,
		func(i int) string { return fmt.Sprintf("rule_specs[%d].security_group_id", i) }); verr != nil {
		return nil, verr
	}
	updated, err := w.SecurityGroups().Update(ctx, &rec.SecurityGroup)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	// Ссылка правила на именованный набор — ПОСЛЕ записи правил и в этой же
	// транзакции: см. validateSGTargetCidrGroup о том, почему порядок несущий.
	if verr := validateSGTargetCidrGroup(ctx, w.CidrGroups(), string(updated.ProjectID), updated.Rules,
		func(i int) string { return fmt.Sprintf("rule_specs[%d].cidr_group_id", i) }); verr != nil {
		return nil, verr
	}
	if oerr := w.Outbox().Emit(ctx, "SecurityGroup", updated.ID, updated.ProjectID, "UPDATED", helpers.DomainToMap(updated)); oerr != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, oerr))
	}
	// Если labels попали в update_mask (или это full-object PATCH) — повторно
	// эмитим register-intent с обновленными labels в ТОЙ ЖЕ writer-TX, чтобы
	// kaname держал resource_mirror актуальным для label-based селектора
	// (label-change reconcile / revoke). Update без labels → re-emit не делаем.
	// Полное снятие labels → upsert с пустыми labels (не Unregister).
	var syncItems []fgaregister.Item
	var intentVersion time.Time
	if labelsInMask(in.UpdateMask) {
		syncItems = []fgaregister.Item{
			fgaregister.ProjectHierarchyItem(string(updated.ProjectID), "vpc_security_group", updated.ID,
				domain.LabelsToMap(updated.Labels)),
		}
		var rerr error
		if intentVersion, rerr = w.FGARegister().EmitRegister(ctx, fgaregister.RegisterItems(syncItems...)); rerr != nil {
			return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: fga register intent: %v", repo.ErrInternal, rerr))
		}
	}
	if cerr := w.Commit(); cerr != nil {
		return nil, serviceerr.MapRepoErr(cerr)
	}
	// Синхронная доставка ПОСЛЕ durable-коммита — симметрия с create-путём.
	fgaregister.DeliverAfterCommit(ctx, u.registrar, syncItems, intentVersion, "SecurityGroup", updated.ID)
	return marshalSecurityGroupRecord(updated)
}

// validateSGUpdate — sync-проверка update_mask и значений (parity с
// Network/Subnet): name/description/labels — через newtype.Validate(),
// mask-семантика — через corevalidate.UpdateMask.
//
// Для каждого поля в mask:
//
//	name        → RcNameVPC.Validate() (разрешительный name-regex).
//	description → RcDescription.Validate() (≤256 chars utf-8).
//	labels      → domain.ValidateLabels() (≤64 пары, ограничения key/value).
//	rule_specs  → каждое правило проходит validateSGRule.
//
// Поле, не упомянутое в mask, не валидируется (= unchanged). Unknown field в
// update_mask → InvalidArgument (corevalidate.UpdateMask).
func validateSGUpdate(in UpdateInput) error {
	known := map[string]struct{}{
		"name": {}, "description": {}, "labels": {}, "rule_specs": {},
	}
	if err := corevalidate.UpdateMask("update_mask", in.UpdateMask, known); err != nil {
		return err
	}
	updates := in.UpdateMask
	if len(updates) == 0 {
		// no-mask = full PATCH. applySGMask применяет rule_specs, если Rules != nil
		// → валидируем их здесь же, иначе full-PATCH писал бы в БД непроверенные
		// правила (малформированный CIDR/direction).
		updates = []string{"name", "description", "labels"}
		if in.SecurityGroup.Rules != nil {
			updates = append(updates, "rule_specs")
		}
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
			if _, err := corevalidate.NameOnUpdate("name", in.UpdateMask, string(in.SecurityGroup.Name)); err != nil {
				return err
			}
		case "description":
			if err := in.SecurityGroup.Description.Validate(); err != nil {
				return err
			}
		case "labels":
			if err := domain.ValidateLabels(in.SecurityGroup.Labels); err != nil {
				return err
			}
		case "rule_specs":
			for i, r := range in.SecurityGroup.Rules {
				if err := validateSGRule(fmt.Sprintf("rule_specs[%d]", i), r); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// labelsInMask сообщает, затрагивает ли update_mask поле `labels`: пустой mask —
// это full-object PATCH (labels применяются), явный mask — когда содержит
// "labels". Управляет register re-emit'ом и должен оставаться в lockstep с
// набором full-PATCH полей в applySGMask.
//
// Дублируется в subnet/network update.go (не вынесено в shared): тривиальный
// one-liner живет рядом со своим applyXxxMask, чтобы набор full-PATCH полей и
// emit-gate не разъехались; общий cross-package helper связал бы несвязанные
// use-case-пакеты без реальной выгоды от переиспользования.
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

// applySGMask — применяет subset полей к существующему domain.SecurityGroup.
// no-mask = full PATCH.
func applySGMask(sg *domain.SecurityGroup, in UpdateInput) {
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
	applyName, _ := corevalidate.NameOnUpdate("name", in.UpdateMask, string(in.SecurityGroup.Name))
	if applyName {
		sg.Name = in.SecurityGroup.Name
	}
	if len(in.UpdateMask) == 0 {
		sg.Description = in.SecurityGroup.Description
		sg.Labels = in.SecurityGroup.Labels
		if in.SecurityGroup.Rules != nil {
			sg.Rules = assignRuleIDs(in.SecurityGroup.Rules)
		}
		return
	}
	for _, field := range in.UpdateMask {
		switch field {
		case "description":
			sg.Description = in.SecurityGroup.Description
		case "labels":
			sg.Labels = in.SecurityGroup.Labels
		case "rule_specs":
			sg.Rules = assignRuleIDs(in.SecurityGroup.Rules)
		}
	}
}
