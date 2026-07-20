// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package service

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	computev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/compute/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/compute/internal/domain"
	"github.com/PRO-Robotech/kacho/services/compute/internal/protoconv"
)

// MachineTypeService — бизнес-логика каталога machine-type (COMP-1 F7). Read
// (Get/List) — sync; admin-мутации (Create/Update/Delete) — async Operation
// (InternalMachineTypeService, :9091, system_admin). Каталог cluster-scoped:
// read ambient (project-scope EXEMPT), listauthz не применяется.
type MachineTypeService struct {
	repo    MachineTypeRepo
	opsRepo operations.Repo
}

// NewMachineTypeService создаёт MachineTypeService.
func NewMachineTypeService(repo MachineTypeRepo, opsRepo operations.Repo) *MachineTypeService {
	return &MachineTypeService{repo: repo, opsRepo: opsRepo}
}

// CreateMachineTypeReq — admin-запрос на создание machine-type (домен-типы;
// handler конвертит proto-enum'ы → domain).
type CreateMachineTypeReq struct {
	Name               string
	Description        string
	Family             domain.MachineTypeFamily
	EffectiveResources domain.EffectiveResources
	AvailableZones     []string
	Status             domain.MachineTypeStatus
	Labels             map[string]string
}

// UpdateMachineTypeReq — admin-запрос на обновление machine-type.
type UpdateMachineTypeReq struct {
	ID                 string
	Description        string
	Family             domain.MachineTypeFamily
	EffectiveResources domain.EffectiveResources
	AvailableZones     []string
	Status             domain.MachineTypeStatus
	Labels             map[string]string
	UpdateMask         []string
}

// Get возвращает machine-type по id. malformed id → sync InvalidArgument первым
// стейтментом (COMP-1-20); well-formed-но-нет → NotFound через repo.Get.
func (s *MachineTypeService) Get(ctx context.Context, id string) (*domain.MachineType, error) {
	if err := corevalidate.ResourceID("machine type", ids.PrefixMachineTypeHyphen, id); err != nil {
		return nil, err
	}
	mt, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return mt, nil
}

// List возвращает machine-type с пагинацией + whitelist-фильтрами. Ambient каталог
// (без project-scope) — валидация pagination выполняется repo (page_size/token).
func (s *MachineTypeService) List(ctx context.Context, f MachineTypeFilter, p Pagination) ([]*domain.MachineType, string, error) {
	return s.repo.List(ctx, f, p)
}

// Create инициирует создание machine-type (admin-only, async Operation).
func (s *MachineTypeService) Create(ctx context.Context, req CreateMachineTypeReq) (*operations.Operation, error) {
	if err := corevalidate.NameCompute("name", req.Name); err != nil {
		return nil, err
	}
	if req.Name == "" {
		return nil, invalidArg("name", "name is required")
	}
	if !req.Family.Valid() {
		return nil, invalidArg("family", "family is required (STANDARD, COMPUTE, MEMORY or GPU)")
	}
	// status по умолчанию AVAILABLE, если не задан (proto-контракт).
	st := req.Status
	if st == domain.MachineTypeStatusUnspecified {
		st = domain.MachineTypeStatusAvailable
	}
	if !st.Valid() {
		return nil, invalidArg("status", "unknown status")
	}
	if err := validateEffectiveResources(req.EffectiveResources); err != nil {
		return nil, err
	}
	if err := corevalidate.Labels("labels", req.Labels); err != nil {
		return nil, err
	}

	mtID := ids.NewHyphenID(ids.PrefixMachineTypeHyphen)
	mt := &domain.MachineType{
		ID:                 mtID,
		Name:               req.Name,
		Description:        req.Description,
		Family:             req.Family,
		EffectiveResources: req.EffectiveResources,
		AvailableZones:     req.AvailableZones,
		Status:             st,
		Labels:             req.Labels,
	}
	return runOp(ctx, s.opsRepo, fmt.Sprintf("Create machine type %s", req.Name),
		&computev1.CreateMachineTypeMetadata{MachineTypeId: mtID},
		func(ctx context.Context) (*anypb.Any, error) {
			created, err := s.repo.Insert(ctx, mt)
			if err != nil {
				return nil, mapRepoErr(err)
			}
			return anypb.New(protoconv.MachineType(created))
		})
}

// Update инициирует обновление machine-type (admin-only, async). Sync-валидация
// маски (immutable-check ДО UpdateMask; field-validate) — до создания Operation.
func (s *MachineTypeService) Update(ctx context.Context, req UpdateMachineTypeReq) (*operations.Operation, error) {
	if err := corevalidate.ResourceID("machine type", ids.PrefixMachineTypeHyphen, req.ID); err != nil {
		return nil, err
	}
	if err := s.validateUpdate(req); err != nil {
		return nil, err
	}
	return runOp(ctx, s.opsRepo, fmt.Sprintf("Update machine type %s", req.ID),
		&computev1.UpdateMachineTypeMetadata{MachineTypeId: req.ID},
		func(ctx context.Context) (*anypb.Any, error) {
			mt, err := s.repo.Get(ctx, req.ID)
			if err != nil {
				return nil, mapRepoErr(err)
			}
			applyMachineTypeUpdate(mt, req)
			updated, err := s.repo.Update(ctx, mt)
			if err != nil {
				return nil, mapRepoErr(err)
			}
			return anypb.New(protoconv.MachineType(updated))
		})
}

// Delete инициирует удаление machine-type (admin-only, async).
func (s *MachineTypeService) Delete(ctx context.Context, id string) (*operations.Operation, error) {
	if err := corevalidate.ResourceID("machine type", ids.PrefixMachineTypeHyphen, id); err != nil {
		return nil, err
	}
	return runOp(ctx, s.opsRepo, fmt.Sprintf("Delete machine type %s", id),
		&computev1.DeleteMachineTypeMetadata{MachineTypeId: id},
		func(ctx context.Context) (*anypb.Any, error) {
			if err := s.repo.Delete(ctx, id); err != nil {
				return nil, mapRepoErr(err)
			}
			return anypb.New(&emptypb.Empty{})
		})
}

// machineTypeUpdateKnown — known-set маски Update (snake_case proto-пути). name/id
// immutable (не в наборе — immutable-check срабатывает первым).
var machineTypeUpdateKnown = map[string]struct{}{
	"description": {}, "family": {}, "effective_resources": {},
	"available_zones": {}, "status": {}, "labels": {},
}

// validateUpdate — sync-валидация маски: immutable-check ДО UpdateMask (иначе
// UpdateMask вернул бы generic "unknown field" вместо конвенционного immutable-текста),
// затем known-set + field-validate замаскированных полей.
func (s *MachineTypeService) validateUpdate(req UpdateMachineTypeReq) error {
	for _, f := range req.UpdateMask {
		switch f {
		case "name", "id", "created_at":
			return invalidArg(f, f+" is immutable after MachineType.Create")
		}
	}
	if err := corevalidate.UpdateMask("update_mask", req.UpdateMask, machineTypeUpdateKnown); err != nil {
		return err
	}
	for _, f := range req.UpdateMask {
		switch f {
		case "family":
			if !req.Family.Valid() {
				return invalidArg("family", "family is required (STANDARD, COMPUTE, MEMORY or GPU)")
			}
		case "status":
			if !req.Status.Valid() {
				return invalidArg("status", "unknown status")
			}
		case "effective_resources":
			if err := validateEffectiveResources(req.EffectiveResources); err != nil {
				return err
			}
		case "labels":
			if err := corevalidate.Labels("labels", req.Labels); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyMachineTypeUpdate применяет замаскированные (или все mutable при пустой
// маске — full-object PATCH) поля к загруженному mt. name/id immutable — не трогаются.
func applyMachineTypeUpdate(mt *domain.MachineType, req UpdateMachineTypeReq) {
	updates := req.UpdateMask
	if len(updates) == 0 {
		updates = []string{"description", "family", "effective_resources", "available_zones", "status", "labels"}
	}
	for _, f := range updates {
		switch f {
		case "description":
			mt.Description = req.Description
		case "family":
			mt.Family = req.Family
		case "effective_resources":
			mt.EffectiveResources = req.EffectiveResources
		case "available_zones":
			mt.AvailableZones = req.AvailableZones
		case "status":
			mt.Status = req.Status
		case "labels":
			mt.Labels = req.Labels
		}
	}
}

// validateEffectiveResources — sizing обязателен и положителен: vCpu>0, memoryMiB>0
// (память в MiB, human-scale). GPU-count = гранулярность каталога (не поле запроса).
func validateEffectiveResources(r domain.EffectiveResources) error {
	if r.VCPU <= 0 {
		return invalidArg("effective_resources.v_cpu", "v_cpu must be > 0")
	}
	if r.MemoryMiB <= 0 {
		return invalidArg("effective_resources.memory_mib", "memory_mib must be > 0 (MiB)")
	}
	if r.GPUs < 0 {
		return invalidArg("effective_resources.gpus", "gpus must be >= 0")
	}
	return nil
}
