# kacho-deploy

Локальный dev-стенд Kachō: kind + Helm + Bitnami Postgres + ingress-nginx.

## Команды

- `make dev-up` — поднять кластер (< 5 мин; с IAM-stack — < 8 мин, см. NFR-9 sub-phase-2.0)
- `make dev-down` — снести
- `make reload-svc SVC=<svc>` — пересобрать и перезагрузить один сервис (включая `SVC=iam`)
- `make logs-svc SVC=<svc>` — `kubectl logs -f`
- `make psql SVC=<svc>` — psql в pod-е
- `make e2e-test` — bash-сценарии в `e2e/` (см. ниже)

### IAM stack (KAC-105, sub-phase 2.0)

- `make reload-svc-iam` — alias for `make reload-svc SVC=iam`
- `make psql-iam` — psql в `kacho_iam`-БД (pg-iam)
- `make logs-iam` — `kubectl logs -f deploy/kacho-iam`
- `make fga-bootstrap` — вручную запустить openfga-bootstrap Job (создаёт store + загружает model)

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
- **OpenFGA** — ReBAC engine (tuple-store, Check-API). gRPC `:8081`, HTTP `:8080`
  (playground в dev — `http://openfga.kacho.local`). Внешний chart
  `openfga.github.io/helm-charts`.

Постгресы — **отдельный инстанс на каждого владельца данных** (запрет #8:
DB-per-service). Перечень выводится из `helm/umbrella/Chart.yaml`, а не выписывается
здесь: `grep -n 'alias: pg-' helm/umbrella/Chart.yaml` (на 2026-08-06 — десять
инстансов; прежняя редакция называла три, включая постгрес снятого поставщика
личности).

**Bootstrap-order** (NFR-9): OpenFGA → kacho-iam. Init-container `wait-for-openfga`
блокирует старт iam, пока `kacho-umbrella-openfga:8080` не ответит (условие
`initContainer.waitForExtAuth.enabled`, по умолчанию включено). OpenFGA store создаётся
`openfga-bootstrap` post-install Job'ом (helm hook), store_id пишется в Secret
`kacho-iam-openfga-store`, kacho-iam читает его при старте через `optional: true`
secretKeyRef. Ожидания поставщика личности в шаблоне больше нет — надгробие с
причиной стоит в самом шаблоне.

**Полезные команды** (см. секцию «IAM stack» выше):
- `make psql-iam` — psql в `kacho_iam`
- `make logs-iam` — логи kacho-iam
- `make fga-bootstrap` — пересоздать OpenFGA store + model вручную

**Persistence**: все постгресы стенда — `emptyDir`, данные пропадают при
`make dev-down`. Bootstrap-job и default-roles seed выполнятся заново при `make dev-up`.

## Проверка посадки

`make dev-up` поднимает стенд; `make dev-prod-up` дополнительно прогоняет гейты
посадки (`assert-rollout-ready`, `assert-production-posture`, `assert-outbox-autovacuum`)
и только после них печатает готовность. Production-posture обязателен на **любом**
поднятом стенде, включая локальный (`security.md` §Production-mode обязателен ВЕЗДЕ),
поэтому вердикт о стенде читается с `dev-prod-up`, а не с `dev-up`.
