// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package check

import (
	"context"

	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/authz"
	"github.com/PRO-Robotech/kacho/pkg/operations"

	"github.com/PRO-Robotech/kacho/services/storage/internal/fgaregister"
)

// ownerConfirmRelation — FGA relation, которую gateway scope_extractor требует на
// mutate (Update/Delete) Volume: required_relation "editor" на storage_volume:<id>
// (gateway permission_catalog VolumeService/{Update,Delete},
// scope_extractor{storage_volume,volume_id}). read-after-register confirm-проба
// (opgate P5) реплицирует ИМЕННО этот Check — FIX-2 consistency: тот же read-path
// (InternalIAMService.Check → OpenFGA), что энфорсит анти-BOLA-резолв gateway'я,
// поэтому op.done(success) ⟹ немедленный gateway-Check мутации уже даёт ALLOW
// (остаточного 403-окна «no direct relations granted» нет).
const ownerConfirmRelation = relationEditor

// VolumeOwnerConfirmer — read-after-register owner-tuple проба Volume.Create поверх
// существующего storage Check-client (reuse InternalIAMService.Check, тот же authzConn
// к kacho-iam :9091, что per-RPC authz-gate — нового cross-service ребра НЕ добавлено,
// OTG-08). Confirm ⇒ Check(subject=creator, relation=editor, object=storage_volume:<id>);
// confirmed=true, когда owner-tuple эффективен в FGA. Read-only, идемпотентна.
type VolumeOwnerConfirmer struct {
	check *IAMCheckClient
}

// NewVolumeOwnerConfirmer строит confirmer поверх conn к internal-листенеру kacho-iam
// (:9091, mTLS) — того же authzConn, что несёт per-RPC Check и Register/Unregister.
func NewVolumeOwnerConfirmer(conn grpc.ClientConnInterface) *VolumeOwnerConfirmer {
	return &VolumeOwnerConfirmer{check: NewIAMCheckClient(conn)}
}

// Confirm подтверждает, что creator имеет mutate-relation (editor) на
// storage_volume:<volumeID> в FGA. subject собирается из Principal тем же
// FormatSubject, что authz-interceptor (user:<id> / service_account:<id>).
func (c *VolumeOwnerConfirmer) Confirm(ctx context.Context, principal operations.Principal, volumeID string) (bool, error) {
	subject := authz.FormatSubject(principal.Type, principal.ID)
	object := fgaregister.ObjectStorageVolume + ":" + volumeID
	return c.check.Check(ctx, subject, ownerConfirmRelation, object)
}
