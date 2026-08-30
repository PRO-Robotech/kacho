// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package listener — gRPC handler + per-RPC UseCases for the
// kacho.cloud.loadbalancer.v1.ListenerService.
//
// Scope (26 scenarios):
//
//   - Get / List          — sync reads.
//   - Create              — async; VIP allocation: BYO `address_id` (atomic
//     SetReference CAS on existing vpc.Address) ИЛИ auto-alloc via
//     vpc.InternalAddressService.AllocateExternalIP/AllocateInternalIP.
//   - Update              — async; mutable fields only (name/description/labels/
//     default_target_group_id/target_group_id). Immutable load_balancer_id/
//     protocol/port/project_id rejected sync с текстом по конвенции Kachō
//     `"<field> is immutable after Listener.Create"`; у project_id к нему
//     добавлен следующий шаг — глагол переноса ВЛАДЕЛЬЦА (#1671). Перечень
//     называется таблицей путь→текст в update.go: здесь он не воспроизводится
//     вторым перечнем, чтобы два места об одном предмете снова не разошлись —
//     прежняя редакция называла среди неизменяемых ip_version/address_id, снятые
//     с контракта листенера вместе с консолидацией VIP.
//   - Delete              — async; free VIP back to pool (auto-alloc) либо
//     clear used_by (BYO); DELETE listener row; emit DELETED + LB UPDATED.
//   - ListOperations      — sync; per-resource history wrapper над
//     `kacho-corelib/operations.Repo.List(filter=resource_id)`.
//
// Architectural pillars (Clean Architecture):
//
//   - Handler — thin transport: parse request → call UseCase → dto.Transfer →
//     proto response. No business logic, no validation beyond `id is required`.
//   - Each UseCase = one file; receives port-interfaces (declared в ports.go)
//     через конструктор (composition root wires concrete adapters).
//   - DB writes + outbox emit live in one writer-TX (`kachorepo.RepositoryWriter`).
//   - Long-running ops via `operations.Run(callerCtx, opsRepo, opID, fn)` —
//     handler returns Operation immediately; worker propagates baggage values
//     (slog logger, principal) but не наследует caller deadline.
//
// Компенсирующей саги у листенера НЕТ — и это не пропуск, а следствие того, где
// живёт адрес.
//
// Листенер не аллоцирует и не освобождает VIP ни на одном пути: адрес
// принадлежит родительскому балансировщику (один anycast-VIP на семейство), и
// компенсирует его сам балансировщик — своим Delete, своей create-компенсацией
// и `jobs/free_ip_runner`, который сканирует ТОЛЬКО load_balancers. В
// `create.go`/`delete.go` листенера ноль обращений к адресному клиенту; колонки
// адреса дропнуты миграцией 0028 вместе с недостижимой release-веткой.
//
// Прежняя редакция описывала здесь три ветви компенсации — отказ постановки
// ссылки на чужой адрес, освобождение аллоцированного адреса при неудачном
// INSERT и фоновую реконсиляцию застрявшей строки листенера. Ни одной из них в
// дереве нет; шапка пережила свой предмет и обещала защиту, которой не
// существует, — то есть была опаснее прямого умолчания.
//
// FGA owner-hierarchy tuple emit (transactional-outbox, replaces the former
// best-effort direct FGA write —):
//   - creator tuple `<subject> #admin @nlb_listener:<id>` (skipped if the principal
//     is system/unauthenticated) + parent-link tuple
//     `nlb_network_load_balancer:<lb_id> #load_balancer @nlb_listener:<id>` are
//     serialised into a `domain.FGARegisterIntent` and persisted via
//     `w.FGARegisterOutbox.Emit(fga.register, …)` in the SAME writer-tx as the
//     listener INSERT (one commit, no dual-write).
//   - the register-drainer (`cmd/kacho-loadbalancer/main.go`) later applies each
//     tuple through kacho-iam `InternalIAMService.RegisterResource` by mTLS;
//     IAM-down → intent stays durable and is retried (tuple is never lost).
//   - Delete emits the symmetric `fga.unregister` intent (parent-link) →
//     `UnregisterResource`.
//
// Test layout (test-first):
//   - *_test.go — unit (in-package), table-driven, fake-port adapters.
//   - integration_test.go — testcontainers Postgres; verifies UNIQUE race,
//     outbox emit and LB region/project denorm correctness.
package listener
