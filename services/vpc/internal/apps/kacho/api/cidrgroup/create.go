// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package cidrgroup

import (
	"context"
	"fmt"
	"log/slog"

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

// CreateCidrGroupUseCase инициирует создание именованного набора префиксов.
//
// Синхронно проверяются формат и границы входа — вызывающий получает отказ
// статусом, а не «успешную операцию, упавшую через секунду». Асинхронная часть
// (`doCreate`) остаётся атомарным backstop'ом: существование проекта у владельца,
// UNIQUE имени и потолок состава отвечают в ней конструкцией базы.
type CreateCidrGroupUseCase struct {
	// quota — совещательная полоса учёта (порт QuotaGuard).
	//
	// nil означает «раннего отказа нет», а НЕ «предела нет»: место по-прежнему
	// занимает триггер в writer-транзакции, и исчерпание приезжает отказом
	// операции. Различие наблюдаемо (429 синхронно против отказа в операции), и
	// потому провязка обязательна на любом поднятом стенде; отсутствие допустимо
	// только там, где нет и соседа, у которого спрашивать величины.
	quota         QuotaGuard
	repo          Repo
	projectClient ProjectClient
	opsRepo       operations.Repo
	// registrar — синхронная регистрация owner-tuple в iam ПОСЛЕ коммита
	// (sync-primary; intent в той же writer-TX остаётся at-least-once
	// backstop'ом). nil → остаётся только очередь.
	registrar fgaregister.Registrar
	logger    *slog.Logger
}

// NewCreateCidrGroupUseCase создаёт CreateCidrGroupUseCase.
func NewCreateCidrGroupUseCase(r Repo, projectClient ProjectClient, opsRepo operations.Repo) *CreateCidrGroupUseCase {
	return &CreateCidrGroupUseCase{repo: r, projectClient: projectClient, opsRepo: opsRepo}
}

// WithRegistrar подключает синхронный owner-tuple registrar.
func (u *CreateCidrGroupUseCase) WithRegistrar(r fgaregister.Registrar) *CreateCidrGroupUseCase {
	u.registrar = r
	return u
}

// WithLogger подключает диагностический логгер async-worker'а: без него упавший
// worker оставил бы набор без register-intent, и каждая пообъектная проверка
// прав отвечала бы «нет пути» без единого следа.
func (u *CreateCidrGroupUseCase) WithLogger(logger *slog.Logger) *CreateCidrGroupUseCase {
	u.logger = logger
	return u
}

// Execute — sync-валидация + Operation + worker.
func (u *CreateCidrGroupUseCase) Execute(ctx context.Context, g domain.CidrGroup) (*operations.Operation, error) {
	if g.ProjectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id required")
	}
	if err := serviceerr.FromValidation(g.Validate()); err != nil {
		return nil, err
	}
	// Кардинальность — ПЕРВОЙ, до поблочного разбора: вход-переросток не должен
	// оплачиваться разбором каждого своего элемента на пути запроса.
	if err := validateCidrGroupCardinality(g.V4CidrBlocks, g.V6CidrBlocks); err != nil {
		return nil, err
	}
	v4, err := normalizeCidrBlocks("v4_cidr_blocks", g.V4CidrBlocks, true)
	if err != nil {
		return nil, err
	}
	v6, err := normalizeCidrBlocks("v6_cidr_blocks", g.V6CidrBlocks, false)
	if err != nil {
		return nil, err
	}
	g.V4CidrBlocks, g.V6CidrBlocks = v4, v6

	// Sync-проверки существования проекта здесь НЕТ намеренно: между ней и
	// async-частью проект может исчезнуть, и ресурс создался бы всё равно.
	// Отсутствие проекта возвращается через ошибку операции из doCreate.
	name := string(g.Name)
	if name != "" {
		rd, rerr := u.repo.Reader(ctx)
		if rerr != nil {
			return nil, serviceerr.MapRepoErr(rerr)
		}
		existing, _, lerr := rd.CidrGroups().List(ctx,
			CidrGroupFilter{ProjectID: g.ProjectID, Name: name}, Pagination{})
		_ = rd.Close()
		if lerr != nil {
			return nil, serviceerr.MapRepoErr(lerr)
		}
		if len(existing) > 0 {
			return nil, status.Errorf(codes.AlreadyExists, "CidrGroup with name %s already exists", name)
		}
	}

	groupID := ids.NewHyphenID(ids.PrefixCidrGroupHyphen)
	// Пустое имя не доживает до записи: ресурса без имени не бывает (#715).
	// Подстановка стоит ПОСЛЕ чеканки идентификатора (умолчание производно от
	// него) и ДО сборки строки — и она же снимает нужду в проверке «а не занято
	// ли»: идентификатор глобально уникален by construction, поэтому уникальность
	// имени остаётся за индексом БД, а не за чтением-перед-вставкой (ban #10).
	name = corevalidate.NameOrDefault(name, groupID)
	g.Name = domain.RcNameVPC(name)
	// Учёт числа ресурсов: ранний отказ ДО создания операции.
	//
	// Здесь же материализуются строки учёта, если проект их ещё не имеет, —
	// момент, когда владелец типа впервые узнаёт о проекте, и есть обращение к
	// нему. Отказ уходит арендатору синхронно тем же текстом и признаком, каким
	// его произвёл бы триггер: у обеих полос один производитель.
	if u.quota != nil {
		if err := u.quota.Admit(ctx, string(g.ProjectID), "vpc.cidrGroup"); err != nil {
			return nil, serviceerr.MapRepoErr(err)
		}
	}

	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Create cidr group %s", name),
		&vpcv1.CreateCidrGroupMetadata{CidrGroupId: groupID},
	)
	if err != nil {
		return nil, err
	}
	if err := u.opsRepo.Create(ctx, op); err != nil {
		return nil, err
	}

	if err := operations.RunSync(ctx, u.opsRepo, &op, func(ctx context.Context) (res *anypb.Any, derr error) {
		defer func() {
			if r := recover(); r != nil {
				derr = fmt.Errorf("panic in CidrGroup.Create doCreate: %v", r)
				if u.logger != nil {
					u.logger.Error("cidr group create operation panicked",
						"op", op.ID, "cidr_group_id", groupID, "project_id", g.ProjectID,
						"panic", fmt.Sprint(r))
				}
			}
		}()
		res, derr = u.doCreate(ctx, groupID, g)
		if derr != nil && u.logger != nil {
			u.logger.Error("cidr group create operation failed",
				"op", op.ID, "cidr_group_id", groupID, "project_id", g.ProjectID,
				"err", derr.Error())
		}
		return res, derr
	}); err != nil {
		return nil, err
	}
	return &op, nil
}

// doCreate — async-часть: проверка проекта у владельца, вставка строки и состава,
// событие и register-intent — всё в ОДНОЙ writer-TX.
func (u *CreateCidrGroupUseCase) doCreate(ctx context.Context, groupID string, g domain.CidrGroup) (*anypb.Any, error) {
	exists, err := u.projectClient.Exists(ctx, g.ProjectID)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "project check: %v", err)
	}
	if !exists {
		return nil, status.Errorf(codes.NotFound, "Project %s not found", g.ProjectID)
	}

	g.ID = groupID

	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer w.Abort()

	created, err := w.CidrGroups().Insert(ctx, &g)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	if err := w.Outbox().Emit(ctx, "CidrGroup", created.ID, created.ProjectID, "CREATED", helpers.DomainToMap(created)); err != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, err))
	}

	items := []fgaregister.Item{
		fgaregister.ProjectHierarchyItem(g.ProjectID, "vpc_cidr_group", created.ID,
			domain.LabelsToMap(created.Labels)),
	}
	intentVersion, err := w.FGARegister().EmitRegister(ctx, fgaregister.RegisterItems(items...))
	if err != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: fga register intent: %v", repo.ErrInternal, err))
	}
	if err := w.Commit(); err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	// Синхронная доставка — ПОСЛЕ durable-коммита: она сокращает окно видимости
	// гранта, но условием успеха мутации не является. Провалить операцию здесь
	// значило бы отдать фантом на уже созданный ресурс.
	fgaregister.DeliverAfterCommit(ctx, u.registrar, items, intentVersion, "CidrGroup", created.ID)

	return marshalCidrGroupRecord(created)
}

// WithQuotaGuard подключает совещательную полосу учёта.
//
// Отдельным глаголом, а не аргументом конструктора: полоса появилась позже
// вызывающих, и обязательный аргумент заставил бы править каждую сборку — в том
// числе те, где соседа с величинами нет вовсе.
func (u *CreateCidrGroupUseCase) WithQuotaGuard(g QuotaGuard) *CreateCidrGroupUseCase {
	u.quota = g
	return u
}
