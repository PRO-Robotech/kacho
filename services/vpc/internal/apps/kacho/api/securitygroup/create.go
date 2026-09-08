// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package securitygroup

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

// CreateSecurityGroupUseCase инициирует создание SG. Sync-проверки (project
// exists, name unique, network exists) выполняются ДО создания Operation —
// клиент получает fast-fail gRPC-status, а не «200 + операция, упавшая через
// секунду». Async-часть (`doCreate`) — атомарный backstop через FK/UNIQUE:
// worker открывает ОДНУ Writer-TX, делает Insert(SG) + outbox-emit в ней, Commit.
//
// Default-SG для Network создается НЕ здесь: она inline в `CreateNetworkUseCase`
// через `domain.NewDefaultSecurityGroup`. Этот use-case — обычный явный Create
// от клиента.
type CreateSecurityGroupUseCase struct {
	// quota — совещательная полоса учёта (порт QuotaGuard).
	//
	// nil означает «раннего отказа нет», а НЕ «предела нет»: место по-прежнему
	// занимает триггер в writer-транзакции, и исчерпание приезжает отказом
	// операции. Различие наблюдаемо (429 синхронно против отказа в операции), и
	// потому провязка обязательна на любом поднятом стенде; отсутствие допустимо
	// только там, где нет и соседа, у которого спрашивать величины.
	quota         QuotaGuard
	repo          Repo
	networkReader NetworkReader
	sgReader      SecurityGroupReader
	projectClient ProjectClient
	opsRepo       operations.Repo
	registrar     fgaregister.Registrar
}

// WithRegistrar подключает синхронный owner-tuple registrar (Decision 2): после
// commit SecurityGroup owner-tuple синхронно регистрируется в kaname. Nil →
// sync-путь пропускается (только async drainer).
func (u *CreateSecurityGroupUseCase) WithRegistrar(r fgaregister.Registrar) *CreateSecurityGroupUseCase {
	u.registrar = r
	return u
}

// WithSGReader уточняет ИСТОЧНИК чтения групп для проверки SG-target-правил
// (composition-root передаёт `cqrsadapter.SecurityGroupAdapter`). Проверку он НЕ
// включает и не выключает: порт уже выведен конструктором из обязательного
// `Repo`, а состояния «порт не передан ⇒ проверка пропускается» у пакета больше
// нет — см. `sgTargetReader`.
func (u *CreateSecurityGroupUseCase) WithSGReader(r SecurityGroupReader) *CreateSecurityGroupUseCase {
	u.sgReader = sgTargetReader(r, u.repo)
	return u
}

// NewCreateSecurityGroupUseCase создает CreateSecurityGroupUseCase.
//
// Порт чтения групп выводится здесь же и потому никогда не пуст: проверка
// «цель правила лежит в моей сети» не имеет выключенного состояния.
func NewCreateSecurityGroupUseCase(r Repo, networkReader NetworkReader, projectClient ProjectClient, opsRepo operations.Repo) *CreateSecurityGroupUseCase {
	return &CreateSecurityGroupUseCase{
		repo:          r,
		networkReader: networkReader,
		sgReader:      sgTargetReader(nil, r),
		projectClient: projectClient,
		opsRepo:       opsRepo,
	}
}

// Execute — sync-валидация + create Operation + запуск worker'а.
//
// Принимает `domain.SecurityGroup` напрямую, без обертки-DTO. Поле `sg.ID` на
// входе пустое — назначаем внутри use-case'а через `ids.NewID(ids.PrefixSecurityGroup)`.
func (u *CreateSecurityGroupUseCase) Execute(ctx context.Context, sg domain.SecurityGroup) (*operations.Operation, error) {
	if sg.ProjectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id required")
	}
	// network_id ОБЯЗАТЕЛЕН: SG обязана принадлежать ровно одной Network своего
	// проекта. Sync required-check — до создания Operation, в одном ряду с
	// `project_id required`.
	if sg.NetworkID == "" {
		return nil, status.Error(codes.InvalidArgument, "network_id required")
	}
	if err := corevalidate.ResourceID("network", ids.PrefixNetwork, sg.NetworkID); err != nil {
		return nil, err
	}

	// Domain self-validation: имя/описание/labels через newtype.Validate() +
	// каждое правило через r.Validate() (description/labels). Cross-cutting
	// rule-валидация (direction, CIDR, protocol) — отдельно через validateSGRule
	// ниже (это не newtype-level).
	if err := serviceerr.FromValidation(sg.Validate()); err != nil {
		return nil, err
	}
	// Потолок числа правил — ПЕРВЫМ, до поэлементной валидации и до любого
	// резолва целей: иначе стоимость запроса задаёт вызывающий.
	if err := validateSGRulesCardinality("rule_specs", sg.Rules); err != nil {
		return nil, err
	}
	for i, r := range sg.Rules {
		if err := validateSGRule(fmt.Sprintf("rule_specs[%d]", i), r); err != nil {
			return nil, err
		}
	}

	// Sync project.Exists precheck намеренно отсутствует: он race-prone — между
	// sync-проверкой и async-частью project может быть удален peer-сервисом, и
	// second-writer-wins безусловно создавал бы ресурс. NotFound по project
	// возвращается через `operation.error` из async `doCreate`. Sync-проверки
	// network-existence/uniqueness (по DB-state в той же сервис-БД) остаются —
	// они race-free относительно peer-сервисов.
	// `networkReader` — обязательный порт (позиционный параметр конструктора), и
	// ветки «порт не передан ⇒ пропустить» здесь нет намеренно: она означала бы
	// «BOLA-guard не настроен = разрешено», неотличимое от «guard разрешил».
	parentNet, err := u.networkReader.Get(ctx, sg.NetworkID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "Network %s not found", sg.NetworkID)
		}
		return nil, serviceerr.MapRepoErr(err)
	}
	// BOLA-guard: parent Network обязана принадлежать проекту вызывающего —
	// иначе SG цеплялась бы к чужой сети (cross-project reference). Ответ —
	// тот же NotFound, что для несуществующей сети (без existence-oracle).
	if parentNet.ProjectID != sg.ProjectID {
		return nil, status.Errorf(codes.NotFound, "Network %s not found", sg.NetworkID)
	}

	// Same-network-валидация SG-target-правил: каждое правило с
	// `security_group_id` обязано ссылаться на SG из той же Network, что и
	// создаваемая SG. Sync fast-fail; async backstop — в doCreate.
	if err := validateSGTargetSameNetwork(ctx, u.sgReader, sg.NetworkID, sg.Rules,
		func(i int) string { return fmt.Sprintf("rule_specs[%d].security_group_id", i) }); err != nil {
		return nil, err
	}
	// Ссылка правила на именованный набор: набор того же проекта и непустой.
	// Быстрый отказ с именем поля; настоящий (гоночно-стойкий) отказ живёт внутри
	// writer-транзакции doCreate — см. комментарий validateSGTargetCidrGroup.
	if err := validateSGTargetCidrGroup(ctx, repoCidrGroupReader{repo: u.repo}, sg.ProjectID, sg.Rules,
		func(i int) string { return fmt.Sprintf("rule_specs[%d].cidr_group_id", i) }); err != nil {
		return nil, err
	}
	name := string(sg.Name)
	if name != "" {
		rd, err := u.repo.Reader(ctx)
		if err != nil {
			return nil, serviceerr.MapRepoErr(err)
		}
		existing, _, lerr := rd.SecurityGroups().List(ctx, SecurityGroupFilter{ProjectID: sg.ProjectID, Name: name}, Pagination{})
		_ = rd.Close()
		if lerr != nil {
			return nil, serviceerr.MapRepoErr(lerr)
		}
		if len(existing) > 0 {
			return nil, status.Errorf(codes.AlreadyExists, "SecurityGroup with name %s already exists", name)
		}
	}

	sgID := ids.NewID(ids.PrefixSecurityGroup)
	// Пустое имя не доживает до записи: ресурса без имени не бывает (#715).
	// Подстановка стоит ПОСЛЕ чеканки идентификатора (умолчание производно от
	// него) и ДО сборки строки — и она же снимает нужду в проверке «а не занято
	// ли»: идентификатор глобально уникален by construction, поэтому уникальность
	// имени остаётся за индексом БД, а не за чтением-перед-вставкой (ban #10).
	name = corevalidate.NameOrDefault(name, sgID)
	sg.Name = domain.RcNameVPC(name)
	// Учёт числа ресурсов: ранний отказ ДО создания операции.
	//
	// Здесь же материализуются строки учёта, если проект их ещё не имеет, —
	// момент, когда владелец типа впервые узнаёт о проекте, и есть обращение к
	// нему. Отказ уходит арендатору синхронно тем же текстом и признаком, каким
	// его произвёл бы триггер: у обеих полос один производитель.
	if u.quota != nil {
		if err := u.quota.Admit(ctx, string(sg.ProjectID), "vpc.securityGroup"); err != nil {
			return nil, serviceerr.MapRepoErr(err)
		}
	}

	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Create security group %s", name),
		&vpcv1.CreateSecurityGroupMetadata{SecurityGroupId: sgID},
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
		return u.doCreate(ctx, sgID, sg)
	})

	return &op, nil
}

// doCreate — async-часть Create (внутри Operation worker'а). Project-exists +
// network-exists повторяются как defensive backstop; затем Insert через CQRS
// writer-TX + outbox-emit в той же TX.
func (u *CreateSecurityGroupUseCase) doCreate(ctx context.Context, sgID string, sg domain.SecurityGroup) (*anypb.Any, error) {
	exists, err := u.projectClient.Exists(ctx, sg.ProjectID)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "project check: %v", err)
	}
	if !exists {
		return nil, status.Errorf(codes.NotFound, "Project %s not found", sg.ProjectID)
	}
	parentNet, gerr := u.networkReader.Get(ctx, sg.NetworkID)
	if gerr != nil {
		return nil, serviceerr.MapRepoErr(gerr)
	}
	// BOLA-guard (async backstop): parent Network обязана принадлежать проекту
	// вызывающего — тот же NotFound, что для отсутствующей сети (без oracle).
	if parentNet.ProjectID != sg.ProjectID {
		return nil, status.Errorf(codes.NotFound, "Network %s not found", sg.NetworkID)
	}
	// Async backstop для same-network SG-target-правил: ловит гонку «target-SG
	// удалена / создана в другой сети после sync-precheck».
	if err := validateSGTargetSameNetwork(ctx, u.sgReader, sg.NetworkID, sg.Rules,
		func(i int) string { return fmt.Sprintf("rule_specs[%d].security_group_id", i) }); err != nil {
		return nil, err
	}

	sg.ID = sgID
	sg.Rules = assignRuleIDs(sg.Rules)

	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer w.Abort()

	created, err := w.SecurityGroups().Insert(ctx, &sg)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	// Ссылка на именованный набор проверяется ПОСЛЕ записи правил и В ЭТОЙ ЖЕ
	// транзакции: запись поставила строки проекции ссылок, и их внешний ключ
	// удерживает набор от опустошения конкурентом. Проверка до записи отвечала бы
	// по снимку, который конкурент уже переписывает.
	if verr := validateSGTargetCidrGroup(ctx, w.CidrGroups(), sg.ProjectID, created.Rules,
		func(i int) string { return fmt.Sprintf("rule_specs[%d].cidr_group_id", i) }); verr != nil {
		return nil, verr
	}
	if err := w.Outbox().Emit(ctx, "SecurityGroup", created.ID, created.ProjectID, "CREATED", helpers.DomainToMap(created)); err != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, err))
	}
	// Публикуем INTENT на vpc_security_group→project hierarchy-tuple в той же
	// writer-TX (at-least-once через transactional-outbox, не теряется на ошибке).
	// В mirror-feed несем labels SG + parent_project_id (ProjectHierarchyItem), а
	// не голый tuple — иначе resource_mirror в kaname остается без labels и
	// ARM_LABELS-селектор не матчит даже только что созданную SG. Симметрично
	// network/create.go и subnet/create.go.
	items := []fgaregister.Item{
		fgaregister.ProjectHierarchyItem(string(sg.ProjectID), "vpc_security_group", created.ID,
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
	// и подменяет сообщение всей цепочкой) на уже созданную SG — фантом.
	// Поэтому предупреждение, а не ошибка.
	fgaregister.DeliverAfterCommit(ctx, u.registrar, items, intentVersion, "SecurityGroup", created.ID)
	return marshalSecurityGroupRecord(created)
}

// WithQuotaGuard подключает совещательную полосу учёта.
//
// Отдельным глаголом, а не аргументом конструктора: полоса появилась позже
// вызывающих, и обязательный аргумент заставил бы править каждую сборку — в том
// числе те, где соседа с величинами нет вовсе.
func (u *CreateSecurityGroupUseCase) WithQuotaGuard(g QuotaGuard) *CreateSecurityGroupUseCase {
	u.quota = g
	return u
}
