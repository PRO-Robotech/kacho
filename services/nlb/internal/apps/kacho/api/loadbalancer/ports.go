// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package loadbalancer

import (
	"context"

	geoclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/geo"
	iamclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/iam"
	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// Port-интерфейсы use-case-слоя NetworkLoadBalancer (Clean Architecture).
//
// Use-case'ы внутри пакета зависят ТОЛЬКО от этих port-ов; конкретные реализации
// (pgx-Repository, gRPC-typed-clients, FGA writer) инжектируются в composition
// root (`cmd/kacho-loadbalancer/main.go`). Тесты подменяют port'ы на ручные
// двойники (см. *_mock_test.go в этом же пакете).

// Repo — корневой CQRS-Repository kacho-nlb. Сохранён как алиас, чтобы handler-
// слой не импортировал leaf-пакет `repo/kacho` напрямую под другим именем.
type Repo = kachorepo.Repository

// ProjectClient — Get(projectID) → *iamclient.Project. sync-precheck `project_id`
// в Create/Move (NotFound → InvalidArgument; недоступен → Unavailable).
type ProjectClient = iamclient.ProjectClient

// CheckClient — per-object FGA authorization gate (iam.InternalIAMService.Check).
// Move использует его для авторизации caller'а на DESTINATION project (`editor on
// project:<dst>`) — per-RPC interceptor проверяет только source-ресурс, поэтому
// dst-authz — задача handler'а. nil НЕ означает «пропустить»: отсутствие
// решателя — отказ (`Unavailable`), см. shared.AuthorizeObject.
type CheckClient = iamclient.CheckClient

// RegionClient — Get(regionID) → *geoclient.Region. sync-precheck `region_id`
// через geo.RegionService.Get (ребро nlb→geo).
type RegionClient = geoclient.RegionClient

// ZoneClient — ListZoneIDsInRegion(regionID) → зоны региона. sync-precheck
// disabled_announce_zones (зоны ∈ регион, не все зоны) и деривация underlying-зоны
// public-VIP (EXTERNAL auto) через geo.ZoneService.List (ребро nlb→geo).
type ZoneClient = geoclient.ZoneClient

// ZoneRegionClient — авторитетный zone→region резолв через владельца Geography
// (geo.v1.ZoneService.Get → `Zone.region_id`, ребро nlb→geo). Нужен для
// region-coherence зоно-привязанного EXTERNAL-адреса: регион НИКОГДА не
// выводится из имени зоны. Impl — `*geoclient.ZoneRegionClient` (per-call
// deadline, fail-closed). nil → регион зоны неустановим → мутация UNAVAILABLE.
type ZoneRegionClient interface {
	RegionOfZone(ctx context.Context, zoneID string) (string, error)
}

// InternalAddressClient — VIP-lifecycle port над vpc InternalAddressService:
// per-family auto-аллокация (AllocateInternalIP/IPv6 из подсети, AllocateExternalIP/
// IPv6 — платформенный public), link существующего Address (AttachExisting) и
// снятие аренды (ReleaseLease) в compensation/Delete/реконсайлере. Concrete
// `*vpcclient.internalAddressClient` удовлетворяет интерфейс структурно.
//
// Снятие аренды — ОДИН глагол, а не пара «снять ссылку» + «удалить адрес».
// Пара спрашивала владельца пообъектно, а на такой вопрос ответ «не найдено»
// не несёт утверждения «аренды нет» — и полоса читала его как выполненную
// работу. Здесь исход приезжает ПОЛЕМ, и решение «удалять или оставить»
// принимает владелец по своей колонке, а не потребитель по своей копии признака.
type InternalAddressClient interface {
	AllocateInternalIP(ctx context.Context, req vpcclient.AllocateInternalIPRequest) (*vpcclient.AllocateResponse, error)
	AllocateInternalIPv6(ctx context.Context, req vpcclient.AllocateInternalIPRequest) (*vpcclient.AllocateResponse, error)
	AllocateExternalIP(ctx context.Context, req vpcclient.AllocateExternalIPRequest) (*vpcclient.AllocateResponse, error)
	AllocateExternalIPv6(ctx context.Context, req vpcclient.AllocateExternalIPRequest) (*vpcclient.AllocateResponse, error)
	AttachExisting(ctx context.Context, req vpcclient.AttachExistingRequest) (*vpcclient.AllocateResponse, error)
	ReleaseLease(ctx context.Context, req vpcclient.ReleaseLeaseRequest) (vpcclient.LeaseOutcome, error)
}

// SubnetClient — Get(subnetID) → *vpcclient.Subnet. sync-precheck placement подсети
// (== placement LB) + derived network_id (dualstack same-network) через
// vpc.SubnetService.Get. not-found → InvalidArgument; недоступен → Unavailable.
type SubnetClient = vpcclient.SubnetClient

// SecurityGroupClient — Get(securityGroupID) → *vpcclient.SecurityGroup. NLB-1b
// MIGRATE peer-validate of NetworkLoadBalancer.security_group_ids: same-project
// existence via vpc.SecurityGroupService.Get. absent/no-access → FailedPrecondition;
// vpc unavailable → Unavailable (fail-closed). nil → SG validation skipped
// (dev/unwired) — the DB CHECK (INTERNAL-only) remains the backstop.
type SecurityGroupClient = vpcclient.SecurityGroupClient

// AddressClient — Get(addressID) → *vpcclient.Address (публичный vpc
// AddressService.Get, authz-gated `v_get`). sync-precheck link-источника: адрес
// резолвится под tenant-identity, проверяются kind/family/ownership/placement.
// Анти-oracle: несоответствие/no-access → generic InvalidArgument.
type AddressClient = vpcclient.AddressClient

// Registrar — sync-primary owner-tuple registrar (kaname
// InternalIAMService.RegisterResource). Create после durable commit ресурса +
// его `fga_register_outbox`-intent'а синхронно регистрирует owner/containment-
// tuple, чтобы grant создателя был виден сразу (закрывает async-only окно). nil
// → sync-путь пропускается (остаётся at-least-once register-drainer backstop).
// BEST-EFFORT: сбой → лог, НЕ фейлит Operation (ban #9). Impl — *iamclient.SyncRegistrar.
type Registrar = iamclient.Registrar

// QuotaGuard — совещательная полоса учёта числа ресурсов.
//
// Порт объявлен здесь, у вызывающего, а реализация живёт в
// `apps/kacho/quota`: use-case не импортирует адаптер, и подставить полосу в
// пробе можно, не поднимая ни базы, ни соседа.
//
// Полоса НЕ является решением: между её ответом и вставкой помещается чужая
// запись, и решает атомарное списание триггера (ban #10, §7.4 приёмки). Она
// существует ради РАННЕГО отказа тем же текстом и признаком, каким его
// произвёл бы триггер, — у обеих полос один производитель в базе.
type QuotaGuard interface {
	// Admit — есть ли место у ПРОЕКТА под ещё одну строку этого вида.
	Admit(ctx context.Context, projectID, kind string) error
	// AdmitCarrier — тот же вопрос про носителя-РОДИТЕЛЯ.
	AdmitCarrier(ctx context.Context, carrierType, carrierID, kind string) error
}
