# NetBox в umbrella-стенде Kachō — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Поднять NetBox внутри local dev стенда `kacho-deploy` рядом с остальными kacho-сервисами по тому же паттерну `pg-<svc>` + Helm.

**Architecture:** В umbrella chart добавляются две новые dependency: `pg-netbox` (alias на Bitnami `postgresql` 13.x) и `netbox` (community chart `netbox-community/netbox-chart`). NetBox указывается на внешний `pg-netbox` сервис через `externalDatabase`, встроенный Redis включён, persistence — `emptyDir`, ingress на `netbox.kacho.local` через существующий `ingress-nginx`.

**Tech Stack:** Helm 3, kind, Bitnami `postgresql` chart, NetBox community Helm chart, ingress-nginx.

**Spec:** [`docs/superpowers/specs/2026-05-05-netbox-umbrella-design.md`](../specs/2026-05-05-netbox-umbrella-design.md)

**Branch:** уже создан `feat/netbox-umbrella` в репо `kacho-deploy`. Все коммиты идут в эту ветку.

**Working directory для всех команд:** `/home/dk/workspace/github/PRO-Robotech/cloud-demo/kacho-deploy/`

---

## File structure

| Файл | Действие | Ответственность |
|---|---|---|
| `helm/umbrella/Chart.yaml` | modify | две новые dependency: `pg-netbox`, `netbox` |
| `helm/umbrella/values.dev.yaml` | modify | два новых блока values: `pg-netbox:`, `netbox:` |
| `helm/umbrella/Chart.lock` | regen | пересоздаётся `helm dep update` |
| `helm/umbrella/charts/postgresql-*.tgz` (доп.) | regen | tgz-кеш subchart'ов |
| `helm/umbrella/charts/netbox-*.tgz` | regen | NetBox subchart |
| `README.md` | modify | абзац про NetBox endpoint и dev-creds |

`Makefile` — **не трогаем** (см. spec).

---

## Task 1: Зафиксировать репо и версию NetBox community chart

**Files:** ничего не изменяется на диске, цель — записать решение в комментарий внутри `Chart.yaml` на следующем шаге.

- [ ] **Step 1: Подготовить helm и репо**

```bash
helm version --short
helm repo add netbox https://charts.netbox.oss.netboxlabs.com/ 2>/dev/null || true
helm repo update netbox 2>/dev/null || true
```

Ожидание: `helm` присутствует. Если первая команда `repo add` падает с unknown protocol — попробовать OCI fallback (см. Step 2).

- [ ] **Step 2: Найти последнюю стабильную версию (HTTP repo first, OCI fallback)**

```bash
helm search repo netbox/netbox --versions | head -5 || true
```

Если поиск пустой/repo не отдал индекс — пробуем OCI:

```bash
helm show chart oci://ghcr.io/netbox-community/charts/netbox 2>&1 | head -20
```

Записать в блокнот:
- `NETBOX_REPO=` точный URL (например `https://charts.netbox.oss.netboxlabs.com/` или `oci://ghcr.io/netbox-community/charts`)
- `NETBOX_CHART_VERSION=` последняя стабильная (НЕ pre-release; pre-release выглядит как `X.Y.Z-rc1` или `-beta`)
- `NETBOX_APP_VERSION=` поле `appVersion` chart'а (это версия самого NetBox)

- [ ] **Step 3: Запомнить ключи values для следующих задач**

```bash
# для HTTP repo:
helm show values netbox/netbox --version "$NETBOX_CHART_VERSION" > /tmp/netbox-values-default.yaml

# либо для OCI:
helm show values oci://ghcr.io/netbox-community/charts/netbox --version "$NETBOX_CHART_VERSION" > /tmp/netbox-values-default.yaml

wc -l /tmp/netbox-values-default.yaml
```

В `/tmp/netbox-values-default.yaml` найти и записать точные имена ключей:

```bash
grep -nE '^(postgresql|externalDatabase|redis|persistence|superuser|ingress|tasksRedis|cachingRedis):' /tmp/netbox-values-default.yaml
```

Возможные различия от spec'а (фиксируем здесь, до правки values.dev.yaml):
- `postgresql.enabled` vs `postgresql: { enabled: }` — chart-specific
- `externalDatabase` vs `postgresql.externalHost`
- Один Redis-блок (`redis.enabled`) vs два (`tasksRedis`, `cachingRedis`)
- `superuser.password` vs `admin.password`
- `ingress.hosts[].host` vs `ingress.hostname`

Записать `OBSERVED_KEYS=…` для использования в Task 6.

**Никаких файловых изменений и коммитов в этой задаче.**

---

## Task 2: Добавить `pg-netbox` dependency в `Chart.yaml`

**Files:**
- Modify: `helm/umbrella/Chart.yaml` (после блока `pg-vpc`)

- [ ] **Step 1: Открыть `Chart.yaml` и вставить блок**

После последнего `pg-*` блока (сейчас это `pg-vpc`, строки ~13-16) добавить:

```yaml
  - name: postgresql
    alias: pg-netbox
    version: 13.x
    repository: https://charts.bitnami.com/bitnami
```

Итоговый порядок в `dependencies:`:
1. `ingress-nginx`
2. `pg-resource-manager`
3. `pg-vpc`
4. **`pg-netbox` ← новый**
5. `resource-manager`
6. `vpc`
7. `vpc-controllers`
8. `api-gateway`
9. `ui`

- [ ] **Step 2: Проверить YAML синтаксис**

```bash
cd helm/umbrella
helm dep list 2>&1 | head -20
```

Ожидание: видны 8 dependency, в т.ч. `pg-netbox` со status `missing` (tgz ещё не скачан).

- [ ] **Step 3: `helm dep update` чтобы скачать subchart**

```bash
helm dep update
ls charts/ | grep postgresql
```

Ожидание: появился (или уже был) `postgresql-*.tgz`. `Chart.lock` обновлён, в нём теперь три записи `postgresql`.

- [ ] **Step 4: helm lint**

```bash
helm lint -f values.dev.yaml
```

Ожидание: `0 chart(s) linted, 0 chart(s) failed` (могут быть warnings — допустимо).

- [ ] **Step 5: Commit**

```bash
cd /home/dk/workspace/github/PRO-Robotech/cloud-demo/kacho-deploy
git add helm/umbrella/Chart.yaml helm/umbrella/Chart.lock helm/umbrella/charts/
git commit -m "feat(umbrella): add pg-netbox postgres dependency

Mirrors the existing pg-resource-manager / pg-vpc pattern. Used as the
external database for NetBox in the next commit.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Добавить блок `pg-netbox:` в `values.dev.yaml`

**Files:**
- Modify: `helm/umbrella/values.dev.yaml` (после блока `pg-vpc:`, до `pg-compute:` если он есть)

- [ ] **Step 1: Вставить блок**

Вставить **после** `pg-vpc:` (строки ~39-51):

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

- [ ] **Step 2: helm lint**

```bash
cd helm/umbrella
helm lint -f values.dev.yaml
```

Ожидание: `0 chart(s) failed`.

- [ ] **Step 3: helm template — проверить, что StatefulSet `pg-netbox` рендерится**

```bash
helm template kacho-umbrella . -f values.dev.yaml | \
  grep -A2 "kind: StatefulSet" | grep "name:.*pg-netbox"
```

Ожидание: одна строка типа `name: kacho-umbrella-pg-netbox`.

Если ничего не нашлось — проверить, что блок `pg-netbox:` действительно в YAML на верхнем уровне (а не вложен по ошибке).

- [ ] **Step 4: Проверить креды в рендере**

```bash
helm template kacho-umbrella . -f values.dev.yaml | \
  grep -B1 -A1 "kacho_netbox\|dev-netbox-password" | head -20
```

Ожидание: `POSTGRES_DATABASE=kacho_netbox`, секрет содержит `dev-netbox-password`.

- [ ] **Step 5: Commit**

```bash
cd /home/dk/workspace/github/PRO-Robotech/cloud-demo/kacho-deploy
git add helm/umbrella/values.dev.yaml
git commit -m "feat(umbrella): pg-netbox values for dev

Auth/db match the rest of the pg-<svc> blocks; persistence emptyDir;
bitnamilegacy images for compatibility with the moved Bitnami registry.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Проверить версию Postgres и при необходимости поднять до PG14+

**Why:** NetBox 4.x требует PostgreSQL 14+. Bitnami `postgresql` chart 13.x по умолчанию может тянуть PG 14+, но это надо подтвердить — иначе NetBox не запустится.

**Files:** при необходимости — modify `helm/umbrella/values.dev.yaml`

- [ ] **Step 1: Узнать дефолтную версию PG-образа в текущем pg-netbox subchart**

```bash
cd helm/umbrella
helm template kacho-umbrella . -f values.dev.yaml | \
  grep -E "image:.*postgresql:" | sort -u
```

Ожидание: одна или больше строк типа `image: bitnamilegacy/postgresql:16.x.y-...`.

- [ ] **Step 2: Сверить с требованием NetBox**

Записать `PG_DEFAULT_TAG=`. Если major часть `>= 14` — переходим к Step 5 (без изменений).

Если major `< 14` — продолжаем со Step 3.

- [ ] **Step 3 (только если major < 14): добавить override `image.tag` в `pg-netbox:`**

В `values.dev.yaml` дополнить блок `pg-netbox:`:

```yaml
pg-netbox:
  # … существующие поля …
  image:
    repository: bitnamilegacy/postgresql
    tag: "16"   # NetBox 4.x требует Postgres 14+, фиксируем major вручную
```

- [ ] **Step 4 (только если правили Step 3): re-render и проверка**

```bash
cd helm/umbrella
helm template kacho-umbrella . -f values.dev.yaml | \
  grep -E "image:.*postgresql:16" | head -3
```

Ожидание: рендер использует `postgresql:16…`.

- [ ] **Step 5: Commit (только если что-то правили в Step 3)**

```bash
cd /home/dk/workspace/github/PRO-Robotech/cloud-demo/kacho-deploy
git add helm/umbrella/values.dev.yaml
git commit -m "fix(pg-netbox): pin postgres major to 16 for NetBox 4.x

NetBox 4.x requires PostgreSQL 14+. Default image from Bitnami chart
13.x ships PG <14, so override the tag explicitly.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Если Step 3 не понадобился — без коммита, переходим к Task 5.

---

## Task 5: Добавить `netbox` dependency в `Chart.yaml`

**Files:**
- Modify: `helm/umbrella/Chart.yaml` (последняя dependency, после `ui`)

- [ ] **Step 1: Вставить блок**

Использовать `NETBOX_REPO` и `NETBOX_CHART_VERSION` из Task 1. Пример (HTTP repo):

```yaml
  - name: netbox
    version: <NETBOX_CHART_VERSION>          # подставить точно: например 5.0.10
    repository: https://charts.netbox.oss.netboxlabs.com/
```

Если репо OCI:

```yaml
  - name: netbox
    version: <NETBOX_CHART_VERSION>
    repository: oci://ghcr.io/netbox-community/charts
```

- [ ] **Step 2: `helm dep update`**

```bash
cd helm/umbrella
helm dep update
ls charts/ | grep netbox
```

Ожидание: появился `netbox-<version>.tgz`. В `Chart.lock` записан репо и версия NetBox.

- [ ] **Step 3: helm lint без values для netbox (ещё не добавлены)**

```bash
helm lint -f values.dev.yaml
```

Ожидание: lint проходит. Возможен `[WARNING]` про отсутствие values для netbox — не блокер.

- [ ] **Step 4: Commit**

```bash
cd /home/dk/workspace/github/PRO-Robotech/cloud-demo/kacho-deploy
git add helm/umbrella/Chart.yaml helm/umbrella/Chart.lock helm/umbrella/charts/
git commit -m "feat(umbrella): add netbox community chart dependency

Pinned to <NETBOX_CHART_VERSION> (NetBox app <NETBOX_APP_VERSION>).
Values are added in the next commit to point it at pg-netbox and
expose the UI through the existing ingress-nginx.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

(В реальном коммит-сообщении подставить версии.)

---

## Task 6: Сопоставить ключи values для NetBox chart

**Why:** Перед записью values.dev.yaml убеждаемся, какие именно ключи поддерживает выбранный community chart, чтобы не писать невалидные пути.

**Files:** ничего не правится, только проверка.

- [ ] **Step 1: Получить дефолтные values из tgz**

```bash
cd helm/umbrella
helm show values charts/netbox-*.tgz > /tmp/netbox-values-default.yaml
wc -l /tmp/netbox-values-default.yaml
```

- [ ] **Step 2: Найти точные ключи для каждой цели**

Заполнить mapping (в блокноте, не в файле):

| Цель | Ожидаемый ключ | Точный ключ из chart'а |
|---|---|---|
| отключить встроенный postgres | `postgresql.enabled` | _проверить_ |
| внешний PG host | `externalDatabase.host` | _проверить_ |
| внешний PG db | `externalDatabase.database` | _проверить_ |
| внешний PG user | `externalDatabase.username` | _проверить_ |
| секрет с паролем PG | `externalDatabase.existingSecretName` / `existingSecretKey` | _проверить_ |
| Redis включён | `redis.enabled` или `tasksRedis.enabled`+`cachingRedis.enabled` | _проверить_ |
| persistence off | `persistence.enabled` | _проверить_ |
| superuser логин | `superuser.name` или `admin.username` | _проверить_ |
| superuser пароль | `superuser.password` или `admin.password` | _проверить_ |
| API token | `superuser.apiToken` или `admin.apiToken` | _проверить_ |
| ingress включён | `ingress.enabled` | _проверить_ |
| ingress class | `ingress.className` или `ingress.ingressClassName` | _проверить_ |
| ingress host | `ingress.hosts[0].host` или `ingress.hostname` | _проверить_ |

Команды, которые помогут:

```bash
grep -nE '^(postgresql|externalDatabase|redis|tasksRedis|cachingRedis|persistence|superuser|admin|ingress):' /tmp/netbox-values-default.yaml

grep -nE '  (host|hostname|enabled|className|ingressClassName|password|apiToken|username|existingSecret)' /tmp/netbox-values-default.yaml | head -40
```

- [ ] **Step 3: Записать решение для Task 7**

В блокнот: «использую ключи X, Y, Z». Коммита нет.

---

## Task 7: Добавить блок `netbox:` в `values.dev.yaml`

**Files:**
- Modify: `helm/umbrella/values.dev.yaml` (в самый конец)

- [ ] **Step 1: Вставить блок (используя точные ключи из Task 6)**

Шаблон ниже использует имена ключей из spec'а — это **отправная точка**. Если Task 6 показал другие имена — заменить **дословно** на наблюдаемые.

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

Если у chart'а secret называется иначе (не `kacho-umbrella-pg-netbox`) — проверить и подставить актуальное имя:

```bash
helm template kacho-umbrella . -f values.dev.yaml | \
  grep -B1 "name:.*pg-netbox" | grep -E "kind:.*Secret|name:" | head -10
```

- [ ] **Step 2: helm lint**

```bash
cd helm/umbrella
helm lint -f values.dev.yaml
```

Ожидание: `0 chart(s) failed`.

- [ ] **Step 3: helm template — четыре проверки**

a) Встроенный postgres внутри netbox **не** рендерится (только наш `pg-netbox`):

```bash
helm template kacho-umbrella . -f values.dev.yaml | \
  grep -E "kind: StatefulSet" -A1 | grep "name:" | sort -u
```

Ожидание: видны `pg-resource-manager`, `pg-vpc`, `pg-netbox`. **Не должно быть** `name: kacho-umbrella-netbox-postgresql` или подобного.

b) NetBox web Deployment рендерится:

```bash
helm template kacho-umbrella . -f values.dev.yaml | \
  grep -E "kind: Deployment" -A1 | grep -E "name:.*netbox"
```

Ожидание: 1+ Deployment с `netbox` в имени.

c) Ingress на правильный host:

```bash
helm template kacho-umbrella . -f values.dev.yaml | \
  grep -B2 -A6 "kind: Ingress" | grep -E "host:|name:|className"
```

Ожидание: один из ingress'ов имеет `host: netbox.kacho.local` и `ingressClassName: nginx` (или `kubernetes.io/ingress.class: nginx`).

d) externalDatabase указан корректно (имя host в env / secret-ref):

```bash
helm template kacho-umbrella . -f values.dev.yaml | \
  grep -E "DB_HOST|POSTGRES_HOST|kacho-umbrella-pg-netbox" | head -10
```

Ожидание: значение указывает на `kacho-umbrella-pg-netbox`.

- [ ] **Step 4: Commit**

```bash
cd /home/dk/workspace/github/PRO-Robotech/cloud-demo/kacho-deploy
git add helm/umbrella/values.dev.yaml
git commit -m "feat(umbrella): netbox dev values

Disables embedded postgres, points NetBox at pg-netbox via Bitnami
secret, keeps embedded redis. Ingress on netbox.kacho.local through
the existing ingress-nginx, persistence emptyDir, dev superuser
admin/admin with a static API token.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: Поднять стенд и убедиться что NetBox работает

**Why:** acceptance criteria из spec'а нельзя проверить без реального kind-кластера. Запускаем `make dev-up`, прогоняем 8 проверок, фиксируем результаты.

**Files:** ничего не правится в этой задаче, только верификация.

**Prereqs:** docker запущен, `kind`/`kubectl`/`helm` установлены, порт 80 свободен, `127.0.0.1 netbox.kacho.local` в `/etc/hosts`.

- [ ] **Step 1: Добавить host (если нет)**

```bash
grep -q "netbox.kacho.local" /etc/hosts || \
  echo "127.0.0.1 netbox.kacho.local" | sudo tee -a /etc/hosts
```

- [ ] **Step 2: Поднять с нуля**

```bash
cd /home/dk/workspace/github/PRO-Robotech/cloud-demo/kacho-deploy
make dev-down
make dev-up
```

Ожидание: вывод `dev-up complete in <N>s`. `<N>` может быть >5 минут на первый запуск из-за NetBox migrations Job — это допустимо.

Если падает по `--timeout 5m` — увеличить таймаут в Makefile (`--timeout 10m`) **только** для проверки, потом откатить и доложить пользователю.

- [ ] **Step 3: Acceptance check 1 — pods Ready**

```bash
kubectl -n kacho get pod -l 'app.kubernetes.io/instance=kacho-umbrella' \
  --field-selector=status.phase!=Succeeded
kubectl -n kacho get statefulset/pg-netbox
kubectl -n kacho get deploy | grep -i netbox
```

Ожидание: `pg-netbox` StatefulSet в `1/1`, NetBox web/worker/housekeeping deployment'ы в `READY`.

- [ ] **Step 4: Acceptance check 2 — `make psql SVC=netbox`**

```bash
make psql SVC=netbox <<< '\dt' | head -20
```

Ожидание: после первой миграции — список таблиц NetBox (`auth_user`, `dcim_device`, …).

- [ ] **Step 5: Acceptance check 3 — HTTP до UI**

```bash
curl -s -o /dev/null -w "%{http_code}\n" -H "Host: netbox.kacho.local" http://127.0.0.1/
```

Ожидание: `200` или `302` (NetBox редиректит на login).

- [ ] **Step 6: Acceptance check 4 — login admin/admin**

Через UI открыть `http://netbox.kacho.local/`, залогиниться. Либо API:

```bash
curl -s -X POST -H "Content-Type: application/json" -H "Host: netbox.kacho.local" \
  -d '{"username":"admin","password":"admin"}' \
  http://127.0.0.1/api/users/tokens/provision/ | head -5
```

Ожидание: JSON с полем `key` или статус 200/201.

- [ ] **Step 7: Acceptance check 5 — нет loop'а connect refused в логах**

```bash
kubectl -n kacho logs deploy/$(kubectl -n kacho get deploy -o name | \
  grep netbox | head -1 | sed 's|deployment.apps/||') --tail=200 | \
  grep -iE "connection refused|could not connect" | head -5
```

Ожидание: пусто (или 1-2 ранних строк до того, как PG поднялся — но не loop).

- [ ] **Step 8: Acceptance check 6 — переподнятие чистое**

```bash
make dev-down
make dev-up
```

Ожидание: повтор Steps 3-7 успешен.

- [ ] **Step 9: Если что-то правили (Makefile timeout, опечатки в values) — commit**

Если изменения были — отдельный коммит вида:

```bash
git add -p
git commit -m "fix(umbrella): bump dev-up timeout for NetBox migrations init Job

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

Если изменений нет — без коммита.

---

## Task 9: Обновить README

**Files:**
- Modify: `README.md` (после раздела `## Persistence`)

- [ ] **Step 1: Добавить раздел**

Дописать в конец:

```markdown
## NetBox

Dev-стенд поднимает NetBox рядом с остальными сервисами.

- В `/etc/hosts` добавить: `127.0.0.1 netbox.kacho.local`
- UI: `http://netbox.kacho.local`
- Dev-creds: `admin / admin`
- Static API token (только для dev): `0123456789abcdef0123456789abcdef01234567`
- Postgres: `make psql SVC=netbox`

Persistence у NetBox/PG — `emptyDir`, как у остальных сервисов: данные пропадают при `make dev-down`.
```

Если выбран другой API token / иной superuser — синхронизировать значения с `values.dev.yaml`.

- [ ] **Step 2: Commit**

```bash
cd /home/dk/workspace/github/PRO-Robotech/cloud-demo/kacho-deploy
git add README.md
git commit -m "docs: NetBox endpoint and dev creds

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 10: Финал — push + сводка

**Files:** none.

- [ ] **Step 1: Свериться с веткой и коммитами**

```bash
cd /home/dk/workspace/github/PRO-Robotech/cloud-demo/kacho-deploy
git status
git log --oneline main..HEAD
```

Ожидание: ветка `feat/netbox-umbrella`, рабочее дерево чистое, серия коммитов соответствует Tasks 2-9 (Tasks без правок не дают коммитов).

- [ ] **Step 2: Запросить у пользователя решение по push/PR**

Не пушить и не открывать PR без явного «да». Сообщить пользователю краткую сводку:
- сколько коммитов поверх main,
- какие файлы затронуты,
- результаты Task 8 acceptance checks,
- известные tradeoff'ы (Makefile `reload-svc`/`logs-svc` не покрывают NetBox).

---

## Self-review

**Spec coverage:**

| Spec section | Покрыто задачей |
|---|---|
| Chart.yaml: `pg-netbox` dependency | Task 2 |
| Chart.yaml: `netbox` dependency | Task 5 |
| values.dev.yaml: `pg-netbox` блок | Task 3 |
| values.dev.yaml: `netbox` блок | Task 7 |
| `Chart.lock` / `charts/*.tgz` regen | Task 2, Task 5 |
| Makefile не трогаем | явно зафиксировано в spec и в file structure плана |
| README обновление | Task 9 |
| Acceptance criteria 1-8 | Task 8 (Steps 3-8) |
| Risk: PG14+ для NetBox 4.x | Task 4 |
| Pin последней стабильной версии chart'а | Task 1 |

**Placeholders:** `<NETBOX_CHART_VERSION>`, `<NETBOX_APP_VERSION>` в Task 5 — это значения, которые engineer **получает** в Task 1 и **подставляет** в Task 5. Это не «TBD/TODO», это явная переменная-плейсхолдер с понятным источником. Аналогично для имён ключей values в Task 7 — explicitly resolved в Task 6.

**Type/name consistency:** имена `pg-netbox`, `kacho_netbox`, `kacho-umbrella-pg-netbox`, `netbox.kacho.local`, `admin/admin`, API token используются единообразно во всех задачах и совпадают со spec'ом.
