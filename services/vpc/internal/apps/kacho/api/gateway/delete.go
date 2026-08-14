// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package gateway

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/fgaregister"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
)

// DeleteGatewayUseCase — async-delete; sync-проверка ID, async — repo.Delete +
// outbox emit Gateway.DELETED в той же writer-TX (атомарно).
type DeleteGatewayUseCase struct {
	repo    Repo
	opsRepo operations.Repo
}

// NewDeleteGatewayUseCase создает DeleteGatewayUseCase.
func NewDeleteGatewayUseCase(r Repo, opsRepo operations.Repo) *DeleteGatewayUseCase {
	return &DeleteGatewayUseCase{repo: r, opsRepo: opsRepo}
}

// Execute инициирует Delete: sync-проверки → Operation → worker.
func (u *DeleteGatewayUseCase) Execute(ctx context.Context, id string) (*operations.Operation, error) {
	if err := corevalidate.ResourceID("gateway", ids.PrefixGateway, id); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "gateway_id required")
	}
	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Delete gateway %s", id),
		&vpcv1.DeleteGatewayMetadata{GatewayId: id},
	)
	if err != nil {
		return nil, err
	}
	if err := u.opsRepo.Create(ctx, op); err != nil {
		return nil, err
	}

	return u.runWorker(ctx, op, id)
}

// releaseGatewayAddress — возврат адреса шлюза и его аренды в пул, внутри уже
// открытой writer-TX вызывающего.
//
// Адрес снимается ЦЕЛИКОМ, а не только отвязывается: он был заказан шлюзом
// (`owned`), то есть его жизнь связана со шлюзом. Оставить его отвязанным
// значило бы копить в проекте адреса, которых никто не заказывал и за которые
// платит арендатор.
//
// Отсутствие адреса терпимо на КАЖДОМ шаге и не является ошибкой: та же
// транзакция могла быть повторена воркером операции, а повторный возврат аренды
// идемпотентен (`INSERT … ON CONFLICT DO NOTHING`). Тем самым путь безопасен к
// повтору, а не только к первому исполнению.
func releaseGatewayAddress(ctx context.Context, w Writer, addressID string) error {
	if cerr := w.Addresses().ClearReference(ctx, addressID); cerr != nil && !errors.Is(cerr, repo.ErrNotFound) {
		return serviceerr.MapRepoErr(cerr)
	}
	deleted, derr := w.Addresses().DeleteGuarded(ctx, addressID)
	if derr != nil {
		if errors.Is(derr, repo.ErrNotFound) {
			return nil
		}
		return serviceerr.MapRepoErr(derr)
	}
	// Аренда вычисляется из УДАЛЁННОЙ строки, а не из прочитанной ранее: между
	// чтением шлюза и этим оператором адрес видит только база.
	if deleted.ExternalIpv4 != nil && deleted.ExternalIpv4.Address != "" && deleted.ExternalIpv4.AddressPoolID != "" {
		if rerr := w.Addresses().ReturnIPToFreelist(ctx,
			deleted.ExternalIpv4.AddressPoolID, deleted.ExternalIpv4.Address); rerr != nil {
			return serviceerr.MapRepoErr(fmt.Errorf("%w: return ip to freelist: %v", repo.ErrInternal, rerr))
		}
	}
	if oerr := w.Outbox().Emit(ctx, "Address", addressID, "DELETED", map[string]any{"id": addressID}); oerr != nil {
		return serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, oerr))
	}
	return nil
}

func (u *DeleteGatewayUseCase) runWorker(ctx context.Context, op operations.Operation, id string) (*operations.Operation, error) {
	operations.Run(ctx, u.opsRepo, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		w, werr := u.repo.Writer(ctx)
		if werr != nil {
			return nil, serviceerr.MapRepoErr(werr)
		}
		defer w.Abort()

		// Читаем projectID до удаления — он нужен как subject unregister-tuple'а.
		// Оттуда же берём внешний адрес: после DELETE его будет негде прочитать,
		// а аренду надо вернуть в пул.
		var unreg []fgaregister.Tuple
		var externalAddressID, addressProject string
		if cur, gerr := w.Gateways().Get(ctx, id); gerr == nil {
			unreg = append(unreg, fgaregister.ProjectHierarchy(cur.ProjectID, "vpc_gateway", id))
			externalAddressID, addressProject = cur.ExternalAddressID, cur.ProjectID
		}

		if derr := w.Gateways().Delete(ctx, id); derr != nil {
			return nil, serviceerr.MapRepoErr(derr)
		}

		// Аренда возвращается в пул В ЭТОЙ ЖЕ транзакции, что и снятие шлюза.
		// Порядок обязателен и вытекает из двух ограничений базы: строка шлюза
		// ссылается на адрес внешним ключом с ON DELETE RESTRICT (сначала шлюз),
		// а охраняемое удаление адреса отвергает занятый адрес (сначала снять
		// ссылку). Пул без возврата аренды исчерпывается — правило B17 выведено
		// из живого исчерпания под параллельным прогоном, а не из опасения.
		if externalAddressID != "" {
			if rerr := releaseGatewayAddress(ctx, w, externalAddressID); rerr != nil {
				return nil, rerr
			}
			unreg = append(unreg, fgaregister.ProjectHierarchy(addressProject, "vpc_address", externalAddressID))
		}
		if oerr := w.Outbox().Emit(ctx, "Gateway", id, "DELETED", map[string]any{"id": id}); oerr != nil {
			return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, oerr))
		}
		if len(unreg) > 0 {
			if rerr := w.FGARegister().EmitUnregister(ctx, fgaregister.RegisterIntent(unreg...)); rerr != nil {
				return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: fga unregister intent: %v", repo.ErrInternal, rerr))
			}
		}
		if cerr := w.Commit(); cerr != nil {
			return nil, serviceerr.MapRepoErr(cerr)
		}
		// Ответ Delete — google.protobuf.Empty.
		return anypb.New(&emptypb.Empty{})
	})

	return &op, nil
}
