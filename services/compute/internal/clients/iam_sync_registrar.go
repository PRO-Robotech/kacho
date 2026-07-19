// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package clients

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"
	"github.com/PRO-Robotech/kacho/pkg/auth"

	"github.com/PRO-Robotech/kacho/services/compute/internal/fgaintent"
	"github.com/PRO-Robotech/kacho/services/compute/internal/ports"
)

// SyncRegistrar — синхронный owner-tuple registrar (owner-tuple op-gating P4;
// compute ранее его НЕ имел, в отличие от vpc/storage). Реализует
// ports.OwnerRegistrar поверх InternalIAMService.RegisterResource: Create-flow
// после durable commit ресурса синхронно регистрирует ТОТ ЖЕ owner-tuple, что
// эмитится транзакционно в compute_fga_register_outbox (fgaintent.
// ProjectHierarchyTuple), — owner-tuple эффективен сразу, без гонки с async
// register-drainer'ом, поэтому confirm-gate happy-path'а резолвится немедленно.
//
// Idempotent: повтор того же tuple → OK (idempotency-контракт RegisterResource), поэтому
// sync-register + последующий drainer на той же durable-intent-строке безопасны
// (exactly-applied). Best-effort по контракту порта: ошибка возвращается наверх и
// логируется вызывающим (service.syncRegisterOwner), Create НЕ проваливается —
// durable outbox-intent + register-drainer остаются at-least-once backstop'ом.
// Использует тот же mTLS-conn к kacho-iam :9091 (identity — из client-cert), что и
// register-drainer (compute→iam fga-proxy edge).
type SyncRegistrar struct {
	cli     IAMRegisterClient
	timeout time.Duration
}

// NewSyncRegistrar собирает registrar поверх conn к kacho-iam internal :9091
// (InternalIAMServiceClient). Зеркалит NewIAMRegisterApplier — composition-root не
// импортирует iamv1.
func NewSyncRegistrar(conn *grpc.ClientConn) *SyncRegistrar {
	return &SyncRegistrar{cli: iamv1.NewInternalIAMServiceClient(conn), timeout: 5 * time.Second}
}

// NewSyncRegistrarWithClient инжектит IAMRegisterClient напрямую (seam для
// unit-тестов с fake-recorder'ом, паритет с NewIAMRegisterApplierWithClient).
func NewSyncRegistrarWithClient(cli IAMRegisterClient) *SyncRegistrar {
	return &SyncRegistrar{cli: cli, timeout: 5 * time.Second}
}

// Register синхронно регистрирует project-hierarchy owner-tuple для ресурса
// (kind ∈ {Instance,Disk,Image,Snapshot}). Неизвестный kind / пустой id/project →
// no-op (nil): нечего регистрировать, ресурс всё равно закоммичен (fail-safe,
// зеркалит repo.emitFGARegisterIntent). Per-call deadline (s.timeout): create-worker
// идёт на detached background-ctx без дедлайна, поэтому ограничиваем здесь.
//
// source_version стампится текущим временем (монотонно >= source_version intent'а,
// который стампится now() внутри writer-TX до commit) — last-source-state-wins,
// как у drainer-applier'а. Labels + parent_project_id форвардятся в iam
// resource_mirror (β mirror-feed, паритет с applier'ом).
func (s *SyncRegistrar) Register(ctx context.Context, kind, resourceID, projectID string, labels map[string]string) error {
	tuple, ok := fgaintent.ProjectHierarchyTuple(kind, resourceID, projectID)
	if !ok {
		return nil
	}
	cctx := ctx
	var cancel context.CancelFunc
	if s.timeout > 0 {
		cctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}
	// Propagate principal MD (parity с applier/peer-calls; identity — из client-cert
	// в проде, anonymous/system в dev).
	if _, err := s.cli.RegisterResource(auth.PropagateOutgoing(cctx), &iamv1.RegisterResourceRequest{
		SubjectId:       tuple.SubjectID,
		Relation:        tuple.Relation,
		Object:          tuple.Object,
		Labels:          labels,
		ParentProjectId: projectID,
		SourceVersion:   timestamppb.New(time.Now()),
	}); err != nil {
		return fmt.Errorf("sync register owner-tuple %s: %w", tuple.Object, err)
	}
	return nil
}

// Compile-time check: adapter реализует use-case port.
var _ ports.OwnerRegistrar = (*SyncRegistrar)(nil)
