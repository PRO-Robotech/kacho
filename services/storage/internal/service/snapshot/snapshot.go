// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package snapshot — use-case ресурса Snapshot.
//
// Use-case слой: domain + порт Repo + peer IAMClient + corelib operations. Get/List
// — sync; Create/Update/Delete — async Operation (ban #9). source_volume_id —
// within-service ссылка на volumes (same-DB FK SET NULL); existence + READY-check
// делает repo атомарным INSERT…SELECT (не TOCTOU). project_id — cross-service →
// kacho-iam (peer-validate на request-path, fail-closed). immutable source_volume_id.
package snapshot

import (
	"context"
	"fmt"
	"log/slog"

	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	storagev1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/storage/v1"
	"github.com/PRO-Robotech/kacho/pkg/filter"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	"github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/storage/internal/domain"
	"github.com/PRO-Robotech/kacho/services/storage/internal/fgaregister"
	"github.com/PRO-Robotech/kacho/services/storage/internal/ports"
	"github.com/PRO-Robotech/kacho/services/storage/internal/protoconv"
)

// Pagination — вход для List: cursor-пагинация + project-scope + filter=name
// (listauthz энфорсится authz-слоем; repo скоупит по project_id).
type Pagination struct {
	PageSize  int64
	PageToken string
	ProjectID string
	Filter    string // name=<v> уже распарсен use-case-слоем в чистое значение
}

// SnapshotUpdate — резолвнутый набор mutable-изменений для Repo.Update. nil-поле →
// без изменения (COALESCE); LabelsSet различает «labels в маске» от «не трогать».
type SnapshotUpdate struct {
	Name        *string
	Description *string
	Labels      map[string]string
	LabelsSet   bool
}

// Repo — порт хранилища snapshots (Reader+Writer). CQRS-split — при необходимости
// read-replica.
type Repo interface {
	Get(ctx context.Context, id string) (*domain.Snapshot, error)
	List(ctx context.Context, p Pagination) ([]*domain.Snapshot, string, error)
	Insert(ctx context.Context, s *domain.Snapshot) (*domain.Snapshot, error)
	Update(ctx context.Context, id string, u SnapshotUpdate) (*domain.Snapshot, error)
	Delete(ctx context.Context, id string) error
}

// IAMClient — peer-валидация project_id через kacho-iam (fail-closed).
type IAMClient interface {
	EnsureProjectExists(ctx context.Context, projectID string) error
}

// ErrToStatus — инжектированный sentinel→gRPC-status mapper.
type ErrToStatus func(error) error

// knownUpdateFields — mutable-поля Snapshot.Update. Immutable (source_volume_id/
// project_id/size_bytes) НЕ входят — immutable-switch отвергает их раньше конвенц-
// сообщением, а не generic «unknown field».
var knownUpdateFields = map[string]struct{}{
	"name":        {},
	"description": {},
	"labels":      {},
}

// UseCase — бизнес-логика Snapshot поверх Repo, peer IAMClient, LRO-стека operations
// и инжектированного transport-mapper'а errStatus.
type UseCase struct {
	repo      Repo
	iam       IAMClient
	ops       operations.Repo
	errStatus ErrToStatus
	// registrar — синхронная регистрация owner-tuple после commit (immediate
	// анти-BOLA; nil → только async register-drainer). Инжектится WithRegistrar.
	registrar fgaregister.Registrar
}

// New собирает UseCase для Snapshot.
func New(repo Repo, iam IAMClient, ops operations.Repo, errStatus ErrToStatus) *UseCase {
	if errStatus == nil {
		errStatus = func(err error) error { return err }
	}
	return &UseCase{repo: repo, iam: iam, ops: ops, errStatus: errStatus}
}

// WithRegistrar подключает синхронный owner-tuple registrar (парити vpc / Volume):
// после Create-commit owner-grant регистрируется сразу для immediate анти-BOLA-резолва
// на свежий снапшот. Best-effort (durable outbox-intent + drainer — at-least-once
// backstop); nil → sync-путь пропускается (dev/no-iam).
func (u *UseCase) WithRegistrar(r fgaregister.Registrar) *UseCase {
	u.registrar = r
	return u
}

// registerOwnerTuple — best-effort синхронная регистрация owner-tuple после commit
// (ошибка не пробрасывается: register-drainer применит durable intent at-least-once).
func (u *UseCase) registerOwnerTuple(ctx context.Context, item fgaregister.Item) {
	if u.registrar == nil {
		return
	}
	if err := u.registrar.Register(ctx, []fgaregister.Item{item}); err != nil {
		slog.WarnContext(ctx, "sync owner-tuple register failed; async drainer will apply",
			"object", item.Tuple.Object, "err", err)
	}
}

// idInvalid — malformed snp-id первым стейтментом: sync InvalidArgument
// "invalid snapshot id '<X>'". well-formed-но-нет → NotFound (repo.Get).
func idInvalid(id string) error {
	if !ids.IsValid(id, domain.PrefixSnapshot) {
		return fmt.Errorf("%w: invalid snapshot id '%s'", ports.ErrInvalidArg, id)
	}
	return nil
}

// Get возвращает Snapshot по id (malformed → sync InvalidArgument первым стейтментом).
func (u *UseCase) Get(ctx context.Context, id string) (*domain.Snapshot, error) {
	if err := idInvalid(id); err != nil {
		return nil, u.errStatus(err)
	}
	s, err := u.repo.Get(ctx, id)
	if err != nil {
		return nil, u.errStatus(err)
	}
	return s, nil
}

// List возвращает снимки (cursor-пагинация; garbage page_size → InvalidArgument;
// filter=name whitelisted через corelib filter).
func (u *UseCase) List(ctx context.Context, p Pagination) ([]*domain.Snapshot, string, error) {
	// projectId — обязательный scope публичного List (in-service backstop к gateway
	// scope_extractor {project,project_id}): пустой projectId вернул бы строки ВСЕХ
	// проектов (repo сужает лишь при ProjectID!=""), поэтому отвергаем СИНХРОННО
	// первым стейтментом — кросс-проектной утечки нет by construction (INV-10;
	// docs/architecture/overview.md; acceptance CS1-S3-07/GAP-C).
	if p.ProjectID == "" {
		return nil, "", u.errStatus(fmt.Errorf("%w: projectId is required", ports.ErrInvalidArg))
	}
	size, err := validate.PageSize("page_size", p.PageSize)
	if err != nil {
		return nil, "", err
	}
	p.PageSize = size
	if p.Filter != "" {
		ast, ferr := filter.Parse(p.Filter, []string{"name"})
		if ferr != nil {
			return nil, "", u.errStatus(fmt.Errorf("%w: %s", ports.ErrInvalidArg, ferr.Error()))
		}
		p.Filter = ast.Value
	}
	snaps, next, err := u.repo.List(ctx, p)
	if err != nil {
		return nil, "", u.errStatus(err)
	}
	return snaps, next, nil
}

// Create создаёт Snapshot тома (async Operation). Sync-фаза: domain-validate
// (source_volume_id обязателен, name-длина) → project_id peer-validate (kacho-iam,
// fail-closed Unavailable). Async-worker: repo.Insert — атомарный INSERT…SELECT
// (source volume существует И state=READY; size_bytes=volumes.size_bytes; state→READY
// сразу). Не-READY/отсутствующий источник → Operation error FAILED_PRECONDITION.
func (u *UseCase) Create(ctx context.Context, s *domain.Snapshot) (*operations.Operation, error) {
	if err := s.Validate(); err != nil {
		return nil, u.errStatus(fmt.Errorf("%w: %s", ports.ErrInvalidArg, err.Error()))
	}
	if err := u.iam.EnsureProjectExists(ctx, s.ProjectID); err != nil {
		return nil, u.errStatus(err)
	}
	s.ID = ids.NewID(domain.PrefixSnapshot)
	op, err := operations.NewFromContext(ctx, domain.PrefixOperation,
		fmt.Sprintf("Create snapshot %s", s.ID),
		&storagev1.CreateSnapshotMetadata{SnapshotId: s.ID, SourceVolumeId: s.SourceVolumeID})
	if err != nil {
		return nil, err
	}
	op.ResourceID = s.ID
	if err := u.ops.Create(ctx, op); err != nil {
		return nil, err
	}
	created := *s
	operations.Run(ctx, u.ops, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		res, derr := u.repo.Insert(ctx, &created)
		if derr != nil {
			return nil, u.errStatus(derr)
		}
		// owner-tuple: durable register-intent уже в writer-TX (repo); синхронно
		// регистрируем для immediate анти-BOLA-резолва (best-effort, backstop — drainer).
		u.registerOwnerTuple(ctx, fgaregister.SnapshotItem(res.ProjectID, res.ID, res.Labels))
		return marshalSnapshot(res)
	})
	return &op, nil
}

// Update меняет mutable-поля Snapshot (async Operation). Sync-фаза: malformed-id
// первым стейтментом → immutable-switch (ДО UpdateMask, api-conventions gotcha) →
// UpdateMask known-set. Пустой mask → full-object PATCH (immutable из тела нет —
// UpdateSnapshotRequest их не несёт). Async: repo.Update (0-row → NotFound).
func (u *UseCase) Update(ctx context.Context, id string, mask []string, name, description string, labels map[string]string) (*operations.Operation, error) {
	if err := idInvalid(id); err != nil {
		return nil, u.errStatus(err)
	}
	for _, p := range mask {
		switch p {
		case "source_volume_id", "project_id", "size_bytes":
			return nil, u.errStatus(fmt.Errorf("%w: %s is immutable after Snapshot.Create", ports.ErrInvalidArg, p))
		}
	}
	if err := validate.UpdateMask("update_mask", mask, knownUpdateFields); err != nil {
		return nil, err
	}
	upd := resolveUpdate(mask, name, description, labels)
	op, err := operations.NewFromContext(ctx, domain.PrefixOperation,
		fmt.Sprintf("Update snapshot %s", id),
		&storagev1.UpdateSnapshotMetadata{SnapshotId: id})
	if err != nil {
		return nil, err
	}
	op.ResourceID = id
	if err := u.ops.Create(ctx, op); err != nil {
		return nil, err
	}
	operations.Run(ctx, u.ops, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		res, derr := u.repo.Update(ctx, id, upd)
		if derr != nil {
			return nil, u.errStatus(derr)
		}
		return marshalSnapshot(res)
	})
	return &op, nil
}

// Delete удаляет Snapshot (async Operation). Malformed-id → sync InvalidArgument.
// Ссылки со стороны volumes (source_snapshot_id) НЕ блокируют — FK SET NULL (§1.2,
// S1-09). Успех → response=Empty.
func (u *UseCase) Delete(ctx context.Context, id string) (*operations.Operation, error) {
	if err := idInvalid(id); err != nil {
		return nil, u.errStatus(err)
	}
	op, err := operations.NewFromContext(ctx, domain.PrefixOperation,
		fmt.Sprintf("Delete snapshot %s", id),
		&storagev1.DeleteSnapshotMetadata{SnapshotId: id})
	if err != nil {
		return nil, err
	}
	op.ResourceID = id
	if err := u.ops.Create(ctx, op); err != nil {
		return nil, err
	}
	operations.Run(ctx, u.ops, op.ID, func(ctx context.Context) (*anypb.Any, error) {
		if derr := u.repo.Delete(ctx, id); derr != nil {
			return nil, u.errStatus(derr)
		}
		return anypb.New(&emptypb.Empty{})
	})
	return &op, nil
}

// resolveUpdate резолвит mutable-изменения из mask + тела. Пустой mask → full-object
// PATCH (все mutable из тела). Непустой mask → только перечисленные поля.
func resolveUpdate(mask []string, name, description string, labels map[string]string) SnapshotUpdate {
	var u SnapshotUpdate
	apply := func(field string) bool {
		if len(mask) == 0 {
			return true // full-object PATCH
		}
		for _, m := range mask {
			if m == field {
				return true
			}
		}
		return false
	}
	if apply("name") {
		n := name
		u.Name = &n
	}
	if apply("description") {
		d := description
		u.Description = &d
	}
	if apply("labels") {
		u.Labels = labels
		u.LabelsSet = true
	}
	return u
}

// marshalSnapshot упаковывает domain.Snapshot в Operation.response через единый
// protoconv.Snapshot (та же проекция, что handler — без дрейфа полей).
func marshalSnapshot(s *domain.Snapshot) (*anypb.Any, error) {
	return anypb.New(protoconv.Snapshot(s))
}
