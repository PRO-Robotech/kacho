// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package routetable

import (
	"context"
	"errors"
	"fmt"

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

// CreateRouteTableUseCase инициирует создание RouteTable. Sync-проверки (parent
// network exists, name unique, static_routes валидны) выполняются ДО создания
// Operation — клиент получает fast-fail gRPC-status, а не «200 + операция,
// упавшая через секунду». Async-часть (`doCreate`) — атомарный backstop через FK/UNIQUE.
//
// Worker открывает Writer-TX и делает в ней Insert(RouteTable) + outbox-emit
// CREATED атомарно. Auto-association DB-trigger дополнительно эмитит
// `Subnet.UPDATED` в той же tx-области — это часть Commit'а единой writer-TX.
type CreateRouteTableUseCase struct {
	repo          Repo
	projectClient ProjectClient
	opsRepo       operations.Repo
	registrar     fgaregister.Registrar

	// confirmer — read-after-register проба owner-tuple (owner-tuple opgate). При
	// non-nil Create-op становится `done=true, response` только после подтверждения
	// owner-tuple RouteTable в FGA (окно 403 на немедленной мутации создателя закрыто).
	// nil → confirm-gate выключен (прежнее поведение — op done сразу после worker-fn).
	confirmer OwnerTupleConfirmer

	// dispatch — точка запуска async Create-worker'а с confirm-gate. Дефолт —
	// operations.RunWithConfirm; тест инжектит Worker с коротким deadline (OTG-05).
	dispatch confirmDispatcher
}

// OwnerTupleConfirmer — read-after-register проба owner-tuple для confirm-gate
// Create-op (owner-tuple opgate). confirmed=true, когда owner-tuple созданного
// RouteTable эффективен в FGA для creator'а (gateway scope_extractor Check немедленной
// мутации `creator #v_update vpc_route_table:<id>` вернёт ALLOW). Реализация —
// check.NewRouteTableOwnerConfirmer (reuse authz.CheckClient, без нового ребра). nil →
// confirm-gate выключен.
type OwnerTupleConfirmer interface {
	Confirm(ctx context.Context, creator operations.Principal, resourceID string) (bool, error)
}

// confirmDispatcher — сигнатура диспетча Create-op с confirm-gate (owner-tuple
// opgate). Совпадает с operations.RunWithConfirm; confirm==nil ≡ operations.Run.
type confirmDispatcher func(ctx context.Context, opsRepo operations.Repo, opID string,
	fn func(context.Context) (*anypb.Any, error), confirm operations.ConfirmFunc)

// WithRegistrar подключает синхронный owner-tuple registrar (Decision 2): после
// commit RouteTable owner-tuple синхронно регистрируется в kacho-iam. Nil →
// sync-путь пропускается (только async drainer).
func (u *CreateRouteTableUseCase) WithRegistrar(r fgaregister.Registrar) *CreateRouteTableUseCase {
	u.registrar = r
	return u
}

// WithConfirmer подключает read-after-register confirmer owner-tuple (owner-tuple
// opgate): Create-op достигает success-`done` только после подтверждения owner-tuple
// в FGA — окно 403 на немедленной мутации создателя закрыто. Nil → confirm-gate выключен.
func (u *CreateRouteTableUseCase) WithConfirmer(c OwnerTupleConfirmer) *CreateRouteTableUseCase {
	u.confirmer = c
	return u
}

// NewCreateRouteTableUseCase создает CreateRouteTableUseCase.
func NewCreateRouteTableUseCase(r Repo, projectClient ProjectClient, opsRepo operations.Repo) *CreateRouteTableUseCase {
	return &CreateRouteTableUseCase{
		repo:          r,
		projectClient: projectClient,
		opsRepo:       opsRepo,
		dispatch:      operations.RunWithConfirm,
	}
}

// Execute — sync-валидация + create Operation + запуск worker'а.
//
// Принимает `domain.RouteTable` напрямую (без тривиальной input-обертки). Поле
// `rt.ID` на входе пустое — назначаем внутри use-case'а через
// `ids.NewID(ids.PrefixRouteTable)`.
func (u *CreateRouteTableUseCase) Execute(ctx context.Context, rt domain.RouteTable) (*operations.Operation, error) {
	if err := corevalidate.ResourceID("network", ids.PrefixNetwork, rt.NetworkID); err != nil {
		return nil, err
	}
	if rt.ProjectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id required")
	}
	if rt.NetworkID == "" {
		return nil, status.Error(codes.InvalidArgument, "network_id required")
	}
	// Domain self-validation.
	if err := serviceerr.FromValidation(rt.Validate()); err != nil {
		return nil, err
	}
	if err := validateStaticRoutes(rt.StaticRoutes); err != nil {
		return nil, err
	}

	// Sync project.Exists precheck здесь не делаем — он race-prone: между
	// sync-проверкой и async-частью project может быть удален peer-сервисом, и
	// тогда ресурс создался бы безусловно. NotFound для project возвращается через
	// `operation.error` из async `doCreate`. Sync uniqueness/network-existence
	// (по DB-state в той же сервис-БД) остаются — они race-free относительно peer'ов.
	// Existence parent Network через CQRS Reader.
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	parentNet, gerr := rd.Networks().Get(ctx, rt.NetworkID)
	if gerr != nil {
		_ = rd.Close()
		if errors.Is(gerr, repo.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "Network %s not found", rt.NetworkID)
		}
		return nil, serviceerr.MapRepoErr(gerr)
	}
	// BOLA-guard: parent Network обязана принадлежать проекту вызывающего — иначе
	// RouteTable ссылалась бы на чужую сеть (cross-project reference). Ответ — тот
	// же NotFound, что для несуществующей сети (без existence-oracle).
	if parentNet.ProjectID != rt.ProjectID {
		_ = rd.Close()
		return nil, status.Errorf(codes.NotFound, "Network %s not found", rt.NetworkID)
	}
	// Uniqueness (project_id, name) — partial UNIQUE WHERE name<>'' покрывает на
	// DB-уровне. Sync-precheck для fast-fail UX.
	name := string(rt.Name)
	if name != "" {
		existing, _, lerr := rd.RouteTables().List(ctx, RouteTableFilter{ProjectID: rt.ProjectID, Name: name}, Pagination{})
		if lerr != nil {
			_ = rd.Close()
			return nil, serviceerr.MapRepoErr(lerr)
		}
		if len(existing) > 0 {
			_ = rd.Close()
			return nil, status.Errorf(codes.AlreadyExists, "RouteTable with name %s already exists", name)
		}
	}
	_ = rd.Close()

	rtID := ids.NewID(ids.PrefixRouteTable)
	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Create route table %s", name),
		&vpcv1.CreateRouteTableMetadata{RouteTableId: rtID},
	)
	if err != nil {
		return nil, err
	}
	if err := u.opsRepo.Create(ctx, op); err != nil {
		return nil, err
	}

	// Confirm-gate owner-tuple (owner-tuple opgate): при подключённом confirmer op
	// достигает success-`done` только после read-after-register подтверждения
	// owner-tuple RouteTable. creator = op.Principal. nil confirmer → confirm=nil →
	// RunWithConfirm ≡ Run (back-compat).
	var confirm operations.ConfirmFunc
	if u.confirmer != nil {
		creator := op.Principal
		confirm = func(cctx context.Context) (bool, error) {
			return u.confirmer.Confirm(cctx, creator, rtID)
		}
	}

	u.dispatch(ctx, u.opsRepo, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		return u.doCreate(ctx, rtID, rt)
	}, confirm)

	return &op, nil
}

// doCreate — async-часть Create. Атомарный backstop:
//   - project-exists peer-API
//   - Writer-TX: Insert(RouteTable) + outbox-emit RouteTable.CREATED
//
// Auto-association trigger внутри Postgres сразу после INSERT route_tables
// перебирает `subnets WHERE network_id = NEW.network_id AND route_table_id IS NULL`
// и проставляет им `route_table_id = NEW.id`; сопутствующие `Subnet.UPDATED`
// события записываются в outbox триггером — все в одной БД-TX, commit'ится
// атомарно с нашим Insert + outbox-emit.
func (u *CreateRouteTableUseCase) doCreate(ctx context.Context, rtID string, rt domain.RouteTable) (*anypb.Any, error) {
	exists, err := u.projectClient.Exists(ctx, rt.ProjectID)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "project check: %v", err)
	}
	if !exists {
		return nil, status.Errorf(codes.NotFound, "Project %s not found", rt.ProjectID)
	}

	rt.ID = rtID

	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer w.Abort()

	// Parent Network existence — повторная проверка внутри writer-TX (FK ниже —
	// атомарный backstop). FK route_tables.network_id → networks(id) дает
	// 23503 если parent исчез между sync-check и Insert.
	parentNet, gerr := w.Networks().Get(ctx, rt.NetworkID)
	if gerr != nil {
		if errors.Is(gerr, repo.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "Network %s not found", rt.NetworkID)
		}
		return nil, serviceerr.MapRepoErr(gerr)
	}
	// BOLA-guard (async backstop): parent Network обязана принадлежать проекту
	// вызывающего — тот же NotFound, что для отсутствующей сети (без oracle).
	if parentNet.ProjectID != rt.ProjectID {
		return nil, status.Errorf(codes.NotFound, "Network %s not found", rt.NetworkID)
	}

	created, err := w.RouteTables().Insert(ctx, &rt)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	if err := w.Outbox().Emit(ctx, "RouteTable", created.ID, "CREATED", helpers.RouteTablePayload(created)); err != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, err))
	}
	// Записываем INTENT на owner-hierarchy-tuple vpc_route_table→project в той же
	// writer-TX — at-least-once через transactional-outbox, без best-effort-потери
	// при ошибке. В mirror-feed несем labels RouteTable + parent_project_id
	// (ProjectHierarchyItem), а не голый tuple — иначе resource_mirror в kacho-iam
	// остается без labels и ARM_LABELS-селектор не матчит даже свежесозданную
	// RouteTable. Симметрично network/subnet/securitygroup create.
	items := []fgaregister.Item{
		fgaregister.ProjectHierarchyItem(string(rt.ProjectID), "vpc_route_table", created.ID,
			domain.LabelsToMap(created.Labels)),
	}
	if err := w.FGARegister().EmitRegister(ctx, fgaregister.RegisterItems(items...)); err != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: fga register intent: %v", repo.ErrInternal, err))
	}
	if err := w.Commit(); err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	// Sync-primary owner-tuple registration (после durable commit); fail-closed.
	if u.registrar != nil {
		if err := u.registrar.Register(ctx, items); err != nil {
			return nil, err
		}
	}
	return marshalRouteTableRecord(created)
}
