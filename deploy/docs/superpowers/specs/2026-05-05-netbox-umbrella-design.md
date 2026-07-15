# NetBox в umbrella-стенде Kachō — design

**Дата:** 2026-05-05
**Скоуп:** `kacho-deploy/helm/umbrella` + `kacho-deploy/Makefile` + `kacho-deploy/README.md`
**Статус:** approved (см. brainstorming-сессию)

## Цель

Поднять NetBox в локальном dev-стенде `make dev-up` рядом с остальными kacho-сервисами, по тому же паттерну, что `kacho-vpc` и `kacho-resource-manager`: отдельный per-service Postgres (`pg-netbox`) на верхнем уровне umbrella + сам NetBox через ingress-nginx.

Никаких других kacho-сервисов это изменение не затрагивает.

## Архитектура

NetBox добавляется как две новые dependency в umbrella chart:

1. **`pg-netbox`** — alias на Bitnami `postgresql` 13.x (полная копия паттерна `pg-vpc`/`pg-resource-manager`).
2. **`netbox`** — официальный community chart `netbox-community/netbox-chart` (последняя стабильная версия на момент `helm dep update`, версия фиксируется в `Chart.lock`).

Внутри NetBox community chart:
- Встроенный postgresql отключаем, ходим во внешний `pg-netbox` через service `kacho-umbrella-pg-netbox` и Bitnami-секрет, который этот subchart создаёт.
- Встроенный redis оставляем как есть — это кеш/очередь NetBox, не «доменная» БД, наружу не выставляем.
- Ingress NetBox использует существующий `ingress-nginx` controller (`ingressClassName: nginx`) на host `netbox.kacho.local`.

Persistence — `emptyDir` (как у всех остальных pg-* и сервисов в umbrella; данные не сохраняются между `dev-down`/`dev-up` сознательно — паттерн воспроизводимости тестов).

## Файлы и изменения

### `helm/umbrella/Chart.yaml`

Добавить в `dependencies:` (порядок: рядом с другими `pg-*`, и сам `netbox` ближе к концу — по аналогии с `api-gateway`/`ui`):

```yaml
- name: postgresql
  alias: pg-netbox
  version: 13.x
  repository: https://charts.bitnami.com/bitnami
- name: netbox
  version: <последняя стабильная на момент helm dep update>
  repository: https://charts.netbox.oss.netboxlabs.com/
```

Точный URL репозитория и версия проверяются и фиксируются на этапе реализации (см. план). Допустимые fallback'и в порядке предпочтения:
1. `https://charts.netbox.oss.netboxlabs.com/` (официальный netbox-community chart)
2. `oci://ghcr.io/netbox-community/charts/netbox`

### `helm/umbrella/values.dev.yaml`

Два новых блока. `pg-netbox` — точная копия паттерна:

```yaml
pg-netbox:
  auth:
    username: netbox
    password: dev-netbox-password
    database: kacho_netbox
  primary:
    persistence:
      enabled: false
  image:
    repository: bitnamilegacy/postgresql
  volumePermissions:
    image:
      repository: bitnamilegacy/os-shell
```

`netbox` — направление на внешний PG, dev-creds, ingress:

```yaml
netbox:
  postgresql:
    enabled: false
  externalDatabase:
    host: kacho-umbrella-pg-netbox
    database: kacho_netbox
    username: netbox
    existingSecretName: kacho-umbrella-pg-netbox
    existingSecretKey: password
  redis:
    tasks:
      enabled: true
    cache:
      enabled: true
  persistence:
    enabled: false
  superuser:
    name: admin
    password: admin
    apiToken: "0123456789abcdef0123456789abcdef01234567"
  ingress:
    enabled: true
    ingressClassName: nginx
    hosts:
      - host: netbox.kacho.local
        paths:
          - /
```

Точные ключи (`externalDatabase.*`, `superuser.*`, `redis.tasks/cache.enabled`) проверяются по схеме values community chart на этапе реализации. Если имена отличаются — следуем схеме chart, цели остаются: внутренний PG off, внешний PG = `pg-netbox`, redis встроенный, ingress на `netbox.kacho.local`, dev-superuser `admin/admin` со статичным API token.

### `Makefile`

`reload-svc` пересобирает локальный Docker-образ `kacho-<svc>:dev`. NetBox — внешний образ, пересобирать нечего, поэтому **в whitelist `reload-svc` netbox не добавляется** — это сознательно, чтобы пользователи не пытались пересобрать чужой образ.

`make psql SVC=netbox` работает без изменений Makefile: текущая команда —
`kubectl exec -it -n kacho statefulset/pg-$(SVC) -- psql -U $(SVC) -d kacho_$(SVC)`,
что для `SVC=netbox` даёт правильный `pg-netbox` / `netbox` / `kacho_netbox`.

`make logs-svc SVC=netbox` тоже не работает «как у всех» — у NetBox в community chart несколько Deployment'ов (web, worker, housekeeping), а Makefile делает `deploy/$(SVC)`. Не лезем в Makefile в этом изменении: если понадобится — отдельное улучшение Makefile под multi-deployment сервисы (вне scope).

### `README.md`

Один абзац в конце:
- добавить `127.0.0.1 netbox.kacho.local` в `/etc/hosts`,
- открыть `http://netbox.kacho.local`,
- dev-creds `admin / admin`, статичный API token из `values.dev.yaml`.

### `helm/umbrella/Chart.lock` и `helm/umbrella/charts/*.tgz`

Регенерируются командой `cd helm/umbrella && helm dep update`. В коммите спускаются (паттерн репозитория — `charts/*.tgz` уже под git, см. текущее состояние).

## Что НЕ делаем (out of scope)

- **Persistence для NetBox/PG** — emptyDir по паттерну umbrella.
- **TLS на ingress** — ни у кого его нет.
- **Плагины NetBox** — нужен будет отдельный design.
- **Seed/fixtures** — пустой NetBox, наполняется вручную.
- **Бэкапы** — это dev-стенд.
- **e2e-сценарий в `e2e/0.1/`** — добавим, когда понадобится.
- **Изменения Makefile под `reload-svc netbox` / `logs-svc netbox`** — вне scope.

## Верификация (acceptance criteria)

1. `make helm-lint` успешно (после `helm dep update`).
2. `make dev-up` поднимает кластер за <5 минут (как сейчас) и в выводе показывает «dev-up complete».
3. `kubectl -n kacho get deploy,statefulset` показывает `pg-netbox` (StatefulSet) и хотя бы `netbox` web-Deployment в `Ready`.
4. `make psql SVC=netbox` подключается и `\dt` показывает таблицы NetBox после первой миграции.
5. `curl -H "Host: netbox.kacho.local" http://127.0.0.1/` отдаёт NetBox login page (HTTP 200/302).
6. Логин `admin/admin` через UI работает.
7. `kubectl -n kacho logs deploy/netbox` не содержит loop'а connection refused к Postgres.
8. `make dev-down && make dev-up` чистый — стенд переподнимается без артефактов.

## Риски и open issues

- **Точная версия NetBox community chart** и точные ключи values — фиксируются на этапе реализации.
- **Initial migrations** community chart обычно делает init-Job'ом. Время первого запуска NetBox может увеличить `make dev-up` на 30–90 сек. Это допустимо — `--timeout 5m` в Makefile должен покрыть.
- **`bitnamilegacy/postgresql` 13.x совместимость с NetBox 4.x** — NetBox 4.x требует Postgres 14+. Если Bitnami `postgresql` 13.x chart версии в umbrella по умолчанию даёт PG <14, то `pg-netbox` нужно явно прописать `image.tag` на актуальный major (14/15/16). Проверка — на этапе реализации.
