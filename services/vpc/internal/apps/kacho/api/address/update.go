// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package address

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

// UpdateInput — параметры для UpdateAddressUseCase.Execute. Address — особый
// случай: name optional (пустой допустим, как и в Create).
//
// Это не зеркало domain-структуры: храним domain-поля плюс orthogonal mask и два
// mutable-bool'а.
type UpdateInput struct {
	AddressID          string
	Name               string
	Description        string
	Labels             map[string]string
	DeletionProtection bool
	Reserved           bool
	UpdateMask         []string
}

// UpdateAddressUseCase — sync-валидация update_mask и значений, затем создание
// Operation + async update в worker'е. Spec-поля (external/internal v4/v6) —
// hard-immutable, через mask их менять нельзя. Writer-TX явный: DML + outbox
// (Address.UPDATED) атомарны.
type UpdateAddressUseCase struct {
	repo      Repo
	opsRepo   operations.Repo
	registrar fgaregister.Registrar
}

// NewUpdateAddressUseCase создает UpdateAddressUseCase.
func NewUpdateAddressUseCase(r Repo, opsRepo operations.Repo) *UpdateAddressUseCase {
	return &UpdateAddressUseCase{repo: r, opsRepo: opsRepo}
}

// WithRegistrar подключает синхронный owner-tuple registrar — тот же, что у
// create-пути. Смена меток меняет проекцию, которую читает селектор владельца
// прав, поэтому она обязана доезжать на пути запроса: durable-intent остаётся
// at-least-once backstop'ом, но ждать его — значит отдать ОТЗЫВ по снятию метки
// глубине очереди (замер стенда 2026-08-05: 188–365 с при клиентском бюджете
// 15 с). nil (dev/no-iam) → остаётся только async-путь.
func (u *UpdateAddressUseCase) WithRegistrar(r fgaregister.Registrar) *UpdateAddressUseCase {
	u.registrar = r
	return u
}

// Execute — sync-проверки и запуск Update в worker'е.
func (u *UpdateAddressUseCase) Execute(ctx context.Context, in UpdateInput) (*operations.Operation, error) {
	if err := corevalidate.ResourceID("address", ids.PrefixAddress, in.AddressID); err != nil {
		return nil, err
	}
	if in.AddressID == "" {
		return nil, status.Error(codes.InvalidArgument, "address_id required")
	}
	if err := serviceerr.FromValidation(validateAddressUpdate(in)); err != nil {
		return nil, err
	}

	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Update address %s", in.AddressID),
		&vpcv1.UpdateAddressMetadata{AddressId: in.AddressID},
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

func (u *UpdateAddressUseCase) doUpdate(ctx context.Context, in UpdateInput) (*anypb.Any, error) {
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer w.Abort()

	// GetForUpdate + Update внутри одной writer-TX: row-lock (`FOR UPDATE`)
	// сериализует read-modify-write — конкурентный disjoint-mask Update не может
	// затереть un-masked поле (напр. deletion_protection). Голый Get здесь был бы
	// TOCTOU-race (project-rule #10).
	rec, err := w.Addresses().GetForUpdate(ctx, in.AddressID)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	applyAddressMask(&rec.Address, in)

	updated, err := w.Addresses().Update(ctx, &rec.Address)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	if err := w.Outbox().Emit(ctx, "Address", updated.ID, updated.ProjectID, "UPDATED", helpers.DomainToMap(updated)); err != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, err))
	}
	// Если labels попали в update_mask (или это full-object PATCH), переэмитим
	// register-intent с обновленными метками в ТОЙ ЖЕ writer-TX, чтобы kaname
	// держал resource_mirror в актуальном виде для ARM_LABELS-селектора (revoke
	// при снятии метки). Update без labels → переэмита нет. Полное снятие labels →
	// upsert с пустыми метками (НЕ Unregister: Address все еще существует). Эталон —
	// network/subnet/securitygroup update.
	var syncItems []fgaregister.Item
	var intentVersion time.Time
	if labelsInMask(in.UpdateMask) {
		syncItems = []fgaregister.Item{
			fgaregister.ProjectHierarchyItem(string(updated.ProjectID), "vpc_address", updated.ID,
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
	fgaregister.DeliverAfterCommit(ctx, u.registrar, syncItems, intentVersion, "Address", updated.ID)
	return marshalAddressRecord(updated)
}

// labelsInMask — затрагивает ли update_mask поле `labels`: пустая маска значит
// full-object PATCH (labels применяются), явная маска матчится, если содержит
// "labels". Управляет переэмитом register-intent — держать в синхроне с
// full-PATCH-набором полей в applyAddressMask.
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

// validateAddressUpdate проверяет name/description/labels в Update Address.
//
// В отличие от других ресурсов, name для Address optional — `name=""` валиден,
// regex применяется только если непустой. Валидация — через domain newtypes.
func validateAddressUpdate(in UpdateInput) error {
	known := map[string]struct{}{
		"name": {}, "description": {}, "labels": {},
		"deletion_protection": {}, "reserved": {},
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
			if _, err := corevalidate.NameOnUpdate("name", in.UpdateMask, in.Name); err != nil {
				return err
			}
		case "description":
			if err := domain.RcDescription(in.Description).Validate(); err != nil {
				return err
			}
		case "labels":
			if err := domain.ValidateLabels(domain.LabelsFromMap(in.Labels)); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyAddressMask — применяет subset полей к существующему domain.Address.
// Пустой mask = full PATCH.
func applyAddressMask(a *domain.Address, in UpdateInput) {
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
	applyName, _ := corevalidate.NameOnUpdate("name", in.UpdateMask, in.Name)
	if applyName {
		a.Name = domain.RcNameVPC(in.Name)
	}
	if len(in.UpdateMask) == 0 {
		a.Description = domain.RcDescription(in.Description)
		a.Labels = domain.LabelsFromMap(in.Labels)
		a.DeletionProtection = in.DeletionProtection
		a.Reserved = in.Reserved
		return
	}
	for _, field := range in.UpdateMask {
		switch field {
		case "description":
			a.Description = domain.RcDescription(in.Description)
		case "labels":
			a.Labels = domain.LabelsFromMap(in.Labels)
		case "deletion_protection":
			a.DeletionProtection = in.DeletionProtection
		case "reserved":
			a.Reserved = in.Reserved
		}
	}
}
