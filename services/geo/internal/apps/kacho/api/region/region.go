// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package region — use-case (бизнес-логика) каталога Region.
//
// Use-case слой чистой архитектуры: импортирует domain + порт RegionRepo +
// corelib operations, не тянет pgx/transport. Публичные RegionService.Get/List —
// read-only (sync). Admin CRUD (Create/Update/Delete) идет через
// InternalRegionService на :9091 и возвращает Operation (async LRO): мутация
// синхронно отдает operation.Operation (done=false), фоновый corelib-worker
// выполняет доменную запись и финализирует операцию (done=true, response=Region
// либо Empty для Delete, либо error=google.rpc.Status). Клиент поллит
// OperationService.Get(id) до done.
package region

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/validate"
	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"

	"github.com/PRO-Robotech/kacho/services/geo/internal/apps/kacho/shared/lro"
	"github.com/PRO-Robotech/kacho/services/geo/internal/domain"
	geoerrors "github.com/PRO-Robotech/kacho/services/geo/internal/errors"
	"github.com/PRO-Robotech/kacho/services/geo/internal/protoconv"
)

// Pagination — вход для List с cursor-пагинацией (page_size + opaque page_token).
type Pagination struct {
	PageSize  int64
	PageToken string
}

// Reader — read-порт справочника regions (Get/List). CQRS-разделён с Writer:
// read-side можно связать с read-replica pool независимо от writer-side.
type Reader interface {
	Get(ctx context.Context, id string) (*domain.Region, error)
	List(ctx context.Context, p Pagination) ([]*domain.Region, string, error)
}

// Writer — write-порт admin-мутаций regions (Insert/Update/Delete + outbox-emit в
// writer-tx). Отделён от Reader (write идёт на primary).
//
// Update — атомарный single-statement (UPDATE … RETURNING), без предварительного
// Get (исключен TOCTOU read-modify-write). name=nil → поле не меняется (COALESCE);
// 0 rows из RETURNING → ErrNotFound.
type Writer interface {
	Insert(ctx context.Context, r *domain.Region) (*domain.Region, error)
	Update(ctx context.Context, id string, name *string) (*domain.Region, error)
	Delete(ctx context.Context, id string) error
}

// Repo — композит Reader+Writer для adapter'а, реализующего обе стороны
// (pg.RegionRepo). Composition root связывает reader/writer раздельно.
type Repo interface {
	Reader
	Writer
}

// ErrToStatus маппит доменную/repo sentinel-ошибку в transport-status,
// сохраняемый async-worker'ом в Operation.error. Инжектится composition root'ом
// (serviceerr.ToStatus) — use-case не выбирает transport-коды сам: выбор кода —
// transport-concern, он остаётся во владении handler/transport-слоя. Пустой (nil)
// mapper → identity (worker сведёт к INTERNAL) — защита от паники в неполном
// wiring; production всегда инжектит реальный.
type ErrToStatus func(error) error

// UseCase — бизнес-логика Region поверх CQRS-портов Reader/Writer, LRO-стека
// operations и инжектированного transport-mapper'а errStatus.
type UseCase struct {
	reader    Reader
	writer    Writer
	ops       operations.Repo
	errStatus ErrToStatus
}

// New собирает UseCase для Region. reader/writer — CQRS-разделённые порты
// (composition root может связать reader с репликой); ops — corelib LRO-репозиторий
// operations-таблицы; errStatus — инжектированный маппер sentinel→gRPC-status.
func New(reader Reader, writer Writer, ops operations.Repo, errStatus ErrToStatus) *UseCase {
	if errStatus == nil {
		errStatus = func(err error) error { return err }
	}
	return &UseCase{reader: reader, writer: writer, ops: ops, errStatus: errStatus}
}

// Get возвращает Region по id.
func (u *UseCase) Get(ctx context.Context, id string) (*domain.Region, error) {
	if err := domain.ValidateID("region id", id); err != nil {
		return nil, fmt.Errorf("%w: %s", geoerrors.ErrInvalidArg, err.Error())
	}
	r, err := u.reader.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// List возвращает регионы (cursor-пагинация по id; garbage page_size → InvalidArgument).
func (u *UseCase) List(ctx context.Context, p Pagination) ([]*domain.Region, string, error) {
	size, err := validate.PageSize("page_size", p.PageSize)
	if err != nil {
		return nil, "", err
	}
	p.PageSize = size
	return u.reader.List(ctx, p)
}

// Create принимает запрос на создание Region (admin-only) и возвращает Operation.
// Малформ/невалидный вход (пустой id, слишком длинное name) отвергается
// СИНХРОННО (InvalidArgument) — операция в таблицу не пишется. Валидный вход →
// LRO-строка (done=false) + фоновый worker, который вставляет регион и
// финализирует операцию (response=Region или error).
func (u *UseCase) Create(ctx context.Context, id, name string) (*operations.Operation, error) {
	r := domain.Region{ID: id, Name: name}
	if err := r.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", geoerrors.ErrInvalidArg, err.Error())
	}
	op, err := operations.NewFromContext(ctx, lro.OperationPrefix,
		fmt.Sprintf("Create region %s", id),
		&geov1.CreateRegionMetadata{RegionId: id})
	if err != nil {
		return nil, err
	}
	if err := u.ops.Create(ctx, op); err != nil {
		return nil, err
	}
	operations.Run(ctx, u.ops, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		created, derr := u.writer.Insert(ctx, &r)
		if derr != nil {
			return nil, u.errStatus(derr)
		}
		return marshalRegion(created)
	})
	return &op, nil
}

// Update принимает запрос на смену name у Region (admin-only) и возвращает
// Operation. Пустой/малформ id → синхронный InvalidArgument первым стейтментом
// (парити с Get: format-check ДО записи LRO-строки — malformed-id не должен
// персистить spurious operation и уходить в UPDATE → async NotFound). name="" →
// поле не меняется (nil в repo). Доменная запись и not-found/конфликт → в Operation.error.
func (u *UseCase) Update(ctx context.Context, id, name string) (*operations.Operation, error) {
	if err := domain.ValidateID("region id", id); err != nil {
		return nil, fmt.Errorf("%w: %s", geoerrors.ErrInvalidArg, err.Error())
	}
	var namePtr *string
	if name != "" {
		if err := domain.ValidateName("region name", name); err != nil {
			return nil, fmt.Errorf("%w: %s", geoerrors.ErrInvalidArg, err.Error())
		}
		namePtr = &name
	}
	op, err := operations.NewFromContext(ctx, lro.OperationPrefix,
		fmt.Sprintf("Update region %s", id),
		&geov1.UpdateRegionMetadata{RegionId: id})
	if err != nil {
		return nil, err
	}
	if err := u.ops.Create(ctx, op); err != nil {
		return nil, err
	}
	operations.Run(ctx, u.ops, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		updated, derr := u.writer.Update(ctx, id, namePtr)
		if derr != nil {
			return nil, u.errStatus(derr)
		}
		return marshalRegion(updated)
	})
	return &op, nil
}

// Delete принимает запрос на удаление Region (admin-only) и возвращает Operation.
// Пустой/малформ id → синхронный InvalidArgument первым стейтментом (парити с Get:
// format-check ДО записи LRO-строки). Блокировка FK RESTRICT (есть зоны) и
// not-found → в Operation.error (FailedPrecondition / NotFound). Успех →
// response=Empty (без тела ресурса).
func (u *UseCase) Delete(ctx context.Context, id string) (*operations.Operation, error) {
	if err := domain.ValidateID("region id", id); err != nil {
		return nil, fmt.Errorf("%w: %s", geoerrors.ErrInvalidArg, err.Error())
	}
	op, err := operations.NewFromContext(ctx, lro.OperationPrefix,
		fmt.Sprintf("Delete region %s", id),
		&geov1.DeleteRegionMetadata{RegionId: id})
	if err != nil {
		return nil, err
	}
	if err := u.ops.Create(ctx, op); err != nil {
		return nil, err
	}
	operations.Run(ctx, u.ops, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		if derr := u.writer.Delete(ctx, id); derr != nil {
			return nil, u.errStatus(derr)
		}
		return anypb.New(&emptypb.Empty{})
	})
	return &op, nil
}

// marshalRegion упаковывает domain.Region в Operation.response, делегируя
// field-mapping единому protoconv.Region (та же проекция, что handler и
// LRO-recovery — без риска дрейфа).
func marshalRegion(r *domain.Region) (*anypb.Any, error) {
	return anypb.New(protoconv.Region(r))
}
