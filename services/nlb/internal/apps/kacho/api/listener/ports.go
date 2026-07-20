// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"github.com/PRO-Robotech/kacho/pkg/operations"

	iamclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/iam"
	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
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

// InternalAddressClient — write-side vpc.InternalAddressService consumer.
// NLB-1b F5 (MIGRATE): VIP-анкер вернулся на Listener → клиент снова аллоцирует/
// линкует VIP на Create (AllocateInternalIP[v6] для auto subnet_id, AttachExisting
// для BYO address_id) и освобождает на Delete (FreeIP / ClearReference).
type InternalAddressClient = vpcclient.InternalAddressClient

// SubnetClient — read-side vpc.SubnetService consumer для VIP placement/zone-
// coherence peer-validate (NLB-1-32/33): subnet.placement_type обязан совпасть с
// placement родительского LB, зона ZONAL-VIP — с зоной LB. subnet_id — foreign vpc
// id (общий prefix) → existence-only peer-validate, НЕ nlb-prefix-check (B4).
type SubnetClient = vpcclient.SubnetClient

// listenerAddressOwnerKind — Reference.type (vpc used_by) для VIP листенера.
// Совпадает с owner-строкой acceptance F5 (`nlb_listener:<id>`): SetReference/
// AttachExisting записывают used_by=nlb_listener:<listener_id> на vpc.Address.
const listenerAddressOwnerKind = "nlb_listener"

// listenerAddressOwner — AddressOwner для VIP-референса листенера в vpc.Address.
func listenerAddressOwner(listenerID, name string) vpcclient.AddressOwner {
	return vpcclient.AddressOwner{Kind: listenerAddressOwnerKind, ID: listenerID, Name: name}
}

// Registrar — sync-primary owner-tuple registrar (kacho-iam
// InternalIAMService.RegisterResource). Create после durable commit листенера +
// его `fga_register_outbox`-intent'а синхронно регистрирует containment-tuple,
// чтобы grant создателя был виден сразу (закрывает async-only окно). BEST-EFFORT:
// сбой → лог, НЕ фейлит Operation (ban #9). Impl — *iamclient.SyncRegistrar.
type Registrar = iamclient.Registrar

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
const (
	outboxActionCreated = "CREATED"
	outboxActionUpdated = "UPDATED"
	outboxActionDeleted = "DELETED"
	outboxActionFailed  = "FAILED"
)

// FGA relation strings live in `internal/domain`:
// `domain.FGARelationAdmin` / `domain.FGARelationLoadBalancer`.
//
// Acting-subject FGA-id извлекается inline в create.go как в sibling-пакетах
// (loadbalancer/targetgroup): `domain.FGASubjectFromPrincipal(p.Type, p.ID)` над
// `operations.PrincipalFromContext(ctx)` — без отдельного single-impl порта
// (subject-format живёт единожды в domain.FGASubjectFromPrincipal).
