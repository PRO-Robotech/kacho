// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package targetgroup

import (
	"context"

	"github.com/PRO-Robotech/kacho/pkg/operations"

	computeclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/compute"
	geoclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/geo"
	iamclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/iam"
	vpcclient "github.com/PRO-Robotech/kacho/services/nlb/internal/clients/vpc"
	kachorepo "github.com/PRO-Robotech/kacho/services/nlb/internal/repo/kacho"
)

// Port-интерфейсы use-case-слоя TargetGroupService (Clean Architecture).
//
// Use-case'ы внутри пакета зависят ТОЛЬКО от этих port-ов; конкретные реализации
// (pgx-Repository, gRPC-typed-clients, FGA writer) инжектируются в composition
// root (`cmd/kacho-loadbalancer/main.go`). Тесты подменяют port'ы на ручные
// двойники (см. fakes_test.go в этом же пакете).

// Repo — корневой CQRS-Repository kacho-nlb. Алиас на `kacho.Repository`, чтобы
// handler-слой не импортировал leaf-пакет под полным путём.
type Repo = kachorepo.Repository

// OpsRepo — async LRO repo (kacho-corelib operations).
type OpsRepo = operations.Repo

// ProjectClient — iam.ProjectService.Get adapter.
type ProjectClient = iamclient.ProjectClient

// CheckClient — per-object FGA authorization gate (iam.InternalIAMService.Check).
// Move использует его для авторизации caller'а на DESTINATION project (`editor on
// project:<dst>`) — per-RPC interceptor проверяет только source-ресурс, поэтому
// dst-authz — задача handler'а. nil НЕ означает «пропустить»: отсутствие
// решателя — отказ (`Unavailable`), см. shared.AuthorizeObject.
type CheckClient = iamclient.CheckClient

// RegionClient — geo.RegionService.Get adapter (stateless pass-through;
// kacho-geo). Используется sync-precheck в Create use-case'е для
// валидации `region_id` через kacho-geo (ребро nlb→geo).
type RegionClient = geoclient.RegionClient

// InstanceClient — compute.InstanceService.Get adapter. Используется
// AddTargets-worker'ом для per-target instance-resolve + region-validate.
// Это НЕ geography-ребро (instance-resolve), поэтому остаётся на kacho-compute.
type InstanceClient = computeclient.InstanceClient

// NetworkInterfaceClient — vpc.NetworkInterfaceService.Get adapter. Используется
// AddTargets-worker'ом для per-target nic-resolve.
type NetworkInterfaceClient = vpcclient.NetworkInterfaceClient

// SubnetClient — vpc.SubnetService.Get adapter. Используется AddTargets-worker'ом
// для ip_ref-target peer-validate (Subnet existence + IP-in-CIDR + region-match).
type SubnetClient = vpcclient.SubnetClient

// ZoneRegionClient — авторитетный zone→region резолв через владельца Geography
// (geo.v1.ZoneService.Get → `Zone.region_id`, ребро nlb→geo). Нужен там, где у
// peer-ресурса есть только зона (compute.Instance): регион **никогда** не
// выводится из имени зоны — имена региона и зоны произвольны, выводимой связи
// между ними нет. Impl — `*geoclient.ZoneRegionClient` (per-call deadline,
// fail-closed). nil → region-coherence неверифицируема → мутация UNAVAILABLE.
type ZoneRegionClient interface {
	RegionOfZone(ctx context.Context, zoneID string) (string, error)
}

// Registrar — sync-primary owner-tuple registrar (kaname
// InternalIAMService.RegisterResource). Create после durable commit TG + его
// `fga_register_outbox`-intent'а синхронно регистрирует owner/containment-tuple,
// чтобы grant создателя был виден сразу (закрывает async-only окно). BEST-EFFORT:
// сбой → лог, НЕ фейлит Operation (ban #9). Impl — *iamclient.SyncRegistrar.
type Registrar = iamclient.Registrar

// FGA owner-hierarchy tuple-регистрация — через transactional-outbox
// (FGARegisterOutbox emit в writer-tx + register-drainer → IAM); FGA object-types
// / relations живут в `internal/domain` (FGAObjectType* / FGARelation*).

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
