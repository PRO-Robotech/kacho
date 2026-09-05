// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package networkinterface

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
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// UpdateInput — параметры для UpdateNetworkInterfaceUseCase.Execute. Несущим
// носителем данных служит сам `domain.NetworkInterface`, чтобы не плодить
// параллельный XxxReq, дублирующий domain. Значимы только потенциально меняемые
// поля (Name/Description/Labels/SecurityGroupIDs/V4AddressIDs/V6AddressIDs/
// BandwidthLimitMbps); project_id/subnet_id/mac — immutable.
type UpdateInput struct {
	NetworkInterfaceID string
	NetworkInterface   domain.NetworkInterface
	UpdateMask         []string
}

// UpdateNetworkInterfaceUseCase инициирует обновление NIC. Sync-часть валидирует
// update_mask и значения (Name/Description/Labels). Async-часть в одной writer-TX
// делает diff address-refs + applyMask + writer.UpdateMeta + outbox-emit.
type UpdateNetworkInterfaceUseCase struct {
	repo      Repo
	opsRepo   operations.Repo
	registrar fgaregister.Registrar
	bandwidth domain.BandwidthLimitPolicy
}

// WithBandwidthLimitPolicy — то же, что у пути создания, и по той же причине:
// правило приёма обязано быть ОДНО на оба пути. Разойдясь, они дали бы стенд, на
// котором величину нельзя задать при создании и можно дописать изменением.
func (u *UpdateNetworkInterfaceUseCase) WithBandwidthLimitPolicy(p domain.BandwidthLimitPolicy) *UpdateNetworkInterfaceUseCase {
	u.bandwidth = p
	return u
}

// NewUpdateNetworkInterfaceUseCase создает UpdateNetworkInterfaceUseCase.
// Address-refs diff идёт через writer-TX (`w.Addresses()`), отдельный AddressRepo
// не инъектируется.
func NewUpdateNetworkInterfaceUseCase(r Repo, opsRepo operations.Repo) *UpdateNetworkInterfaceUseCase {
	return &UpdateNetworkInterfaceUseCase{repo: r, opsRepo: opsRepo}
}

// WithRegistrar подключает синхронный owner-tuple registrar — тот же, что у
// create-пути. Смена меток меняет проекцию, которую читает селектор владельца
// прав, поэтому она обязана доезжать на пути запроса: durable-intent остаётся
// at-least-once backstop'ом, но ждать его — значит отдать ОТЗЫВ по снятию метки
// глубине очереди (замер стенда 2026-08-05: 188–365 с при клиентском бюджете
// 15 с). nil (dev/no-iam) → остаётся только async-путь.
func (u *UpdateNetworkInterfaceUseCase) WithRegistrar(r fgaregister.Registrar) *UpdateNetworkInterfaceUseCase {
	u.registrar = r
	return u
}

// Execute — sync-валидация и запуск Update в worker'е.
func (u *UpdateNetworkInterfaceUseCase) Execute(ctx context.Context, in UpdateInput) (*operations.Operation, error) {
	if err := niResourceID(in.NetworkInterfaceID); err != nil {
		return nil, err
	}
	if in.NetworkInterfaceID == "" {
		return nil, status.Error(codes.InvalidArgument, "network_interface_id required")
	}
	known := map[string]struct{}{
		"name": {}, "description": {}, "labels": {},
		"security_group_ids": {}, "v4_address_ids": {}, "v6_address_ids": {},
		"bandwidth_limit_mbps": {},
	}
	if err := corevalidate.UpdateMask("update_mask", in.UpdateMask, known); err != nil {
		return nil, err
	}
	// Решение об имени принимает ЕДИНСТВЕННАЯ функция дерева: она читает форму
	// запроса (маска × значение) и отвечает сразу на два вопроса — законен ли
	// ввод и следует ли имя применять. Ту же функцию зовёт применение маски,
	// поэтому проверка и запись разойтись не могут.
	//
	// Пять исходов и их причина — в godoc `validate.NameOnUpdate`. Коротко о том,
	// что здесь неочевидно: при ПУСТОЙ маске пустое имя законно и означает «не
	// прислано» — в proto3 это неотличимо от отсутствия поля.
	if _, err := corevalidate.NameOnUpdate("name", in.UpdateMask, string(in.NetworkInterface.Name)); err != nil {
		return nil, err
	}
	// Domain self-validation: name/description/labels через newtype.Validate() —
	// для полей, которые клиент мог прислать; mask-aware применение — в worker'е.
	if err := serviceerr.FromValidation(in.NetworkInterface.Validate()); err != nil {
		return nil, err
	}
	if err := validateNICAddressCardinality(in.NetworkInterface.V4AddressIDs, in.NetworkInterface.V6AddressIDs); err != nil {
		return nil, err
	}
	// Форма присланных ссылок на адреса — синхронно, как в Create. Проверяется то,
	// что прислал вызывающий, независимо от маски — тем же порядком, каким это уже
	// делает проверка кардинальности строкой выше: негодный по форме идентификатор
	// не станет годным ни в одной маске.
	if err := validateNICAddressRefIDs(in.NetworkInterface.V4AddressIDs, in.NetworkInterface.V6AddressIDs); err != nil {
		return nil, err
	}
	// Потолок числа групп — синхронно (см. Create).
	if err := validateNICSecurityGroupCardinality(in.NetworkInterface.SecurityGroupIDs); err != nil {
		return nil, err
	}
	// Ограничение полосы — синхронно, тем же правилом, что и на создании.
	// Проверяется присланное значение независимо от маски — тем же порядком, каким
	// это уже делают проверки выше: негодная величина не станет годной ни в одной
	// маске, а на пустой маске (full-object PATCH) она применяется в любом случае.
	if err := validateNICBandwidthLimit(u.bandwidth, in.NetworkInterface.BandwidthLimitMbps); err != nil {
		return nil, err
	}

	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Update network interface %s", in.NetworkInterfaceID),
		&vpcv1.UpdateNetworkInterfaceMetadata{NetworkInterfaceId: in.NetworkInterfaceID},
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

// doUpdate — async-тело Update. Detach убранных + attach добавленных address-refs
// + UpdateMeta + outbox-emit — все в ОДНОЙ writer-TX, иначе при сбое UpdateMeta
// address_references остались бы рассинхронизированы с метаданными NIC. На любой
// ошибке `defer w.Abort()` откатывает весь diff — компенсация не нужна.
func (u *UpdateNetworkInterfaceUseCase) doUpdate(ctx context.Context, in UpdateInput) (*anypb.Any, error) {
	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer w.Abort()

	// GetForUpdate: row-lock (`FOR UPDATE`) сериализует read-modify-write mutable-
	// колонок NIC — конкурентный disjoint-mask Update не может затереть un-masked
	// поле (напр. security_group_ids). address-ref side ниже уже защищён
	// SetReference-CAS; голый Get здесь был бы TOCTOU-race (project-rule #10).
	rec, err := w.NetworkInterfaces().GetForUpdate(ctx, in.NetworkInterfaceID)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	// address-refs diff — в той же writer-TX (`w.Addresses()`).
	ar := w.Addresses()
	newV4 := nicMaskV4(rec, in)
	newV6 := nicMaskV6(rec, in)
	if !strSetEqual(rec.V4AddressIDs, newV4) || !strSetEqual(rec.V6AddressIDs, newV6) {
		oldAll := append(append([]string{}, rec.V4AddressIDs...), rec.V6AddressIDs...)
		newAll := strSet(append(append([]string{}, newV4...), newV6...))
		var removed []string
		for _, id := range oldAll {
			if !newAll[id] {
				removed = append(removed, id)
			}
		}
		if derr := detachNICAddresses(ctx, ar, removed); derr != nil {
			return nil, derr
		}
		oldAllSet := strSet(oldAll)
		var addedV4, addedV6 []string
		for _, id := range newV4 {
			if !oldAllSet[id] {
				addedV4 = append(addedV4, id)
			}
		}
		for _, id := range newV6 {
			if !oldAllSet[id] {
				addedV6 = append(addedV6, id)
			}
		}
		// Проект берётся у СТРОКИ интерфейса (`rec`), а не из присланного тела:
		// project_id интерфейса immutable, и вызывающий его на этом пути не задаёт —
		// принимать его из тела значило бы отдать выбор проверяемого проекта тому,
		// кого проверяют.
		if err := attachNICAddresses(ctx, ar, rec.ID, derefName(in, rec), rec.ProjectID, rec.SubnetID, addedV4, addedV6); err != nil {
			return nil, err
		}
	}
	nic := &rec.NetworkInterface
	prevSGs := append([]string{}, nic.SecurityGroupIDs...)
	applyNICMask(nic, in)
	// Группы безопасности проверяются ПОСЛЕ применения маски — проверяется
	// итоговый набор, а не присланный кусок (пустая маска = полная замена).
	// Читаем в той же writer-TX, поэтому проверенный набор не может разъехаться
	// с записываемым. Набор не изменился — читать нечего.
	if !strSetEqual(prevSGs, nic.SecurityGroupIDs) {
		parentSub, serr := w.Subnets().Get(ctx, nic.SubnetID)
		if serr != nil {
			return nil, serviceerr.MapRepoErr(serr)
		}
		if sgErr := validateNICSecurityGroupRefs(ctx, w.SecurityGroups(), nic.SecurityGroupIDs,
			string(nic.ProjectID), parentSub.NetworkID); sgErr != nil {
			return nil, sgErr
		}
	}
	updated, err := w.NetworkInterfaces().UpdateMeta(ctx, nic)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	if oerr := w.Outbox().Emit(ctx, "NetworkInterface", updated.ID, updated.ProjectID, "UPDATED", helpers.DomainToMap(updated)); oerr != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, oerr))
	}
	// Если labels попали в update_mask (или это full-object PATCH), переэмитим
	// register-intent с обновленными метками в ТОЙ ЖЕ writer-TX, чтобы kaname
	// держал resource_mirror в актуальном виде для ARM_LABELS-селектора (revoke
	// при снятии метки). Update без labels → переэмита нет. Полное снятие labels →
	// upsert с пустыми метками (НЕ Unregister: NIC все еще существует). Эталон —
	// network/subnet/securitygroup update.
	var syncItems []fgaregister.Item
	var intentVersion time.Time
	if labelsInMask(in.UpdateMask) {
		syncItems = []fgaregister.Item{
			fgaregister.ProjectHierarchyItem(string(updated.ProjectID), "vpc_network_interface", updated.ID,
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
	fgaregister.DeliverAfterCommit(ctx, u.registrar, syncItems, intentVersion, "NetworkInterface", updated.ID)
	return marshalNetworkInterfaceRecord(updated)
}

// labelsInMask — затрагивает ли update_mask поле `labels`: пустая маска значит
// full-object PATCH (labels применяются), явная маска матчится, если содержит
// "labels". Управляет переэмитом register-intent — держать в синхроне с
// full-PATCH-набором полей в applyNICMask.
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

// derefName — name to apply: либо из mask (если включен), либо текущее имя.
func derefName(in UpdateInput, rec *kachorepo.NetworkInterfaceRecord) string {
	if len(in.UpdateMask) == 0 {
		return string(in.NetworkInterface.Name)
	}
	for _, f := range in.UpdateMask {
		if f == "name" {
			return string(in.NetworkInterface.Name)
		}
	}
	return string(rec.Name)
}

// nicMaskV4 — какой набор v4_address_ids применять (новый или текущий).
func nicMaskV4(rec *kachorepo.NetworkInterfaceRecord, in UpdateInput) []string {
	if len(in.UpdateMask) == 0 {
		return in.NetworkInterface.V4AddressIDs
	}
	for _, f := range in.UpdateMask {
		if f == "v4_address_ids" {
			return in.NetworkInterface.V4AddressIDs
		}
	}
	return rec.V4AddressIDs
}

// nicMaskV6 — какой набор v6_address_ids применять.
func nicMaskV6(rec *kachorepo.NetworkInterfaceRecord, in UpdateInput) []string {
	if len(in.UpdateMask) == 0 {
		return in.NetworkInterface.V6AddressIDs
	}
	for _, f := range in.UpdateMask {
		if f == "v6_address_ids" {
			return in.NetworkInterface.V6AddressIDs
		}
	}
	return rec.V6AddressIDs
}

// strSet / strSetEqual — мини-helper'ы для diff-логики address-refs.
func strSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

func strSetEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sa, sb := strSet(a), strSet(b)
	for k := range sa {
		if !sb[k] {
			return false
		}
	}
	return true
}

// applyNICMask — применяет subset полей UpdateInput к существующему domain.NIC.
// Пустой mask = full-PATCH (применяются все mutable-поля).
func applyNICMask(n *domain.NetworkInterface, in UpdateInput) {
	src := in.NetworkInterface
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
	applyName, _ := corevalidate.NameOnUpdate("name", in.UpdateMask, string(src.Name))
	if applyName {
		n.Name = src.Name
	}
	if len(in.UpdateMask) == 0 {
		n.Description = src.Description
		n.Labels = src.Labels
		n.SecurityGroupIDs = src.SecurityGroupIDs
		n.V4AddressIDs, n.V6AddressIDs = src.V4AddressIDs, src.V6AddressIDs
		n.BandwidthLimitMbps = src.BandwidthLimitMbps
		return
	}
	for _, f := range in.UpdateMask {
		switch f {
		case "description":
			n.Description = src.Description
		case "labels":
			n.Labels = src.Labels
		case "security_group_ids":
			n.SecurityGroupIDs = src.SecurityGroupIDs
		case "v4_address_ids":
			n.V4AddressIDs = src.V4AddressIDs
		case "v6_address_ids":
			n.V6AddressIDs = src.V6AddressIDs
		case "bandwidth_limit_mbps":
			n.BandwidthLimitMbps = src.BandwidthLimitMbps
		}
	}
}
