// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package address — use-case-слой ресурса Address.
//
// Бизнес-логика разложена по use-case'ам: CreateAddressUseCase /
// UpdateAddressUseCase / DeleteAddressUseCase / GetAddressUseCase /
// ListAddressesUseCase / GetByValueUseCase / ListBySubnetUseCase /
// ListOperationsUseCase плюс тонкий gRPC-handler. Multi-family allocation flow
// (external v4/v6, internal v4/v6) и composition с AddressPoolService — внутри
// CreateAddressUseCase.
//
// Use-case'ы работают через CQRS-Repository (Reader / Writer), а не через узкий
// AddressRepo. Каждый открывает TX явно (`u.repo.Writer(ctx)` или `Reader(ctx)`),
// и outbox-emit лежит в той же writer-TX — атомарность DML + outbox гарантирована.
// IPAM-flow (Insert + Allocate + Outbox) тоже атомарен внутри одной writer-TX в
// CreateAddressUseCase.doCreate.
//
// Pool service для cascade-резолва AddressPool по family живет в
// `internal/apps/kacho/api/addresspool/`. Здесь объявлен лишь port `PoolService`,
// которому `*addresspool.ResolverService` удовлетворяет в composition root.
package address

import (
	"context"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/apps/kacho/api/addresspool"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// Pagination, *Filter — пере-используем единые value-объекты `internal/repo`.
type (
	Pagination    = repo.Pagination
	AddressFilter = repo.AddressFilter
)

// Re-export CQRS-Repository типов из `internal/repo/kacho` — use-case-код
// работает с ними под коротким именем (`Repo` / `Reader` / `Writer`). Type-alias
// (не type wrap) — тип взаимозаменяем с источником, никаких shim'ов.
type (
	Repo               = kachorepo.Repository
	Reader             = kachorepo.RepositoryReader
	Writer             = kachorepo.RepositoryWriter
	AddressReaderIface = kachorepo.AddressReaderIface
	AddressWriterIface = kachorepo.AddressWriterIface
	OutboxEmitter      = kachorepo.OutboxEmitter
)

// SubnetReader — узкое чтение Subnet, нужное Address use-case'ам:
//   - Create.validateInternalIPInSubnet (sync-проверка что explicit IP в CIDR);
//   - Create.doCreate / Allocate*IP / AllocateInternalIPv6 — FK-валидация подсети;
//   - ListBySubnet — child-list через AddressesBySubnet.
//
// Port возвращает `*kacho.SubnetRecord`. В composition root реализуется
// `cqrsadapter.Subnet` поверх kachoRepo.
type SubnetReader interface {
	Get(ctx context.Context, id string) (*kachorepo.SubnetRecord, error)
	AddressesBySubnet(ctx context.Context, subnetID string, p Pagination) ([]*kachorepo.AddressRecord, string, error)
}

// ProjectClient — то, что use-case'ам Address нужно от peer-сервиса
// kaname: проверка существования project'а на request-path /
// в worker'е Create.
type ProjectClient interface {
	Exists(ctx context.Context, projectID string) (bool, error)
}

// ZoneRegistry — port проверки существования зоны (geo.v1.ZoneService.Get,
// Geography — leaf-домен kacho-geo). Используется CreateAddressUseCase для
// existence-валидации `zone_id` external-адреса (placement-coherence). Локальный
// порт (как subnet/iface.go) — реализация `*clients.GeoZoneClient` удовлетворяет
// структурно. Get возвращает repo.ErrNotFound для несуществующей зоны.
type ZoneRegistry interface {
	Get(ctx context.Context, id string) (*domain.Zone, error)
}

// PoolService — узкий port AddressPool-resolver'а для cascade-резолва pool по
// family. Реализуется `*addresspool.ResolverService`.
//
// Использует FamilyV4 / FamilyV6 как enum (alias на addresspool.AddressFamily —
// не вводим параллельный тип, чтобы вызывающий handler/cmd прозрачно
// переиспользовал константы pool resolver'а).
type PoolService interface {
	ResolvePoolForAddressObjFamily(ctx context.Context, addr *kachorepo.AddressRecord, family addresspool.AddressFamily) (*addresspool.ResolvedPool, error)
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
