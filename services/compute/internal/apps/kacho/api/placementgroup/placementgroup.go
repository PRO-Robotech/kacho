// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package placementgroup — жизненный цикл правил взаимного размещения машин.
package placementgroup

import (
	"context"

	"google.golang.org/protobuf/types/known/anypb"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"

	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/ownerregister"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/shared/lro"
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/shared/ownersync"
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/shared/peercheck"
	"github.com/PRO-Robotech/kacho/services/compute/internal/apps/kacho/shared/serviceerr"
	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
	"github.com/PRO-Robotech/kacho/services/compute/internal/protoconv"
)

const (
	groupResource = "PlacementGroup"
	groupPrefix   = "plg"
)

// Repo — порт хранения групп.
type Repo interface {
	Get(ctx context.Context, id string) (*domain.PlacementGroup, error)
	List(ctx context.Context, projectID, filterExpr string, p ports.Pagination) ([]*domain.PlacementGroup, string, error)
	Insert(ctx context.Context, g *domain.PlacementGroup) (*domain.PlacementGroup, []ownerregister.Registration, error)
	Update(ctx context.Context, id string, u ports.PlacementGroupUpdate) (*domain.PlacementGroup, error)
	Delete(ctx context.Context, id string) error
}

// ZoneRegistry — владелец Geography: существование зоны и её регион.
type ZoneRegistry = ports.ZoneRegistry

// RegionRegistry — существование региона у владельца Geography.
type RegionRegistry interface {
	GetRegion(ctx context.Context, regionID string) error
}

// Service — use-case групп размещения.
type Service struct {
	repo      Repo
	opsRepo   operations.Repo
	projects  ports.ProjectClient
	zones     ZoneRegistry
	regions   RegionRegistry
	registrar ports.OwnerRegistrar
	// quota — совещательная полоса учёта (порт QuotaGuard).
	//
	// nil означает «раннего отказа нет», а НЕ «предела нет»: место по-прежнему
	// занимает триггер в writer-транзакции. Но материализация строк учёта висит
	// здесь же, поэтому на поднятом стенде провязка ОБЯЗАТЕЛЬНА: без неё
	// триггеру нечего списывать и первая же вставка отвергается «потолок не
	// назван».
	quota QuotaGuard
}

// WithQuotaGuard подключает совещательную полосу учёта.
func (s *Service) WithQuotaGuard(g QuotaGuard) *Service {
	s.quota = g
	return s
}

// NewService собирает use-case.
func NewService(r Repo, ops operations.Repo, projects ports.ProjectClient,
	zones ZoneRegistry, regions RegionRegistry,
) *Service {
	return &Service{repo: r, opsRepo: ops, projects: projects, zones: zones, regions: regions}
}

// WithOwnerRegistrar подключает синхронную половину регистрации прав.
func (s *Service) WithOwnerRegistrar(r ports.OwnerRegistrar) *Service {
	s.registrar = r
	return s
}

// Get возвращает группу.
func (s *Service) Get(ctx context.Context, id string) (*domain.PlacementGroup, error) {
	if err := corevalidate.ResourceID(groupResource, groupPrefix, id); err != nil {
		return nil, err
	}
	g, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, serviceerr.MapRepoErr(err)
	}
	return g, nil
}

// List возвращает страницу групп проекта.
//
// Проверка постраничности идёт ПЕРВОЙ — до любого решения о видимости.
func (s *Service) List(ctx context.Context, projectID, filterExpr string, p ports.Pagination) ([]*domain.PlacementGroup, string, error) {
	if err := ValidateListPagination(p); err != nil {
		return nil, "", err
	}
	if projectID == "" {
		return nil, "", serviceerr.InvalidArg("project_id", "projectId is required")
	}
	out, next, err := s.repo.List(ctx, projectID, filterExpr, p)
	if err != nil {
		return nil, "", serviceerr.MapRepoErr(err)
	}
	return out, next, nil
}

// ValidateListPagination проверяет курсор и размер страницы.
//
// Вынесена отдельно, чтобы её мог позвать и транспортный слой: свойство
// «формат проверен до решения о видимости» обязано держаться в той же функции,
// которая замыкается на пустом гранте.
func ValidateListPagination(p ports.Pagination) error {
	_, err := corevalidate.PageSize("page_size", p.PageSize)
	return err
}

// CreateReq — вход создания.
type CreateReq struct {
	ProjectID     string
	Name          string
	Description   string
	Labels        map[string]string
	Strategy      domain.PlacementStrategy
	PlacementType domain.PlacementAnchorType
	ZoneID        string
	RegionID      string
}

// Create заводит группу.
//
// # Якорь взаимоисключающий, и это проверяется здесь по имени поля
//
// Схема то же самое держит ограничением — и держит по-настоящему, потому что
// мимо неё не пройдёт ни один путь. Но её отказ не называет поля, поэтому
// вызывающий узнал бы, что «что-то не так со строкой», вместо того что он задал
// две координаты сразу.
func (s *Service) Create(ctx context.Context, req CreateReq) (*operations.Operation, error) {
	if req.ProjectID == "" {
		return nil, serviceerr.InvalidArg("project_id", "projectId is required")
	}
	if err := corevalidate.NameOnCreate("name", req.Name); err != nil {
		return nil, err
	}
	if err := corevalidate.Description("description", req.Description); err != nil {
		return nil, err
	}
	if err := corevalidate.Labels("labels", req.Labels); err != nil {
		return nil, err
	}
	if req.Strategy.StrategyName() == "" {
		return nil, serviceerr.InvalidArg("strategy",
			"strategy is required: a group without one describes no intent (SPREAD or PACK)")
	}

	switch req.PlacementType {
	case domain.PlacementTypeZonal:
		if req.ZoneID == "" {
			return nil, serviceerr.InvalidArg("zone_id", "zoneId is required for a ZONAL placement group")
		}
		if req.RegionID != "" {
			return nil, serviceerr.InvalidArg("region_id",
				"regionId must be empty for a ZONAL placement group: a group is anchored by exactly one coordinate")
		}
		if err := s.zones.GetZone(ctx, req.ZoneID); err != nil {
			return nil, serviceerr.MapZoneRefErr(err, req.ZoneID)
		}
	case domain.PlacementTypeRegional:
		if req.RegionID == "" {
			return nil, serviceerr.InvalidArg("region_id", "regionId is required for a REGIONAL placement group")
		}
		if req.ZoneID != "" {
			return nil, serviceerr.InvalidArg("zone_id",
				"zoneId must be empty for a REGIONAL placement group: a group is anchored by exactly one coordinate")
		}
		if err := s.regions.GetRegion(ctx, req.RegionID); err != nil {
			return nil, serviceerr.MapZoneRefErr(err, req.RegionID)
		}
	default:
		return nil, serviceerr.InvalidArg("placement_type",
			"placementType is required and must be ZONAL or REGIONAL")
	}

	if err := peercheck.Project(ctx, s.projects, req.ProjectID); err != nil {
		return nil, err
	}

	// Учёт числа ресурсов: ранний отказ ДО создания операции. Здесь же
	// материализуются строки учёта, если проект их ещё не имеет, — момент, когда
	// владелец типа впервые узнаёт о проекте, и есть обращение к нему.
	//
	// После проверки проекта, а не до неё: резолв величин спрашивает у соседа
	// аккаунт проекта, и на несуществующем проекте отказ обязан называть проект,
	// а не пределы.
	if s.quota != nil {
		if err := s.quota.Admit(ctx, req.ProjectID, "compute.placementGroup"); err != nil {
			return nil, err
		}
	}

	groupID := ids.NewHyphenID(groupPrefix)
	g := &domain.PlacementGroup{
		ID:        groupID,
		ProjectID: req.ProjectID,
		// Пустое имя не доживает до записи: ресурса без имени не бывает.
		Name:          corevalidate.NameOrDefault(req.Name, groupID),
		Description:   req.Description,
		Labels:        req.Labels,
		Strategy:      req.Strategy,
		PlacementType: req.PlacementType,
		ZoneID:        req.ZoneID,
		RegionID:      req.RegionID,
	}
	return lro.RunOp(ctx, s.opsRepo, "Create placement group "+g.Name,
		&computev1.CreatePlacementGroupMetadata{PlacementGroupId: g.ID},
		func(ctx context.Context) (*anypb.Any, error) {
			created, regs, err := s.repo.Insert(ctx, g)
			if err != nil {
				return nil, serviceerr.MapRepoErr(err)
			}
			ownersync.Register(ctx, s.registrar, regs)
			return anypb.New(protoconv.PlacementGroup(created))
		})
}

// UpdateReq — вход правки.
type UpdateReq struct {
	ID          string
	UpdateMask  []string
	Name        string
	Description string
	Labels      map[string]string
}

var groupMutable = []string{"name", "description", "labels"}

var groupUpdateKnown = map[string]struct{}{"name": {}, "description": {}, "labels": {}}

// Update правит косметическую часть группы.
//
// Неизменяемое отвергается ДО проверки маски по известному набору: известный
// набор их не содержит, поэтому обратный порядок вернул бы общее «неизвестное
// поле», и вызывающий, назвавший стратегию, узнал бы, что такого поля нет.
func (s *Service) Update(ctx context.Context, req UpdateReq) (*operations.Operation, error) {
	if err := corevalidate.ResourceID(groupResource, groupPrefix, req.ID); err != nil {
		return nil, err
	}
	for _, f := range req.UpdateMask {
		switch f {
		case "strategy", "placement_type", "zone_id", "region_id", "project_id", "id", "created_at":
			return nil, serviceerr.InvalidArg(f, f+" is immutable after PlacementGroup.Create")
		}
	}
	if err := corevalidate.UpdateMask("update_mask", req.UpdateMask, groupUpdateKnown); err != nil {
		return nil, err
	}

	applied := req.UpdateMask
	if len(applied) == 0 {
		applied = groupMutable
	}
	var upd ports.PlacementGroupUpdate
	for _, f := range applied {
		switch f {
		case "name":
			// Решение об имени принимает ЕДИНСТВЕННАЯ функция дерева: пять исходов
			// маски и значения. Пустое имя при ПУСТОЙ маске означает «не прислали»,
			// при названной маске — «сними имя», а снять его нельзя.
			applyName, nerr := corevalidate.NameOnUpdate("name", req.UpdateMask, req.Name)
			if nerr != nil {
				return nil, nerr
			}
			if !applyName {
				continue
			}
			name := req.Name
			upd.Name = &name
		case "description":
			if err := corevalidate.Description("description", req.Description); err != nil {
				return nil, err
			}
			desc := req.Description
			upd.Description = &desc
		case "labels":
			if err := corevalidate.Labels("labels", req.Labels); err != nil {
				return nil, err
			}
			upd.Labels, upd.LabelsSet = req.Labels, true
		}
	}

	return lro.RunOp(ctx, s.opsRepo, "Update placement group "+req.ID,
		&computev1.UpdatePlacementGroupMetadata{PlacementGroupId: req.ID},
		func(ctx context.Context) (*anypb.Any, error) {
			updated, err := s.repo.Update(ctx, req.ID, upd)
			if err != nil {
				return nil, serviceerr.MapRepoErr(err)
			}
			return anypb.New(protoconv.PlacementGroup(updated))
		})
}

// Delete снимает группу.
func (s *Service) Delete(ctx context.Context, id string) (*operations.Operation, error) {
	if err := corevalidate.ResourceID(groupResource, groupPrefix, id); err != nil {
		return nil, err
	}
	return lro.RunOp(ctx, s.opsRepo, "Delete placement group "+id,
		&computev1.DeletePlacementGroupMetadata{PlacementGroupId: id},
		func(ctx context.Context) (*anypb.Any, error) {
			if err := s.repo.Delete(ctx, id); err != nil {
				return nil, serviceerr.MapRepoErr(err)
			}
			return nil, nil
		})
}

// ListOperations возвращает операции над группой.
//
// Существование проверяется ПЕРВЫМ: иначе список операций несуществующей группы
// отдавал бы пустую страницу — ответ, неотличимый от «операций не было».
func (s *Service) ListOperations(ctx context.Context, id string, p ports.Pagination) ([]operations.Operation, string, error) {
	if err := corevalidate.ResourceID(groupResource, groupPrefix, id); err != nil {
		return nil, "", err
	}
	if _, err := s.repo.Get(ctx, id); err != nil {
		return nil, "", serviceerr.MapRepoErr(err)
	}
	return operations.ListForCaller(ctx, s.opsRepo,
		operations.ListFilter{ResourceID: id, PageSize: p.PageSize, PageToken: p.PageToken})
}

// QuotaGuard — совещательная полоса учёта числа ресурсов.
//
// Порт объявлен здесь, у вызывающего; реализация — `apps/kacho/shared/quota`.
//
// Полоса НЕ ПРИНИМАЕТ решения: между её ответом и вставкой помещается чужая
// запись, и место занимает атомарное списание триггера в writer-транзакции
// — чтение с последующим сравнением не даёт
// блокировки строки. Она существует ради РАННЕГО отказа — иначе исчерпание предела
// наблюдается как «операция принята и упала через секунду», — и ради
// материализации строк учёта на промахе: без неё триггеру нечего списывать.
type QuotaGuard interface {
	Admit(ctx context.Context, projectID, kind string) error
}
