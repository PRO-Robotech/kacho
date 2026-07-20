// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package registry

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/protobuf/types/known/anypb"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// Create — async создание реестра. Sync-часть: domain-валидация, cross-domain
// project-existence через iam (fail-closed → Unavailable), id-gen (prefix "reg"),
// атомарный INSERT registries + owner-tuple register-intent в registry_outbox
// ОДНОЙ writer-tx. Async worker (с проброшенным principal): lazy zot-namespace
// (репо появляется на push, создавать нечего) + финализация Operation ресурсом.
//
// Порядок negatives (строка/tuple/namespace НЕ появляются): authz-Check (interceptor)
// → domain-валидация → project-existence → INSERT. Всё до INSERT — sync-reject.
func (u *UseCase) Create(ctx context.Context, spec CreateSpec) (*operations.Operation, error) {
	if err := u.assertWired(); err != nil {
		return nil, err
	}

	if spec.ProjectID == "" {
		return nil, failInvalidArg("projectId is required")
	}
	if spec.RegionID == "" {
		// REG-1 F4/REG-1-15: regionId обязателен на Create (optional-server-default
		// отложен — источник дефолт-региона не определён). Первым sync-стейтментом,
		// операция НЕ создаётся.
		return nil, failInvalidArg("regionId is required")
	}
	if err := corevalidate.Labels("labels", spec.Labels); err != nil {
		return nil, err
	}

	// F3 two-identity: globalSlug опущен → сервер деривит default-slug (глобально-
	// уникален by construction). Opt-in bare-global slug используется как есть —
	// авторитетная уникальность — DB partial UNIQUE(global_slug) (REG-1-12);
	// probe-then-insert TOCTOU не строим (ban #10).
	globalSlug := spec.GlobalSlug
	if globalSlug == "" {
		globalSlug = deriveDefaultGlobalSlug(spec.ProjectID, spec.Name)
	}

	reg := &domain.Namespace{
		ID:          ids.NewHyphenID(ids.PrefixNamespace),
		ProjectID:   spec.ProjectID,
		Name:        spec.Name,
		Description: spec.Description,
		Labels:      spec.Labels,
		Status:      domain.NamespaceStatusActive,
		RegionID:    spec.RegionID,
		GlobalSlug:  globalSlug,
	}
	// Self-validating domain: name DNS-safe (OCI-namespace segment), status,
	// project_id. Ошибка → InvalidArgument (каноничный "Illegal argument"-класс).
	if err := reg.Validate(); err != nil {
		return nil, failInvalidArg("Illegal argument: %s", err.Error())
	}

	// Cross-domain existence project'а на request-path:
	// not-found → InvalidArgument; iam недоступен → Unavailable (мутация fail-closed).
	if err := u.iam.ProjectExists(ctx, spec.ProjectID); err != nil {
		return nil, projectExistsErr(spec.ProjectID, err)
	}

	// Cross-domain existence региона (geo.v1.RegionService.Get) — новое ребро
	// registry→geo (REG-1 F4). not-found → InvalidArgument; geo недоступен →
	// Unavailable (мутация fail-closed, REG-1-17). Per-call deadline — в geo-adapter'е.
	if err := u.geo.RegionExists(ctx, spec.RegionID); err != nil {
		return nil, regionExistsErr(spec.RegionID, err)
	}

	// Principal захватывается в sync-ctx (реальный вызывающий от interceptor'а) —
	// нужен и для owner-tuple, и для worker'а (иначе authz_no_principal).
	principal := operations.PrincipalFromContext(ctx)
	intent := domain.RegisterIntentForCreate(reg, principal.Type, principal.ID)

	// LRO-ordering (CWE-662): pending-Operation персистится ПЕРВЫМ,
	// затем — durable INSERT. Иначе Insert-commit с последующим сбоем Operation-create
	// (две разные транзакции) оставил бы закоммиченный ресурс + owner-tuple без
	// сопутствующего Operation-envelope (осиротевший ресурс). reg.ID/reg.Name уже
	// известны (id сгенерирован выше) — метадату можно построить до INSERT.
	op, err := operations.NewFromContext(ctx,
		ids.PrefixOperationReg,
		fmt.Sprintf("Create Namespace %s", reg.Name),
		&registryv1.CreateNamespaceMetadata{NamespaceId: reg.ID},
	)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if err := u.ops.CreateWithPrincipal(ctx, op, principal); err != nil {
		return nil, mapRepoErr(err)
	}

	// Атомарно: строка registry + register-intent (project-tuple ПЕРВЫМ, затем
	// owner-tuple) в одной writer-tx. partial UNIQUE(project_id,name)WHERE
	// status<>'DELETING' → 23505 → ALREADY_EXISTS с именем. INSERT — синхронно
	// (сохраняет sync-reject семантику REG-04): дубликат → немедленный gRPC
	// AlreadyExists клиенту, а не async-Operation с error. При ошибке INSERT
	// уже созданный pending-Operation финализируется как failed (не оставляем
	// подвисший done=false envelope, который клиент поллил бы вечно).
	created, err := u.writer.Insert(ctx, reg, intent)
	if err != nil {
		syncErr := mapRepoErr(err)
		if errors.Is(err, regerrors.ErrAlreadyExists) {
			syncErr = alreadyExistsErr(spec, reg.Name)
		}
		// Финализируем осиротевший pending-Operation тем же статусом (worker переведёт
		// его в done=true+error). Клиент всё равно получает sync-ошибку ниже.
		finalErr := syncErr
		operations.Run(ctx, u.ops, op.ID, func(_ context.Context) (*anypb.Any, error) {
			return nil, finalErr
		})
		return nil, syncErr
	}

	operations.Run(ctx, u.ops, op.ID, func(_ context.Context) (*anypb.Any, error) {
		// Строка реестра + owner-tuple intent уже записаны СИНХРОННО (writer.Insert
		// с request-ctx, несущим principal). zot-namespace lazy — материализуется на
		// первом docker push, отдельного provisioning-шага нет. Worker лишь финализирует
		// Operation созданным ресурсом: downstream/peer-вызовов нет, principal в
		// worker-ctx не требуется (в отличие от update/delete/deletetag/gc, где он
		// принудительно пробрасывается перед downstream-вызовом).
		return u.namespaceAny(created)
	})

	return &op, nil
}

// namespaceAny упаковывает Registry в Operation.response (google.protobuf.Any).
func (u *UseCase) namespaceAny(r *domain.Namespace) (*anypb.Any, error) {
	out, err := anypb.New(u.ProtoNamespace(r))
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return out, nil
}

// deriveDefaultGlobalSlug строит default global-slug при омитнутом входе. Целевой
// контракт F3 — "<accountSlug>-<name>", но источник accountSlug (iam) ещё НЕ определён
// (REG-1 accountSlug-addendum, cross-phase зависимость). ВРЕМЕННО деривим
// "<projectId>-<name>" — глобально-уникален by construction (projectId уникален),
// echo-корректен и не broken. TODO(REG-1 accountSlug-addendum): заменить projectId
// на accountSlug (потребует нового iam-поля/lookup'а).
func deriveDefaultGlobalSlug(projectID, name string) string {
	return projectID + "-" + name
}

// alreadyExistsErr различает конфликт по natural-key (project,name) и по bare-global
// globalSlug. Явный opt-in globalSlug → collision по глобальному slug (derived-slug
// уникален by construction) → tenant-prefix-подсказка (REG-1-10); иначе — name
// collision (REG-1-11). Авторитетный арбитр — DB partial UNIQUE (ban #10); это лишь тон.
func alreadyExistsErr(spec CreateSpec, name string) error {
	if spec.GlobalSlug != "" {
		return failAlreadyExists("explicit globalSlug '%s' is globally unique across ALL tenants and is already taken; omit globalSlug to auto-derive a tenant-prefixed slug (e.g. acme-payments), or choose a tenant-prefixed one", spec.GlobalSlug)
	}
	return failAlreadyExists("namespace %s already exists", name)
}

// regionExistsErr — маппинг cross-domain region-precheck (geo) в gRPC-status:
//
//	not-found / invalid → InvalidArgument ("region <id> not found")
//	geo недоступен      → Unavailable (fail-closed для мутации, REG-1-17)
func regionExistsErr(regionID string, err error) error {
	switch {
	case errors.Is(err, regerrors.ErrInvalidArg), errors.Is(err, regerrors.ErrNotFound):
		return failInvalidArg("region %s not found", regionID)
	case errors.Is(err, regerrors.ErrUnavailable):
		return failUnavailable("region existence check unavailable")
	}
	return mapRepoErr(err)
}

// projectExistsErr — маппинг cross-domain project-precheck в gRPC-status:
//
//	not-found / invalid → InvalidArgument ("project <id> not found")
//	iam недоступен      → Unavailable (fail-closed для мутации)
func projectExistsErr(projectID string, err error) error {
	switch {
	case errors.Is(err, regerrors.ErrInvalidArg), errors.Is(err, regerrors.ErrNotFound):
		return failInvalidArg("project %s not found", projectID)
	case errors.Is(err, regerrors.ErrUnavailable):
		return failUnavailable("project existence check unavailable")
	}
	return mapRepoErr(err)
}
