// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"github.com/PRO-Robotech/kacho/pkg/operations"

	iamclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/iam"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// Port interfaces for the listener package (workspace CLAUDE.md «Чистая
// архитектура»): use-cases depend on these abstractions, not on concrete
// adapters. Adapters live in `internal/clients/*` and `internal/repo/kacho/pg`;
// composition root (`cmd/kacho-loadbalancer/main.go`) wires them в Handler.

// RepoFactory — opens read/write transactions over kacho-nlb DB.
// Aliased from `internal/repo/kacho.Repository` to keep package boundary clean.
type RepoFactory = kachorepo.Repository

// OperationsRepo — async LRO repo (shared `kacho-corelib/operations.Repo`).
// Aliased to local name so use-cases don't reach into corelib by full path.
type OperationsRepo = operations.Repo

// Registrar — sync-primary owner-tuple registrar (kacho-iam
// InternalIAMService.RegisterResource). Create после durable commit листенера +
// его `fga_register_outbox`-intent'а синхронно регистрирует containment-tuple,
// чтобы grant создателя был виден сразу (закрывает async-only окно). BEST-EFFORT:
// сбой → лог, НЕ фейлит Operation (ban #9). Impl — *iamclient.SyncRegistrar.
type Registrar = iamclient.Registrar

// CheckClient — per-object FGA authorization gate (iam.InternalIAMService.Check).
// Create/Update используют его для авторизации caller'а на caller-supplied
// `targetGroupId` (`viewer` на `nlb_target_group:<id>`): per-RPC interceptor
// скоупит только parent LoadBalancer / сам Listener, поэтому TG остаётся
// необойдённым объектом (CWE-863). nil → Check пропускается (dev/unwired).
// Parity с `loadbalancer.CheckClient` / `targetgroup.CheckClient`.
type CheckClient = iamclient.CheckClient

// FGA owner-hierarchy / creator / parent-link tuple-регистрация — через
// transactional-outbox (FGARegisterOutbox emit в writer-tx + register-drainer →
// IAM), не прямым FGA-клиентом. FGA object-types / relations — `internal/domain`.

// FGA object-type strings live in `internal/domain` (single source of truth,
// kacho-nlb-wide): `domain.FGAObjectTypeListener` / `domain.FGAObjectTypeLoadBalancer`.

// outboxResourceTypeListener / outboxResourceTypeLoadBalancer — resource_type
// в `nlb_outbox` (ограничено CHECK CONSTRAINT в миграции 0001).
const (
	outboxResourceTypeListener     = "nlb_listener"
	outboxResourceTypeLoadBalancer = "nlb_load_balancer"
)

// Outbox action strings (CHECK constraint в nlb_outbox; см. миграцию 0001).
// `FAILED` листенером больше не эмитится: его единственным источником была
// release-ветка VIP в Delete, снятая вместе с адресной моделью листенера
// (миграция 0028) — адрес принадлежит родительскому LoadBalancer'у.
const (
	outboxActionCreated = "CREATED"
	outboxActionUpdated = "UPDATED"
	outboxActionDeleted = "DELETED"
)

// FGA relation strings live in `internal/domain`: `domain.FGARelationAdmin` is
// named there because the AccessBinding flow writes it; it is not emitted in a
// register-intent, because the iam proxy refuses a privilege relation from a
// module. The parent-link relation that used to be named alongside it is gone
// from the model as well — nothing wrote it.
//
// Acting-subject FGA-id извлекается inline в create.go как в sibling-пакетах
// (loadbalancer/targetgroup): `domain.FGASubjectFromPrincipal(p.Type, p.ID)` над
// `operations.PrincipalFromContext(ctx)` — без отдельного single-impl порта
// (subject-format живёт единожды в domain.FGASubjectFromPrincipal).
