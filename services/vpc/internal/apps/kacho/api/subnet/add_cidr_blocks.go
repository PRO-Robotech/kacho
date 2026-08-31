// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
)

// AddCidrBlocksUseCase — атомарное добавление CIDR-блоков к подсети.
// Возвращает Operation; внутри worker'а:
//   - Get subnet (FOR UPDATE) → если не найден → NotFound.
//   - Validate каждого CIDR (host-bits=0).
//   - Get parent Network → каждый добавляемый блок ⊆ супернета сети (F7/VPC-1-34);
//     блок вне супернета → InvalidArgument.
//   - Проверка overlap внутри новой объединенной коллекции (v4 + v6).
//   - SetCidrBlocks (DB UPDATE) — внутри него child-таблица subnet_cidr_blocks
//     пересобирается и ее EXCLUDE gist ловит пересечение ЛЮБОГО блока (primary и
//     вторичного) с блоками других подсетей той же сети.
//
// Cross-subnet non-overlap гарантируется только на DB-уровне (network-scoped
// EXCLUDE): GetForUpdate сериализует лишь операции над ОДНОЙ подсетью, поэтому
// пересечение блоков РАЗНЫХ подсетей одной сети ловит declarative-инвариант,
// а не software-проверка (она была бы TOCTOU-prone между подсетями).
//
// Get + SetCidrBlocks + outbox-emit UPDATED атомарны в одной writer-TX.
type AddCidrBlocksUseCase struct {
	repo     Repo
	opsRepo  operations.Repo
	reserved domain.ReservedPrefixes
}

// NewAddCidrBlocksUseCase создает AddCidrBlocksUseCase.
func NewAddCidrBlocksUseCase(r Repo, opsRepo operations.Repo) *AddCidrBlocksUseCase {
	return &AddCidrBlocksUseCase{repo: r, opsRepo: opsRepo}
}

// WithReservedPrefixes подключает перечень адресных диапазонов, которые платформа
// держит за собой.
//
// Этот глагол несёт ту же проверку, что `Create`, и по той же причине: он —
// ВТОРОЕ и последнее место, где диапазон подсети объявляется. Закрыв только
// создание, мы оставили бы обход в один запрос: создать подсеть законным блоком и
// добавить служебный вторым вызовом. Обоснование нулевого значения и то, чем
// держится провязка, — в `CreateSubnetUseCase.WithReservedPrefixes`.
func (u *AddCidrBlocksUseCase) WithReservedPrefixes(r domain.ReservedPrefixes) *AddCidrBlocksUseCase {
	u.reserved = r
	return u
}

// Execute — sync-валидация id/CIDR-формата + Operation + async-merge в worker'е.
func (u *AddCidrBlocksUseCase) Execute(ctx context.Context, id string, v4, v6 []string) (*operations.Operation, error) {
	if err := corevalidate.ResourceID("subnet", ids.PrefixSubnet, id); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "subnet_id required")
	}
	if len(v4) == 0 && len(v6) == 0 {
		return nil, serviceerr.InvalidArg(blocksCidrFields.v4,
			blocksCidrFields.v4+" or "+blocksCidrFields.v6+" is required")
	}
	// Потолок — ПЕРВЫМ, до поэлементной валидации и до квадратичной проверки
	// пересечений: стоимость запроса не может задаваться вызывающим.
	if err := validateSubnetCidrCardinality(blocksCidrFields.v4, v4); err != nil {
		return nil, err
	}
	if err := validateSubnetCidrCardinality(blocksCidrFields.v6, v6); err != nil {
		return nil, err
	}
	for i, c := range v4 {
		if err := validateSubnetV4CIDR(blocksCidrFields.V4Slot(i), c); err != nil {
			return nil, err
		}
	}
	for i, c := range v6 {
		if err := validateSubnetV6CIDR(blocksCidrFields.V6Slot(i), c); err != nil {
			return nil, err
		}
	}
	// Disjointness внутри переданного v6-списка (sync; mirror v4 — для v4 это
	// проверяется ниже на merged-наборе, что покрывает и intra-request).
	if err := checkCIDRDisjoint(blocksCidrFields.v6, v6); err != nil {
		return nil, err
	}
	// Служебное адресное пространство платформы (`dataplane.reserved-prefixes`) —
	// синхронно, до создания операции. Проверяется ПРИСЛАННЫЙ набор, а не
	// накопленный: уже записанные блоки этот запрос не объявляет, а отказ по ним
	// заблокировал бы расширение подсети, созданной до объявления перечня.
	if err := validateSubnetNotReserved(blocksCidrFields, u.reserved, v4, v6); err != nil {
		return nil, err
	}

	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Add CIDR blocks to subnet %s", id),
		&vpcv1.UpdateSubnetMetadata{SubnetId: id},
	)
	if err != nil {
		return nil, err
	}
	if err := u.opsRepo.Create(ctx, op); err != nil {
		return nil, err
	}
	if err := operations.RunSync(ctx, u.opsRepo, &op, func(ctx context.Context) (*anypb.Any, error) {
		w, werr := u.repo.Writer(ctx)
		if werr != nil {
			return nil, serviceerr.MapRepoErr(werr)
		}
		defer w.Abort()

		// Порядок захвата локов — ЕДИНЫЙ глобальный network → subnet (тот же, что
		// у Subnet.Create и Network.Delete), поэтому родителя резолвим plain-read'ом
		// (network_id подсети immutable) и лочим сеть ДО подсети. Обратный порядок
		// (subnet → network) дал бы inversion с Network.Delete (lock networks-строки
		// → FK RESTRICT key-share по subnets) и вырождался бы в 40P01.
		sub0, gerr := w.Subnets().Get(ctx, id)
		if gerr != nil {
			return nil, serviceerr.MapRepoErr(gerr)
		}
		// F7 (VPC-1-34): добавляемый диапазон обязан лежать ВНУТРИ объявленного
		// супернета родительской сети (within-service, та же БД). FOR SHARE (не
		// plain Get): супернет параллельно переписывает Network.Add/RemoveCidrBlocks
		// под `FOR UPDATE`, и без конфликтующего лока обе стороны решали бы по своим
		// снимкам — блок коммитился вне итогового супернета (ban #10). Требование
		// безусловно, как и в Subnet.Create: сеть, не объявившая супернет этого
		// семейства, диапазон семейства не принимает — расширять нечего. Блок вне
		// супернета → InvalidArgument "subnet CIDR <X> is not within any network CIDR block".
		parentNet, nerr := w.Networks().GetForShare(ctx, sub0.NetworkID)
		if nerr != nil {
			return nil, serviceerr.MapRepoErr(nerr)
		}
		// FOR UPDATE: сериализует конкурентные Add/RemoveCidrBlocks на этой
		// подсети — закрывает lost-update. Берётся ПОСЛЕ network-лока (см. выше).
		sub, gerr := w.Subnets().GetForUpdate(ctx, id)
		if gerr != nil {
			return nil, serviceerr.MapRepoErr(gerr)
		}
		if verr := validateSubnetWithinSupernet(parentNet.IPv4CidrBlocks, parentNet.IPv6CidrBlocks, v4, v6); verr != nil {
			return nil, verr
		}
		mergedV4 := append([]string{}, sub.V4CidrBlocks...)
		mergedV4 = append(mergedV4, v4...)
		// Потолок на НАКОПЛЕННОМ наборе (под row-lock подсети): аддитивный
		// глагол иначе растит набор серией законных запросов.
		if err := validateSubnetCidrCardinality(blocksCidrFields.v4, mergedV4); err != nil {
			return nil, err
		}
		// Проверка пересечений внутри объединенного набора (sync, host-bits уже OK).
		// Покрывает overlap нового блока с уже существующим в этой же подсети.
		if err := checkCIDRDisjoint(blocksCidrFields.v4, mergedV4); err != nil {
			return nil, err
		}
		// v6: то же самое.
		mergedV6 := append([]string{}, sub.V6CidrBlocks...)
		mergedV6 = appendDedup(mergedV6, v6)
		if err := validateSubnetCidrCardinality(blocksCidrFields.v6, mergedV6); err != nil {
			return nil, err
		}
		if err := checkCIDRDisjoint(blocksCidrFields.v6, mergedV6); err != nil {
			return nil, err
		}
		updated, uerr := w.Subnets().SetCidrBlocks(ctx, id, mergedV4, mergedV6)
		if uerr != nil {
			return nil, serviceerr.MapRepoErr(uerr)
		}
		if err := w.Outbox().Emit(ctx, "Subnet", updated.ID, updated.ProjectID, "UPDATED", helpers.DomainToMap(updated)); err != nil {
			return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, err))
		}
		if err := w.Commit(); err != nil {
			return nil, serviceerr.MapRepoErr(err)
		}
		return marshalSubnetRecord(updated)
	}); err != nil {
		return nil, err
	}
	return &op, nil
}
