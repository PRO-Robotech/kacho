// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// sync_registrar.go — синхронный owner-tuple registrar поверх kacho-iam
// InternalIAMService.RegisterResource (fga-proxy). Мирроринг storage
// SyncRegistrar: Create-flow после durable-commit ресурса СИНХРОННО регистрирует
// те же register-tuple'ы, что эмитятся в registry_outbox — чтобы owner/pull-grant
// был доступен сразу, без гонки с async register-drainer'ом (иначе под burst
// создания repo/registry drainer сериализуется → owner-tuple лагает → repo GET 404
// в окне материализации).
//
// Register-ONLY: применяет register-tuple; unregister идёт исключительно
// async-drainer'ом. Каждый tuple → один RegisterResource с per-call 5s deadline
// (architecture.md: per-call deadline на КАЖДОМ внешнем вызове). Первая ошибка
// прекращает набор и возвращается наверх — вызывающий (use-case) логирует WARN и
// продолжает (durable outbox-intent + drainer остаются at-least-once backstop'ом).
// Field-mapping — 1:1 parity с NewRegisterApplier (register-ветка).
package iam

import (
	"context"
	"errors"
	"fmt"
	"time"

	iamv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/iam/v1"

	registry "github.com/PRO-Robotech/kacho/services/registry/internal/apps/kacho/api/registry"
	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
)

// errSyncRegisterClientNotConfigured — iam-peer не сконфигурирован (nil client). В проде
// serve.go подключает sync-registrar только при непустом iamConn, поэтому это defensive.
var errSyncRegisterClientNotConfigured = errors.New("iam register client not configured")

// SyncRegistrar реализует use-case-порт registry.SyncRegistrar поверх
// RegisterResourceClient. Тот же mTLS-conn к kacho-iam :9091, что и register-drainer.
type SyncRegistrar struct {
	cli     RegisterResourceClient
	timeout time.Duration
}

// NewSyncRegistrar собирает registrar поверх RegisterResourceClient. timeout по умолчанию
// 5s на один RegisterResource (create-path может идти на ctx без жёсткого дедлайна —
// ограничиваем здесь; per-call deadline, architecture.md).
func NewSyncRegistrar(cli RegisterResourceClient) *SyncRegistrar {
	return &SyncRegistrar{cli: cli, timeout: 5 * time.Second}
}

// Register синхронно регистрирует каждый tuple каждого intent через iam RegisterResource.
// Field-mapping (SubjectId/Relation/Object/TraceId=ResourceID/Labels/ParentProjectId) —
// parity с NewRegisterApplier (register-ветка). Первая ошибка прекращает набор и
// возвращается (idempotent: durable outbox-intent + register-drainer повторят at-least-once).
func (s *SyncRegistrar) Register(ctx context.Context, intents []domain.RegisterIntent) error {
	if s.cli == nil {
		return errSyncRegisterClientNotConfigured
	}
	for _, intent := range intents {
		for _, t := range intent.Tuples {
			cctx := ctx
			var cancel context.CancelFunc
			if s.timeout > 0 {
				cctx, cancel = context.WithTimeout(ctx, s.timeout)
			}
			_, err := s.cli.RegisterResource(cctx, &iamv1.RegisterResourceRequest{
				SubjectId:       t.SubjectID,
				Relation:        t.Relation,
				Object:          t.Object,
				TraceId:         intent.ResourceID,
				Labels:          intent.Labels,
				ParentProjectId: intent.ParentProjectID,
			})
			if cancel != nil {
				cancel()
			}
			if err != nil {
				return fmt.Errorf("sync register owner-tuple %s: %w", t.Object, err)
			}
		}
	}
	return nil
}

// Compile-time check: SyncRegistrar удовлетворяет use-case-порту.
var _ registry.SyncRegistrar = (*SyncRegistrar)(nil)
