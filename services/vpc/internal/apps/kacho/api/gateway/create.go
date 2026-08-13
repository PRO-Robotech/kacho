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
}

// NewCreateGatewayUseCase создает CreateGatewayUseCase.
func NewCreateGatewayUseCase(r Repo, projectClient ProjectClient, opsRepo operations.Repo) *CreateGatewayUseCase {
	return &CreateGatewayUseCase{
		repo:          r,
		projectClient: projectClient,
		opsRepo:       opsRepo,
	}
}

// WithRegistrar подключает синхронный owner-tuple registrar (Decision 2): после
// commit Gateway owner-tuple синхронно регистрируется в kacho-iam. Nil →
// sync-путь пропускается (только async drainer).
func (u *CreateGatewayUseCase) WithRegistrar(r fgaregister.Registrar) *CreateGatewayUseCase {
	u.registrar = r
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
	// Ветвь oneof `gateway` ОБЯЗАТЕЛЬНА и отвергается ИМЕНЕМ ПОЛЯ: шлюз без вида
	// не несёт поведения, которое можно создать. Проверка идёт по набору
	// известных видов, а не «не пусто»: неизвестное значение (например пришедшее
	// из старого клиента) обязано получить отказ, а не уехать в CHECK базы
	// фиксированным INTERNAL.
	switch g.GatewayType {
	case domain.GatewayTypeNat, domain.GatewayTypeEgressOnly:
	default:
		return nil, serviceerr.InvalidArg("gateway", "gateway: required")
	}
	// Якорь размещения обязателен и проверяется по формату СВОЕГО id первым
	// стейтментом: подсеть принадлежит vpc, значит id own-owned. Существование и
	// когерентность семейства решает оператор вставки (repo), не проверка здесь —
	// иначе между проверкой и записью подсеть могла бы исчезнуть.
	if g.SubnetID == "" {
		return nil, serviceerr.InvalidArg("subnet_id", "subnet_id: required")
	}
	if err := corevalidate.ResourceID("subnet", ids.PrefixSubnet, g.SubnetID); err != nil {
		return nil, err
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

	// Create — durable commit → op done сразу после worker-fn. Owner-tuple
	// материализуется eventually-consistent (sync-registrar + drainer/reconciler
	// backstop), а не гейтит done.
	operations.Run(ctx, u.opsRepo, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		return u.doCreate(ctx, gwID, g)
	})

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

	// Вид шлюза уже проверен по закрытому набору в Execute — подстановки по
	// умолчанию здесь НЕТ и быть не может: молчаливый выбор вида за вызывающего
	// означал бы шлюз, делающий не то, о чём просили.
	g.ID = gwID

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
	// Версия, которой БД проштамповала intent ВНУТРИ writer-TX: её же понесёт
	// синхронная регистрация ниже, чтобы повторную доставку гасило монотонное
	// сравнение у принимающей стороны независимо от того, кто пришёл первым.
	intentVersion, err := w.FGARegister().EmitRegister(ctx, fgaregister.RegisterItems(items...))
	if err != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: fga register intent: %v", repo.ErrInternal, err))
	}
	if err := w.Commit(); err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	// Sync-primary owner-tuple registration идёт ПОСЛЕ durable commit: она
	// сокращает окно видимости гранта, но НЕ является условием успеха мутации.
	// Intent на тот же tuple лежит в fga_register_outbox той же writer-TX →
	// at-least-once дренаж доведёт грант сам. Провалить операцию здесь значило бы
	// отдать вызывающему код узла прав (status.FromError достаёт вложенный статус
	// и подменяет сообщение всей цепочкой) на уже созданный gateway — фантом.
	// Поэтому предупреждение, а не ошибка.
	fgaregister.DeliverAfterCommit(ctx, u.registrar, items, intentVersion, "Gateway", created.ID)
	return marshalGatewayRecord(created)
}
