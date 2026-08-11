# kacho-compute

Compute-сервис Kachō: control-plane для **Instance** и справочника
**MachineType**. Compute-NIC бэкуется ресурсом vpc `NetworkInterface` (`nic_id`,
эпик `KAC-9`). Подробности — `docs/content/` (страницы сервиса) и `docs/engineering/` (инженерные записки).

> [!note] Блочное хранение и ось размещения принадлежат другим сервисам
> Здесь перечислялись как свои ещё четыре ресурса блочного хранения и два
> справочника оси размещения. Ни одного из них compute не объявляет: в его
> контрактах остались четыре сервиса — машина, тип машины и два внутренних, —
> а край обслуживает под доменом compute только адреса машин, типов машин и
> внутренней поверхности. Владельцы: блочное хранение — storage, регион и зона —
> geo. Снятые адреса ниже не воспроизводятся: процитированные, они читаются как
> живые, и смоук копируют в терминал первым делом.

## Quick start (локальный стенд)

Команды запускаются **от корня репозитория**: дерево одно, соседних
репозиториев стенда рядом с ним нет.

```bash
# 1. Поднять полный стенд (kind + helm + Postgres + все сервисы)
make -C deploy dev-up

# 2. Прокинуть api-gateway наружу
kubectl -n kacho port-forward svc/api-gateway 18080:8080 &

# 3. Smoke
curl 'http://localhost:18080/compute/v1/machineTypes'
curl 'http://localhost:18080/compute/v1/instances?projectId=<project>&pageSize=5'
```

Перезапуск только compute после изменений в коде:
```bash
make -C deploy reload-svc SVC=compute
make -C deploy logs-svc SVC=compute        # tail логов
make -C deploy psql SVC=compute            # psql kacho_compute
```

## Архитектура

Clean Architecture (`internal/domain` → `internal/apps/kacho/api/<resource>` →
`internal/handler`, `internal/repo`, `internal/clients`); `cmd/compute/main.go` —
единственный composition root. Слоя с именем «service» у сервиса нет — use-case
живёт срезом на ресурс. Все мутации (`Create/Update/Delete/Start/Stop/...`)
возвращают `Operation` (LRO), выполнение worker'ом через `pkg/operations`.`Run`
(общий фундамент лежит в каталоге `pkg/` монорепо; прежнее имя отдельного
репозитория фундамента здесь не воспроизводится — координатой оно не является).
Outbox + LISTEN/NOTIFY дают event stream через
`InternalWatchService` (для admin-tooling / UI). Подробности по слоям и
паттернам — правила воркспейса и `docs/engineering/architecture/`.

### Dual gRPC ports

| Порт   | Сервисы                                                                  | Кто использует                  |
|--------|--------------------------------------------------------------------------|----------------------------------|
| `:9090`| `InstanceService`, `MachineTypeService`, `OperationService` | api-gateway (external + UI) |
| `:9091`| `InternalWatchService`, `InternalMachineTypeService` | admin-tooling / UI (через api-gateway internal mux) — НЕ на external TLS endpoint |

## Тесты

Команды — **от корня репозитория**; цели сборки объявлены в `services/compute/Makefile`.

```bash
make -C services/compute test-short    # unit (use-case/handler) + -short
make -C services/compute test          # unit + integration (testcontainers Postgres 16)
# E2E (нужен port-forward api-gateway):
python3 services/compute/tests/newman/scripts/gen.py && ./services/compute/tests/newman/scripts/run.sh
```

Три уровня: unit (`internal/apps/kacho/api/<resource>/*_test.go` и `internal/handler/*_test.go`, моки port-интерфейсов из
`internal/ports/portmock`), integration (`internal/repo/*integration_test.go`,
testcontainers), e2e (`tests/newman/`, декларативные `cases/*.py` → `gen.py` →
Postman-коллекции). Критерий приёмки: newman-кейс зеленеет против объявленного
контракта Kachō, а не против того, что сервис сегодня отвечает.

## Полезное

- Открытые задачи / баги: **только** GitHub Issues в `PRO-Robotech/kacho`
  (кросс-репо — `PRO-Robotech/kacho-workspace`). Файла-списка задач в репозитории
  нет и не заводится: список, за который никто не отвечает, переживает свои пункты.
- By-design отступления от конвенций: `docs/engineering/architecture/07-known-divergences.md`.
- Proto: `proto/kacho/cloud/compute/v1/` — единственный дом контрактов, в каталоге
  сервиса `.proto` нет.
- Эталон-паттерны: `services/vpc/` (compute написан на них).
