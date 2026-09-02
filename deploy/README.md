# deploy/ — dev-стенд платформы

Локальный dev-стенд Kachō: kind + Helm + Bitnami Postgres + ingress-nginx.

## Команды

- `make dev-up` — поднять кластер (< 5 мин; с IAM-stack — < 8 мин, см. NFR-9 sub-phase-2.0)
- `make dev-down` — снести
- `make reload-svc SVC=<svc>` — пересобрать и перезагрузить один сервис (включая `SVC=iam`)
- `make logs-svc SVC=<svc>` — `kubectl logs -f`
- `make psql SVC=<svc>` — psql в pod-е
- `make e2e-test` — bash-сценарии в `e2e/` (см. ниже)
- `make module-manifests-configmap` — положить на стенд ConfigMap с манифестами
  модулей (`services/*/manifest.yaml`). Зовётся сам из `dev-up` и `stack-up`
  ПЕРЕД первым прогоном helm: служба читает каталог доставки на старте и на
  пустом отказывается подниматься. Стенд выбирается `MODULE_MANIFESTS_STACK=`
  (умолчание `dev`); стенд, не объявивший `kacho-iam.manifests.configMapName`,
  объект не получает, и цель говорит это вслух, а не отказом.

### IAM stack (KAC-105, sub-phase 2.0)

- `make reload-svc-iam` — alias for `make reload-svc SVC=iam`
- `make psql-iam` — psql в `kacho_iam`-БД (pg-iam)
- `make logs-iam` — `kubectl logs -f deploy/kacho-iam`

Здесь стояла ещё одна цель — про начальные учётные данные администратора того
поставщика личности, который стенд поднимал до KAC-127. Поставщик заменён на Ory
(Kratos + Hydra), цель снята вместе с ним, и её имя тут намеренно не
воспроизводится: процитированное, оно читается как живая команда. Надгробие с
причиной стоит в самом `Makefile`, рядом с тем местом, где цель была.

## E2E (`e2e/`) и CI

Bash-сценарии против поднятого стенда через REST api-gateway (`BASE_URL`):

- `e2e/cp-resource-model.sh` — e2e публичной NetworkInterface-модели: NIC — lean
  публичная проекция (`id/folder/name/subnet_id/primary_v4_address/security_group_ids/used_by/status`),
  `used_by` attach/detach. **Плюс негативный infra-leak audit**: краулит все публичные
  vpc & compute list/get endpoints и проверяет, что ни один не отдаёт
  инфра-чувствительных ключей (`sid`/`sidLocator`).

Запускается вручную против поднятого стенда. Ни один workflow его не зовёт: job
`e2e-on-kind`, на который ссылался этот абзац, в `.github/workflows/` отсутствует.
Newman-suite kacho-vpc ускорена (`tests/newman/scripts/run.sh`
— per-request delay 100→15 ms, коллекции гоняются параллельно с cap 4) — CI
newman-job ~7 мин → ~3 мин.

## Требования

Перечень — тот, что проверяет цель `preflight`, а не отдельный список рядом с ней:
**docker** (демон запущен), **kind**, **kubectl**, **helm**. Прежняя редакция
требовала сверх этого ещё и bats — во всём каталоге развёртывания нет ни одного
файла, который бы его звал.

Запись в `/etc/hosts` **не требуется ни для одного пути прогона**, и это говорит
сама цель `preflight`: харнесс доходит до каждого листенера через
`kubectl port-forward`, включая объявленный внешний TLS. Запись полезна только
тому, кто хочет постучать в Ingress руками, — и даже тогда kind публикует
node:80 на 28080 хоста (443 не публикуется вовсе), а тот Ingress ходит по GRPCS,
поэтому REST через него отвечает 502. Прежняя редакция требовала четыре имени, из
которых одно принадлежало снятому поставщику личности, а второе — снятому
инвентарю адресов.

## Persistence

Postgres использует `emptyDir` — данные не сохраняются между `dev-down`/`dev-up`. Это сознательно для воспроизводимости тестов (`03-deployment-and-operations.md` §5).

> [!note] Здесь стоял раздел про внешний инвентарь адресов — стенд его не поднимает
> Раздел на тринадцать строк описывал сторонний компонент как живую часть стенда:
> свой чарт, свой постгрес, своё имя в `/etc/hosts`, свои учётные данные и совет
> смотреть его логи по метке. Во всём каталоге развёртывания у него **ноль**
> упоминаний вне этого README — ни зависимости в `Chart.yaml`, ни ключа в
> `values.dev.yaml`, ни цели в `Makefile`. Имена здесь не воспроизводятся: адрес в
> обратных кавычках читается как живое утверждение, а учётные данные к
> несуществующему UI — приглашение искать то, чего нет.

## IAM stack (KAC-105, sub-phase 2.0; auth-tier переведён на Ory в KAC-127)

Dev-стенд поднимает рядом с остальными сервисами:

- **kacho-iam** — control-plane сервис IAM (Account / Project / User / ServiceAccount /
  Group / Role / AccessBinding). gRPC `:9090` (public) + `:9091` (internal, admin-only).
  Sub-chart живёт в `helm/umbrella/charts/kacho-iam/`. Image: `kacho-iam:dev`
  (build из `services/iam/` **этого** репозитория — отдельного репозитория сервиса
  не существует с переходом на монорепо).
- **Ory Kratos + Ory Hydra** — identity и OIDC-issuer. Оба приезжают внешними чартами
  `k8s.ory.sh/helm/charts`, у каждого свой постгрес (`pg-kratos`, `pg-hydra`). Публичный
  адрес выдающего в dev — `http://localhost:28080/.ory/hydra/public/` через kind-ingress;
  `auth.kacho.local` заведён в `values.dev.yaml` для тех, кто ходит браузером. Hydra
  остаётся **единственным** подписантом, а iam — единственным фасадом к ней
  (`security.md` §Production-mode обязателен ВЕЗДЕ).
- **kratos-selfservice-ui** — sub-chart в `helm/umbrella/charts/kratos-selfservice-ui/`,
  интерактивный логин для тех кейсов, где нужен человек.

Постгресы — **отдельный инстанс на каждого владельца данных** (запрет #8:
DB-per-service). Перечень выводится из `helm/umbrella/Chart.yaml`, а не выписывается
здесь: `grep -n 'alias: pg-' helm/umbrella/Chart.yaml` (на 2026-08-06 — десять
инстансов; прежняя редакция называла три, включая постгрес снятого поставщика
личности).

**Bootstrap-order**: у ярусa прав отдельного движка больше нет — решение о
доступе вычисляет сама iam в своей базе (S6 эпика #747). Вместе с движком сняты
его подчарт начальной настройки, выделенная база и init-контейнер ожидания;
надгробия с причинами стоят в самих шаблонах. Ожидания поставщика личности в
шаблоне тоже нет — по той же форме.

**Полезные команды** (см. секцию «IAM stack» выше):
- `make psql-iam` — psql в `kacho_iam`
- `make logs-iam` — логи kacho-iam

**Persistence**: все постгресы стенда — `emptyDir`, данные пропадают при
`make dev-down`. Default-roles seed выполнится заново при `make dev-up`.

## Проверка посадки

`make dev-up` поднимает стенд; `make dev-prod-up` дополнительно прогоняет гейты
посадки (`assert-rollout-ready`, `assert-production-posture`, `assert-outbox-autovacuum`)
и только после них печатает готовность. Production-posture обязателен на **любом**
поднятом стенде, включая локальный (`security.md` §Production-mode обязателен ВЕЗДЕ),
поэтому вердикт о стенде читается с `dev-prod-up`, а не с `dev-up`.
