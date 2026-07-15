// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package zone — use-case (бизнес-логика) каталога Zone.
//
// Use-case слой чистой архитектуры: импортирует domain + порт ZoneRepo + corelib
// operations, не тянет pgx/transport. Публичные ZoneService.Get/List — read-only
// (sync). Admin CRUD идет через InternalZoneService на :9091 и возвращает
// Operation (async LRO): мутация синхронно отдает operation.Operation
// (done=false), фоновый corelib-worker выполняет доменную запись и финализирует
// операцию (response=Zone либо Empty для Delete, либо error). Клиент поллит
// OperationService.Get(id) до done.
package zone

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	geov1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/geo/v1"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/validate"

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

// UpdateParams — опциональные поля partial-Update зоны. nil → поле не меняется
// (repo делает COALESCE-апдейт). Позволяет атомарный single-statement UPDATE без
// предварительного Get (исключен TOCTOU read-modify-write).
type UpdateParams struct {
	RegionID *string
	Name     *string
	Status   *domain.ZoneStatus
}

// Reader — read-порт справочника zones (Get/List). CQRS-разделён с Writer:
// read-side можно связать с read-replica pool независимо от writer-side.
type Reader interface {
	Get(ctx context.Context, id string) (*domain.Zone, error)
	List(ctx context.Context, p Pagination) ([]*domain.Zone, string, error)
}

// Writer — write-порт admin-мутаций zones (Insert/Update/Delete + outbox-emit в
// writer-tx). Отделён от Reader (write идёт на primary).
//
// Update — атомарный single-statement (UPDATE … RETURNING) по UpdateParams;
// 0 rows из RETURNING → ErrNotFound.
type Writer interface {
	Insert(ctx context.Context, z *domain.Zone) (*domain.Zone, error)
	Update(ctx context.Context, id string, p UpdateParams) (*domain.Zone, error)
	Delete(ctx context.Context, id string) error
}

// Repo — композит Reader+Writer для adapter'а, реализующего обе стороны
// (pg.ZoneRepo). Composition root связывает reader/writer раздельно.
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

// UseCase — бизнес-логика Zone поверх CQRS-портов Reader/Writer, LRO-стека
// operations и инжектированного transport-mapper'а errStatus.
type UseCase struct {
	reader    Reader
	writer    Writer
	ops       operations.Repo
	errStatus ErrToStatus
}

// New собирает UseCase для Zone. reader/writer — CQRS-разделённые порты
// (composition root может связать reader с репликой); ops — corelib LRO-репозиторий
// operations-таблицы; errStatus — инжектированный маппер sentinel→gRPC-status.
func New(reader Reader, writer Writer, ops operations.Repo, errStatus ErrToStatus) *UseCase {
	if errStatus == nil {
		errStatus = func(err error) error { return err }
	}
	return &UseCase{reader: reader, writer: writer, ops: ops, errStatus: errStatus}
}

// Get возвращает Zone по id.
func (u *UseCase) Get(ctx context.Context, id string) (*domain.Zone, error) {
	if err := domain.ValidateID("zone id", id); err != nil {
		return nil, fmt.Errorf("%w: %s", geoerrors.ErrInvalidArg, err.Error())
	}
	z, err := u.reader.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return z, nil
}

// List возвращает зоны (cursor-пагинация по id; garbage page_size → InvalidArgument).
func (u *UseCase) List(ctx context.Context, p Pagination) ([]*domain.Zone, string, error) {
	size, err := validate.PageSize("page_size", p.PageSize)
	if err != nil {
		return nil, "", err
	}
	p.PageSize = size
	return u.reader.List(ctx, p)
}

// Create принимает запрос на создание Zone (admin-only) и возвращает Operation.
// Малформ/невалидный вход (пустой id/region_id, длинное name, out-of-range
// status) отвергается СИНХРОННО (InvalidArgument). Несуществующий region_id —
// FK-нарушение на вставке → Operation.error FailedPrecondition (источник истины
// DB, не software-precheck).
func (u *UseCase) Create(ctx context.Context, id, regionID, name string, st domain.ZoneStatus) (*operations.Operation, error) {
	// Омитнутый статус (proto default STATUS_UNSPECIFIED=0) → UP: интент схемы
	// (zones.status DEFAULT 'UP'). repo.Insert всегда пишет явное значение, поэтому
	// DB-DEFAULT никогда не срабатывает — дефолт применяем здесь, чтобы Create без
	// status не персистил бессмысленный STATUS_UNSPECIFIED (undefined UP/DOWN).
	if st == domain.ZoneStatusUnspecified {
		st = domain.ZoneStatusUp
	}
	z := domain.Zone{ID: id, RegionID: regionID, Name: name, Status: st}
	if err := z.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", geoerrors.ErrInvalidArg, err.Error())
	}
	op, err := operations.NewFromContext(ctx, lro.OperationPrefix,
		fmt.Sprintf("Create zone %s", id),
		&geov1.CreateZoneMetadata{ZoneId: id})
	if err != nil {
		return nil, err
	}
	if err := u.ops.Create(ctx, op); err != nil {
		return nil, err
	}
	operations.Run(ctx, u.ops, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		created, derr := u.writer.Insert(ctx, &z)
		if derr != nil {
			return nil, u.errStatus(derr)
		}
		return marshalZone(created)
	})
	return &op, nil
}

// Update принимает запрос на partial-смену Zone (admin-only) и возвращает
// Operation. Пустой/малформ id → синхронный InvalidArgument первым стейтментом
// (парити с Get и region_id-веткой: format-check ДО записи LRO-строки — malformed-id
// не должен персистить spurious operation и уходить в UPDATE → async NotFound).
// Пустые regionID/name и unspecified-status НЕ меняют поле (nil → COALESCE в repo).
// not-found/конфликт → Operation.error.
func (u *UseCase) Update(ctx context.Context, id, regionID, name string, st domain.ZoneStatus) (*operations.Operation, error) {
	if err := domain.ValidateID("zone id", id); err != nil {
		return nil, fmt.Errorf("%w: %s", geoerrors.ErrInvalidArg, err.Error())
	}
	var p UpdateParams
	if regionID != "" {
		if err := domain.ValidateID("zone region_id", regionID); err != nil {
			return nil, fmt.Errorf("%w: %s", geoerrors.ErrInvalidArg, err.Error())
		}
		p.RegionID = &regionID
	}
	if name != "" {
		if err := domain.ValidateName("zone name", name); err != nil {
			return nil, fmt.Errorf("%w: %s", geoerrors.ErrInvalidArg, err.Error())
		}
		p.Name = &name
	}
	if st != domain.ZoneStatusUnspecified {
		if err := st.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %s", geoerrors.ErrInvalidArg, err.Error())
		}
		p.Status = &st
	}
	op, err := operations.NewFromContext(ctx, lro.OperationPrefix,
		fmt.Sprintf("Update zone %s", id),
		&geov1.UpdateZoneMetadata{ZoneId: id})
	if err != nil {
		return nil, err
	}
	if err := u.ops.Create(ctx, op); err != nil {
		return nil, err
	}
	operations.Run(ctx, u.ops, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		updated, derr := u.writer.Update(ctx, id, p)
		if derr != nil {
			return nil, u.errStatus(derr)
		}
		return marshalZone(updated)
	})
	return &op, nil
}

// Delete принимает запрос на удаление Zone (admin-only) и возвращает Operation.
// Пустой/малформ id → синхронный InvalidArgument первым стейтментом (парити с Get:
// format-check ДО записи LRO-строки). not-found → Operation.error NotFound.
// Успех → response=Empty.
func (u *UseCase) Delete(ctx context.Context, id string) (*operations.Operation, error) {
	if err := domain.ValidateID("zone id", id); err != nil {
		return nil, fmt.Errorf("%w: %s", geoerrors.ErrInvalidArg, err.Error())
	}
	op, err := operations.NewFromContext(ctx, lro.OperationPrefix,
		fmt.Sprintf("Delete zone %s", id),
		&geov1.DeleteZoneMetadata{ZoneId: id})
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

// marshalZone упаковывает domain.Zone в Operation.response, делегируя field-mapping
// единому protoconv.Zone (та же проекция, что handler и LRO-recovery — без дрейфа).
func marshalZone(z *domain.Zone) (*anypb.Any, error) {
	return anypb.New(protoconv.Zone(z))
}
