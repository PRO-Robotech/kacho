// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package addresspool

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// AsyncMutations — конвенционная оболочка `Operation` вокруг ТЕХ ЖЕ use-case'ов,
// которыми пользуется внутренний сервис (под-фаза ADM-1 S1, запрет 9).
//
// # Почему оболочка, а не второй набор use-case'ов
//
// Окно расширения открывает два пути записи в ОДНУ таблицу. Расхождение между
// ними — разная валидация, разные умолчания, разный набор заполняемых полей — не
// видно ни одному сценарию, который смотрит на один путь; наблюдаемым оно
// становится ровно тогда, когда консоль уже переведена на публичный путь, а
// оператор ещё пользуется внутренним. Поэтому расхождение сделано НЕВЫРАЗИМЫМ:
// применение здесь не дублируется, а вызывается — то же тело, те же проверки,
// тот же порядок.
//
// # Почему RunSync, а не фоновый исполнитель
//
// Мутация пула durable после ОДНОЙ writer-TX: применять нечего, ждать нечего,
// компенсировать нечего. `RunSync` исполняет тело в том же запросе и отдаёт уже
// терминальную операцию (`done=true` с телом в `response`) — «операция в
// ответе», как у сети и подсети этого же сервиса. Фоновый исполнитель добавил бы
// вызывающему круг опроса, ничего не купив.
//
// # Что здесь СИНХРОННО, а что внутри операции
//
// Синхронно — всё, что решается без обращения к своей БД: форма идентификатора,
// дисциплина маски правки, форма CIDR-блоков, существование чужой зоны у её
// владельца. Такой отказ обязан приехать кодом отказа, а не `200` с отказом
// внутри операции: «принято» на том, что не принято, вызывающий записывает в
// своё состояние как успех.
type AsyncMutations struct {
	opsRepo    operations.Repo
	create     *CreateAddressPoolUseCase
	update     *UpdateAddressPoolUseCase
	deleteUC   *DeleteAddressPoolUseCase
	bindNet    *BindAsNetworkDefaultUseCase
	unbindNet  *UnbindNetworkDefaultUseCase
	addCidr    *AddCidrBlocksUseCase
	removeCidr *RemoveCidrBlocksUseCase
}

// NewAsyncMutations собирает оболочку поверх уже собранных use-case'ов.
// Composition root передаёт СЮДА ТЕ ЖЕ указатели, что и во внутренний handler, —
// иначе «то же тело» держалось бы на совпадении, а не на построении.
func NewAsyncMutations(
	opsRepo operations.Repo,
	create *CreateAddressPoolUseCase,
	update *UpdateAddressPoolUseCase,
	deleteUC *DeleteAddressPoolUseCase,
	bindNet *BindAsNetworkDefaultUseCase,
	unbindNet *UnbindNetworkDefaultUseCase,
	addCidr *AddCidrBlocksUseCase,
	removeCidr *RemoveCidrBlocksUseCase,
) *AsyncMutations {
	return &AsyncMutations{
		opsRepo:    opsRepo,
		create:     create,
		update:     update,
		deleteUC:   deleteUC,
		bindNet:    bindNet,
		unbindNet:  unbindNet,
		addCidr:    addCidr,
		removeCidr: removeCidr,
	}
}

const (
	// poolKind / poolDisplay — тип ресурса в машинной детали отказа и его имя в
	// контрактном тоне. Названы один раз: два литерала об одном предмете
	// разъезжаются молча.
	poolKind    = "vpc.address_pool"
	poolDisplay = "AddressPool"

	networkKind    = "vpc.network"
	networkDisplay = "Network"
)

// ValidatePoolID — полоса sync-формата для СВОЕГО идентификатора пула.
//
// Две проверки, и обе обязательны. `corevalidate.ResourceID` по контракту
// ПРОПУСКАЕТ пустую строку — обязательность поля это отдельная ответственность
// вызывающего; без своей проверки пустая строка уехала бы в репозиторий и
// вернулась контрактным тоном отсутствия с вырезанным идентификатором
// («AddressPool  not found»), то есть утверждением об отсутствии ресурса,
// которого вызывающий не называл.
//
// Проверка family-agnostic по контракту самой `ResourceID`: чужой по семейству,
// но платформенный префикс проходит сюда и отвергается уже отсутствием строки —
// это «не нашёл», а не «исправь ввод», и полоса у него другая.
func ValidatePoolID(id string) error {
	if id == "" {
		return serviceerr.InvalidArg("pool_id", "pool_id: required")
	}
	if err := corevalidate.ResourceID("address pool", ids.PrefixAddressPool, id); err != nil {
		return serviceerr.InvalidIDLane(poolKind, "address pool", id)
	}
	return nil
}

// ValidateNetworkID — то же для идентификатора сети (глаголы привязки пула).
func ValidateNetworkID(id string) error {
	if id == "" {
		return serviceerr.InvalidArg("network_id", "network_id: required")
	}
	if err := corevalidate.ResourceID("network", ids.PrefixNetwork, id); err != nil {
		return serviceerr.InvalidIDLane(networkKind, "network", id)
	}
	return nil
}

// MapPublicErr — отображение отказа на ПУБЛИЧНОЙ поверхности.
//
// Отличается от внутреннего ровно одним: промах прямого чтения получает
// контрактный тон `"<Resource> <id> not found"` и машинный признак полосы.
// Общий классификатор отдаёт здесь голый текст sentinel'а — идентификатор ему
// неизвестен, а вызывающему он известен.
//
// Внутренний путь свой прежний тон сохраняет: его читает служебная оснастка, а
// не арендатор, контрактом он никогда не был, и снимается он вместе с самим
// внутренним глаголом (стадия S3). Расхождение тона состоянием не является —
// негодный по форме идентификатор не совпадает со строкой ни на одном из путей.
func MapPublicErr(err error, resourceKind, display, id string) error {
	if err == nil {
		return nil
	}
	if isNotFound(err) {
		return serviceerr.NotFoundLane(resourceKind, display, id)
	}
	return serviceerr.MapRepoErrLeakSafe(err, "address pool error")
}

// isNotFound — промах прямого чтения в своей БД, каким его отдаёт репозиторий.
func isNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

// marshalPoolRecord — тело успеха операции: та же проекция, что отдаёт
// синхронное чтение. Одна проекция на оба пути — иначе публичный `Get` и
// `response` операции разошлись бы по составу полей, и вызывающий, обновляющий
// состояние из ответа мутации, увидел бы не тот ресурс, что при следующем
// чтении.
func marshalPoolRecord(rec *kachorepo.AddressPoolRecord) (*anypb.Any, error) {
	out, err := anypb.New(poolToProto(rec))
	if err != nil {
		return nil, fmt.Errorf("marshal AddressPool: %w", err)
	}
	return out, nil
}

// marshalEmpty — тело успеха для глаголов, у которых предмет ответа исчерпан
// самим фактом применения (удаление, снятие привязки).
func marshalEmpty() (*anypb.Any, error) {
	out, err := anypb.New(&emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("marshal Empty: %w", err)
	}
	return out, nil
}

// begin — заведение операции. Отдельной функцией, потому что повторяется семь
// раз и отличается только описанием и метаданными.
func (a *AsyncMutations) begin(ctx context.Context, description string, metadata proto.Message) (operations.Operation, error) {
	op, err := operations.NewFromContext(ctx, ids.PrefixOperationVPC, description, metadata)
	if err != nil {
		return operations.Operation{}, err
	}
	if err := a.opsRepo.Create(ctx, op); err != nil {
		return operations.Operation{}, err
	}
	return op, nil
}

// Create — проверка входа синхронно (включая существование зоны у её владельца),
// затем применение внутри операции. Идентификатор пула чеканится проверкой и
// уезжает в метаданные ДО применения.
func (a *AsyncMutations) Create(ctx context.Context, req CreatePoolReq) (*operations.Operation, error) {
	p, err := a.create.Validate(ctx, req)
	if err != nil {
		return nil, err
	}
	op, err := a.begin(ctx, fmt.Sprintf("Create address pool %s", p.ID),
		&vpcv1.CreateAddressPoolMetadata{AddressPoolId: p.ID})
	if err != nil {
		return nil, err
	}
	if err := operations.RunSync(ctx, a.opsRepo, &op, func(ctx context.Context) (*anypb.Any, error) {
		rec, perr := a.create.Persist(ctx, p)
		if perr != nil {
			return nil, MapPublicErr(perr, poolKind, poolDisplay, p.ID)
		}
		return marshalPoolRecord(rec)
	}); err != nil {
		return nil, err
	}
	return &op, nil
}

// Update — дисциплина маски синхронно, применение внутри операции.
func (a *AsyncMutations) Update(ctx context.Context, req UpdatePoolReq) (*operations.Operation, error) {
	if err := ValidatePoolID(req.ID); err != nil {
		return nil, err
	}
	if err := a.update.Validate(req); err != nil {
		return nil, err
	}
	op, err := a.begin(ctx, fmt.Sprintf("Update address pool %s", req.ID),
		&vpcv1.UpdateAddressPoolMetadata{AddressPoolId: req.ID})
	if err != nil {
		return nil, err
	}
	if err := operations.RunSync(ctx, a.opsRepo, &op, func(ctx context.Context) (*anypb.Any, error) {
		rec, uerr := a.update.Execute(ctx, req)
		if uerr != nil {
			return nil, MapPublicErr(uerr, poolKind, poolDisplay, req.ID)
		}
		return marshalPoolRecord(rec)
	}); err != nil {
		return nil, err
	}
	return &op, nil
}

// Delete — форма идентификатора синхронно, применение внутри операции.
func (a *AsyncMutations) Delete(ctx context.Context, id string) (*operations.Operation, error) {
	if err := ValidatePoolID(id); err != nil {
		return nil, err
	}
	op, err := a.begin(ctx, fmt.Sprintf("Delete address pool %s", id),
		&vpcv1.DeleteAddressPoolMetadata{AddressPoolId: id})
	if err != nil {
		return nil, err
	}
	if err := operations.RunSync(ctx, a.opsRepo, &op, func(ctx context.Context) (*anypb.Any, error) {
		if derr := a.deleteUC.Execute(ctx, id); derr != nil {
			return nil, MapPublicErr(derr, poolKind, poolDisplay, id)
		}
		return marshalEmpty()
	}); err != nil {
		return nil, err
	}
	return &op, nil
}

// AddCidrBlocks — форма идентификатора и блоков синхронно.
func (a *AsyncMutations) AddCidrBlocks(ctx context.Context, id string, v4, v6 []string) (*operations.Operation, error) {
	if err := ValidatePoolID(id); err != nil {
		return nil, err
	}
	if err := a.addCidr.Validate(id, v4, v6); err != nil {
		return nil, err
	}
	op, err := a.begin(ctx, fmt.Sprintf("Add CIDR blocks to address pool %s", id),
		&vpcv1.UpdateAddressPoolMetadata{AddressPoolId: id})
	if err != nil {
		return nil, err
	}
	if err := operations.RunSync(ctx, a.opsRepo, &op, func(ctx context.Context) (*anypb.Any, error) {
		rec, aerr := a.addCidr.Execute(ctx, id, v4, v6)
		if aerr != nil {
			return nil, MapPublicErr(aerr, poolKind, poolDisplay, id)
		}
		return marshalPoolRecord(rec)
	}); err != nil {
		return nil, err
	}
	return &op, nil
}

// RemoveCidrBlocks — форма идентификатора и непустота набора синхронно.
func (a *AsyncMutations) RemoveCidrBlocks(ctx context.Context, id string, v4, v6 []string) (*operations.Operation, error) {
	if err := ValidatePoolID(id); err != nil {
		return nil, err
	}
	if err := a.removeCidr.Validate(id, v4, v6); err != nil {
		return nil, err
	}
	op, err := a.begin(ctx, fmt.Sprintf("Remove CIDR blocks from address pool %s", id),
		&vpcv1.UpdateAddressPoolMetadata{AddressPoolId: id})
	if err != nil {
		return nil, err
	}
	if err := operations.RunSync(ctx, a.opsRepo, &op, func(ctx context.Context) (*anypb.Any, error) {
		rec, rerr := a.removeCidr.Execute(ctx, id, v4, v6)
		if rerr != nil {
			return nil, MapPublicErr(rerr, poolKind, poolDisplay, id)
		}
		return marshalPoolRecord(rec)
	}); err != nil {
		return nil, err
	}
	return &op, nil
}

// BindAsNetworkDefault — обе ссылки проверяются по форме синхронно.
func (a *AsyncMutations) BindAsNetworkDefault(ctx context.Context, networkID, poolID string) (*operations.Operation, error) {
	if err := ValidateNetworkID(networkID); err != nil {
		return nil, err
	}
	if err := ValidatePoolID(poolID); err != nil {
		return nil, err
	}
	op, err := a.begin(ctx, fmt.Sprintf("Bind address pool %s as default for network %s", poolID, networkID),
		&vpcv1.AddressPoolBindingMetadata{NetworkId: networkID, AddressPoolId: poolID})
	if err != nil {
		return nil, err
	}
	if err := operations.RunSync(ctx, a.opsRepo, &op, func(ctx context.Context) (*anypb.Any, error) {
		if berr := a.bindNet.Execute(ctx, networkID, poolID); berr != nil {
			return nil, MapPublicErr(berr, networkKind, networkDisplay, networkID)
		}
		return marshalEmpty()
	}); err != nil {
		return nil, err
	}
	return &op, nil
}

// UnbindNetworkDefault — пул вызывающим не называется, поэтому в метаданных его
// поле остаётся ПУСТЫМ: это «не задано», а не «пул с пустым идентификатором».
func (a *AsyncMutations) UnbindNetworkDefault(ctx context.Context, networkID string) (*operations.Operation, error) {
	if err := ValidateNetworkID(networkID); err != nil {
		return nil, err
	}
	op, err := a.begin(ctx, fmt.Sprintf("Unbind default address pool from network %s", networkID),
		&vpcv1.AddressPoolBindingMetadata{NetworkId: networkID})
	if err != nil {
		return nil, err
	}
	if err := operations.RunSync(ctx, a.opsRepo, &op, func(ctx context.Context) (*anypb.Any, error) {
		if uerr := a.unbindNet.Execute(ctx, networkID); uerr != nil {
			return nil, MapPublicErr(uerr, networkKind, networkDisplay, networkID)
		}
		return marshalEmpty()
	}); err != nil {
		return nil, err
	}
	return &op, nil
}
