// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package disktype — use-case ресурса DiskType.
//
// Публичный DiskTypeService.Get/List — read-only (sync). Admin CRUD —
// InternalDiskTypeService на :9091 (Create/Update/Delete СИНХРОННЫ, возвращают
// ресурс, не Operation: admin-справочник не нуждается в LRO). id — admin-assigned
// slug; zone_ids — cross-service ссылки на geo.Zone (без FK).
package disktype

import (
	"context"
	"fmt"

	"github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
)

// Pagination — вход для List с cursor-пагинацией.
type Pagination struct {
	PageSize  int64
	PageToken string
}

// Repo — порт хранилища disk_types (публичный read + admin write). Update —
// full-replace (proto UpdateDiskTypeRequest без FieldMask → тело замещает все
// mutable-поля целиком).
type Repo interface {
	Get(ctx context.Context, id string) (*domain.DiskType, error)
	List(ctx context.Context, p Pagination) ([]*domain.DiskType, string, error)
	Insert(ctx context.Context, d *domain.DiskType) (*domain.DiskType, error)
	Update(ctx context.Context, id, name, description string, zoneIDs []string, performanceTier string) (*domain.DiskType, error)
	Delete(ctx context.Context, id string) error
}

// UseCase — бизнес-логика DiskType.
type UseCase struct {
	repo Repo
}

// New собирает UseCase для DiskType.
func New(repo Repo) *UseCase { return &UseCase{repo: repo} }

// Get возвращает DiskType по id (public read).
func (u *UseCase) Get(ctx context.Context, id string) (*domain.DiskType, error) {
	return u.repo.Get(ctx, id)
}

// List возвращает типы дисков (public read, cursor-пагинация; garbage page_size →
// InvalidArgument).
func (u *UseCase) List(ctx context.Context, p Pagination) ([]*domain.DiskType, string, error) {
	size, err := validate.PageSize("page_size", p.PageSize)
	if err != nil {
		return nil, "", err
	}
	p.PageSize = size
	return u.repo.List(ctx, p)
}

// CreateAdmin создаёт DiskType (Internal :9091, sync). Self-validating domain: пустой
// id / слишком длинный name → InvalidArgument ДО repo (иначе ” — валидный PK-slug).
func (u *UseCase) CreateAdmin(ctx context.Context, d *domain.DiskType) (*domain.DiskType, error) {
	if err := d.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", ports.ErrInvalidArg, err.Error())
	}
	return u.repo.Insert(ctx, d)
}

// UpdateAdmin меняет mutable-поля DiskType (Internal :9091, sync, full-replace).
// Self-validating domain (парити с CreateAdmin): пустой id / over-long name →
// InvalidArgument ДО repo, а не только через DB-CHECK.
func (u *UseCase) UpdateAdmin(ctx context.Context, id, name, description string, zoneIDs []string, performanceTier string) (*domain.DiskType, error) {
	d := domain.DiskType{ID: id, Name: name, Description: description, ZoneIDs: zoneIDs, PerformanceTier: performanceTier}
	if err := d.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", ports.ErrInvalidArg, err.Error())
	}
	return u.repo.Update(ctx, id, name, description, zoneIDs, performanceTier)
}

// DeleteAdmin удаляет DiskType (Internal :9091, sync).
func (u *UseCase) DeleteAdmin(ctx context.Context, id string) error {
	return u.repo.Delete(ctx, id)
}
