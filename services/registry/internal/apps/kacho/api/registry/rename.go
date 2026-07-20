// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package registry

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/types/known/anypb"

	registryv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/registry/v1"
	"github.com/PRO-Robotech/kacho/pkg/ids"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// RenameNamespace — async смена immutable name namespace (REG-1 F2 :rename-verb).
// name immutable через UpdateNamespace; смена — ТОЛЬКО здесь. Sync-часть (verb-guard
// первым стейтментом): id-формат; newName DNS-safe; no-op (== текущее) → InvalidArgument.
// Async worker: атомарный UPDATE name (+ re-derive default globalSlug) под partial
// UNIQUE(project,name) — collision → Operation error ALREADY_EXISTS (REG-1-07). id —
// стабильный якорь (не меняется). Opt-in bare-global slug НЕ пересчитывается (slug
// отличается от default-от-старого-имени → предмет отдельного verb-контракта).
func (u *UseCase) RenameNamespace(ctx context.Context, namespaceID, newName string) (*operations.Operation, error) {
	if err := u.assertWired(); err != nil {
		return nil, err
	}
	if err := ValidateNamespaceID(namespaceID); err != nil {
		return nil, err
	}
	// verb-guard: newName DNS-safe (REG-1-07 malformed → InvalidArgument первым).
	if err := domain.ValidateName("newName", newName); err != nil {
		return nil, failInvalidArg("Illegal argument: %s", err.Error())
	}

	// Sync-read текущего namespace: oldName/projectId/slug — для no-op guard и
	// default-slug re-derive. well-formed-но-нет → NOT_FOUND.
	current, err := u.reader.Get(ctx, namespaceID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if newName == current.Name {
		// no-op: смена на текущее имя бессмысленна → InvalidArgument (verb-guard).
		return nil, failInvalidArg("newName must differ from the current name")
	}

	spec := UpdateSpec{
		NamespaceID: namespaceID,
		Name:        newName,
		ApplyName:   true,
	}
	// Re-derive ТОЛЬКО default-derived slug (slug == default-от-старого-имени). Opt-in
	// bare-global slug (не равен default) не трогается (REG-1-06). NB: default derive
	// временно projectId-based (accountSlug-addendum отложен, см. create.go).
	if current.GlobalSlug == deriveDefaultGlobalSlug(current.ProjectID, current.Name) {
		spec.GlobalSlug = deriveDefaultGlobalSlug(current.ProjectID, newName)
		spec.ApplyGlobalSlug = true
	}

	op, err := operations.NewFromContext(ctx,
		ids.PrefixOperationReg,
		fmt.Sprintf("Rename Namespace %s to %s", namespaceID, newName),
		&registryv1.RenameNamespaceMetadata{NamespaceId: namespaceID, NewName: newName},
	)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	principal := operations.PrincipalFromContext(ctx)
	if err := u.ops.CreateWithPrincipal(ctx, op, principal); err != nil {
		return nil, mapRepoErr(err)
	}

	operations.Run(ctx, u.ops, op.ID, func(workerCtx context.Context) (*anypb.Any, error) {
		workerCtx = operations.WithPrincipal(workerCtx, principal)
		renamed, uerr := u.writer.Update(workerCtx, spec, mirrorIntent)
		if uerr != nil {
			return nil, mapRepoErr(uerr)
		}
		return u.namespaceAny(renamed)
	})

	return &op, nil
}
