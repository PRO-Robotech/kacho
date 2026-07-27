// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package subnet

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"

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

// CreateSubnetUseCase инициирует создание Subnet. Sync-проверки (project exists,
// parent network exists, name unique, CIDR validity / non-overlap) выполняются
// ДО создания Operation — клиент получает fast-fail gRPC-status, а не «200 +
// операция, упавшая через секунду». Async-часть (`doCreate`) — атомарный
// backstop через FK + EXCLUDE constraint.
//
// Worker открывает ОДНУ Writer-TX и делает Insert(Subnet) + outbox-emit
// Subnet.CREATED атомарно.
type CreateSubnetUseCase struct {
	repo          Repo
	projectClient ProjectClient
	zoneReg       ZoneRegistry
	regionReg     RegionRegistry
	opsRepo       operations.Repo
	registrar     fgaregister.Registrar
}

// WithRegistrar подключает синхронный owner-tuple registrar (Decision 2): после
// commit Subnet тот же owner-tuple синхронно регистрируется в kacho-iam (грант
// доступен сразу). Nil → sync-путь пропускается, остается только async drainer.
func (u *CreateSubnetUseCase) WithRegistrar(r fgaregister.Registrar) *CreateSubnetUseCase {
	u.registrar = r
	return u
}

// NewCreateSubnetUseCase создает CreateSubnetUseCase. zoneReg/regionReg —
// peer-валидаторы Geography (kacho-geo): zoneReg проверяет zone_id ZONAL-подсети,
// regionReg — region_id REGIONAL-подсети.
func NewCreateSubnetUseCase(
	r Repo,
	projectClient ProjectClient,
	zoneReg ZoneRegistry,
	regionReg RegionRegistry,
	opsRepo operations.Repo,
) *CreateSubnetUseCase {
	return &CreateSubnetUseCase{
		repo:          r,
		projectClient: projectClient,
		zoneReg:       zoneReg,
		regionReg:     regionReg,
		opsRepo:       opsRepo,
	}
}

// Execute — sync-валидация + create Operation + запуск worker'а.
//
// Принимает `domain.Subnet` напрямую: отдельная обертка-DTO не нужна — она лишь
// перепаковывала бы domain.Subnet без дополнительного контекста. Поле `s.ID` на
// входе пустое — назначим внутри use-case'а через `ids.NewID(ids.PrefixSubnet)`.
func (u *CreateSubnetUseCase) Execute(ctx context.Context, s domain.Subnet) (*operations.Operation, error) {
	if err := corevalidate.ResourceID("network", ids.PrefixNetwork, s.NetworkID); err != nil {
		return nil, err
	}
	if s.ProjectID == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id required")
	}
	if s.NetworkID == "" {
		return nil, status.Error(codes.InvalidArgument, "network_id required")
	}
	// Placement (F6): placementType° **server-derived, unwritable** из zoneId XOR
	// regionId. placementType в теле → explicit reject; оба/ни одного → reject;
	// иначе выводим дискриминатор и записываем его в domain (Insert → placement_type-
	// колонка). Существование zone/region валидируется у owner-домена geo (fail-closed).
	placement, err := resolvePlacement(ctx, u.zoneReg, u.regionReg, s)
	if err != nil {
		return nil, err
	}
	s.PlacementType = placement
	// Proto contract: v4_cidr_blocks НЕ required — подсеть может быть создана без
	// IPv4-диапазона. Пустой список легален; переданные CIDR'ы все равно
	// валидируются (host-bits=0, /16../28).
	for i, c := range s.V4CidrBlocks {
		if err := validateSubnetV4CIDR(fmt.Sprintf("v4_cidr_blocks[%d]", i), c); err != nil {
			return nil, err
		}
	}
	// v6_cidr_blocks — опциональны; если переданы, валидируем как IPv6 CIDR
	// (host-bits=0). Immutable после Create (как v4).
	for i, c := range s.V6CidrBlocks {
		if err := validateSubnetV6CIDR(fmt.Sprintf("v6_cidr_blocks[%d]", i), c); err != nil {
			return nil, err
		}
	}
	// Domain-self-validation: Name/Description/Labels валидируются через newtypes
	// внутри domain — use-case-слой не зовет corevalidate напрямую.
	if err := serviceerr.FromValidation(s.Validate()); err != nil {
		return nil, err
	}
	// VPC-1-43: dhcp_options снят by design — на Create не принимается/не валидируется.

	// Sync project.Exists precheck убран — он race-prone: между sync-проверкой и
	// async-частью project может быть удален peer-сервисом, и second-writer-wins
	// безусловно создавал ресурс. NotFound теперь возвращается через
	// `operation.error` из async `doCreate`. Sync uniqueness/overlap-проверки
	// (через DB-state в той же сервис-БД) остаются — они race-free относительно
	// peer-сервисов.
	//
	// Sync existence / uniqueness / overlap — все через single Reader-TX.
	rd, err := u.repo.Reader(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	parentNet, gerr := rd.Networks().Get(ctx, s.NetworkID)
	if gerr != nil {
		_ = rd.Close()
		if errors.Is(gerr, repo.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "Network %s not found", s.NetworkID)
		}
		return nil, serviceerr.MapRepoErr(gerr)
	}
	// BOLA-guard: parent Network обязана принадлежать проекту вызывающего. Иначе
	// caller создал бы Subnet, ссылающуюся на чужую сеть (cross-project reference).
	// Ответ — тот же NotFound, что для несуществующей сети (без existence-oracle:
	// «существует, но не твоя» неотличимо от «нет такой»).
	if parentNet.ProjectID != s.ProjectID {
		_ = rd.Close()
		return nil, status.Errorf(codes.NotFound, "Network %s not found", s.NetworkID)
	}
	// F7: каждый CIDR подсети обязан лежать в объявленном супернете сети
	// (within-service, против только что прочитанной network-строки). Пустой
	// супернет (legacy) → skip. Нарушение → InvalidArgument (format-класс), sync.
	if err := validateSubnetWithinSupernet(parentNet.IPv4CidrBlocks, parentNet.IPv6CidrBlocks, s.V4CidrBlocks, s.V6CidrBlocks); err != nil {
		_ = rd.Close()
		return nil, err
	}
	name := string(s.Name)
	if name != "" {
		existing, _, lerr := rd.Subnets().List(ctx, SubnetFilter{ProjectID: s.ProjectID, Name: name}, Pagination{})
		if lerr != nil {
			_ = rd.Close()
			return nil, serviceerr.MapRepoErr(lerr)
		}
		if len(existing) > 0 {
			_ = rd.Close()
			return nil, status.Errorf(codes.AlreadyExists, "Subnet with name %s already exists", name)
		}
	}
	if err := u.checkSubnetCIDROverlap(ctx, rd, s.ProjectID, s.NetworkID, s.V4CidrBlocks); err != nil {
		_ = rd.Close()
		return nil, err
	}
	_ = rd.Close()

	subID := ids.NewID(ids.PrefixSubnet)
	op, err := operations.NewFromContext(
		ctx,
		ids.PrefixOperationVPC,
		fmt.Sprintf("Create subnet %s", name),
		&vpcv1.CreateSubnetMetadata{SubnetId: subID},
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
	if err := operations.RunSync(ctx, u.opsRepo, &op, func(ctx context.Context) (*anypb.Any, error) {
		return u.doCreate(ctx, subID, s)
	}); err != nil {
		return nil, err
	}

	return &op, nil
}

// doCreate — async-часть Create (внутри Operation worker'а). Атомарный backstop:
// project-exists + parent network-exists + Insert (FK ограничения / EXCLUDE для
// overlap) + outbox-emit Subnet.CREATED — все в одной writer-TX.
func (u *CreateSubnetUseCase) doCreate(ctx context.Context, subID string, s domain.Subnet) (*anypb.Any, error) {
	exists, err := u.projectClient.Exists(ctx, s.ProjectID)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "project check: %v", err)
	}
	if !exists {
		return nil, status.Errorf(codes.NotFound, "Project %s not found", s.ProjectID)
	}

	s.ID = subID

	w, err := u.repo.Writer(ctx)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	defer w.Abort()

	// Parent network existence — повторная проверка в writer-TX (atomic backstop
	// — FK violation на subnets.network_id даст 23503; sync-check уже отверг бы).
	//
	// FOR SHARE (не plain Get): решение F7-backstop'а ниже зависит от супернета
	// этой строки, а супернет параллельно мутирует Network.Add/RemoveCidrBlocks
	// под `FOR UPDATE`. Share-lock конфликтует с ним, поэтому пара сериализуется
	// на DB-уровне (ban #10 — не software check-then-act): либо мы ждём writer'а
	// и перечитываем актуальный супернет, либо он ждёт нас и его ∉-guard видит
	// нашу закоммиченную подсеть. С plain Get оба проходили по своим снимкам и
	// подсеть оставалась вне объявленного адресного пространства сети.
	// Сам себе share-lock не конфликтует → параллельные Subnet.Create в одной
	// сети не сериализуются. Порядок захвата — network, затем subnet (INSERT
	// берёт FK KEY SHARE на уже залоченной нами строке — self-compatible), тот
	// же, что у Network.Delete → инверсии/дедлока нет.
	parentNet, gerr := w.Networks().GetForShare(ctx, s.NetworkID)
	if gerr != nil {
		return nil, status.Errorf(codes.NotFound, "Network %s not found", s.NetworkID)
	}
	// BOLA-guard (async backstop): parent Network обязана принадлежать проекту
	// вызывающего — тот же NotFound, что для отсутствующей сети (без oracle).
	if parentNet.ProjectID != s.ProjectID {
		return nil, status.Errorf(codes.NotFound, "Network %s not found", s.NetworkID)
	}
	// F7 backstop (writer-TX): супернет-принадлежность против актуальной
	// network-строки (супернет мог сузиться между sync-read и Insert).
	if err := validateSubnetWithinSupernet(parentNet.IPv4CidrBlocks, parentNet.IPv6CidrBlocks, s.V4CidrBlocks, s.V6CidrBlocks); err != nil {
		return nil, err
	}
	// F8 (VPC-1-37): подсеть без явного routeTableId ассоциируется с ЯВНЫМ
	// дефолтом сети `network.defaultRouteTableId°` — детерминированно, из строки
	// сети, прочитанной в ЭТОЙ ЖЕ writer-TX под share-lock'ом (не software
	// check-then-act поверх чужого снимка). Заменяет недетерминированный
	// legacy-выбор «самая ранняя RT сети» (триггер subnet_auto_pick_rt, снят
	// миграцией 0017). Явный routeTableId тенанта не перетирается; legacy-сеть
	// без дефолта → поле остаётся пустым (легальное состояние).
	if s.RouteTableID == "" && parentNet.DefaultRouteTableID != "" {
		s.RouteTableID = parentNet.DefaultRouteTableID
	}

	// Пересечения v4 CIDR в рамках одной сети ловятся атомарно DB-level EXCLUDE
	// constraint (subnets_no_overlap_v4, baseline 0001); pg-impl маппит
	// SQLSTATE 23P01 на ErrFailedPrecondition.
	created, err := w.Subnets().Insert(ctx, &s)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	if err := w.Outbox().Emit(ctx, "Subnet", created.ID, "CREATED", helpers.DomainToMap(created)); err != nil {
		return nil, serviceerr.MapRepoErr(fmt.Errorf("%w: outbox emit: %v", repo.ErrInternal, err))
	}
	// Публикуем intent на vpc_subnet→project hierarchy-tuple в той же writer-TX
	// (один commit, без dual-write). register-drainer применяет его через
	// kacho-iam. Intent несет subnet labels + parent_project_id, чтобы kacho-iam
	// материализовал resource_mirror для label-селектора.
	items := []fgaregister.Item{
		fgaregister.ProjectHierarchyItem(string(s.ProjectID), "vpc_subnet", created.ID,
			domain.LabelsToMap(created.Labels)),
	}
	if err := w.FGARegister().EmitRegister(ctx, fgaregister.RegisterItems(items...)); err != nil {
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
	// и подменяет сообщение всей цепочкой) на уже созданную подсеть, чей CIDR уже
	// занят EXCLUDE-ограничением, — фантом. Поэтому предупреждение, а не ошибка.
	if u.registrar != nil {
		if err := u.registrar.Register(ctx, items); err != nil {
			slog.WarnContext(ctx, "sync owner-tuple register failed; register-drainer will apply the durable intent",
				"resource", "Subnet", "id", created.ID, "err", err)
		}
	}
	return marshalSubnetRecord(created)
}

// checkSubnetCIDROverlap — sync FAILED_PRECONDITION "Subnet CIDRs can not
// overlap", если любой из запрошенных v4 CIDR пересекается с CIDR существующей
// подсети в той же сети/проекте. DB EXCLUDE constraint (subnets_no_overlap_v4,
// baseline 0001) остается атомарным backstop'ом в doCreate.
func (u *CreateSubnetUseCase) checkSubnetCIDROverlap(ctx context.Context, rd Reader, projectID, networkID string, v4 []string) error {
	if len(v4) == 0 {
		return nil
	}
	newPrefixes := make([]netip.Prefix, 0, len(v4))
	for _, c := range v4 {
		pr, err := netip.ParsePrefix(c)
		if err != nil {
			// host-bits / формат уже провалидированы выше; защищаемся на всякий случай.
			return serviceerr.InvalidArg("v4_cidr_blocks", "must be valid CIDR")
		}
		newPrefixes = append(newPrefixes, pr)
	}
	existing, _, err := rd.Subnets().List(ctx, SubnetFilter{ProjectID: projectID, NetworkID: networkID}, Pagination{})
	if err != nil {
		return serviceerr.MapRepoErr(err)
	}
	for _, sub := range existing {
		for _, raw := range sub.V4CidrBlocks {
			pr, perr := netip.ParsePrefix(raw)
			if perr != nil {
				continue
			}
			for _, np := range newPrefixes {
				if prefixesOverlap(pr, np) {
					return status.Errorf(codes.FailedPrecondition, "Subnet CIDRs can not overlap")
				}
			}
		}
	}
	return nil
}
