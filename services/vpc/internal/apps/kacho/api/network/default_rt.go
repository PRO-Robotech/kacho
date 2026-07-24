// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package network

import (
	"context"
	"fmt"

	"github.com/PRO-Robotech/kacho/pkg/ids"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// CreateDefaultRTUseCase — inline-провижн системной default-RouteTable при
// Network.Create (VPC-1 F3 / VPC-1-11). Точная симметрия CreateDefaultSGUseCase:
// работает в УЖЕ открытой writer-TX caller'а (`CreateNetworkUseCase.doCreate`),
// сам TX не открывает и не коммитит — поэтому Network + RT + оба outbox-события
// видны либо все вместе (Commit), либо никак (Abort/crash), без orphan-окна.
//
// Зачем ресурс, а не «просто колонка»: `Network.defaultRouteTableId°` —
// ЕДИНСТВЕННЫЙ источник истины «какая RT дефолтная для сети», к которому
// детерминированно привязываются новые подсети (Subnet.Create auto-assoc).
// Он замещает недетерминированный legacy-выбор «самая ранняя RT сети»
// (триггер subnet_auto_pick_rt, снят миграцией 0017).
//
// Stateless — конструктор сохранён для parity с остальными use-case'ами.
type CreateDefaultRTUseCase struct{}

// NewCreateDefaultRTUseCase создаёт stateless CreateDefaultRTUseCase.
func NewCreateDefaultRTUseCase() *CreateDefaultRTUseCase {
	return &CreateDefaultRTUseCase{}
}

// Execute создаёт default-RouteTable для только что вставленной Network и
// проставляет её id в `Network.default_route_table_id`. Все DML и outbox-emit
// идут через переданный writer-TX caller'а.
//
// Возвращает updated NetworkRecord с заполненным `default_route_table_id`; на
// любой ошибке — уже обёрнутую gRPC-ошибку (caller пробрасывает её наверх,
// worker превращает в Operation.error).
func (u *CreateDefaultRTUseCase) Execute(
	ctx context.Context,
	w Writer,
	network domain.Network,
) (*kachorepo.NetworkRecord, error) {
	rt := domain.NewDefaultRouteTable(ids.NewID(ids.PrefixRouteTable), network)
	rtRec, err := w.RouteTables().Insert(ctx, &rt)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	if err := w.Outbox().Emit(ctx, "RouteTable", rtRec.ID, "CREATED", helpers.DomainToMap(rtRec)); err != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, err))
	}
	upd, err := w.Networks().SetDefaultRouteTableID(ctx, network.ID, rtRec.ID)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	if err := w.Outbox().Emit(ctx, "Network", upd.ID, "UPDATED", helpers.DomainToMap(upd)); err != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, err))
	}
	return upd, nil
}
