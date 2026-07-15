# kacho-deploy — CLAUDE.md

Dev-стенд Kachō (kind + Helm + Postgres + ingress) + e2e. Базовые правила Kachō
(`.claude/rules/*`) — локальная копия, синхронизируемая из workspace (`./sync-tooling.sh`;
источник истины — `kacho-workspace/.claude/rules/`, копию здесь не редактировать).
`@import` ниже делает репо самодостаточным и при standalone-клоне.

## Базовые правила Kachō (@import — синканная копия из workspace)

@.claude/rules/00-kacho-core.md
@.claude/rules/api-conventions.md
@.claude/rules/polyrepo.md
@.claude/rules/architecture.md
@.claude/rules/data-integrity.md
@.claude/rules/security.md
@.claude/rules/git-youtrack.md
@.claude/rules/testing.md
@.claude/rules/vault.md
@.claude/rules/ai-tooling.md

## Специфика репо

- Стенд: `make dev-up` / `make dev-down`; `make reload-svc SVC=<vpc|compute|iam>` ·
  `make logs-svc SVC=…` · `make psql SVC=…`.
- Dockerfile'ы single-repo (`COPY . .` + versioned GitHub-deps, build-context = свой
  репо `kacho-<svc>`, НЕ parent); database-per-service —
  отдельная Postgres-схема `kacho_<domain>` на сервис (`@.claude/rules/data-integrity.md`).
- e2e/newman гоняются против REST api-gateway (`localhost:18080` при port-forward).
