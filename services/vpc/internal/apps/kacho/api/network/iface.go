// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package network — use-case-слой ресурса Network: CreateNetworkUseCase,
// UpdateNetworkUseCase, DeleteNetworkUseCase плюс тонкий gRPC-handler.
//
// Use-case'ы работают через CQRS-Repository (`kacho.Repository` с Reader /
// Writer), а не через узкий `NetworkRepo`. Каждый use-case открывает TX явно
// (`u.repo.Writer(ctx)` или `Reader(ctx)`), и outbox-emit лежит в той же tx
// writer'а — атомарность DML + outbox гарантирована.
package network

import (
	"context"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// Pagination, *Filter — пере-используем единые value-объекты `internal/repo`
// (alias'ы, не копии). Иначе пришлось бы дублировать структуры или гонять между
// пакетами через двойную конверсию.
type (
	Pagination          = repo.Pagination
	NetworkFilter       = repo.NetworkFilter
	SubnetFilter        = repo.SubnetFilter
	RouteTableFilter    = repo.RouteTableFilter
	SecurityGroupFilter = repo.SecurityGroupFilter
)

// Re-export CQRS-Repository типов из `internal/repo/kacho` — use-case-код
// работает с ними под коротким именем (`Repo` / `Reader` / `Writer`). Type-alias
// (не type wrap) — тип взаимозаменяем с источником, без shim'ов.
type (
	Repo               = kacho.Repository
	Reader             = kacho.RepositoryReader
	Writer             = kacho.RepositoryWriter
	NetworkReaderIface = kacho.NetworkReaderIface
	NetworkWriterIface = kacho.NetworkWriterIface
	OutboxEmitter      = kacho.OutboxEmitter
)

// SubnetReader — узкое чтение Subnet, нужное для ListSubnets / checkNetworkEmpty.
type SubnetReader interface {
	List(ctx context.Context, f SubnetFilter, p Pagination) ([]*kacho.SubnetRecord, string, error)
}

// RouteTableReader — узкое чтение RouteTable, нужное для ListRouteTables /
// checkNetworkEmpty.
type RouteTableReader interface {
	List(ctx context.Context, f RouteTableFilter, p Pagination) ([]*kacho.RouteTableRecord, string, error)
}

// SecurityGroupRepo — то, что use-case'ам Network нужно от репозитория SG: List
// (для checkNetworkEmpty / ListSecurityGroups), Insert (для inline default-SG),
// Delete (для cleanup default-SG при Network.Delete).
type SecurityGroupRepo interface {
	List(ctx context.Context, f SecurityGroupFilter, p Pagination) ([]*kacho.SecurityGroupRecord, string, error)
	Insert(ctx context.Context, sg *domain.SecurityGroup) (*kacho.SecurityGroupRecord, error)
	Delete(ctx context.Context, id string) error
}

// ProjectClient — то, что use-case'ам Network нужно от peer-сервиса
// kaname: проверка существования project'а на request-path /
// в worker'е.
type ProjectClient interface {
	Exists(ctx context.Context, projectID string) (bool, error)
}

// QuotaGuard — совещательная полоса учёта числа ресурсов.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), DoD S2 п.3 и п.5. Порт объявлен здесь, у вызывающего;
// реализация — `apps/kacho/shared/quota`.
//
// Полоса НЕ ПРИНИМАЕТ решения: между её ответом и вставкой помещается чужая
// запись, и место занимает атомарное списание триггера в writer-транзакции
// (ban #10). Она существует ради РАННЕГО отказа — иначе исчерпание предела
// наблюдается как «200 и операция, упавшая через секунду», — и ради
// материализации строк учёта на промахе: без неё триггеру нечего списывать.
type QuotaGuard interface {
	Admit(ctx context.Context, projectID, kind string) error
}
