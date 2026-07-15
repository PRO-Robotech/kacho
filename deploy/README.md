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
- `make zitadel-admin` — pod-листинг + initial admin credentials Zitadel (читает из логов)
- `make fga-bootstrap` — вручную запустить openfga-bootstrap Job (создаёт store + загружает model)

## E2E (`e2e/`) и CI

Bash-сценарии против поднятого стенда через REST api-gateway (`BASE_URL`):

- `e2e/geography-move.sh` — Geography (Region/Zone) переехала в kacho-compute
  (`/compute/v1/regions`,`/compute/v1/zones`), kacho-vpc больше зон не отдаёт.
- `e2e/cp-resource-model.sh` — e2e публичной NetworkInterface-модели: NIC — lean
  публичная проекция (`id/folder/name/subnet_id/primary_v4_address/security_group_ids/used_by/status`),
  `used_by` attach/detach. **Плюс негативный infra-leak audit**: краулит все публичные
  vpc & compute list/get endpoints и проверяет, что ни один не отдаёт
  инфра-чувствительных ключей (`sid`/`sidLocator`).

Оба запускаются в nightly CI-job `e2e-on-kind` (`.github/workflows/ci.yaml`,
`cron: 0 3 * * *`). Newman-suite kacho-vpc ускорена (`tests/newman/scripts/run.sh`
— per-request delay 100→15 ms, коллекции гоняются параллельно с cap 4) — CI
newman-job ~7 мин → ~3 мин.

## Требования

- docker, kind v0.20+, kubectl, helm 3, bats-core
- Свободный порт 80 на host-машине
- В `/etc/hosts`: `127.0.0.1 api.kacho.local kacho.local zitadel.kacho.local openfga.kacho.local`
  (последние два — для KAC-105 IAM stack, см. ниже)

## Persistence

Postgres использует `emptyDir` — данные не сохраняются между `dev-down`/`dev-up`. Это сознательно для воспроизводимости тестов (`03-deployment-and-operations.md` §5).

## NetBox

Dev-стенд поднимает NetBox рядом с остальными сервисами (chart: `netbox-community/netbox`, app v4.5.x). PG — alias `pg-netbox` по тому же паттерну, что `pg-vpc`/`pg-resource-manager`. Valkey (Redis-совместимый) — встроенный subchart NetBox.

- В `/etc/hosts` добавить: `127.0.0.1 netbox.kacho.local`
- UI: `http://netbox.kacho.local`
- Dev-creds: `admin` / `admin`
- API token: получить через UI или `POST /api/users/tokens/provision/` с `admin`/`admin` (NetBox 4.x не принимает legacy hex-токены, поэтому статичный токен в values не задан)
- Postgres: `make psql SVC=netbox`

Persistence у NetBox media/reports/scripts и `pg-netbox` — `emptyDir`, как у остальных сервисов: данные пропадают при `make dev-down`.

`make reload-svc SVC=netbox` и `make logs-svc SVC=netbox` не работают — NetBox не пересобирается локально (внешний образ), а `logs-svc` ожидает один Deployment с именем сервиса, у NetBox их несколько (web/worker). Используйте `kubectl logs -n kacho -l app.kubernetes.io/name=netbox -f`.

## IAM stack (KAC-105, sub-phase 2.0)

Dev-стенд поднимает три новых компонента рядом с остальными сервисами:

- **kacho-iam** — control-plane сервис IAM (Account / Project / User / ServiceAccount /
  Group / Role / AccessBinding). gRPC `:9090` (public) + `:9091` (internal, admin-only).
  Sub-chart живёт в `helm/umbrella/charts/kacho-iam/`. Image: `kacho-iam:dev`
  (build из `project/kacho-iam/`).
- **Zitadel** — OIDC issuer (источник identity, signup, JWT). UI на
  `http://zitadel.kacho.local`. Внешний chart `charts.zitadel.com`.
- **OpenFGA** — REBAC engine (Zanzibar-модель, tuple-store, Check-API). gRPC `:8081`,
  HTTP `:8080` (playground enabled в dev — `http://openfga.kacho.local`). Внешний chart
  `openfga.github.io/helm-charts`.

Постгресы — три отдельных инстанса (запрет #8: DB-per-service):

- `pg-iam` → `kacho_iam` БД (`iam` / `dev-iam-password`)
- `pg-zitadel` → `zitadel` БД (`zitadel` / `dev-zitadel-password`)
- `pg-openfga` → `openfga` БД (`openfga` / `dev-openfga-password`)

**Bootstrap-order** (NFR-9): Zitadel-postgres → Zitadel → kacho-iam (init-container
`wait-for-zitadel` / `wait-for-openfga` блокирует startup до :8080 ready). OpenFGA store
создаётся `openfga-bootstrap` post-install Job'ом (helm hook), store_id пишется в Secret
`kacho-iam-openfga-store`, kacho-iam читает его при старте через `optional: true` secretKeyRef.

**Полезные команды** (см. секцию «IAM stack» выше):
- `make psql-iam` — psql в `kacho_iam`
- `make logs-iam` — логи kacho-iam
- `make zitadel-admin` — credentials Zitadel UI
- `make fga-bootstrap` — пересоздать OpenFGA store + model вручную

**Persistence**: `pg-iam` / `pg-zitadel` / `pg-openfga` — все `emptyDir`, данные пропадают
при `make dev-down`. Bootstrap-job и default-roles seed выполнятся заново при `make dev-up`.
