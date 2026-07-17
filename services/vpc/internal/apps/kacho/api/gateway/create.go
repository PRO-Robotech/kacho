// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package gateway

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
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/fgaregister"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/helpers"
)

// CreateGatewayUseCase инициирует создание Gateway. Sync-проверки формата входа
// выполняются ДО создания Operation — клиент получает fast-fail gRPC-status, а не
// «200 + операция, упавшая через секунду». Async-часть (`doCreate`) — атомарный
// backstop через FK.
//
// Worker открывает одну Writer-TX, делает Insert(Gateway) + outbox emit и Commit:
// либо все видно, либо ничего — окно orphan-Gateway / потерянного outbox-event'а
// закрыто.
type CreateGatewayUseCase struct {
	repo          Repo
	projectClient ProjectClient
	opsRepo       operations.Repo
	registrar     fgaregister.Registrar

	// confirmer — read-after-register проба owner-tuple (owner-tuple opgate). При
	// non-nil Create-op становится `done=true, response` только после подтверждения
	// owner-tuple Gateway в FGA (окно 403 на немедленной мутации создателя закрыто).
	// nil → confirm-gate выключен (прежнее поведение — op done сразу после worker-fn).
	confirmer OwnerTupleConfirmer

	// dispatch — точка запуска async Create-worker'а с confirm-gate. Дефолт —
	// operations.RunWithConfirm; тест инжектит Worker с коротким deadline (OTG-05).
	dispatch confirmDispatcher
}

// OwnerTupleConfirmer — read-after-register проба owner-tuple для confirm-gate
// Create-op (owner-tuple opgate). confirmed=true, когда owner-tuple созданного
// Gateway эффективен в FGA для creator'а (gateway scope_extractor Check немедленной
// мутации `creator #v_update vpc_gateway:<id>` вернёт ALLOW). Реализация —
// check.NewGatewayOwnerConfirmer (reuse authz.CheckClient, без нового ребра). nil →
// confirm-gate выключен.
type OwnerTupleConfirmer interface {
	Confirm(ctx context.Context, creator operations.Principal, resourceID string) (bool, error)
}

// confirmDispatcher — сигнатура диспетча Create-op с confirm-gate (owner-tuple
// opgate). Совпадает с operations.RunWithConfirm; confirm==nil ≡ operations.Run.
type confirmDispatcher func(ctx context.Context, opsRepo operations.Repo, opID string,
	fn func(context.Context) (*anypb.Any, error), confirm operations.ConfirmFunc)

// NewCreateGatewayUseCase создает CreateGatewayUseCase.
func NewCreateGatewayUseCase(r Repo, projectClient ProjectClient, opsRepo operations.Repo) *CreateGatewayUseCase {
	return &CreateGatewayUseCase{
		repo:          r,
		projectClient: projectClient,
		opsRepo:       opsRepo,
		dispatch:      operations.RunWithConfirm,
	}
}

// WithRegistrar подключает синхронный owner-tuple registrar (Decision 2): после
// commit Gateway owner-tuple синхронно регистрируется в kacho-iam. Nil →
// sync-путь пропускается (только async drainer).
func (u *CreateGatewayUseCase) WithRegistrar(r fgaregister.Registrar) *CreateGatewayUseCase {
	u.registrar = r
	return u
}

// WithConfirmer подключает read-after-register confirmer owner-tuple (owner-tuple
// opgate): Create-op достигает success-`done` только после подтверждения owner-tuple
// в FGA — окно 403 «no direct relations granted» на немедленной мутации создателя
// закрыто. Nil → confirm-gate выключен.
func (u *CreateGatewayUseCase) WithConfirmer(c OwnerTupleConfirmer) *CreateGatewayUseCase {
	u.confirmer = c
	return u
}

// Execute — sync-валидация + create Operation + запуск worker'а. Возвращает
// созданный Operation указателем (caller'у нужен он для `OperationService.Get`).
//
// Принимает `domain.Gateway` напрямую, без обертки-DTO. Поле `g.ID` на входе
// пустое — назначим внутри use-case'а через `ids.NewID(ids.PrefixGateway)`.
func (u *CreateGatewayUseCase) Execute(ctx context.Context, g domain.Gateway) (*operations.Operation, error) {
	if g.ProjectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id required")
	}
	name := string(g.Name)
	// Gateway.Name — строгий regex (lowercase, без uppercase/underscore).
	if err := corevalidate.NameGateway("name", name); err != nil {
		return nil, err
	}
	// Domain self-validation для description/labels.
	if err := serviceerr.FromValidation(g.Validate()); err != nil {
		return nil, err
	}
	// gateway-type oneof обязателен. Сейчас единственный тип — shared_egress
	// (SharedEgressGatewaySpec).
	if g.GatewayType != domain.GatewayTypeSharedEgress {
		return nil, status.Error(codes.InvalidArgument, "Illegal argument gateway")
	}

	// Sync project.Exists precheck тут не делаем — он race-prone: между sync-проверкой
	// и async-частью project может быть удален peer-сервисом, и second-writer-wins
	// безусловно создал бы ресурс. NotFound возвращается через `operation.error` из
	// async `doCreate`. Имена Gateway НЕ уникальны — name-uniqueness тут не проверяем
	// (в отличие от Network/Subnet/RouteTable/SecurityGroup).

	gwID := ids.NewID(ids.PrefixGateway)
	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Create gateway %s", name),
		&vpcv1.CreateGatewayMetadata{GatewayId: gwID},
	)
	if err != nil {
		return nil, err
	}
	if err := u.opsRepo.Create(ctx, op); err != nil {
		return nil, err
	}

	// Confirm-gate owner-tuple (owner-tuple opgate): при подключённом confirmer op
	// достигает success-`done` только после read-after-register подтверждения
	// owner-tuple Gateway. creator = op.Principal. nil confirmer → confirm=nil →
	// RunWithConfirm ≡ Run (back-compat).
	var confirm operations.ConfirmFunc
	if u.confirmer != nil {
		creator := op.Principal
		confirm = func(cctx context.Context) (bool, error) {
			return u.confirmer.Confirm(cctx, creator, gwID)
		}
	}

	u.dispatch(ctx, u.opsRepo, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		return u.doCreate(ctx, gwID, g)
	}, confirm)

	return &op, nil
}

// doCreate — async-часть Create (внутри Operation worker'а). Атомарный backstop:
// project-exists + Insert (FK / UNIQUE-нарушения).
//
// ВСЕ в одной writer-TX: Insert(Gateway) + outbox emit Gateway.CREATED ходят через
// ту же pgx.Tx writer'а, поэтому либо оба видны (Commit), либо ни один (Abort/crash).
func (u *CreateGatewayUseCase) doCreate(ctx context.Context, gwID string, g domain.Gateway) (*anypb.Any, error) {
	exists, err := u.projectClient.Exists(ctx, g.ProjectID)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "project check: %v", err)
	}
	if !exists {
		return nil, status.Errorf(codes.NotFound, "Project %s not found", g.ProjectID)
	}

	gtype := g.GatewayType
	if gtype == "" {
		gtype = domain.GatewayTypeSharedEgress
	}
	g.ID = gwID
	g.GatewayType = gtype

	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer w.Abort()

	created, err := w.Gateways().Insert(ctx, &g)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	if oerr := w.Outbox().Emit(ctx, "Gateway", created.ID, "CREATED", helpers.DomainToMap(created)); oerr != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, oerr))
	}
	// Записываем INTENT hierarchy-tuple vpc_gateway→project в той же writer-TX,
	// чтобы register-намерение было атомарно с Insert и не терялось при ошибке.
	// В mirror-feed несем labels Gateway + parent_project_id (ProjectHierarchyItem),
	// а не голый tuple — иначе resource_mirror в kacho-iam остается без labels и
	// ARM_LABELS-селектор не матчит даже свежесозданный Gateway. Симметрично
	// network/subnet/securitygroup create.
	items := []fgaregister.Item{
		fgaregister.ProjectHierarchyItem(string(g.ProjectID), "vpc_gateway", created.ID,
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
	return marshalGatewayRecord(created)
}
