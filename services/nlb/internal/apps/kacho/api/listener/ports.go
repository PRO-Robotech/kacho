// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package listener

import (
	"context"

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

// Registrar — sync-primary owner-tuple registrar (kaname
// InternalIAMService.RegisterResource). Create после durable commit листенера +
// его `fga_register_outbox`-intent'а синхронно регистрирует containment-tuple,
// чтобы grant создателя был виден сразу (закрывает async-only окно). BEST-EFFORT:
// сбой → лог, НЕ фейлит Operation (ban #9). Impl — *iamclient.SyncRegistrar.
type Registrar = iamclient.Registrar

// CheckClient — per-object FGA authorization gate (iam.InternalIAMService.Check).
// Create/Update используют его для авторизации caller'а на caller-supplied
// `targetGroupId` (`viewer` на `nlb_target_group:<id>`): per-RPC interceptor
// скоупит только parent LoadBalancer / сам Listener, поэтому TG остаётся
// необойдённым объектом (CWE-863). nil НЕ означает «пропустить»: отсутствие
// решателя — отказ (`Unavailable`), см. shared.AuthorizeObject.
// Parity с `loadbalancer.CheckClient` / `targetgroup.CheckClient`.
type CheckClient = iamclient.CheckClient

// FGA owner-hierarchy / creator / parent-link tuple-регистрация — через
// transactional-outbox (FGARegisterOutbox emit в writer-tx + register-drainer →
// IAM), не прямым FGA-клиентом. FGA object-types / relations — `internal/domain`.

// FGA object-type strings live in `internal/domain` (single source of truth,
// kacho-nlb-wide): `domain.FGAObjectTypeListener` / `domain.FGAObjectTypeLoadBalancer`.

// Слово вида предмета журнала (`resource_type` в `nlb_outbox`) здесь БОЛЬШЕ НЕ
// ОБЪЯВЛЯЕТСЯ: точки эмиссии этого пакета зовут общую константу
// `kachorepo.OutboxResource{Listener,LoadBalancer}` — ту же, которую читает
// объявление журнала подписки (`internal/subscriptionjournal`).
//
// # Почему копия была снята, хотя во время исполнения вреда не приносила
//
// Значения совпадали, и отказать копия не могла. Вред она приносила ПЕРЕПИСИ, и
// он уже наступил дважды. Предикат «где эмитится этот вид», записанный по
// канонической константе, отвечал по слушателю НОЛЬ при четырёх точках — и по
// нему выходило, что обогащать у слушателя нечего. Тем же способом была занижена
// цена обогащения балансировщика: шапка журнала называла ПЯТЬ точек Go при семи,
// потому что две из них лежат здесь и звали копию (#1550).
//
// Держит это разбор дерева use-case'ов
// (`subscriptionjournal.TestEveryEmissionNamesTheKindByTheCanonicalConstant`), а
// не внимание: он судит УЗЕЛ аргумента вида, поэтому следующая копия не заведётся
// молча.

// Слово РОДА ИЗМЕНЕНИЯ здесь тоже больше не объявляется: точки эмиссии зовут
// `kachorepo.OutboxAction{Created,Updated,Deleted}`.
//
// Копия рода снята тем же заходом и по той же причине, что копия вида (#1550), —
// но нашла её не перепись, а ГЕЙТ, заведённый по виду: судя род по канонической
// константе, он не узнал местную копию и посчитал точку снятия слушателя за
// точку с состоянием. То есть слепая зона распознавателя воспроизвелась на
// соседней оси немедленно, стоило завести проверку, которая эту ось читает.
//
// `FAILED` слушателем не эмитится: его единственным источником была
// release-ветка VIP в Delete, снятая вместе с адресной моделью листенера
// (миграция 0028) — адрес принадлежит родительскому LoadBalancer'у.

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
