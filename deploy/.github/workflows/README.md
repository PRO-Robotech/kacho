# GitHub Actions workflows — kacho-deploy

## ci.yaml — helm lint + chart smoke

`helm dep build` + lint umbrella-чарта. Триггеры — `push` / `pull_request` /
nightly schedule / `workflow_dispatch`. Чекаутит sibling-репо рядом с main-репо
в `kacho-workspace/project/`-layout (umbrella ссылается на чарты сервисов через
`repository: file://...`).

## newman-e2e.yml — Newman authz E2E (kind + helm umbrella)

Тяжёлый black-box authz-гейт: собирает все `kacho-*:dev` образы, грузит в kind,
helm-ставит umbrella (Postgres + Ory + OpenFGA + api-gateway + iam + vpc +
compute), сидит shared authz-fixtures и гоняет `kacho-iam` Newman authz-матрицы
через REST api-gateway.

Гейтит две сьюты (`kacho-iam/tests/newman/cases/`):

- `authz-deny` — 6-субъектная default-deny матрица (288 кейсов);
- `authz-sa-apitoken` — ServiceAccount + API-token модели 5-6 (30 кейсов).

Отдельный workflow от быстрого per-service `ci.yaml` — полный kind bring-up
занимает 15-30 мин, не должен тормозить fast feedback loop.

### Триггеры

- `push` в `main` / `KAC-127`
- `pull_request` в `main` / `KAC-127`
- `workflow_dispatch` — ручной прогон
- `schedule` — nightly (`0 4 * * *`)
- **`repository_dispatch` type `newman-e2e`** — cross-repo гейт (KAC-127, см. ниже)

### Cross-repo гейт — `repository_dispatch` (KAC-127)

Authz-код, который сьюты проверяют, живёт в сервисных репо (`kacho-iam`,
`kacho-vpc`, `kacho-compute`, `kacho-api-gateway`). Их собственный `ci.yaml`
гоняет только per-service unit/integration тесты — не cross-stack authz-матрицу.

Поэтому каждый из этих 4 репо несёт `newman-trigger.yml`, который на PR / push
шлёт `repository_dispatch` (event type `newman-e2e`) в этот репо и ждёт
результат прогона.

Workflow принимает `client_payload`:

| Поле | Назначение |
|---|---|
| `repo` | sibling под тестом (`kacho-vpc` / bare или `PRO-Robotech/kacho-vpc`) |
| `ref` | ветка этого sibling'а под тестом |
| `sha` | commit sha (для диагностики) |
| `source` | `<repo>#<run_id>` источника-dispatcher'а (для диагностики) |

Шаг `resolve sibling refs` переопределяет checkout **именно этого** sibling'а на
переданный `ref`; остальные siblings остаются на pin'е `KAC-127`. Так PR
сервисного репо проверяется против интеграционной ветки всех прочих компонентов.
Без `client_payload` (обычный push/PR/schedule) все siblings — на `KAC-127`.

### Требуемые GitHub secrets

`newman-e2e.yml` сам не требует custom secret (использует `GITHUB_TOKEN` для
checkout публичных siblings; `kacho-ui` приватный — best-effort).

Cross-repo dispatch инициируется **из сервисных репо** — там нужен
`WORKFLOW_DISPATCH_TOKEN` (PAT с правом POST `repos/PRO-Robotech/kacho-deploy/dispatches`).
См. `.github/workflows/README.md` каждого из `kacho-iam` / `kacho-vpc` /
`kacho-compute` / `kacho-api-gateway`.
