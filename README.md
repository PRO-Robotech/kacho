# Kachō — monorepo

Единый репозиторий платформы **Kachō** (cloud control-plane). Заменяет polyrepo
`kacho-proto` / `kacho-corelib` / `kacho-<svc>` / `kacho-api-gateway` / `kacho-deploy` —
сегодня это каталоги `proto/`, `pkg/`, `services/<svc>/`, `gateway/`, `deploy/` одного дерева.
Старые репозитории остаются **архивом** (история туда и не переносилась).

## Раскладка

```
proto/     — .proto-исходники + buf.yaml. ЕДИНСТВЕННЫЙ дом .proto.
pkg/       — общий фундамент, импортируемый кем угодно:
  api/       сгенерённые Go-стабы (buf generate → сюда; РУКАМИ НЕ ПРАВИТЬ)
  ids/ db/ grpcsrv/ grpcclient/ authz/ operations/ outbox/ … — shared-библиотеки
services/  — iam vpc compute geo nlb storage registry
gateway/   — api-gateway (edge: gRPC-proxy + grpc-gateway REST)
deploy/    — helm/стенд/e2e
```

## Модули Go: перечень ВЫВОДИТСЯ, а не выписывается

```sh
git ls-files '*go.mod'
```

Числа здесь намеренно нет. У выписанного числа не бывает владельца: оно устареет
молча на первой же вынесенной службе — и утащит за собой выводы, сделанные из
него. Команда выше отвечает за секунду и стареть не умеет.

Корневой модуль платформы — `github.com/PRO-Robotech/kacho`. Вынесенная служба
живёт своим модулем и резолвит платформу **опубликованной версией** (сегодня —
псевдоверсией), а не подменой пути.

### Что монорепо действительно снимает

Внутри платформы — цепочки PR `proto → фундамент → службы → край`: правка,
не выходящая за корневой модуль, идёт **одним PR, атомарно**. Любой пакет
импортируется откуда угодно, поэтому общий код живёт в `pkg/` и переиспользуется,
а не копируется по службам.

### Что осталось и действует — на границе с вынесенной службой

| | предикат | ожидаемое |
|---|---|---|
| служба пинит платформу псевдоверсией | `git grep -n 'PRO-Robotech/kacho v' -- '*go.mod'` | непусто |
| `replace` на внутренний модуль запрещён | `git grep -n '^replace github.com/PRO-Robotech' -- '*go.mod'` | **пусто** |
| локальная кросс-модульная правка — рабочим пространством | `cp go.work.example go.work` | образец в индексе |

Порядок пина, ловушка «локально зелено — в конвейере красно» и почему рабочее
пространство ничего **не доставляет** —
[`docs/architecture/cross-module-development.md`](docs/architecture/cross-module-development.md).
Перечень модулей в `go.work.example` держит гейт `internal/repohygiene`
`TestCrossModuleWorkspaceExampleNamesEveryModule` — сверка двусторонняя.

## Генерация proto

```bash
cd proto && buf generate     # → pkg/api/...
```

`go_package` в `.proto` указывает на `github.com/PRO-Robotech/kacho/pkg/api/...`.

> [!warning] Сгенерённые `.pb.go` НЕЛЬЗЯ править текстом (в т.ч. sed'ом по import-путям).
> В них зашит `rawDesc` — сериализованный FileDescriptorProto с длино-префиксными
> полями. Замена подстроки другой длины бьёт дескриптор, и это **не ловится**
> `go build`/`go vet` — только рантайм-паникой `slice bounds out of range` при
> init-регистрации. Меняешь пути — правь `.proto` + `buf generate`.
