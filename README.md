# Kachō — monorepo

Единый репозиторий платформы **Kachō** (cloud control-plane). Заменяет polyrepo
`kacho-proto` / `kacho-corelib` / `kacho-<svc>` / `kacho-api-gateway` / `kacho-deploy`.
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

## Один Go-модуль

`github.com/PRO-Robotech/kacho` — **один** `go.mod` на весь репозиторий.

Это главный смысл монорепы: исчезает вся polyrepo-церемония — pseudo-версии,
запрет `replace`, `go.work`, пины sibling-веток в CI и цепочки PR
proto → corelib → сервисы → gateway. Кросс-доменная фича = **один PR, атомарно**.

Любой пакет импортируется откуда угодно → общий код живёт в `pkg/` и
переиспользуется, а не копируется по сервисам.

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
