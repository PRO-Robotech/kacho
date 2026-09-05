// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package subnet — use-case-структура ресурса Subnet: бизнес-логика
// CreateSubnetUseCase / UpdateSubnetUseCase / DeleteSubnetUseCase /
// GetSubnetUseCase / ListSubnetsUseCase / AddCidrBlocksUseCase /
// RemoveCidrBlocksUseCase / ListUsedAddressesUseCase / ListOperationsUseCase
// плюс тонкий gRPC-handler.
//
// Use-case'ы работают через CQRS `kacho.Repository` (Reader / Writer), а не
// напрямую через узкий repo-интерфейс. Каждый use-case открывает TX явно
// (`u.repo.Writer(ctx)` или `Reader(ctx)`), и outbox-emit лежит в той же
// tx writer'а — атомарность DML + outbox гарантирована.
package subnet

import (
	"context"

	"github.com/PRO-Robotech/kacho/services/vpc/internal/domain"
	"github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	kachorepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho"
)

// Pagination, *Filter — пере-используем единые value-объекты `internal/repo`
// (alias'ы, не копии). Иначе пришлось бы дублировать структуры или гонять между
// пакетами через двойную конверсию.
type (
	Pagination   = repo.Pagination
	SubnetFilter = repo.SubnetFilter
)

// Re-export CQRS-Repository типов из `internal/repo/kacho` — use-case-код
// работает с ними под коротким именем (`Repo` / `Reader` / `Writer`). Type-alias
// (не type wrap) — тип взаимозаменяем с источником, никаких shim'ов.
type (
	Repo              = kachorepo.Repository
	Reader            = kachorepo.RepositoryReader
	Writer            = kachorepo.RepositoryWriter
	SubnetReaderIface = kachorepo.SubnetReaderIface
	SubnetWriterIface = kachorepo.SubnetWriterIface
	OutboxEmitter     = kachorepo.OutboxEmitter
)

// AddressRefRepo — узкий интерфейс для обогащения ListUsedAddresses записями
// referrer'ов (кто использует адрес). Optional — `nil` → references[] пуст
// (graceful degradation). Используется только в ListUsedAddressesUseCase.
type AddressRefRepo interface {
	ReferencesForAddresses(ctx context.Context, addressIDs []string) (map[string]*domain.AddressReference, error)
}

// NetworkInterfaceRepo — узкий интерфейс для precondition-проверки в Delete
// (подсеть с NIC, приаттаченным к инстансу, удалить нельзя). Optional —
// `nil` → проверка пропускается (FK RESTRICT в worker'е все равно подберет
// address-bearing NIC через цепочку NIC → Address → Subnet). NIC-репо живет в
// `internal/repo/kacho/pg/network_interface.go` — wire через composition root.
type NetworkInterfaceRepo interface {
	// CountBySubnet — «сколько интерфейсов держат подсеть» + несколько их
	// идентификаторов для текста отказа. Предусловию не нужны ни строки
	// целиком, ни полный список идентификаторов.
	CountBySubnet(ctx context.Context, subnetID string, sample int) (int64, []string, error)
}

// ProjectClient — то, что use-case'ам Subnet нужно от peer-сервиса
// kaname: проверка существования project'а на request-path / в
// worker'е.
type ProjectClient interface {
	Exists(ctx context.Context, projectID string) (bool, error)
}

// ZoneRegistry — port для проверки существования зоны (используется Create,
// validateZoneID). Реализация — gRPC-клиент к `geo.v1.ZoneService.Get`
// (Geography — leaf-домен kacho-geo).
type ZoneRegistry interface {
	Get(ctx context.Context, id string) (*domain.Zone, error)
}

// RegionRegistry — port для проверки существования региона (используется Create
// REGIONAL-подсети, validateRegionID). Реализация — gRPC-клиент к
// `geo.v1.RegionService.Get` (Geography — leaf-домен kacho-geo).
type RegionRegistry interface {
	Get(ctx context.Context, id string) (*domain.Region, error)
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
